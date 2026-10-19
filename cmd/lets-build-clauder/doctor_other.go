//go:build !linux && !darwin

package main

import "github.com/Endless-Winning-AI/lets-build-clauder/internal/config"

func accessFix() string { return "" }

func desktopChecks(c *checker, _ config.Config) {
	c.check(false, "this platform is not supported yet", "")
}
