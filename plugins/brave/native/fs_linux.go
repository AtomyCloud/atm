//go:build linux

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

func ownedByCurrentUser(i os.FileInfo) bool {
	s, ok := i.Sys().(*syscall.Stat_t)
	return ok && s.Uid == uint32(os.Geteuid())
}
func rootOwned(i os.FileInfo) bool { s, ok := i.Sys().(*syscall.Stat_t); return ok && s.Uid == 0 }
func openNoFollow(r *os.Root, n string) (*os.File, error) {
	return r.OpenFile(n, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
}
func renameNoReplace(a, b string) error {
	return unix.Renameat2(unix.AT_FDCWD, a, unix.AT_FDCWD, b, unix.RENAME_NOREPLACE)
}
func systemCommand(name string) (string, error) {
	switch name {
	case "apt-get", "apt-cache", "dnf", "sudo", "mktemp", "install", "rm", "sha256sum":
	default:
		return "", fmt.Errorf("unknown system command")
	}
	for _, dir := range []string{"/usr/bin", "/usr/sbin", "/bin", "/sbin"} {
		p := filepath.Join(dir, name)
		if _, e := os.Lstat(p); os.IsNotExist(e) {
			continue
		}
		resolved, e := filepath.EvalSymlinks(p)
		if e != nil {
			return "", e
		}
		if e = trustedSystemFile(resolved, true); e != nil {
			return "", e
		}
		return resolved, nil
	}
	return "", fmt.Errorf("trusted %s unavailable", name)
}
func trustedSystemFile(path string, executable bool) error {
	if e := absolutePath(path); e != nil {
		return e
	}
	for p := path; ; p = filepath.Dir(p) {
		i, e := os.Lstat(p)
		if e != nil {
			return e
		}
		if !rootOwned(i) || i.Mode().Perm()&0022 != 0 || i.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("untrusted system file/ancestor: %s", p)
		}
		if p == path {
			if !i.Mode().IsRegular() || (executable && i.Mode().Perm()&0111 == 0) {
				return fmt.Errorf("regular trusted system file required")
			}
		} else if !i.IsDir() {
			return fmt.Errorf("invalid ancestor")
		}
		if p == "/" {
			return nil
		}
	}
}
