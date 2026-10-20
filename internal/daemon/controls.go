package daemon

import (
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Endless-Winning-AI/lets-build-clauder/internal/config"
	"github.com/Endless-Winning-AI/lets-build-clauder/internal/keys"
)

const (
	holdFor    = 500 * time.Millisecond  // a dial press held this long is "hold"
	tapFor     = 250 * time.Millisecond  // a hold-and-tap key let go sooner is a tap
	tapDown    = 30 * time.Millisecond   // how long a tap's chord is held
	chordQuiet = 1500 * time.Millisecond // send is ignored this long after a pin
	doubleFor  = 400 * time.Millisecond  // two taps this close are a double tap
	stickFire  = 0.8                     // how far the stick goes to act
	stickRearm = 0.3                     // and how near the center to act again
)

// keyControls names the action keys by the switch under them. The mic is one
// wide key on two switches.
var keyControls = map[string]string{
	"ACT06": "fast", "ACT07": "approve", "ACT08": "deny", "ACT09": "branch",
	"ACT10": "mic", "ACT11": "mic", "ACT12": "send",
}

// controls is the pad's controls, mid-gesture.
type controls struct {
	down       map[string]bool          // switches held down
	holding    map[string]config.Action // hold actions in force, by control
	pending    map[string]*tapWait      // hold-and-tap keys down, not yet told apart
	chorded    bool                     // an agent key was pressed while send was held
	chordedAt  time.Time                // when, so the pad's repeats of send after it are ignored
	sendTapAt  time.Time                // the last lone tap of send, for double taps
	checkAt    time.Time                // when the check key went down
	checkPin   bool                     // an agent key was pressed while check was held
	checkPinAt time.Time                // when, so the pad's repeats of check after it are ignored
	dialDown   time.Time
	stickArmed bool
	warned     map[string]bool // problems already told of
}

// tapWait is a hold-and-tap key held down, until it is let go (a tap) or
// the timer fires (a hold).
type tapWait struct {
	timer  *time.Timer
	action config.Action
}

func newControls() controls {
	return controls{down: map[string]bool{}, holding: map[string]config.Action{}, pending: map[string]*tapWait{},
		stickArmed: true, warned: map[string]bool{}}
}

// onNotify handles a notification from the pad.
func (d *Daemon) onNotify(method string, params json.RawMessage) {
	switch method {
	case "v.oai.hid":
		var p struct {
			K   string `json:"k"`
			Act int    `json:"act"`
		}
		if json.Unmarshal(params, &p) == nil {
			d.onKey(p.K, p.Act)
		}
	case "v.oai.rad":
		var p struct{ A, D float64 }
		if json.Unmarshal(params, &p) == nil {
			d.onStick(p.A, p.D)
		}
	}
}

// onKey handles a switch: act 1 is down, 0 up, and 2 a dial step.
func (d *Daemon) onKey(key string, act int) {
	switch {
	case strings.HasPrefix(key, "AG"):
		if slot, err := strconv.Atoi(key[2:]); err == nil && act == 1 {
			// With the send or check key held, an agent key pins the
			// current session to it instead of switching.
			d.mu.Lock()
			sendHeld, checkHeld := d.pinKeysDown()
			chord := sendHeld || checkHeld
			if sendHeld {
				d.chorded = true
				d.chordedAt = time.Now()
			}
			if checkHeld {
				d.checkPin = true
				d.checkPinAt = time.Now()
			}
			d.mu.Unlock()
			if chord {
				go d.pin(slot)
			} else {
				go d.jump(slot)
			}
		}
	case key == "ENC_CW" && act == 2:
		d.control("dial", "right")
	case key == "ENC_CC" && act == 2:
		d.control("dial", "left")
	case key == "ENC_CLK":
		d.mu.Lock()
		held := time.Since(d.dialDown)
		if act == 1 {
			d.dialDown = time.Now()
		}
		pressed := act == 0 && !d.dialDown.IsZero()
		if act == 0 {
			d.dialDown = time.Time{}
		}
		d.mu.Unlock()
		if pressed && held >= holdFor {
			d.control("dial", "hold")
		} else if pressed {
			d.control("dial", "press")
		}
	case keyControls[key] != "":
		d.onSwitch(key, act == 1)
	}
}

