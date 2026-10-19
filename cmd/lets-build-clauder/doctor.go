package main

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"github.com/Endless-Winning-AI/lets-build-clauder/internal/config"
	"github.com/Endless-Winning-AI/lets-build-clauder/internal/daemon"
	"github.com/Endless-Winning-AI/lets-build-clauder/internal/pad"
)

// checker collects a report of checks, each passed or failed with a fix.
type checker struct {
	lines []string
	ok    bool
}

func (c *checker) check(passed bool, text, fix string) {
	c.ok = c.ok && passed
	mark := "✓"
	if !passed {
		mark = "✗"
	}
	c.lines = append(c.lines, mark+" "+text)
	if !passed && fix != "" {
		for _, line := range strings.Split(fix, "\n") {
			c.lines = append(c.lines, "    "+line)
		}
	}
}

func (c *checker) note(text string) {
	c.lines = append(c.lines, "  "+text)
}

// doctor checks each part of the setup, writes a report, and says whether
// all is well.
func doctor(w io.Writer) bool {
	c := &checker{ok: true}
	c.note(fmt.Sprintf("lets-build-clauder %s (%s/%s)", version, runtime.GOOS, runtime.GOARCH))

	if found := pad.Find(); len(found) > 0 {
		f := found[0]
		problem := pad.AccessProblem(f)
		text := f.Model + " over " + f.Transport
		if problem != "" {
			text += ": " + problem
		}
		c.check(problem == "", text, accessFix())
	} else {
		c.check(false, "pad not found",
			"Pair it over Bluetooth, or connect USB-C and tap the touch control until\n"+
				"the light under the pad is white. Press any key to wake it.")
	}

	var st daemon.Status
	if err := get("/status", &st); err != nil {
		c.check(false, "daemon: "+err.Error(), "See "+config.LogFile()+".")
	} else {
		d := st.Device
		switch {
		case d.Connected:
			text := fmt.Sprintf("daemon %s, firmware %s", st.Version, d.Firmware)
			if d.Battery != nil {
				text += fmt.Sprintf(", battery %d%%", *d.Battery)
			}
			c.check(true, text, "")
		case d.Problem != "":
			c.check(false, "daemon "+st.Version+" has not reached the pad", d.Problem)
		default:
			c.check(false, "daemon "+st.Version+" has not reached the pad", "See "+config.LogFile()+".")
		}
		var keys []string
		for _, s := range st.Sessions {
			if s.Key != nil {
				keys = append(keys, fmt.Sprintf("key %d %s", *s.Key, s.State))
			}
		}
		if len(keys) == 0 {
			keys = []string{"none"}
		}
		c.note("sessions: " + strings.Join(keys, ", "))
	}

	cfg, problems := config.Load(config.File())
	desktopChecks(c, cfg)

	switch _, err := os.Stat(config.File()); {
	case len(problems) > 0:
		c.check(false, "config "+config.File(), strings.Join(problems, "\n"))
	case err == nil:
		c.check(true, "config "+config.File(), "")
	default:
		c.note("config: defaults (no config.toml)")
	}

	fmt.Fprintln(w, strings.Join(c.lines, "\n"))
	return c.ok
}

// holds lists the keys config.toml holds for dictation apps.
func holds(cfg config.Config) []string {
	var out []string
	for _, action := range cfg.Controls["keys"] {
		if action.Hold != "" {
			out = append(out, action.Hold)
		}
	}
	return out
}
