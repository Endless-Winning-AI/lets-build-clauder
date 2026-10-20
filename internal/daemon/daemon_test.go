package daemon

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/Endless-Winning-AI/lets-build-clauder/internal/desktop"
	"github.com/Endless-Winning-AI/lets-build-clauder/internal/keys"
)

// fakeDesktop finds each session in a window named for its pid, and records
// what is done to windows.
type fakeDesktop struct {
	desktop.None
	mu     sync.Mutex
	done   []string
	claude bool // the Claude app is in front
}

func (f *fakeDesktop) ClaudeInFront() bool { return f.claude }

func (f *fakeDesktop) Name() string                    { return "fake" }
func (f *fakeDesktop) Locate(pid int, _ string) string { return "w" + strconv.Itoa(pid) }
func (f *fakeDesktop) record(s string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.done = append(f.done, s)
	return true
}
func (f *fakeDesktop) Focus(w string) bool               { return f.record("focus " + w) }
func (f *fakeDesktop) Approve(w string) bool             { return f.record("approve " + w) }
func (f *fakeDesktop) Press(w string, c keys.Chord) bool { return f.record("press " + c.Key + " " + w) }
func (f *fakeDesktop) Hold(c keys.Chord, down bool) error {
	f.record("hold " + c.Key + " " + map[bool]string{true: "down", false: "up"}[down])
	return nil
}

