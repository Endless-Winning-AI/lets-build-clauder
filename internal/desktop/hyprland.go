package desktop

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Endless-Winning-AI/lets-build-clauder/internal/keys"
	"github.com/Endless-Winning-AI/lets-build-clauder/internal/proc"
)

// plainName matches what Press may put in a Lua string.
var plainName = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

// Hyprland drives windows through hyprctl and follows focus on its event
// socket. Windows are addresses such as "0x55d0c8a1b2c0".
type Hyprland struct {
	None
	events string
}

// NewHyprland returns the running Hyprland, or nil.
func NewHyprland(runtimeDir string) *Hyprland {
	sig := os.Getenv("HYPRLAND_INSTANCE_SIGNATURE")
	if sig == "" {
		return nil
	}
	return &Hyprland{events: filepath.Join(runtimeDir, "hypr", sig, ".socket2.sock")}
}

func (*Hyprland) Name() string { return "Hyprland" }

func hyprctl(args ...string) string {
	out, _ := output(2*time.Second, append([]string{"hyprctl"}, args...)...)
	return strings.TrimSpace(out)
}

// Locate finds the window owned by pid or the nearest of its parents. A
// terminal serving several windows from one process can't be told apart:
// that is its first window.
func (*Hyprland) Locate(pid int, _ string) string {
	var clients []struct {
		Address string `json:"address"`
		PID     int    `json:"pid"`
	}
	if json.Unmarshal([]byte(hyprctl("clients", "-j")), &clients) != nil {
		return ""
	}
	byPID := map[int]string{}
	for _, c := range clients {
		if _, ok := byPID[c.PID]; !ok {
			byPID[c.PID] = c.Address
		}
	}
	for _, p := range proc.Ancestors(pid) {
		if address, ok := byPID[p]; ok {
			return address
		}
	}
	return ""
}

func (*Hyprland) Focus(window string) bool {
	// Hyprland 0.56 moved dispatchers to Lua; older releases take the
	// classic form.
	if hyprctl("dispatch", fmt.Sprintf(`hl.dsp.focus({ window = "address:%s" })`, window)) == "ok" {
		return true
	}
	return hyprctl("dispatch", "focuswindow", "address:"+window) == "ok"
}

// Press sends the key down and up separately: a one-shot shortcut can
// leave it stuck.
func (*Hyprland) Press(window string, c keys.Chord) bool {
	if !plainName.MatchString(c.Key + strings.Join(c.Mods, "")) {
		return false
	}
	key := map[string]string{"escape": "Escape", "tab": "Tab", "enter": "Return", "space": "space"}[c.Key]
	if key == "" {
		key = c.Key
	}
	mods := strings.ToUpper(strings.Join(c.Mods, " "))
	for _, state := range []string{"down", "up"} {
		call := fmt.Sprintf(`hl.dsp.send_key_state({ mods = "%s", key = "%s", state = "%s", window = "address:%s" })`,
			mods, key, state, window)
		if hyprctl("dispatch", call) != "ok" {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
	return true
}

func (h *Hyprland) Approve(window string) bool {
	return h.Press(window, keys.Chord{Key: "1"})
}

func (h *Hyprland) Follow(ctx context.Context, onFocus func(string)) {
	for ctx.Err() == nil {
		h.follow(ctx, onFocus)
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
		}
	}
}

func (h *Hyprland) follow(ctx context.Context, onFocus func(string)) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", h.events)
	if err != nil {
		return
	}
	defer conn.Close()
	defer context.AfterFunc(ctx, func() { conn.Close() })()
	lines := bufio.NewScanner(conn)
	for lines.Scan() {
		event, data, _ := strings.Cut(lines.Text(), ">>")
		if event == "activewindowv2" {
			if data != "" && !strings.HasPrefix(data, "0x") {
				data = "0x" + data
			}
			onFocus(data)
		}
	}
}
