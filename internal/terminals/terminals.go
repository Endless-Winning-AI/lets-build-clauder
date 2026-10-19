// Package terminals drives terminals and multiplexers that can be scripted
// from outside, on any platform: tmux, WezTerm and kitty.
//
// Each one tells the programs inside it where they run through environment
// variables, so a session's pane is known from its environment alone. Panes
// are named "<kind>:<target>@<where>"; any other window belongs to the desktop.
package terminals

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/Endless-Winning-AI/lets-build-clauder/internal/keys"
)

// EnvKeys are the variables a session reports from its environment.
var EnvKeys = []string{
	"TMUX", "TMUX_PANE", "WEZTERM_PANE", "WEZTERM_UNIX_SOCKET",
	"KITTY_WINDOW_ID", "KITTY_LISTEN_ON", "ITERM_SESSION_ID", "TERM_PROGRAM",
	// The Claude desktop app's Code tab: no terminal, but a session of its own.
	"CLAUDE_CODE_ENTRYPOINT", "CLAUDE_CODE_HOST_SESSION_ID",
}

// DesktopApp names the Claude desktop app session a session's environment
// places it in, as "Claude:<host session id>", or "".
func DesktopApp(env map[string]string) string {
	if env["CLAUDE_CODE_ENTRYPOINT"] == "claude-desktop" && env["CLAUDE_CODE_HOST_SESSION_ID"] != "" {
		return "Claude:" + env["CLAUDE_CODE_HOST_SESSION_ID"]
	}
	return ""
}

// Identify names the pane a session's environment places it in, or "".
func Identify(env map[string]string) string {
	switch {
	case env["TMUX_PANE"] != "" && env["TMUX"] != "" && installed("tmux"):
		socket, _, _ := strings.Cut(env["TMUX"], ",")
		return "tmux:" + env["TMUX_PANE"] + "@" + socket
	case env["WEZTERM_PANE"] != "" && installed("wezterm"):
		return "wezterm:" + env["WEZTERM_PANE"] + "@" + env["WEZTERM_UNIX_SOCKET"]
	case env["KITTY_WINDOW_ID"] != "" && env["KITTY_LISTEN_ON"] != "" && installed("kitty"):
		return "kitty:" + env["KITTY_WINDOW_ID"] + "@" + env["KITTY_LISTEN_ON"]
	case env["ITERM_SESSION_ID"] != "":
		// "w0t0p0:<uuid>": iTerm2's AppleScript knows the session by the uuid.
		id := env["ITERM_SESSION_ID"]
		return "iTerm2:" + id[strings.LastIndexByte(id, ':')+1:]
	}
	return ""
}

// Handles reports whether a window is a pane this package drives.
func Handles(window string) bool {
	kind, _, _ := strings.Cut(window, ":")
	return kind == "tmux" || kind == "wezterm" || kind == "kitty"
}

// Focus brings a pane forward inside its terminal. For tmux, whose panes run
// apart from any window, it returns the pid of a client showing the pane,
// for the desktop to raise; otherwise 0.
func Focus(pane string) int {
	kind, target, where := split(pane)
	switch kind {
	case "tmux":
		run(nil, "", "tmux", "-S", where, "select-window", "-t", target)
		run(nil, "", "tmux", "-S", where, "select-pane", "-t", target)
		out, _ := run(nil, "", "tmux", "-S", where, "list-clients", "-F", "#{client_pid}")
		for _, field := range strings.Fields(out) {
			if pid, err := strconv.Atoi(field); err == nil {
				return pid
			}
		}
	case "wezterm":
		run(wezterm(where), "", "wezterm", "cli", "activate-pane", "--pane-id", target)
	case "kitty":
		run(nil, "", "kitty", "@", "--to", where, "focus-window", "--match", "id:"+target)
	}
	return 0
}

// Press types a key into a pane without focusing it.
func Press(pane string, c keys.Chord) bool {
	kind, target, where := split(pane)
	var err error
	switch kind {
	case "tmux":
		_, err = run(nil, "", "tmux", "-S", where, "send-keys", "-t", target, tmuxKey(c))
	case "wezterm":
		_, err = run(wezterm(where), c.Text(), "wezterm", "cli", "send-text", "--pane-id", target, "--no-paste")
	case "kitty":
		_, err = run(nil, c.Text(), "kitty", "@", "--to", where, "send-text", "--match", "id:"+target, "--stdin")
	default:
		return false
	}
	return err == nil
}

// Approve answers a permission prompt in a pane with its first option, Yes.
func Approve(pane string) bool {
	return Press(pane, keys.Chord{Key: "1"})
}

func tmuxKey(c keys.Chord) string {
	switch {
	case c.Key == "tab" && c.Only("shift"):
		return "BTab"
	case c.Has("ctrl"):
		return "C-" + c.Key
	}
	if name, ok := map[string]string{"escape": "Escape", "tab": "Tab", "enter": "Enter", "space": "Space"}[c.Key]; ok {
		return name
	}
	return c.Key
}

func split(pane string) (kind, target, where string) {
	kind, rest, _ := strings.Cut(pane, ":")
	target, where, _ = strings.Cut(rest, "@")
	return kind, target, where
}

func wezterm(socket string) []string {
	if socket == "" {
		return nil
	}
	return append(os.Environ(), "WEZTERM_UNIX_SOCKET="+socket)
}

func installed(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func run(env []string, stdin string, argv ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = env
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.Output()
	return string(out), err
}
