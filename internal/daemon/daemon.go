// Package daemon owns the pad. Each Claude Code session attaches to it for as
// long as the session lives, and gets a key, a light and the pad's controls.
//
// The daemon serves HTTP on a Unix socket:
//
//	POST /attach                 attach a session; the response streams what
//	                             the daemon tells it, one JSON object per line,
//	                             for as long as the session lives
//	POST /sessions/{id}/state    set a session's state: {"state": "working"}
//	POST /sessions/{id}/ask      wait for the pad to answer the session's
//	                             permission prompt: {"answer": "allow"},
//	                             "deny", or null when answered otherwise
//	GET  /status                 the pad and each session, for doctor
package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Endless-Winning-AI/lets-build-clauder/internal/config"
	"github.com/Endless-Winning-AI/lets-build-clauder/internal/desktop"
	"github.com/Endless-Winning-AI/lets-build-clauder/internal/pad"
	"github.com/Endless-Winning-AI/lets-build-clauder/internal/terminals"
)

// Protocol is the version of the API above. A client speaking another is
// turned away; between versions of the same protocol the newest daemon wins.
const Protocol = 1

const slots = 6

// States a session can be in.
const (
	Idle     = "idle"
	Working  = "working"
	Awaiting = "awaiting" // on a permission prompt
	Unread   = "unread"   // finished, with an answer not yet looked at
	Error    = "error"
)

// Hello is what a session attaches with.
type Hello struct {
	PID      int               `json:"pid"`
	CWD      string            `json:"cwd"`
	Env      map[string]string `json:"env"`
	Slot     *int              `json:"slot"` // the key it had, to keep it across restarts
	Version  string            `json:"version"`
	Protocol int               `json:"protocol"`
}

// Session is an attached Claude Code session.
type Session struct {
	id    string
	pid   int
	cwd   string
	env   map[string]string
	state string
	slot  int // -1 for none
	since time.Time

	// Where keys go: a pane (see terminals) or a desktop window. And the
	// desktop window around it, to raise it and to tell when it has focus.
	window, gui string
	locate      sync.Once
	located     chan struct{} // closed once window and gui are known

	out  chan []byte
	done chan struct{} // closed to end the session's stream
	ask  chan string   // the plugin waiting on the pad to answer its prompt
	end  sync.Once
}

func (s *Session) close() { s.end.Do(func() { close(s.done) }) }

// DeviceInfo is what sessions are told of the pad.
type DeviceInfo struct {
	Connected bool   `json:"connected"`
	Model     string `json:"model,omitempty"`
	Transport string `json:"transport,omitempty"`
	Firmware  string `json:"firmware,omitempty"`
	Battery   *int   `json:"battery,omitempty"`
	Charging  bool   `json:"charging,omitempty"`
	Problem   string `json:"problem,omitempty"` // why it could not be opened
}

// Daemon is the running daemon.
type Daemon struct {
	version    string
	socket     string
	configPath string
	desktop    desktop.Desktop
	log        *log.Logger
	dirty      chan struct{} // the lights need sending

	mu          sync.Mutex // guards everything below
	sessions    map[string]*Session
	device      *pad.Device
	info        DeviceInfo
	focused     *Session          // the session whose window has focus
	last        *Session          // the session a control last went to
	active      *Session          // the session the person last typed in
	sidebar     string            // where the desktop app keeps its chats
	chats       []string          // the chats the knob steps through, in sidebar order
	chatsAt     time.Time         // when the knob last stepped
	chatAt      string            // where the knob is
	pins        *pins             // sessions pinned to keys, by host id
	flash       map[int]time.Time // keys lit in a blink until when
	config      config.Config
	configStamp int64
	controls    // the pad's controls, mid-gesture
}

// New makes a daemon that will listen on socket.
func New(version, socket string, d desktop.Desktop, logger *log.Logger) *Daemon {
	path := config.File()
	c, problems := config.Load(path)
	for _, p := range problems {
		logger.Println("config:", p)
	}
	return &Daemon{
		version:     version,
		socket:      socket,
		configPath:  path,
		desktop:     d,
		log:         logger,
		dirty:       make(chan struct{}, 1),
		sessions:    map[string]*Session{},
		pins:        loadPins(config.PinsFile()),
		sidebar:     sidebarDir(),
		flash:       map[int]time.Time{},
		config:      c,
		configStamp: config.Modified(path),
		controls:    newControls(),
	}
}

// Run serves until the context ends.
func (d *Daemon) Run(ctx context.Context) error {
	listener, err := listen(d.socket)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: d.handler()}

	go d.deviceLoop(ctx)
	go d.lightsLoop(ctx)
	go d.reapLoop(ctx)
	go d.desktop.Follow(ctx, d.onFocus)
	go func() {
		<-ctx.Done()
		server.Close()
	}()
	d.log.Println("daemon", d.version, "listening on", d.socket)
	if err := server.Serve(listener); err != http.ErrServerClosed {
		return err
	}
	return nil
}