// onSwitch handles an action key going down or up. Most act on the way
// down; a hold action is held for as long as the key is.
func (d *Daemon) onSwitch(key string, down bool) {
	control := keyControls[key]
	d.mu.Lock()
	was := d.isDown(control)
	if down {
		d.down[key] = true
	} else {
		delete(d.down, key)
	}
	now := d.isDown(control)
	action, holding := d.holding[control]
	wait, waiting := d.pending[control]
	kind := d.kindOf(control)
	switch {
	case now && !was:
		action = d.action("keys", control)
		switch {
		case action.Hold != "" && action.Tap != "":
			// Told apart on the way up, or when the timer fires first.
			w := &tapWait{action: action}
			w.timer = time.AfterFunc(tapFor, func() { d.holdAfterTap(control, w) })
			d.pending[control] = w
			d.mu.Unlock()
			return
		case action.Hold != "":
			d.holding[control] = action
		case kind == "send":
			// Send acts on the way up, so that an agent key pressed while
			// it is held can mean "pin" instead.
			d.chorded = false
			d.mu.Unlock()
			return
		case kind == "check":
			// Check acts on the way up, and only for a quick tap: held
			// longer it does nothing, and with an agent key it pins.
			d.checkAt = time.Now()
			d.checkPin = false
			d.mu.Unlock()
			return
		}
	case was && !now && waiting:
		// Let go before the hold began: a tap. (Once the hold has begun,
		// holdAfterTap has moved the key from pending to holding.)
		wait.timer.Stop()
		delete(d.pending, control)
		d.mu.Unlock()
		d.tap(wait.action)
		return
	case was && !now && holding:
		delete(d.holding, control)
	case was && !now && kind == "check":
		held := time.Since(d.checkAt)
		pinned := d.checkPin || time.Since(d.checkPinAt) < chordQuiet
		d.checkPin = false
		d.mu.Unlock()
		if pinned {
			return
		}
		if held >= tapFor {
			d.log.Println("control keys approve held", held.Round(time.Millisecond), "- cancelled")
			return
		}
		action = d.action("keys", control)
		d.log.Println("control keys", control, "->", action)
		go d.run(action)
		return
	case was && !now && kind == "send":
		// After a chord, this release and the pad's repeats of it mean nothing.
		chorded := d.chorded || time.Since(d.chordedAt) < chordQuiet
		d.chorded = false
		if chorded {
			d.mu.Unlock()
			return
		}
		action = d.action("keys", control)
		if action.Double == "" {
			d.mu.Unlock()
			d.log.Println("control keys", control, "->", action)
			go d.run(action)
			return
		}
		double := time.Since(d.sendTapAt) < doubleFor
		if double {
			d.sendTapAt = time.Time{} // a third tap starts over
		} else {
			d.sendTapAt = time.Now()
		}
		d.mu.Unlock()
		if double {
			d.log.Println("control keys", control, "double ->", action.Double)
			go d.press1(action.Double)
		}
		return
	default:
		d.mu.Unlock()
		return
	}
	d.mu.Unlock()

	if action.Hold != "" {
		d.hold(action, now)
	} else if now {
		d.log.Println("control keys", control, "->", action)
		go d.run(action)
	}
}

// holdAfterTap begins a hold-and-tap key's hold, once it has been down long
// enough not to be a tap.
func (d *Daemon) holdAfterTap(control string, w *tapWait) {
	d.mu.Lock()
	if d.pending[control] != w {
		d.mu.Unlock()
		return // let go first: a tap, handled on the way up
	}
	delete(d.pending, control)
	d.holding[control] = w.action
	d.mu.Unlock()
	d.hold(w.action, true)
}

// tap presses and releases a hold-and-tap key's tap chord once.
func (d *Daemon) tap(action config.Action) {
	d.log.Println("control keys tap ->", action.Tap)
	d.press1(action.Tap)
}

// press1 presses and releases a chord once, for whichever app is in front.
func (d *Daemon) press1(chord string) {
	c := keys.Parse(chord)
	if err := d.desktop.Hold(c, true); err != nil {
		d.warnHold(chord, err)
		return
	}
	time.Sleep(tapDown)
	d.desktop.Hold(c, false)
}

// kindOf says how a key acts, from what it is set to, wherever it sits on
// the pad: "check" for approve (on a quick tap, on the way up), "send" for
// the send key or a double-tap key (on the way up), or "". Both pin when held
// with an agent key. The caller holds d.mu.
func (d *Daemon) kindOf(control string) string {
	action := d.action("keys", control)
	switch {
	case action.Hold != "":
		return ""
	case action.Name == "approve":
		return "check"
	case control == "send" || action.Double != "":
		return "send"
	}
	return ""
}

