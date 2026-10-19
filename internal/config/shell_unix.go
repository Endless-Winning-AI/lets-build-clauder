//go:build !windows

package config

func shell(command string) []string { return []string{"/bin/sh", "-c", command} }
