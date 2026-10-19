package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/Endless-Winning-AI/lets-build-clauder/internal/config"
)

const logLimit = 1 << 20

// daemonClient talks HTTP to the daemon over its socket, starting the daemon
// when nothing answers.
var daemonClient = &http.Client{Transport: &http.Transport{
	DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		if err := config.MakePrivateDir(config.RuntimeDir()); err != nil {
			return nil, err
		}
		var d net.Dialer
		conn, err := d.DialContext(ctx, "unix", config.Socket())
		if err == nil {
			return conn, nil
		}
		if err := startDaemon(); err != nil {
			return nil, err
		}
		for range 40 {
			time.Sleep(100 * time.Millisecond)
			if conn, err := d.DialContext(ctx, "unix", config.Socket()); err == nil {
				return conn, nil
			}
		}
		return nil, fmt.Errorf("the daemon did not start; see %s", config.LogFile())
	},
}}

// launchAgent is the label of the LaunchAgent that runs the daemon, if set up.
const launchAgent = "ai.endlesswinning.lets-build-clauder"

// startDaemon starts the daemon through a short-lived `start`, so that the
// daemon belongs to no session's process tree: Claude Code ends a session's
// whole tree when it exits.
func startDaemon() error {
	// When the daemon runs as a LaunchAgent, only launchd starts it: a copy
	// started from a session would run without the agent's macOS permissions.
	if home, err := os.UserHomeDir(); err == nil {
		if _, err := os.Stat(filepath.Join(home, "Library/LaunchAgents", launchAgent+".plist")); err == nil {
			return exec.Command("launchctl", "kickstart", fmt.Sprintf("gui/%d/%s", os.Getuid(), launchAgent)).Run()
		}
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	return exec.Command(self, "start").Run()
}

// spawnDaemon starts the daemon in the background, logging to the log file.
func spawnDaemon() error {
	path := config.LogFile()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if info, err := os.Stat(path); err == nil && info.Size() > logLimit {
		os.Rename(path, path+".1")
	}
	logFile, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer logFile.Close()
	self, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(self, "daemon")
	cmd.Stdout, cmd.Stderr = logFile, logFile
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// get asks the daemon something.
func get(path string, v any) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://lets-build-clauder"+path, nil)
	resp, err := daemonClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New(resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// post sends the daemon a JSON body, and returns the response unread.
func post(ctx context.Context, path string, body any) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://lets-build-clauder"+path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	return daemonClient.Do(req)
}

// errorOf reads the message from an error response.
func errorOf(resp *http.Response) string {
	var body struct {
		Message string `json:"message"`
	}
	data, _ := io.ReadAll(resp.Body)
	if json.Unmarshal(data, &body) == nil && body.Message != "" {
		return body.Message
	}
	return resp.Status
}
