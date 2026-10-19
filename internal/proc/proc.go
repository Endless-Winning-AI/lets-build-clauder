// Package proc finds out about processes: their parents, their terminal
// device, and when they exit.
package proc

import (
	"os/exec"
	"strconv"
	"strings"
)

// Ancestors lists pid and each of its parents, nearest first.
func Ancestors(pid int) []int {
	var out []int
	seen := map[int]bool{}
	for pid > 1 && !seen[pid] {
		seen[pid] = true
		out = append(out, pid)
		pid = parent(pid)
	}
	return out
}

// Command is the path of the program a process runs, or "".
func Command(pid int) string {
	return ps(pid, "comm=")
}

// TTY is the terminal device a process runs on, such as /dev/ttys003, or "".
func TTY(pid int) string {
	tty := ps(pid, "tty=")
	switch {
	case tty == "" || tty == "?" || tty == "??":
		return ""
	case strings.HasPrefix(tty, "/dev/"):
		return tty
	}
	return "/dev/" + tty
}

func ps(pid int, field string) string {
	out, err := exec.Command("ps", "-o", field, "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