// -- the API ------------------------------------------------------------------

func (d *Daemon) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /attach", d.handleAttach)
	mux.HandleFunc("POST /sessions/{id}/state", d.handleState)
	mux.HandleFunc("POST /sessions/{id}/ask", d.handleAsk)
	mux.HandleFunc("GET /status", d.handleStatus)
	return mux
}

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (d *Daemon) handleAttach(w http.ResponseWriter, r *http.Request) {
	var hello Hello
	if err := json.NewDecoder(r.Body).Decode(&hello); err != nil || hello.PID <= 0 {
		reply(w, http.StatusBadRequest, map[string]string{"type": "error", "message": "bad hello"})
		return
	}
	switch {
	case hello.Protocol != Protocol:
		reply(w, http.StatusConflict, map[string]string{"type": "error",
			"message": "the lets-build-clauder daemon and this session's plugin differ; restart Claude Code"})
		return
	case newer(hello.Version, d.version):
		// A newer plugin: make way for its daemon.
		d.log.Println("newer client", hello.Version, "- exiting")
		reply(w, http.StatusServiceUnavailable, map[string]string{"type": "restart"})
		time.AfterFunc(200*time.Millisecond, func() { os.Exit(0) })
		return
	}

	s := d.attach(hello)
	defer d.detach(s)
	w.Header().Set("Content-Type", "application/x-ndjson")
	flusher, _ := w.(http.Flusher)
	for {
		select {
		case msg := <-s.out:
			if _, err := w.Write(msg); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		case <-s.done:
			return
		case <-r.Context().Done():
			return
		}
	}
}

