package main

import (
	"os"
	"strings"

	"github.com/Endless-Winning-AI/lets-build-clauder/internal/config"
	"github.com/Endless-Winning-AI/lets-build-clauder/internal/daemon"
	"github.com/Endless-Winning-AI/lets-build-clauder/internal/desktop"
	"github.com/Endless-Winning-AI/lets-build-clauder/internal/proc"
)

func accessFix() string { return "" }

func desktopChecks(c *checker, cfg config.Config) {
	app := desktop.HostApp(os.Getppid())
	if app == "" {
		// The Claude desktop app is no terminal, so HostApp doesn't know it.
		for _, p := range proc.Ancestors(os.Getppid()) {
			if strings.Contains(proc.Command(p), "Claude.app/Contents/MacOS/Claude") {
				app = "Claude"
				break
			}
		}
	}
	switch app {
	case "Claude":
		c.check(true, "Claude desktop app: agent keys bring Claude forward; deny stops the turn; approve must be clicked", "")
	case "Ghostty":
		c.check(true, "Ghostty: agent keys switch terminals; approve presses Enter, deny presses Escape", "")
	case "iTerm2":
		c.check(true, "iTerm2: agent keys switch sessions; approve and deny answer prompts", "")
	case "Terminal":
		c.check(true, "Terminal: agent keys switch tabs; approve and deny need Ghostty, iTerm2 or tmux", "")
	default:
		c.check(false, "terminal app not recognized",
			"Agent keys, approve and deny work in Ghostty, iTerm2, Terminal, or inside tmux.")
	}
	if held := holds(cfg); len(held) > 0 {
		// The daemon posts the keys, so the daemon program is what needs the
		// permission; ask it. Unreachable, this program stands in for it.
		trusted := desktop.Trusted()
		var st daemon.Status
		if get("/status", &st) == nil {
			trusted = st.CanHold
		}
		c.check(trusted, "holding "+strings.Join(held, ", ")+" for dictation",
			"Allow the lets-build-clauder daemon program under System Settings > Privacy & Security > Accessibility,\n"+
				"then restart the daemon (launchctl kickstart -k gui/$(id -u)/"+launchAgent+").")
	}
}
