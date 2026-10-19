//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

const noctty = syscall.O_NOCTTY

// detach starts a command in a session of its own, so it outlives this one.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
