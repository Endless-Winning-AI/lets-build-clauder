package daemon

import (
	"time"

	"github.com/Endless-Winning-AI/lets-build-clauder/internal/keys"
	"github.com/Endless-Winning-AI/lets-build-clauder/internal/terminals"
)

// windowOf finds where a session's keys go, and the desktop window around
// it. The first call looks; later ones wait for its answer.
func (d *Daemon) windowOf(s *Session) (window, gui string) {
	s.locate.Do(func() {
		window := terminals.Identify(s.env)
		var gui string
		if app := terminals.DesktopApp(s.env); app != "" {
			// A Claude desktop app session: no terminal to find; the app is
			// the window, and the desktop brings it forward.
			window, gui = app, app
		} else {
			gui = d.desktop.Locate(s.pid, s.cwd)
		}
		if gui == "" && window == "" {
			gui = d.findByTitle(s)
		}
		if window == "" {
			window = gui
		}
		d.mu.Lock()
		s.window, s.gui = window, gui
		d.mu.Unlock()
		close(s.located)
		if window != "" {
			d.log.Println("session", s.pid, "is in", window)
		}
	})
	<-s.located
	d.mu.Lock()
	defer d.mu.Unlock()
	return s.window, s.gui
}

// findByTitle tells apart terminals that look alike: the session titles its
// own with a one-off marker, the desktop finds the window showing it, and
// the session puts its title back.
func (d *Daemon) findByTitle(s *Session) string {
	marker := "lets-build-clauder " + newID()
	d.mu.Lock()
	d.send(s, map[string]string{"type": "title", "text": marker})
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		d.send(s, map[string]string{"type": "title", "text": ""})
		d.mu.Unlock()
	}()
	for range 10 {
		time.Sleep(200 * time.Millisecond)
		if window := d.desktop.Titled(marker); window != "" {
			return window
		}
	}
	return ""
}

// focus brings a session forward: its pane inside its terminal, then the
// terminal's window on the desktop.
func (d *Daemon) focus(s *Session) {
	window, gui := d.windowOf(s)
	switch {
	case window == "":
	case terminals.Handles(window):
		client := terminals.Focus(window)
		if client != 0 {
			if found := d.desktop.Locate(client, s.cwd); found != "" {
				gui = found
			}
		}
		if gui != "" {
			d.desktop.Focus(gui)
		} else if client != 0 {
			d.desktop.Raise(client)
		} else {
			d.desktop.Raise(s.pid)
		}
	default:
		d.desktop.Focus(window)
	}
}

// press types a key into a session's terminal.
func (d *Daemon) press(s *Session, c keys.Chord) bool {
	window, _ := d.windowOf(s)
	switch {
	case window == "":
		return false
	case terminals.Handles(window):
		return terminals.Press(window, c)
	}
	return d.desktop.Press(window, c)
}

// approve answers a session's permission prompt with Yes.
func (d *Daemon) approve(s *Session) bool {
	window, _ := d.windowOf(s)
	switch {
	case window == "":
		return false
	case terminals.Handles(window):
		return terminals.Approve(window)
	}
	return d.desktop.Approve(window)
}

// onFocus hears which window has focus.
func (d *Daemon) onFocus(window string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.focused = nil
	if window == "" {
		return
	}
	for _, s := range d.sessions {
		if s.gui == window || s.window == window {
			d.focused, d.last = s, s
			if s.state == Unread || s.state == Error {
				// Looking at a finished answer reads it.
				d.setState(s, Idle, true)
			}
			return
		}
	}
}
