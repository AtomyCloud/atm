//go:build linux

package main

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

var codexConfigDumpArgs = []string{"-o", "APT::Config::Dump::Format=%F=%V%n", "-o", "APT::Config::Dump::EmptyValue=true", "dump"}

func codexSystemEnvironment() []string {
	return []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "DEBIAN_FRONTEND=noninteractive", "TERM=dumb"}
}

func checkCodexSystemPolicy(ctx context.Context, t *codexDesktopTools) error {
	if err := checkSystemPolicy(ctx, t.commands); err != nil {
		return err
	}
	ex, err := t.commands.resolve("apt-config")
	if err != nil {
		return err
	}
	out, err := t.commands.run(ctx, ex, codexConfigDumpArgs)
	if err != nil {
		return err
	}
	dpkg, err := t.commands.resolve("dpkg")
	if err != nil {
		return err
	}
	if err := validateCodexAPTConfig(out, dpkg); err != nil {
		return err
	}
	if t.configFiles == nil {
		return fmt.Errorf("dpkg configuration inspection unavailable")
	}
	return t.configFiles()
}

// apt-config's percent-escaped key/value format is parsed as data, never shell.
// Reject binary-specific overrides rather than attempting to emulate APT merging.
func validateCodexAPTConfig(data, dpkg string) error {
	if len(data) > 64<<10 {
		return fmt.Errorf("APT configuration exceeds bound")
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSuffix(data, "\n"), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("malformed APT configuration dump")
		}
		key, err := url.PathUnescape(key)
		if err != nil {
			return err
		}
		value, err = url.PathUnescape(value)
		if err != nil {
			return err
		}
		key = strings.ToLower(key)
		if strings.ContainsAny(key, "\x00\r\n=\t ") {
			return fmt.Errorf("unsafe APT configuration key")
		}
		if seen[key] && !strings.HasSuffix(key, "::") {
			return fmt.Errorf("duplicate APT configuration key")
		}
		seen[key] = true
		bad := false
		switch key {
		case "dir":
			bad = value != "/"
		case "rootdir":
			bad = value != ""
		case "dir::state":
			bad = value != "var/lib/apt" && value != "var/lib/apt/" && value != "/var/lib/apt" && value != "/var/lib/apt/"
		case "dir::state::status":
			bad = value != "/var/lib/dpkg/status"
		case "dir::bin::dpkg":
			bad = value != dpkg && value != "/usr/bin/dpkg"
		case "dpkg::chroot-directory":
			bad = value != "" && value != "/"
		case "apt::system", "dpkg::path":
			bad = value != ""
		}
		for _, prefix := range []string{"binary::apt-get", "dpkg::options", "dpkg::run-directory", "dpkg::pre-invoke"} {
			if key == prefix || strings.HasPrefix(key, prefix+"::") {
				bad = bad || value != ""
			}
		}
		if strings.HasPrefix(key, "debug::") && value != "" && value != "false" && value != "0" && value != "no" {
			bad = true
		}
		if bad {
			return fmt.Errorf("incompatible APT configuration %s; fixed dpkg database and locking required", key)
		}
	}
	if !seen["dir"] || !seen["dir::state"] {
		return fmt.Errorf("incomplete APT configuration evidence")
	}
	return nil
}

func checkCodexDpkgConfig() error {
	paths := []string{"/etc/dpkg/dpkg.cfg"}
	const parts = "/etc/dpkg/dpkg.cfg.d"
	if i, err := os.Lstat(parts); err == nil {
		if !i.IsDir() || owner(i) != 0 || i.Mode().Perm()&0022 != 0 {
			return fmt.Errorf("unsafe dpkg configuration directory")
		}
		f, err := os.OpenFile(parts, os.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
		if err != nil {
			return err
		}
		es, readErr := f.ReadDir(129)
		f.Close()
		if readErr != nil && readErr != io.EOF || len(es) > 128 {
			return fmt.Errorf("dpkg configuration exceeds bound")
		}
		for _, e := range es {
			paths = append(paths, filepath.Join(parts, e.Name()))
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	// HOME=/ is fixed in the trusted command runner, so dpkg may also read this.
	paths = append(paths, "/.dpkg.cfg")
	for _, p := range paths {
		if _, err := os.Lstat(p); os.IsNotExist(err) {
			continue
		}
		if err := trustedRootFile(p); err != nil {
			return err
		}
		f, err := os.OpenFile(p, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
		if err != nil {
			return err
		}
		data, readErr := io.ReadAll(io.LimitReader(f, (1<<20)+1))
		f.Close()
		if readErr != nil || len(data) > 1<<20 {
			return fmt.Errorf("cannot inspect bounded dpkg configuration")
		}
		if err := validateCodexDpkgConfig(string(data)); err != nil {
			return err
		}
	}
	return nil
}

func validateCodexDpkgConfig(data string) error {
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key := strings.TrimLeft(strings.Fields(line)[0], "-")
		key, _, _ = strings.Cut(key, "=")
		// A narrow allowlist avoids config-file indirection and future root options.
		switch key {
		case "no-debsig", "log", "no-pager":
		default:
			return fmt.Errorf("custom dpkg option %s unsupported in initial-only preview", key)
		}
	}
	return nil
}
