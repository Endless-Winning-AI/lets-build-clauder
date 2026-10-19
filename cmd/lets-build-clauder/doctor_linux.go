package main

import (
	"strings"

	"github.com/Endless-Winning-AI/lets-build-clauder/internal/config"
	"github.com/Endless-Winning-AI/lets-build-clauder/internal/desktop"
)

const udevRule = `KERNEL=="hidraw*", ATTRS{idVendor}=="303a", MODE="0660", TAG+="uaccess"
KERNEL=="hidraw*", KERNELS=="0005:303A:*", MODE="0660", TAG+="uaccess"`

func accessFix() string {
	lines := []string{
		"Install the udev rule, then reconnect the pad:",
		"  sudo tee /etc/udev/rules.d/70-lets-build-clauder.rules <<'EOF'",
	}
	for _, rule := range strings.Split(udevRule, "\n") {
		lines = append(lines, "  "+rule)
	}
	lines = append(lines, "  EOF", "  sudo udevadm control --reload && sudo udevadm trigger")
	return strings.Join(lines, "\n")
}

func desktopChecks(c *checker, cfg config.Config) {
	if desktop.Detect(config.RuntimeDir()).Name() != "" {
		c.check(true, "Hyprland: agent keys switch windows; approve and deny answer prompts", "")
	} else {
		c.check(false, "desktop not supported",
			"Lights and Claude Code actions work everywhere. Switching windows, approve\n"+
				"and deny need Hyprland, or a session inside tmux, WezTerm or kitty.")
	}
	if len(holds(cfg)) > 0 {
		c.check(false, "hold is not supported on Linux yet",
			"Bind your dictation app's toggle to a run action instead.")
	}
}