func (d *Daemon) handleState(w http.ResponseWriter, r *http.Request) {
	var body struct {
		State  string `json:"state"`
		Active bool   `json:"active"` // the person just typed in this session
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil || looks[body.State] == (look{}) {
		http.Error(w, "unknown state", http.StatusBadRequest)
		return
	}
	d.mu.Lock()
	s := d.sessions[r.PathValue("id")]
	if s != nil {
		if body.Active {
			d.active = s
		}
		d.setState(s, body.State, false)
	}
	d.mu.Unlock()
	if s == nil {
		http.Error(w, "no such session", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Status is what GET /status answers.
type Status struct {
	Version  string          `json:"version"`
	Device   DeviceInfo      `json:"device"`
	Desktop  string          `json:"desktop"`
	CanHold  bool            `json:"canHold"` // the daemon may post held keys (macOS: Accessibility)
	Sessions []SessionStatus `json:"sessions"`
}

// SessionStatus describes one session.
type SessionStatus struct {
	PID     int    `json:"pid"`
	CWD     string `json:"cwd"`
	Key     *int   `json:"key"`
	State   string `json:"state"`
	Window  string `json:"window,omitempty"`
	Focused bool   `json:"focused"`
	Active  bool   `json:"active"`           // the person last typed here
	Pinned  bool   `json:"pinned,omitempty"` // its key is pinned to it
}

func (d *Daemon) handleStatus(w http.ResponseWriter, _ *http.Request) {
	d.mu.Lock()
	st := Status{Version: d.version, Device: d.info, Desktop: d.desktop.Name(), CanHold: d.desktop.CanHold(), Sessions: []SessionStatus{}}
	for _, s := range d.sessions {
		st.Sessions = append(st.Sessions, SessionStatus{
			PID: s.pid, CWD: s.cwd, Key: keyOf(s), State: s.state, Window: s.window, Focused: s == d.focused,
			Active: s == d.active, Pinned: s.slot >= 0 && d.pins.slot(hostOf(s)) == s.slot,
		})
	}
	d.mu.Unlock()
	reply(w, http.StatusOK, st)
}

// -- sessions -----------------------------------------------------------------

func (d *Daemon) attach(hello Hello) *Session {
	env := map[string]string{}
	for _, k := range terminals.EnvKeys {
		if v, ok := hello.Env[k]; ok {
			env[k] = v
		}
	}
	s := &Session{
		id: newID(), pid: hello.PID, cwd: hello.CWD, env: env, state: Idle, slot: -1, since: time.Now(),
		out: make(chan []byte, 64), done: make(chan struct{}), located: make(chan struct{}),
	}

	d.mu.Lock()
	var old *Session
	for _, o := range d.sessions {
		if o.pid == s.pid {
			old = o
		}
	}
	if old != nil {
		// The same session, back: it keeps its key and state.
		s.slot, s.state = old.slot, old.state
		d.remove(old)
	} else if !d.settings().Manual {
		s.slot = d.freeSlot(hello.Slot)
	}
	d.sessions[s.id] = s
	if old == nil {
		if pinned := d.pins.slot(hostOf(s)); pinned >= 0 {
			d.claim(s, pinned) // a pinned session takes its key back
		}
	}
	d.send(s, map[string]string{"type": "attached", "session": s.id, "socket": d.socket})
	d.changed()
	d.mu.Unlock()

	d.log.Println("attach", s.pid, "key", keyName(s), s.cwd)
	go d.windowOf(s)
	return s
}

func (d *Daemon) detach(s *Session) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.sessions[s.id] == s {
		d.remove(s)
		d.log.Println("detach", s.pid)
		d.changed()
	}
}

// remove forgets a session. The caller holds d.mu.
func (d *Daemon) remove(s *Session) {
	delete(d.sessions, s.id)
	s.close()
	if d.focused == s {
		d.focused = nil
	}
	if d.last == s {
		d.last = nil
	}
	if d.active == s {
		d.active = nil
	}
}

// hostOf is a Claude desktop session's host id ("local_..."), or "".
func hostOf(s *Session) string {
	return strings.TrimPrefix(terminals.DesktopApp(s.env), "Claude:")
}

// freeSlot is the key a session asks for, when free, else the first free key,
// else -1. The caller holds d.mu.
func (d *Daemon) freeSlot(wanted *int) int {
	used := map[int]bool{}
	for _, s := range d.sessions {
		used[s.slot] = true
	}
	if wanted != nil && *wanted >= 0 && *wanted < slots && !used[*wanted] {
		return *wanted
	}
	for i := range slots {
		if !used[i] {
			return i
		}
	}
	return -1
}

// send queues a message for a session. The caller holds d.mu.
func (d *Daemon) send(s *Session, msg any) {
	line, err := json.Marshal(msg)
	if err != nil {
		return
	}
	select {
	case s.out <- append(line, '\n'):
	default:
		d.log.Println("session", s.pid, "is not reading; dropped", string(line))
	}
}

// changed tells every session the news and has the lights sent. The caller
// holds d.mu.
func (d *Daemon) changed() {
	d.relight()
	for _, s := range d.sessions {
		waiting := 0
		for _, o := range d.sessions {
			if o != s && o.state == Awaiting {
				waiting++
			}
		}
		d.send(s, map[string]any{"type": "status", "slot": slotOf(s), "device": d.info, "waiting": waiting})
	}
}

func (d *Daemon) handleAsk(w http.ResponseWriter, r *http.Request) {
	ask := make(chan string, 1)
	d.mu.Lock()
	s := d.sessions[r.PathValue("id")]
	if s != nil {
		d.dropAsk(s)
		s.ask = ask
	}
	d.mu.Unlock()
	if s == nil {
		http.Error(w, "no such session", http.StatusNotFound)
		return
	}
	var answer *string
	select {
	case a, ok := <-ask:
		if ok {
			answer = &a
		}
	case <-s.done:
	case <-r.Context().Done():
	}
	d.mu.Lock()
	if s.ask == ask {
		s.ask = nil
	}
	d.mu.Unlock()
	reply(w, http.StatusOK, map[string]*string{"answer": answer})
}

// dropAsk ends a session's wait on the pad unanswered. The caller holds d.mu.
func (d *Daemon) dropAsk(s *Session) {
	if s.ask != nil {
		close(s.ask)
		s.ask = nil
	}
}

// setState moves a session to a state; tell says the session did not move
// itself and should hear of it. The caller holds d.mu.
func (d *Daemon) setState(s *Session, state string, tell bool) {
	if s.state == state {
		return
	}
	if s.state == Awaiting {
		d.dropAsk(s) // answered some other way
	}
	s.state, s.since = state, time.Now()
	if tell {
		d.send(s, map[string]string{"type": "state", "state": state})
	}
	d.changed()
}

// live reports whether a session is still attached. The caller holds d.mu.
func (d *Daemon) live(s *Session) bool {
	return s != nil && d.sessions[s.id] == s
}

func newID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func slotOf(s *Session) *int {
	if s.slot < 0 {
		return nil
	}
	return &s.slot
}

func keyOf(s *Session) *int {
	if s.slot < 0 {
		return nil
	}
	key := s.slot + 1
	return &key
}

func keyName(s *Session) string {
	if s.slot < 0 {
		return "none"
	}
	return strconv.Itoa(s.slot + 1)
}

// newer reports whether version a is newer than b, both like "1.2.3".
func newer(a, b string) bool {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := range max(len(as), len(bs)) {
		var x, y int
		if i < len(as) {
			x, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			y, _ = strconv.Atoi(bs[i])
		}
		if x != y {
			return x > y
		}
	}
	return false
}
