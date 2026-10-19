package daemon

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Endless-Winning-AI/lets-build-clauder/internal/pad"
)

const (
	heartbeat  = 30 * time.Second // the pad forgets status colors after a quiet spell
	pollEvery  = 60 * time.Second // battery and charging
	scanEvery  = 2 * time.Second  // looking for the pad while it is away
	reapEvery  = 3 * time.Second
	flashTick  = 150 * time.Millisecond // a pinned key blinks on and off this fast
	flashFor   = 6 * flashTick          //   for this long
	callFor    = 3 * time.Second
	retryAfter = time.Second
)

// The ChatGPT app's palette, so the pad looks the same in either.
const (
	effectOff    = 0
	effectSolid  = 1
	effectBreath = 4
)

type look struct {
	color  int
	effect int
}

var looks = map[string]look{
	Idle:     {0xFFFFFF, effectSolid},
	Working:  {0x304FFE, effectSolid},
	Unread:   {0x00FF4C, effectSolid},
	Awaiting: {0xFF6D00, effectBreath},
	Error:    {0xFF0033, effectSolid},
}

// deviceLoop keeps the pad open, and its battery known.
func (d *Daemon) deviceLoop(ctx context.Context) {
	for {
		d.mu.Lock()
		device := d.device
		d.mu.Unlock()
		wait := scanEvery
		if device == nil {
			device = d.open()
		}
		var lost <-chan struct{}
		if device != nil {
			d.pollStatus(device)
			wait, lost = pollEvery, device.Lost()
		}
		select {
		case <-ctx.Done():
			return
		case <-lost:
		case <-time.After(wait):
		}
	}
}

func (d *Daemon) open() *pad.Device {
	found := pad.Find()
	if len(found) == 0 {
		return nil
	}
	device, err := pad.Open(found[0], d.onNotify)
	var firmware struct {
		Version string `json:"version"`
	}
	if err == nil {
		var result json.RawMessage
		if result, err = device.Call("sys.version", nil, callFor); err == nil {
			err = json.Unmarshal(result, &firmware)
		}
		if err != nil {
			device.Close()
		}
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if err != nil {
		if d.info.Problem != err.Error() {
			d.log.Println("open failed:", err)
			d.info = DeviceInfo{Problem: err.Error()}
			d.changed()
		}
		return nil
	}
	d.device = device
	d.info = DeviceInfo{Connected: true, Model: device.Model, Transport: device.Transport, Firmware: firmware.Version}
	d.log.Println("device", device.Model, device.Transport, firmware.Version)
	d.changed()
	go func() {
		<-device.Lost()
		d.mu.Lock()
		defer d.mu.Unlock()
		if d.device == device {
			d.log.Println("device lost")
			d.device = nil
			d.info = DeviceInfo{}
			d.changed()
		}
	}()
	return device
}

func (d *Daemon) pollStatus(device *pad.Device) {
	result, err := device.Call("device.status", nil, callFor)
	var st struct {
		Battery    *int `json:"battery"`
		IsCharging bool `json:"is_charging"`
	}
	if err != nil || json.Unmarshal(result, &st) != nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.device != device {
		return
	}
	info := d.info
	info.Battery, info.Charging = st.Battery, st.IsCharging
	if !sameInfo(info, d.info) {
		d.info = info
		d.changed()
	}
}

func sameInfo(a, b DeviceInfo) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(ab) == string(bb)
}

// lightsLoop sends the pad each key whose look changed, and every key on the
// heartbeat. Over Bluetooth a report can take a third of a second while the
// link idles, so a change sends only its own key.
func (d *Daemon) lightsLoop(ctx context.Context) {
	lit := map[int]look{} // what the pad shows now
	var shown *pad.Device
	failures := 0
	beat := time.NewTimer(heartbeat)
	for {
		everything := false
		select {
		case <-ctx.Done():
			return
		case <-d.dirty:
		case <-beat.C:
			everything = true
		}
		if everything {
			beat.Reset(heartbeat)
		}

		d.mu.Lock()
		device := d.device
		want := map[int]look{}
		for i := range slots {
			want[i] = look{0, effectOff}
		}
		for _, s := range d.sessions {
			if s.slot >= 0 {
				want[s.slot] = looks[s.state]
			}
		}
		now := time.Now()
		for slot, until := range d.flash {
			switch left := until.Sub(now); {
			case left <= 0:
				delete(d.flash, slot)
			case (left/flashTick)%2 == 0:
				want[slot] = look{0xFFFFFF, effectSolid}
			default:
				want[slot] = look{0, effectOff}
			}
		}
		d.mu.Unlock()
		if device == nil {
			continue
		}
		if device != shown {
			lit, shown = map[int]look{}, device
		}

		type light struct {
			ID         int      `json:"id"`
			Color      int      `json:"color"`
			Brightness int      `json:"brightness"`
			Effect     int      `json:"effect"`
			Speed      *float64 `json:"speed,omitempty"`
		}
		var params []light
		for i := range slots {
			l := want[i]
			if old, ok := lit[i]; ok && old == l && !everything {
				continue
			}
			p := light{ID: i, Color: l.color, Effect: l.effect}
			if l.color != 0 {
				p.Brightness = 1
			}
			if l.effect == effectBreath {
				speed := 0.5
				p.Speed = &speed
			}
			params = append(params, p)
		}
		if len(params) == 0 {
			continue
		}
		if _, err := device.Call("v.oai.thstatus", params, callFor); err != nil {
			// Often a link still settling: try again shortly, then less often.
			if failures++; failures <= 3 || failures%10 == 0 {
				d.log.Printf("lights failed (%d): %v", failures, err)
			}
			lit = map[int]look{}
			time.AfterFunc(min(retryAfter*time.Duration(failures), heartbeat), d.relight)
			continue
		}
		failures = 0
		for _, p := range params {
			lit[p.ID] = want[p.ID]
		}
	}
}

// startFlash blinks a key for a moment, to show it was just pinned. The
// caller holds d.mu.
func (d *Daemon) startFlash(slot int) {
	d.flash[slot] = time.Now().Add(flashFor)
	for i := 1; i <= int(flashFor/flashTick); i++ {
		time.AfterFunc(time.Duration(i)*flashTick, d.relight)
	}
}

// relight has the lights sent again.
func (d *Daemon) relight() {
	select {
	case d.dirty <- struct{}{}:
	default:
	}
}

// reapLoop ends any session whose process is gone, in case its attach
// hangs instead of closing the connection.
func (d *Daemon) reapLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(reapEvery):
		}
		d.mu.Lock()
		for _, s := range d.sessions {
			if !alive(s.pid) {
				d.log.Println("reap", s.pid)
				s.close()
			}
		}
		d.mu.Unlock()
	}
}