// pinKeysDown reports whether a send-kind or check-kind key is held. The
// caller holds d.mu.
func (d *Daemon) pinKeysDown() (send, check bool) {
	for key := range d.down {
		switch d.kindOf(keyControls[key]) {
		case "send":
			send = true
		case "check":
			check = true
		}
	}
	return send, check
}

// isDown reports whether any switch of a control is down. The caller holds d.mu.
func (d *Daemon) isDown(control string) bool {
	for key := range d.down {
		if keyControls[key] == control {
			return true
		}
	}
	return false
}

func (d *Daemon) hold(action config.Action, down bool) {
	err := d.desktop.Hold(keys.Parse(action.Hold), down)
	if err == nil || !down {
		return
	}
	d.warnHold(action.Hold, err)
}

// warnHold logs a failure to hold or tap keys, and tells a session of it
// once.
func (d *Daemon) warnHold(chord string, err error) {
	d.log.Println("hold", chord+":", err)
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.warned[err.Error()] {
		d.warned[err.Error()] = true
		d.tell(d.target(), map[string]any{"type": "notice", "text": "Let's Build Clauder: " + err.Error()})
	}
}

// onStick acts once per push past stickFire, and again once the stick has
// come back near the center.
func (d *Daemon) onStick(angle, distance float64) {
	d.mu.Lock()
	fire := false
	switch {
	case distance < stickRearm:
		d.stickArmed = true
	case distance >= stickFire && d.stickArmed:
		d.stickArmed, fire = false, true
	}
	d.mu.Unlock()
	if fire {
		// The angle is a fraction of a turn, clockwise from the right.
		quarter := int(math.Round(angle*4)) % 4
		d.control("stick", []string{"right", "down", "left", "up"}[(quarter+4)%4])
	}
}

func (d *Daemon) control(section, name string) {
	d.mu.Lock()
	action := d.action(section, name)
	d.mu.Unlock()
	d.log.Println("control", section, name, "->", action)
	if action.Name == "chat-next" || action.Name == "chat-prev" {
		d.run(action) // in order: the knob's clicks come fast
		return
	}
	go d.run(action)
}

// action is what a control does, from config.toml as it is now. The caller
// holds d.mu.
func (d *Daemon) action(section, name string) config.Action {
	return d.settings().Action(section, name)
}

// settings is config.toml as it is now. The caller holds d.mu.
func (d *Daemon) settings() config.Config {
	if stamp := config.Modified(d.configPath); stamp != d.configStamp {
		var problems []string
		d.config, problems = config.Load(d.configPath)
		d.configStamp = stamp
		d.log.Println("config reloaded")
		for _, p := range problems {
			d.log.Println("config:", p)
		}
	}
	return d.config
}

// target is the session a control acts on: the focused one, else the
// Claude app chat on screen, else the one a control last went to, else the
// only one. The caller holds d.mu.
func (d *Daemon) target() *Session {
	if !d.live(d.focused) {
		if shown := shownChat(d.sidebar); shown != "" {
			for _, s := range d.sessions {
				if hostOf(s) == shown {
					return s
				}
			}
		}
	}
	for _, s := range []*Session{d.focused, d.last} {
		if d.live(s) {
			return s
		}
	}
	if len(d.sessions) == 1 {
		for _, s := range d.sessions {
			return s
		}
	}
	return nil
}

// tell has a session's plugin carry out a message. The caller holds d.mu.
func (d *Daemon) tell(s *Session, msg map[string]any) {
	if s != nil {
		d.last = s
		d.send(s, msg)
	}
}

