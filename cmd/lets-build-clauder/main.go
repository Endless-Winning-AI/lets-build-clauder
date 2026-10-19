// Command lets-build-clauder connects a Codex Micro, or a Work Louder
// Creator Micro 2, to Claude Code.
//
// One daemon owns the pad. Each Claude Code session runs `attach`, which
// holds the session's place with the daemon for as long as the session lives.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/Endless-Winning-AI/lets-build-clauder/internal/config"
	"github.com/Endless-Winning-AI/lets-build-clauder/internal/daemon"
	"github.com/Endless-Winning-AI/lets-build-clauder/internal/desktop"
)

const version = "0.7.0"

const usage = `usage: lets-build-clauder <command>

  attach             hold the calling session's place with the daemon, and
                     print what the daemon tells it, one JSON object per line
  doctor             check the setup and explain any problem
  status             print the daemon's state as JSON
  launch DIR CMD...  open CMD in a new terminal window in DIR
  daemon             run the daemon in the foreground
  start              start the daemon in the background
  version            print the version

attach starts the daemon when it is not running. Controls are configured in
` + "~/.config/lets-build-clauder/config.toml.\n"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch cmd, args := os.Args[1], os.Args[2:]; {
	case cmd == "attach":
		err = attach()
	case cmd == "doctor":
		if !doctor(os.Stdout) {
			os.Exit(1)
		}
	case cmd == "status":
		var st daemon.Status
		if err = get("/status", &st); err == nil {
			out, _ := json.MarshalIndent(st, "", "  ")
			fmt.Println(string(out))
		}
	case cmd == "launch" && len(args) >= 2:
		err = launch(args[0], args[1:])
	case cmd == "daemon":
		err = runDaemon()
	case cmd == "start":
		err = spawnDaemon()
	case cmd == "version":
		fmt.Printf("lets-build-clauder %s (%s/%s)\n", version, runtime.GOOS, runtime.GOARCH)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "lets-build-clauder:", err)
		os.Exit(1)
	}
}

func runDaemon() error {
	logger := log.New(os.Stdout, "", log.Ltime)
	ctx, cancel := context.WithCancel(context.Background())
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	go func() {
		logger.Println("stopping on", <-signals)
		cancel()
	}()
	return daemon.New(version, config.Socket(), desktop.Detect(config.RuntimeDir()), logger).Run(ctx)
}