// await waits for what the desktop is asked to do.
func (f *fakeDesktop) await(t *testing.T, want ...string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		f.mu.Lock()
		got := append([]string(nil), f.done...)
		f.mu.Unlock()
		if equal(got, want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("desktop did %q, want %q", got, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

type harness struct {
	t       *testing.T
	daemon  *Daemon
	desktop *fakeDesktop
	server  *httptest.Server
}

func newHarness(t *testing.T, configText string) *harness {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if configText != "" {
		os.MkdirAll(filepath.Join(dir, "lets-build-clauder"), 0o755)
		os.WriteFile(filepath.Join(dir, "lets-build-clauder", "config.toml"), []byte(configText), 0o644)
	}
	fd := &fakeDesktop{}
	d := New("1.0.0", "/run/test.sock", fd, log.New(io.Discard, "", 0))
	// Not this computer's pins, nor its desktop app's chats.
	d.pins = loadPins(filepath.Join(dir, "pins.json"))
	d.sidebar = filepath.Join(dir, "sidebar")
	server := httptest.NewServer(d.handler())
	t.Cleanup(server.Close)
	return &harness{t, d, fd, server}
}

// client is an attached session, as its attach process sees it.
type client struct {
	t     *testing.T
	id    string
	lines *bufio.Scanner
	stop  context.CancelFunc
}

func (h *harness) attach(pid int, slot *int) *client {
	h.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	h.t.Cleanup(cancel)
	body, _ := json.Marshal(Hello{PID: pid, CWD: "/src", Slot: slot, Version: "1.0.0", Protocol: Protocol})
	req, _ := http.NewRequestWithContext(ctx, "POST", h.server.URL+"/attach", bytes.NewReader(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		h.t.Fatalf("attach: %v %v", err, resp)
	}
	c := &client{t: h.t, lines: bufio.NewScanner(resp.Body), stop: cancel}
	c.id = c.next("attached")["session"].(string)
	return c
}

// next reads messages until one of the given type.
func (c *client) next(kind string) map[string]any {
	c.t.Helper()
	for c.lines.Scan() {
		var m map[string]any
		json.Unmarshal(c.lines.Bytes(), &m)
		if m["type"] == kind {
			return m
		}
	}
	c.t.Fatalf("stream ended before a %q message", kind)
	return nil
}

func (h *harness) setState(c *client, state string) {
	h.t.Helper()
	resp, err := http.Post(h.server.URL+"/sessions/"+c.id+"/state", "application/json",
		bytes.NewReader([]byte(`{"state":"`+state+`"}`)))
	if err != nil || resp.StatusCode != http.StatusNoContent {
		h.t.Fatalf("set state: %v %v", err, resp)
	}
}

func (h *harness) waitFor(cond func() bool) {
	h.t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			h.t.Fatal("timed out")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestSessionsGetKeysInOrderAndFreeThemOnDetach(t *testing.T) {
	h := newHarness(t, "")
	a := h.attach(101, nil)
	if slot := a.next("status")["slot"]; slot != 0.0 {
		t.Fatalf("first session got slot %v", slot)
	}
	b := h.attach(102, nil)
	if slot := b.next("status")["slot"]; slot != 1.0 {
		t.Fatalf("second session got slot %v", slot)
	}
	a.stop()
	h.waitFor(func() bool {
		h.daemon.mu.Lock()
		defer h.daemon.mu.Unlock()
		return len(h.daemon.sessions) == 1
	})
	c := h.attach(103, nil)
	if slot := c.next("status")["slot"]; slot != 0.0 {
		t.Fatalf("freed key not reused: slot %v", slot)
	}
}

func TestASessionKeepsItsKeyAcrossRestarts(t *testing.T) {
	h := newHarness(t, "")
	four := 3
	a := h.attach(101, &four)
	if slot := a.next("status")["slot"]; slot != 3.0 {
		t.Fatalf("asked for slot 3, got %v", slot)
	}
}

func TestOthersHearHowManyWait(t *testing.T) {
	h := newHarness(t, "")
	a := h.attach(101, nil)
	b := h.attach(102, nil)
	h.setState(a, Awaiting)
	for {
		if m := b.next("status"); m["waiting"] == 1.0 {
			break
		}
	}
}

func TestApproveGoesToTheSessionWaiting(t *testing.T) {
	h := newHarness(t, "")
	h.attach(101, nil)
	b := h.attach(102, nil)
	h.setState(b, Awaiting)
	h.daemon.onKey("ACT07", 1)
	h.daemon.onKey("ACT07", 0)
	h.desktop.await(t, "approve w102")
}

func TestSwappedKeysActByTheirSettings(t *testing.T) {
	// The send switch set to approve, and the check switch to double-tap Enter.
	h := newHarness(t, "[keys]\nsend = \"approve\"\napprove = { double = \"enter\" }\n")
	h.attach(101, nil)
	b := h.attach(102, nil)
	h.setState(b, Awaiting)
	h.daemon.onKey("ACT12", 1) // tap the send switch: approves
	h.daemon.onKey("ACT12", 0)
	h.desktop.await(t, "approve w102")

	h.daemon.onKey("ACT07", 1) // double-tap the check switch: Enter
	h.daemon.onKey("ACT07", 0)
	h.daemon.onKey("ACT07", 1)
	h.daemon.onKey("ACT07", 0)
	h.desktop.await(t, "approve w102", "hold enter down", "hold enter up")

	h.daemon.onKey("ACT12", 1) // held: cancelled
	time.Sleep(tapFor + 50*time.Millisecond)
	h.daemon.onKey("ACT12", 0)
	time.Sleep(300 * time.Millisecond)
	h.desktop.await(t, "approve w102", "hold enter down", "hold enter up")
}

func TestSwappedCheckPlusAgentKeyPins(t *testing.T) {
	h := newHarness(t, "[keys]\nsend = \"approve\"\napprove = { double = \"enter\" }\n")
	h.attachDesktop(11, "local_a")
	second := h.attachDesktop(12, "local_b")
	h.setActive(second)
	h.daemon.onKey("ACT12", 1)
	h.daemon.onKey("AG0", 1)
	h.daemon.onKey("AG0", 0)
	h.daemon.onKey("ACT12", 0)
	h.waitFor(func() bool { return h.slotOf(12) == 0 })
	time.Sleep(300 * time.Millisecond)
	h.desktop.await(t)
}

func TestHeldCheckIsCancelled(t *testing.T) {
	h := newHarness(t, "")
	h.attach(101, nil)
	b := h.attach(102, nil)
	h.setState(b, Awaiting)
	h.daemon.onKey("ACT07", 1)
	time.Sleep(tapFor + 50*time.Millisecond)
	h.daemon.onKey("ACT07", 0)
	time.Sleep(300 * time.Millisecond)
	h.desktop.await(t) // nothing approved
}

func TestCheckPlusAgentKeyPinsInsteadOfApproving(t *testing.T) {
	h := newHarness(t, "")
	h.attachDesktop(11, "local_a")           // key 1
	second := h.attachDesktop(12, "local_b") // key 2
	h.setActive(second)
	h.desktop.claude = true

	h.daemon.onKey("ACT07", 1) // check down
	h.daemon.onKey("AG0", 1)   // agent key 1
	h.daemon.onKey("AG0", 0)
	h.daemon.onKey("ACT07", 0) // check up: no approve or Enter, it was a chord
	h.waitFor(func() bool { return h.slotOf(12) == 0 })
	time.Sleep(300 * time.Millisecond)
	h.desktop.await(t) // nothing switched, nothing pressed
}

func TestDenyWithNothingWaitingInterrupts(t *testing.T) {
	h := newHarness(t, "")
	a := h.attach(101, nil)
	h.setState(a, Working)
	h.daemon.onKey("ACT08", 1)
	if m := a.next("action"); m["action"] != "interrupt" {
		t.Fatalf("got %v", m)
	}
}

func TestMicHoldsAKeyWhileEitherSwitchIsDown(t *testing.T) {
	h := newHarness(t, "[keys]\nmic = { hold = \"fn\" }\n")
	h.daemon.onKey("ACT10", 1)
	h.daemon.onKey("ACT11", 1)
	h.daemon.onKey("ACT10", 0)
	h.daemon.onKey("ACT11", 0)
	h.desktop.await(t, "hold fn down", "hold fn up")
}

func TestStickActsOncePerPush(t *testing.T) {
	h := newHarness(t, `
[stick]
up = { text = "up" }
down = { text = "down" }
left = { text = "left" }
right = { text = "right" }
`)
	a := h.attach(101, nil)
	for _, push := range []struct {
		angle float64
		want  string
	}{{0, "right"}, {0.25, "down"}, {0.5, "left"}, {0.75, "up"}, {0.98, "right"}} {
		h.daemon.onStick(push.angle, 0.9)
		h.daemon.onStick(push.angle, 1.0) // still pushed: no second action
		h.daemon.onStick(push.angle, 0.1)
		if m := a.next("action"); m["text"] != push.want {
			t.Fatalf("angle %v: got %v, want %s", push.angle, m["text"], push.want)
		}
	}
}

func TestNewerVersions(t *testing.T) {
	for _, tt := range []struct {
		a, b string
		want bool
	}{{"0.10.0", "0.9.1", true}, {"1.0", "1.0.0", false}, {"0.7.0", "0.7.1", false}} {
		if got := newer(tt.a, tt.b); got != tt.want {
			t.Errorf("newer(%s, %s) = %v", tt.a, tt.b, got)
		}
	}
}

func TestMicTapPressesTheTapChordOnce(t *testing.T) {
	h := newHarness(t, "[keys]\nmic = { hold = \"f13\", tap = \"f14\" }\n")
	h.daemon.onKey("ACT10", 1)
	h.daemon.onKey("ACT10", 0)
	h.desktop.await(t, "hold f14 down", "hold f14 up")
}

func TestMicHeldPastTheTapHoldsTheHoldChord(t *testing.T) {
	h := newHarness(t, "[keys]\nmic = { hold = \"f13\", tap = \"f14\" }\n")
	h.daemon.onKey("ACT10", 1)
	time.Sleep(tapFor + 100*time.Millisecond)
	h.desktop.await(t, "hold f13 down")
	h.daemon.onKey("ACT10", 0)
	h.desktop.await(t, "hold f13 down", "hold f13 up")
}

// attachDesktop attaches a Claude desktop app session.
func (h *harness) attachDesktop(pid int, host string) *client {
	h.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	h.t.Cleanup(cancel)
	env := map[string]string{"CLAUDE_CODE_ENTRYPOINT": "claude-desktop", "CLAUDE_CODE_HOST_SESSION_ID": host}
	body, _ := json.Marshal(Hello{PID: pid, CWD: "/src", Env: env, Version: "1.0.0", Protocol: Protocol})
	req, _ := http.NewRequestWithContext(ctx, "POST", h.server.URL+"/attach", bytes.NewReader(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		h.t.Fatalf("attach: %v %v", err, resp)
	}
	c := &client{t: h.t, lines: bufio.NewScanner(resp.Body), stop: cancel}
	c.id = c.next("attached")["session"].(string)
	return c
}

func (h *harness) setActive(c *client) {
	h.t.Helper()
	resp, err := http.Post(h.server.URL+"/sessions/"+c.id+"/state", "application/json",
		bytes.NewReader([]byte(`{"state":"idle","active":true}`)))
	if err != nil || resp.StatusCode != http.StatusNoContent {
		h.t.Fatalf("set active: %v %v", err, resp)
	}
}

func (h *harness) slotOf(pid int) int {
	h.daemon.mu.Lock()
	defer h.daemon.mu.Unlock()
	for _, s := range h.daemon.sessions {
		if s.pid == pid {
			return s.slot
		}
	}
	return -2
}

func TestSendPlusAgentKeyPinsTheActiveSessionAndRemembersIt(t *testing.T) {
	h := newHarness(t, "")
	h.attachDesktop(11, "local_a")           // key 1
	second := h.attachDesktop(12, "local_b") // key 2
	h.setActive(second)

	h.daemon.onKey("ACT12", 1) // send down
	h.daemon.onKey("AG0", 1)   // agent key 1
	h.daemon.onKey("AG0", 0)
	h.daemon.onKey("ACT12", 0) // send up: no submit, it was a chord
	h.waitFor(func() bool { return h.slotOf(12) == 0 && h.slotOf(11) == 1 })
	if n := second.next("notice"); n["text"] != "Let's Build Clauder: pinned to key 1" {
		t.Errorf("notice = %v", n)
	}
	h.desktop.await(t) // nothing switched, nothing pressed
	if got := h.daemon.pins.slot("local_b"); got != 0 {
		t.Errorf("pinned slot = %d, want 0", got)
	}

	// Back after a restart, with another session on its key: it takes it back.
	second.stop()
	h.waitFor(func() bool { return h.slotOf(12) == -2 })
	h.attachDesktop(13, "local_c")
	h.waitFor(func() bool { return h.slotOf(13) == 0 })
	h.attachDesktop(14, "local_b")
	h.waitFor(func() bool { return h.slotOf(14) == 0 && h.slotOf(13) == 2 })
}

func TestSendAloneStillSubmits(t *testing.T) {
	h := newHarness(t, "")
	c := h.attach(21, nil)
	h.daemon.onKey("ACT12", 1)
	h.daemon.onKey("ACT12", 0)
	if m := c.next("action"); m["action"] != "submit" {
		t.Errorf("action = %v", m)
	}
}

func TestSendDoubleTapPressesTheChordAndOneTapDoesNothing(t *testing.T) {
	h := newHarness(t, "[keys]\nsend = { double = \"enter\" }\n")
	h.daemon.onKey("ACT12", 1)
	h.daemon.onKey("ACT12", 0)
	time.Sleep(doubleFor + 100*time.Millisecond)
	h.desktop.await(t) // one tap: nothing
	h.daemon.onKey("ACT12", 1)
	h.daemon.onKey("ACT12", 0)
	h.daemon.onKey("ACT12", 1)
	h.daemon.onKey("ACT12", 0)
	h.desktop.await(t, "hold enter down", "hold enter up")
}

func TestAttentionWithNothingWaitingGoesToTheNextSession(t *testing.T) {
	h := newHarness(t, "[keys]\napprove = \"attention\"\n")
	h.attachDesktop(11, "local_a") // key 1
	h.attachDesktop(12, "local_b") // key 2
	h.daemon.onKey("ACT07", 1)     // check key, nothing waiting
	h.daemon.onKey("ACT07", 0)
	h.desktop.await(t, "focus Claude:local_a")
}

func TestManualKeysGoOnlyToPinnedSessions(t *testing.T) {
	h := newHarness(t, "[agents]\nmanual = true\n")
	h.attachDesktop(11, "local_a")
	second := h.attachDesktop(12, "local_b")
	if h.slotOf(11) != -1 || h.slotOf(12) != -1 {
		t.Fatalf("keys = %d, %d; want none", h.slotOf(11), h.slotOf(12))
	}
	h.setActive(second)
	h.daemon.onKey("ACT12", 1)
	h.daemon.onKey("AG2", 1)
	h.daemon.onKey("AG2", 0)
	h.daemon.onKey("ACT12", 0)
	h.waitFor(func() bool { return h.slotOf(12) == 2 })
	if h.slotOf(11) != -1 {
		t.Errorf("unpinned session got key %d", h.slotOf(11))
	}
}

func TestKeyOfAClosedPinnedChatOpensIt(t *testing.T) {
	h := newHarness(t, "[agents]\nmanual = true\n")
	h.daemon.pins.set("local_gone", 3)
	h.daemon.onKey("AG3", 1)
	h.desktop.await(t, "focus Claude:local_gone")
}

func TestKnobFollowsTheSidebarFromTheChatOnScreen(t *testing.T) {
	h := newHarness(t, "[agents]\nmanual = true\nhide = [\"Hidden\"]\n[dial]\nright = \"chat-next\"\nleft = \"chat-prev\"\n")
	account := filepath.Join(h.daemon.sidebar, "acct", "org")
	os.MkdirAll(account, 0o755)
	chat := func(id, cwd string, activity, focused int64, parent string) {
		c := map[string]any{"sessionId": id, "cwd": cwd, "lastActivityAt": activity, "lastFocusedAt": focused}
		if parent != "" {
			c["spawnedFrom"] = map[string]string{"sessionId": parent}
		}
		data, _ := json.Marshal(c)
		os.WriteFile(filepath.Join(account, id+".json"), data, 0o644)
	}
	// Sidebar: Alpha (b newer than a, a's side chat under it), Beta, Hidden (skipped), This computer.
	chat("local_a", "/p/Alpha", 1, 0, "")
	chat("local_b", "/p/Alpha", 5, 9, "") // on screen
	chat("local_a1", "/p/Alpha", 2, 0, "local_a")
	chat("local_c", "/p/beta", 3, 0, "")
	chat("local_h", "/p/Hidden", 8, 0, "")
	chat("local_s", "/x/scratch-workspaces/y", 7, 0, "")
	order, shown := sidebarOrder(h.daemon.sidebar, []string{"Hidden"})
	if want := []string{"local_b", "local_a", "local_a1", "local_c", "local_s"}; !equal(order, want) || shown != "local_b" {
		t.Fatalf("order = %v, shown %q; want %v, local_b", order, shown, want)
	}
	h.attachDesktop(11, "local_a") // a running chat goes through the session
	turn := func(key string) { h.daemon.onKey(key, 2) }
	turn("ENC_CW")
	turn("ENC_CW")
	turn("ENC_CC")
	turn("ENC_CC")
	turn("ENC_CC") // already at the top: stays
	h.desktop.await(t, "focus Claude:local_a", "focus Claude:local_a1", "focus Claude:local_a", "focus Claude:local_b")
}

func TestSendIsQuietAfterAPin(t *testing.T) {
	h := newHarness(t, "")
	c := h.attach(31, nil)
	h.daemon.onKey("ACT12", 1) // send down
	h.daemon.onKey("AG1", 1)   // pin
	h.daemon.onKey("AG1", 0)
	h.daemon.onKey("ACT12", 0) // the chord's own release
	h.daemon.onKey("ACT12", 1) // and the pad repeating it right after
	h.daemon.onKey("ACT12", 0)
	if n := c.next("notice"); n["text"] != "Let's Build Clauder: pinned to key 2" {
		t.Errorf("notice = %v", n)
	}
	h.daemon.onKey("ACT06", 1) // the next action to reach the session is this one, not a submit
	if m := c.next("action"); m["command"] != "fast" {
		t.Errorf("action = %v", m)
	}
	if h.slotOf(31) != 1 {
		t.Errorf("key = %d, want 2", h.slotOf(31)+1)
	}
}

func TestControlsGoToTheChatOnScreen(t *testing.T) {
	h := newHarness(t, "[stick]\nup = \"effort-up\"\n")
	account := filepath.Join(h.daemon.sidebar, "acct", "org")
	os.MkdirAll(account, 0o755)
	for id, focused := range map[string]int64{"local_a": 1, "local_b": 9} {
		data, _ := json.Marshal(map[string]any{"sessionId": id, "cwd": "/p/x", "lastFocusedAt": focused})
		os.WriteFile(filepath.Join(account, id+".json"), data, 0o644)
	}
	h.attachDesktop(11, "local_a")
	b := h.attachDesktop(12, "local_b")
	h.daemon.onStick(0.75, 1) // up: effort-up, given below
	if m := b.next("action"); m["action"] != "effort-up" {
		t.Errorf("action = %v", m)
	}
}

func TestCheckAnswersAPromptTheChatIsWaitingOn(t *testing.T) {
	h := newHarness(t, "")
	c := h.attachDesktop(11, "local_a")
	resp, err := http.Post(h.server.URL+"/sessions/"+c.id+"/state", "application/json",
		bytes.NewReader([]byte(`{"state":"awaiting"}`)))
	if err != nil || resp.StatusCode != http.StatusNoContent {
		t.Fatalf("set awaiting: %v %v", err, resp)
	}
	answered := make(chan string, 1)
	go func() {
		resp, err := http.Post(h.server.URL+"/sessions/"+c.id+"/ask", "application/json", nil)
		if err != nil {
			answered <- err.Error()
			return
		}
		var body struct{ Answer *string }
		json.NewDecoder(resp.Body).Decode(&body)
		if body.Answer == nil {
			answered <- "null"
			return
		}
		answered <- *body.Answer
	}()
	h.waitFor(func() bool {
		h.daemon.mu.Lock()
		defer h.daemon.mu.Unlock()
		for _, s := range h.daemon.sessions {
			if s.ask != nil {
				return true
			}
		}
		return false
	})
	h.daemon.onKey("ACT07", 1) // check
	h.daemon.onKey("ACT07", 0)
	select {
	case got := <-answered:
		if got != "allow" {
			t.Errorf("answer = %q, want allow", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no answer")
	}
}

func TestCheckWithNothingToApprovePressesEnterInClaude(t *testing.T) {
	h := newHarness(t, "")
	h.attachDesktop(11, "local_a")
	h.desktop.claude = true
	h.daemon.onKey("ACT07", 1)
	h.daemon.onKey("ACT07", 0)
	h.desktop.await(t, "hold enter down", "hold enter up")
}

func TestCheckWithNothingToApproveDoesNothingOutsideClaude(t *testing.T) {
	h := newHarness(t, "")
	h.attachDesktop(11, "local_a")
	h.daemon.onKey("ACT07", 1)
	h.daemon.onKey("ACT07", 0)
	time.Sleep(300 * time.Millisecond)
	h.desktop.await(t)
}
