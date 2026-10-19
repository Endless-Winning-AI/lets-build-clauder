// Package config holds where this program keeps its files, and what each
// control on the pad does.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// RuntimeDir holds the daemon's socket and lock.
func RuntimeDir() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "lets-build-clauder")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("lets-build-clauder-%d", os.Getuid()))
}

// MakePrivateDir makes dir, or checks that it is this user's alone: anyone
// who could place a socket in it could pose as the daemon.
func MakePrivateDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return CheckPrivateDir(dir)
}

// Socket is where the daemon listens.
func Socket() string { return filepath.Join(RuntimeDir(), "daemon.sock") }

// LogFile is where the daemon writes what it does.
func LogFile() string {
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Logs", "lets-build-clauder", "daemon.log")
	}
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		state = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(state, "lets-build-clauder", "daemon.log")
}

// File is the user's config.toml.
func File() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "lets-build-clauder", "config.toml")
}

// PinsFile is where sessions pinned to agent keys are remembered.
func PinsFile() string {
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support", "lets-build-clauder", "pins.json")
	}
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		state = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(state, "lets-build-clauder", "pins.json")
}
