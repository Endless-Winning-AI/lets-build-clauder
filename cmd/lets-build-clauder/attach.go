package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/Endless-Winning-AI/lets-build-clauder/internal/daemon"
	"github.com/Endless-Winning-AI/lets-build-clauder/internal/proc"
	"github.com/Endless-Winning-AI/lets-build-clauder/internal/terminals"
)

// attach holds the place of the Claude Code session that started it, its
// parent, until the session exits. It passes on what the daemon says, but
// handles titles itself: Claude Code's children have no terminal of their
// own to title.
func attach() error {
	owner := os.Getppid()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		watchExit(owner)
		cancel()
	}()

	hello := daemon.Hello{PID: owner, Env: map[string]string{}, Version: version, Protocol: daemon.Protocol}
	// Claude Code's own directory, where its terminal opened, rather than
	// wherever it ran this.
	if hello.CWD = proc.CWD(owner); hello.CWD == "" {
		hello.CWD, _ = os.Getwd()
	}
	for _, k := range terminals.EnvKeys {
		if v, ok := os.LookupEnv(k); ok {
			hello.Env[k] = v
		}
	}
	out := json.NewEncoder(os.Stdout)

	for ctx.Err() == nil {
		err := hold(ctx, &hello)
		var refused refusal
		switch {
		case ctx.Err() != nil:
		case errors.As(err, &refused):
			return refused
		case err != nil:
			out.Encode(map[string]string{"type": "error", "message": err.Error()})
			sleep(ctx, 5*time.Second)
		default:
			// The daemon restarted: attach again, to a new one if need be.
			sleep(ctx, 300*time.Millisecond)
		}
	}
	return nil
}

type refusal string

func (r refusal) Error() string { return string(r) }

// hold attaches once, and relays until the daemon goes away.
func hold(ctx context.Context, hello *daemon.Hello) error {
	resp, err := post(ctx, "/attach", hello)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusServiceUnavailable:
		// An older daemon is making way: start this one's before an older
		// session starts its own.
		time.Sleep(250 * time.Millisecond)
		return startDaemon()
	default:
		return refusal(errorOf(resp))
	}

	lines := bufio.NewScanner(resp.Body)
	for lines.Scan() {
		var msg struct {
			Type string `json:"type"`
			Text string `json:"text"`
			Slot *int   `json:"slot"`
		}
		if json.Unmarshal(lines.Bytes(), &msg) != nil {
			continue
		}
		switch msg.Type {
		case "title":
			setTitle(hello.PID, msg.Text)
			continue
		case "status":
			// Asked for again on attaching, so a restarted daemon puts the
			// session back on its key.
			hello.Slot = msg.Slot
		}
		if _, err := fmt.Printf("%s\n", lines.Bytes()); err != nil {
			os.Exit(0) // Claude Code stopped listening
		}
	}
	return nil
}

// watchExit returns once pid has exited.
func watchExit(pid int) {
	if exited, err := proc.Exited(pid); err == nil {
		<-exited
		return
	}
	for os.Getppid() == pid {
		time.Sleep(time.Second)
	}
}

// setTitle titles the terminal pid runs in, with an OSC 2 sequence written
// to its terminal device. An empty title lets the terminal's own show again.
func setTitle(pid int, text string) {
	tty := proc.TTY(pid)
	if tty == "" {
		return
	}
	f, err := os.OpenFile(tty, os.O_WRONLY|noctty, 0)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "\x1b]2;%s\x07", text)
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}
