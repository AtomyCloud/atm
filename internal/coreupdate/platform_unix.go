//go:build linux || darwin

package coreupdate

import (
	"fmt"
	"os"
	"syscall"
)

// Same nonblocking OS-lock primitive as internal/install; locks die with process.
func lockFile(f *os.File) error { return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) }
func singleLink(_ *os.File, info os.FileInfo) error {
	s, ok := info.Sys().(*syscall.Stat_t)
	if !ok || s.Nlink != 1 {
		return fmt.Errorf("hard-linked file refused")
	}
	return nil
}
