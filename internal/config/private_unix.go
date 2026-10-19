//go:build !windows

package config

import (
	"fmt"
	"os"
	"syscall"
)

// CheckPrivateDir checks that dir is a directory this user owns and no one
// else can use.
func CheckPrivateDir(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || !ok || int(st.Uid) != os.Getuid() || info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s must be a directory only you can use", dir)
	}
	return nil
}
