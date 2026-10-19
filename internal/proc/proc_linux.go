package proc

import (
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func parent(pid int) int {
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0
	}
	// The command name may hold spaces or parentheses: skip past its last ")".
	fields := strings.Fields(string(stat[strings.LastIndexByte(string(stat), ')')+1:]))
	if len(fields) < 2 {
		return 0
	}
	ppid, _ := strconv.Atoi(fields[1])
	return ppid
}

// CWD is the working directory of a process, or "".
func CWD(pid int) string {
	dir, _ := os.Readlink("/proc/" + strconv.Itoa(pid) + "/cwd")
	return dir
}

// Exited is closed when pid exits.
func Exited(pid int) (<-chan struct{}, error) {
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		return nil, err
	}
	done := make(chan struct{})
	go func() {
		defer unix.Close(fd)
		fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		for {
			if _, err := unix.Poll(fds, -1); err != unix.EINTR {
				break
			}
		}
		close(done)
	}()
	return done, nil
}
