package main

import (
	"errors"
	"os"
	"os/exec"

	"github.com/Endless-Winning-AI/lets-build-clauder/internal/config"
	"github.com/Endless-Winning-AI/lets-build-clauder/internal/desktop"
)

// launch opens argv in a new terminal window in dir: the configured
// terminal, else one like the session's own.
func launch(dir string, argv []string) error {
	c, _ := config.Load(config.File())
	if c.Terminal == nil && desktop.Detect(config.RuntimeDir()).Open(dir, argv, os.Getppid()) {
		return nil
	}
	term := c.Terminal
	if term == nil {
		term = defaultTerminal()
	}
	if len(term) == 0 {
		return errors.New("no terminal found; set [terminal] command in " + config.File())
	}
	cmd := exec.Command(term[0], append(term[1:], argv...)...)
	cmd.Dir = dir
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// defaultTerminal is a command that opens a program in a new terminal
// window, on Linux.
func defaultTerminal() []string {
	if installed("xdg-terminal-exec") {
		return []string{"xdg-terminal-exec"}
	}
	if t := os.Getenv("TERMINAL"); t != "" {
		return []string{t, "-e"}
	}
	for _, t := range [][]string{
		{"foot"}, {"kitty"}, {"alacritty", "-e"}, {"ghostty", "-e"},
		{"wezterm", "start", "--"}, {"gnome-terminal", "--"}, {"konsole", "-e"},
	} {
		if installed(t[0]) {
			return t
		}
	}
	return nil
}

func installed(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
