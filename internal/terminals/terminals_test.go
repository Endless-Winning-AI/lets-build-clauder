package terminals

import (
	"os"
	"path/filepath"
	"testing"
)

// withPrograms puts stand-ins for the named programs on PATH.
func withPrograms(t *testing.T, names ...string) {
	dir := t.TempDir()
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
}

func TestIdentify(t *testing.T) {
	withPrograms(t, "tmux", "wezterm", "kitty")
	tests := []struct {
		env  map[string]string
		want string
	}{
		{map[string]string{"TMUX": "/tmp/tmux-1000/default,123,0", "TMUX_PANE": "%3"}, "tmux:%3@/tmp/tmux-1000/default"},
		{map[string]string{"WEZTERM_PANE": "7", "WEZTERM_UNIX_SOCKET": "/run/wez"}, "wezterm:7@/run/wez"},
		{map[string]string{"KITTY_WINDOW_ID": "2", "KITTY_LISTEN_ON": "unix:/tmp/kitty"}, "kitty:2@unix:/tmp/kitty"},
		{map[string]string{"ITERM_SESSION_ID": "w0t1p0:ABC-123"}, "iTerm2:ABC-123"},
		{map[string]string{"TERM_PROGRAM": "ghostty"}, ""},
	}
	for _, tt := range tests {
		if got := Identify(tt.env); got != tt.want {
			t.Errorf("Identify(%v) = %q, want %q", tt.env, got, tt.want)
		}
	}
}

func TestIdentifyPrefersTheMultiplexer(t *testing.T) {
	withPrograms(t, "tmux")
	env := map[string]string{"TMUX": "/tmp/t,1,0", "TMUX_PANE": "%1", "ITERM_SESSION_ID": "w0t0p0:X"}
	if got := Identify(env); got != "tmux:%1@/tmp/t" {
		t.Errorf("Identify = %q, want the tmux pane", got)
	}
}

func TestIdentifySkipsMissingPrograms(t *testing.T) {
	withPrograms(t)
	if got := Identify(map[string]string{"WEZTERM_PANE": "7"}); got != "" {
		t.Errorf("Identify = %q without wezterm installed", got)
	}
}

func TestDesktopApp(t *testing.T) {
	tests := []struct {
		env  map[string]string
		want string
	}{
		{map[string]string{"CLAUDE_CODE_ENTRYPOINT": "claude-desktop", "CLAUDE_CODE_HOST_SESSION_ID": "local_abc"}, "Claude:local_abc"},
		{map[string]string{"CLAUDE_CODE_ENTRYPOINT": "claude-desktop"}, ""},
		{map[string]string{"CLAUDE_CODE_ENTRYPOINT": "cli", "CLAUDE_CODE_HOST_SESSION_ID": "local_abc"}, ""},
	}
	for _, tt := range tests {
		if got := DesktopApp(tt.env); got != tt.want {
			t.Errorf("DesktopApp(%v) = %q, want %q", tt.env, got, tt.want)
		}
	}
}
