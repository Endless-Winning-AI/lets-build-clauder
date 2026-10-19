package pad

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Found is a pad attached to this computer.
type Found struct {
	Model     string
	Transport string // "usb" or "bluetooth"
	path      string // what the platform opens it by
}

// transport is an open pad: reports go out through it and come back through
// the callback given when it was opened.
type transport interface {
	write(report []byte) error
	close()
}

// ErrLost is returned by calls in flight when the pad goes away.
var ErrLost = errors.New("the pad went away")

// Device is an open pad.
type Device struct {
	Found
	conn   transport
	notify func(method string, params json.RawMessage)
	lost   chan struct{}

	mu      sync.Mutex // guards the fields below
	buf     []byte
	nextID  int
	pending map[int]chan message
	closed  bool

	writing sync.Mutex
}

type message struct {
	ID     *int            `json:"id"`
	Method string          `json:"m"`
	Params json.RawMessage `json:"p"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

// Open opens a pad. notify receives each notification the pad sends, on a
// goroutine of the transport's.
func Open(f Found, notify func(method string, params json.RawMessage)) (*Device, error) {
	d := &Device{Found: f, notify: notify, lost: make(chan struct{}), pending: map[int]chan message{}}
	conn, err := open(f, d.receive, func() { d.shut() })
	if err != nil {
		return nil, err
	}
	d.conn = conn
	return d, nil
}

// Lost is closed when the pad goes away or the device is closed.
func (d *Device) Lost() <-chan struct{} { return d.lost }

// Call makes a JSON-RPC call and returns its result.
func (d *Device) Call(method string, params any, timeout time.Duration) (json.RawMessage, error) {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return nil, ErrLost
	}
	d.nextID = d.nextID%9999 + 1
	id := d.nextID
	reply := make(chan message, 1)
	d.pending[id] = reply
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		delete(d.pending, id)
		d.mu.Unlock()
	}()

	msg, err := json.Marshal(struct {
		Method string `json:"method"`
		Params any    `json:"params"`
		ID     int    `json:"id"`
	}{method, params, id})
	if err != nil {
		return nil, err
	}
	if err := d.send(msg); err != nil {
		return nil, err
	}

	select {
	case m, ok := <-reply:
		if !ok {
			return nil, ErrLost
		}
		if len(m.Error) > 0 && string(m.Error) != "null" {
			return nil, fmt.Errorf("%s: %s", method, m.Error)
		}
		return m.Result, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("%s: no reply in %v", method, timeout)
	}
}

func (d *Device) send(msg []byte) error {
	d.writing.Lock()
	defer d.writing.Unlock()
	reports := frames(msg)
	for i, r := range reports {
		if i > 0 {
			// Back-to-back reports overrun the pad's receive buffer.
			time.Sleep(4 * time.Millisecond)
		}
		if err := d.write(r); err != nil {
			return fmt.Errorf("report %d of %d: %w", i+1, len(reports), err)
		}
	}
	return nil
}

// write sends one report. Over Bluetooth on macOS a report is now and then
// refused while the link is busy: it goes again after a pause.
func (d *Device) write(report []byte) error {
	var err error
	for attempt := range 4 {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 25 * time.Millisecond)
		}
		if err = d.conn.write(report); err == nil {
			return nil
		}
	}
	return err
}

func (d *Device) receive(report []byte) {
	text := payload(report)
	if text == nil {
		return
	}
	d.mu.Lock()
	objs, rest := splitJSON(append(d.buf, text...))
	d.buf = append([]byte(nil), rest...)
	var notes []message
	for _, obj := range objs {
		var m message
		if json.Unmarshal(obj, &m) != nil {
			continue
		}
		if reply, ok := d.pending[idOf(m)]; ok {
			delete(d.pending, *m.ID)
			reply <- m
		} else if m.Method != "" {
			notes = append(notes, m)
		}
	}
	d.mu.Unlock()
	for _, m := range notes {
		d.notify(m.Method, m.Params)
	}
}

func idOf(m message) int {
	if m.ID == nil {
		return 0
	}
	return *m.ID
}

// Close closes the pad.
func (d *Device) Close() {
	if d.shut() {
		d.conn.close()
	}
}

func (d *Device) shut() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return false
	}
	d.closed = true
	for id, reply := range d.pending {
		close(reply)
		delete(d.pending, id)
	}
	close(d.lost)
	return true
}
