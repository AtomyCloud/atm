//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func procRead(p string, limit int64) ([]byte, error) {
	f, e := os.Open(p)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	v, e := io.ReadAll(io.LimitReader(f, limit+1))
	if int64(len(v)) > limit {
		return nil, fmt.Errorf("process metadata limit")
	}
	return v, e
}
func browserClosed(ctx context.Context, profile string) error {
	return checkBrowser(ctx, profile, "/proc")
}
func checkBrowser(ctx context.Context, profile, proc string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := realDirs(filepath.Dir(profile), false, false); err != nil {
		return err
	}
	if i, e := os.Lstat(profile); e == nil {
		if !i.IsDir() || i.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("profile is not a real directory")
		}
		r, e := openDir(profile)
		if e != nil {
			return e
		}
		defer r.Close()
		for _, name := range []string{"SingletonLock", "SingletonSocket", "SingletonCookie"} {
			if _, e = r.Lstat(name); !os.IsNotExist(e) {
				return fmt.Errorf("close Brave first; ambiguous/present %s must be inspected manually", name)
			}
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	mounts, e := procRead(filepath.Join(proc, "mounts"), 1<<20)
	if e != nil {
		return fmt.Errorf("cannot establish process visibility: %w", e)
	}
	visible := false
	for _, line := range strings.Split(string(mounts), "\n") {
		f := strings.Fields(line)
		if len(f) < 4 || f[2] != "proc" {
			continue
		}
		if f[1] != "/proc" {
			continue
		}
		visible = true
		for _, opt := range strings.Split(f[3], ",") {
			if strings.HasPrefix(opt, "hidepid=") && opt != "hidepid=0" {
				return fmt.Errorf("restricted proc visibility; close-state unknown")
			}
		}
	}
	if !visible {
		return fmt.Errorf("process visibility unknown")
	}
	dir, e := os.Open(proc)
	if e != nil {
		return e
	}
	defer dir.Close()
	entries, e := dir.ReadDir(65537)
	if e != nil && e != io.EOF {
		return e
	}
	if len(entries) > 65536 {
		return fmt.Errorf("process count limit")
	}
	for _, entry := range entries {
		if _, e = strconv.Atoi(entry.Name()); e != nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		p := filepath.Join(proc, entry.Name())
		i, e := os.Stat(p)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return e
		}
		if !ownedByCurrentUser(i) {
			continue
		}
		cmd, e := procRead(filepath.Join(p, "cmdline"), 128<<10)
		if e != nil {
			if _, gone := os.Stat(p); os.IsNotExist(gone) {
				continue
			}
			return fmt.Errorf("cannot inspect owned process: %w", e)
		}
		if len(cmd) == 0 {
			continue
		} // Kernel threads and zombies have no user-space browser state.
		comm, ce := procRead(filepath.Join(p, "comm"), 16)
		exe, xe := os.Readlink(filepath.Join(p, "exe"))
		if ce != nil || xe != nil {
			if _, gone := os.Stat(p); os.IsNotExist(gone) {
				continue
			}
		}
		if ce != nil {
			return fmt.Errorf("cannot inspect owned process name: %w", ce)
		}
		if e = inspectProcess(cmd, comm, exe, xe, profile); e != nil {
			return fmt.Errorf("owned process %s: %w", entry.Name(), e)
		}
	}
	return nil
}
func inspectProcess(cmd, comm []byte, exe string, exeErr error, profile string) error {
	if len(cmd) == 0 || len(cmd) > 128<<10 || cmd[len(cmd)-1] != 0 {
		return fmt.Errorf("malformed process command line")
	}
	argv := strings.Split(string(cmd[:len(cmd)-1]), "\x00")
	if argv[0] == "" {
		return fmt.Errorf("empty process identity")
	}
	name := strings.TrimSuffix(string(comm), "\n")
	if len(name) == 0 || len(name) > 15 || strings.ContainsAny(name, "\x00\r\n") {
		return fmt.Errorf("invalid process comm")
	}
	isBrave := func(s string) bool {
		s = strings.ToLower(s)
		n := filepath.Base(strings.TrimSuffix(s, " (deleted)"))
		return n == "brave" || strings.HasPrefix(n, "brave-browser") || strings.Contains(s, "/brave.com/") || strings.Contains(n, "brave_crashpad")
	}
	if isBrave(exe) || isBrave(argv[0]) || isBrave(name) {
		return fmt.Errorf("close Brave first")
	}
	for j, a := range argv {
		if strings.HasPrefix(a, "--user-data-dir=") {
			v := strings.TrimPrefix(a, "--user-data-dir=")
			if !filepath.IsAbs(v) || filepath.Clean(v) == profile {
				return fmt.Errorf("profile process or ambiguous relative user-data-dir")
			}
		}
		if a == "--user-data-dir" && (j+1 >= len(argv) || !filepath.IsAbs(argv[j+1]) || filepath.Clean(argv[j+1]) == profile) {
			return fmt.Errorf("profile process or ambiguous user-data-dir")
		}
	}
	if exeErr == nil {
		return nil
	}
	if !errors.Is(exeErr, syscall.EACCES) && !errors.Is(exeErr, syscall.EPERM) {
		return fmt.Errorf("cannot resolve owned process executable: %w", exeErr)
	}
	// Nondumpable system credential agents often deny /proc/exe to their own UID.
	// This narrow fallback is not a defense against hostile same-user identity spoofing.
	if (name == "ssh-agent" || name == "gpg-agent") && filepath.Base(argv[0]) == name && (argv[0] == name || argv[0] == "/usr/bin/"+name) {
		return nil
	}
	return fmt.Errorf("cannot resolve owned process executable; unknown identity: %w", exeErr)
}
