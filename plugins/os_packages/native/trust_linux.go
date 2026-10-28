//go:build linux

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func systemCommand(name string) (string, error) {
	switch name {
	case "apt-get", "dnf", "yum", "pacman", "apk", "sudo":
	default:
		return "", fmt.Errorf("unknown system executable")
	}
	for _, dir := range []string{"/usr/bin", "/usr/sbin", "/bin", "/sbin"} {
		p := filepath.Join(dir, name)
		if _, err := os.Lstat(p); os.IsNotExist(err) {
			continue
		}
		resolved, err := filepath.EvalSymlinks(p)
		if err != nil {
			return "", err
		}
		if err = trustedSystemFile(resolved); err != nil {
			return "", err
		}
		return resolved, nil
	}
	return "", fmt.Errorf("%s unavailable in trusted system directories", name)
}
func trustedSystemFile(path string) error {
	if err := absolutePath(path); err != nil {
		return err
	}
	for p := path; ; p = filepath.Dir(p) {
		i, err := os.Lstat(p)
		if err != nil {
			return err
		}
		st, ok := i.Sys().(*syscall.Stat_t)
		if !ok || st.Uid != 0 || i.Mode().Perm()&0022 != 0 || i.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("untrusted system executable/ancestor: %s", p)
		}
		if p == path {
			if !i.Mode().IsRegular() || i.Mode().Perm()&0111 == 0 {
				return fmt.Errorf("system executable is not a regular executable")
			}
		} else if !i.IsDir() {
			return fmt.Errorf("non-directory executable ancestor")
		}
		if p == "/" {
			break
		}
	}
	return nil
}

func ownedByCurrentUser(info os.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && st.Uid == uint32(os.Geteuid())
}

// Shared sticky root directories (such as /tmp) may contain a private journal.
// Every other ancestor must prevent replacement by a different user.
func journalDirectory(path string, create bool) error {
	if err := absolutePath(path); err != nil {
		return err
	}
	current := "/"
	for _, part := range strings.Split(path, "/") {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		i, err := os.Lstat(current)
		if os.IsNotExist(err) && create {
			if err = os.Mkdir(current, 0700); err != nil {
				return err
			}
			i, err = os.Lstat(current)
		}
		if err != nil {
			return err
		}
		st, ok := i.Sys().(*syscall.Stat_t)
		if !ok || !i.IsDir() || i.Mode()&os.ModeSymlink != 0 || (st.Uid != 0 && !ownedByCurrentUser(i)) {
			return fmt.Errorf("untrusted journal ancestor: %s", current)
		}
		if i.Mode().Perm()&0022 != 0 && !(st.Uid == 0 && i.Mode()&os.ModeSticky != 0 && current != path) {
			return fmt.Errorf("writable journal ancestor refused: %s", current)
		}
		if current == path && (!ownedByCurrentUser(i) || i.Mode().Perm() != 0700) {
			return fmt.Errorf("journal directory must be current-user owned and private mode 0700")
		}
	}
	return nil
}
