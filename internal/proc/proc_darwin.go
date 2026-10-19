package proc

import (
	"golang.org/x/sys/unix"

	"github.com/Endless-Winning-AI/lets-build-clauder/internal/macos"
)

func parent(pid int) int {
	info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return 0
	}
	return int(info.Eproc.Ppid)
}

// CWD is the working directory of a process, or "".
func CWD(pid int) string {
	return macos.ProcessCWD(pid)
}

// Exited is closed when pid exits.
func Exited(pid int) (<-chan struct{}, error) {
	kq, err := unix.Kqueue()
	if err != nil {
		return nil, err
	}
	watch := []unix.Kevent_t{{Ident: uint64(pid), Filter: unix.EVFILT_PROC, Flags: unix.EV_ADD, Fflags: unix.NOTE_EXIT}}
	if _, err := unix.Kevent(kq, watch, nil, nil); err != nil {
		unix.Close(kq)
		return nil, err
	}
	done := make(chan struct{})
	go func() {
		defer unix.Close(kq)
		events := make([]unix.Kevent_t, 1)
		for {
			if _, err := unix.Kevent(kq, nil, events, nil); err != unix.EINTR {
				break
			}
		}
		close(done)
	}()
	return done, nil
}