// do has the target session's plugin carry out an action inside Claude Code.
func (d *Daemon) do(action string, fields ...any) {
	msg := map[string]any{"type": "action", "action": action}
	for i := 0; i+1 < len(fields); i += 2 {
		msg[fields[i].(string)] = fields[i+1]
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.tell(d.target(), msg)
}

// run carries out an action.
func (d *Daemon) run(a config.Action) {
	switch {
	case a.Text != "":
		d.do("text", "text", a.Text, "submit", a.Submit)
	case a.Keys != "":
		d.pressTarget(keys.Parse(a.Keys))
	case a.Run != nil:
		cmd := exec.Command(a.Run[0], a.Run[1:]...)
		detach(cmd)
		if err := cmd.Start(); err != nil {
			d.log.Println("run:", err)
			return
		}
		go cmd.Wait()
	case strings.HasPrefix(a.Name, "/"):
		command, args, _ := strings.Cut(a.Name[1:], " ")
		d.do("command", "command", command, "args", args)
	case a.Name == "approve" || a.Name == "deny":
		d.answer(a.Name == "approve")
	case a.Name == "fast" || a.Name == "voice":
		d.do("command", "command", a.Name, "args", "")
	case a.Name == "cycle-mode":
		d.pressTarget(keys.Chord{Mods: []string{"shift"}, Key: "tab"})
	case a.Name == "attention":
		d.mu.Lock()
		s := d.mostUrgent()
		d.mu.Unlock()
		if s != nil {
			d.goTo(s)
		} else {
			d.cycle(1) // nothing needs you: go round the sessions
		}
	case a.Name == "next-waiting":
		d.nextWaiting()
	case a.Name == "chat-next":
		d.stepChat(1)
	case a.Name == "chat-prev":
		d.stepChat(-1)
	case a.Name == "next-session":
		d.cycle(1)
	case a.Name == "prev-session":
		d.cycle(-1)
	case a.Name != "none" && a.Name != "":
		// submit, interrupt, fork, effort-up and effort-down happen inside
		// Claude Code.
		d.do(a.Name)
	}
}

func (d *Daemon) pressTarget(c keys.Chord) {
	d.mu.Lock()
	s := d.target()
	d.mu.Unlock()
	if s != nil {
		d.press(s, c)
	}
}

// mostUrgent is the session that has waited longest on you: for a prompt
// first, then on an error, then with an answer. The caller holds d.mu.
func (d *Daemon) mostUrgent() *Session {
	for _, state := range []string{Awaiting, Error, Unread} {
		var first *Session
		for _, s := range d.sessions {
			if s.state == state && (first == nil || s.since.Before(first.since)) {
				first = s
			}
		}
		if first != nil {
			return first
		}
	}
	return nil
}

// nextWaiting goes to the next chat waiting on you (amber), oldest first,
// going round them on each press. With none waiting it does nothing.
func (d *Daemon) nextWaiting() {
	d.mu.Lock()
	var waiting []*Session
	for _, s := range d.sessions {
		if s.state == Awaiting {
			waiting = append(waiting, s)
		}
	}
	if len(waiting) == 0 {
		d.mu.Unlock()
		d.log.Println("next-waiting: no chat waiting")
		return
	}
	sort.Slice(waiting, func(i, j int) bool { return waiting[i].since.Before(waiting[j].since) })
	next := waiting[0]
	for i, s := range waiting {
		if s == d.last {
			next = waiting[(i+1)%len(waiting)]
		}
	}
	d.mu.Unlock()
	d.goTo(next)
}

// jump brings forward the session on a key.
func (d *Daemon) jump(slot int) {
	d.mu.Lock()
	var s *Session
	for _, o := range d.sessions {
		if o.slot == slot && slot >= 0 {
			s = o
		}
	}
	closed := ""
	if s == nil {
		closed = d.pins.host(slot) // a pinned chat that isn't open
	}
	d.mu.Unlock()
	if closed != "" {
		d.desktop.Focus("Claude:" + closed)
		return
	}
	d.goTo(s)
}

// goTo brings a session forward, keyed or not.
func (d *Daemon) goTo(s *Session) {
	d.mu.Lock()
	if !d.live(s) {
		s = nil
	}
	if s != nil {
		d.last = s
		// Going to a finished session is looking at it: the desktop app can't
		// tell us otherwise.
		if s.state == Unread || s.state == Error {
			d.setState(s, Idle, true)
		}
	}
	d.mu.Unlock()
	if s != nil {
		d.focus(s)
	}
}

// chatsFrozen is how long the knob's order holds after a step, so that
// chats don't move under it while it turns.
const chatsFrozen = 3 * time.Second

// stepChat moves through the desktop app's chats in its sidebar's order,
// from the one on screen. It stops at either end.
func (d *Daemon) stepChat(step int) {
	d.mu.Lock()
	if time.Since(d.chatsAt) > chatsFrozen || d.chatAt == "" {
		d.chats, d.chatAt = sidebarOrder(d.sidebar, d.settings().Hide)
	}
	d.chatsAt = time.Now()
	if len(d.chats) == 0 {
		d.mu.Unlock()
		return
	}
	at := slices.Index(d.chats, d.chatAt)
	next := min(max(at+step, 0), len(d.chats)-1)
	if at == -1 {
		next = 0
	}
	id := d.chats[next]
	d.chatAt = id
	var s *Session
	for _, o := range d.sessions {
		if hostOf(o) == id {
			s = o
		}
	}
	d.mu.Unlock()
	switch {
	case next == at:
	case s != nil:
		d.goTo(s)
	default:
		d.desktop.Focus("Claude:" + id) // a chat that isn't running opens too
	}
}

// cycle moves to the next or previous session by key.
func (d *Daemon) cycle(step int) {
	d.mu.Lock()
	var keyed []*Session
	for _, s := range d.sessions {
		if s.slot >= 0 {
			keyed = append(keyed, s)
		}
	}
	if len(keyed) == 0 {
		d.mu.Unlock()
		return
	}
	sort.Slice(keyed, func(i, j int) bool { return keyed[i].slot < keyed[j].slot })
	at := -step
	current := d.target()
	for i, s := range keyed {
		if s == current {
			at = i
		}
	}
	slot := keyed[((at+step)%len(keyed)+len(keyed))%len(keyed)].slot
	d.mu.Unlock()
	d.jump(slot)
}

// answer answers the permission prompt of the session waiting on you: the
// focused one, else the one waiting longest. With none waiting, approve
// presses Enter if the Claude app is in front, and deny stops the running
// turn.
func (d *Daemon) answer(approve bool) {
	d.mu.Lock()
	s := d.focused
	if !d.live(s) || s.state != Awaiting {
		s = nil
		for _, o := range d.sessions {
			if o.state == Awaiting && (s == nil || o.since.Before(s.since)) {
				s = o
			}
		}
	}
	if s == nil {
		t := d.target()
		d.mu.Unlock()
		switch {
		case approve:
			// Nothing to approve: Enter, but only into the Claude app.
			if d.desktop.ClaudeInFront() {
				d.log.Println("approve: no prompt waiting; enter")
				d.press1("enter")
			}
		case t != nil && t.state == Working:
			d.do("interrupt")
		}
		return
	}
	d.last = s
	if s.ask != nil {
		// The session's plugin is waiting on the pad: it answers the prompt.
		answer := map[bool]string{true: "allow", false: "deny"}[approve]
		d.log.Println("session", s.pid, "prompt:", answer)
		s.ask <- answer
		s.ask = nil
		d.mu.Unlock()
		return
	}
	d.mu.Unlock()

	if window, _ := d.windowOf(s); strings.HasPrefix(window, "Claude:") {
		// No plugin waiting, and the Claude app's prompt can't be typed into:
		// deny stops the turn, which drops the prompt; approve has to be clicked.
		d.mu.Lock()
		defer d.mu.Unlock()
		if approve {
			d.tell(s, map[string]any{"type": "notice", "text": "Let's Build Clauder: click Allow in the Claude app"})
		} else {
			d.tell(s, map[string]any{"type": "action", "action": "interrupt"})
		}
		return
	}

	if approve {
		d.approve(s)
	} else {
		d.press(s, keys.Chord{Key: "escape"})
	}
}

// pin gives the agent key at slot to the current session: the one last
// typed in, else the one controls go to.
func (d *Daemon) pin(slot int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	s := d.active
	if !d.live(s) {
		s = d.target()
	}
	if s == nil {
		d.log.Println("pin key", slot+1, "- no session to pin")
		return
	}
	d.claim(s, slot)
	if host := hostOf(s); host != "" {
		if err := d.pins.set(host, slot); err != nil {
			d.log.Println("pins:", err)
		}
	}
	d.log.Println("pin", s.pid, "key", slot+1)
	d.startFlash(slot)
	d.tell(s, map[string]any{"type": "notice", "text": fmt.Sprintf("Let's Build Clauder: pinned to key %d", slot+1)})
	d.changed()
}

// claim moves a session to a key, and whoever had the key to a free one,
// or to none. The caller holds d.mu.
func (d *Daemon) claim(s *Session, slot int) {
	var bumped *Session
	for _, o := range d.sessions {
		if o != s && o.slot == slot {
			bumped = o
			o.slot = -1
		}
	}
	s.slot = slot
	if bumped != nil {
		if !d.settings().Manual {
			bumped.slot = d.freeSlot(nil)
		}
		d.log.Println("session", bumped.pid, "moved to key", keyName(bumped))
	}
}
