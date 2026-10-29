//go:build linux

package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const hermesSystem = "hermes-desktop-system"

// These seams are assigned only by Go fixtures, never by runtime environment.
type systemTools struct {
	resolve   func(string) (string, error)
	run       func(context.Context, string, []string) (string, error)
	quietRun  func(context.Context, string, []string) (string, error)
	uid       func() int
	stage     func(string) error
	installed func() (string, string, error)
	policy    func() error
}

func defaultSystemTools() *systemTools {
	return &systemTools{resolve: systemCommand, run: runSystemCommand, quietRun: runQuietSystemCommand, uid: os.Geteuid, stage: checkSystemStage, installed: installedHermes, policy: checkAPTPreferences}
}

func systemCommand(name string) (string, error) {
	switch name {
	case "apt-get", "apt-config", "dpkg", "dpkg-deb", "rpm", "dnf5", "cat", "stat", "ls", "env", "sudo", "mktemp", "install", "sha256sum", "rm", "rmdir":
	default:
		return "", fmt.Errorf("unsupported system tool")
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
		if err = trustedRootFile(resolved); err != nil {
			return "", err
		}
		i, err := os.Lstat(resolved)
		if err != nil || i.Mode().Perm()&0111 == 0 {
			return "", fmt.Errorf("nonexecutable system tool")
		}
		return resolved, nil
	}
	return "", fmt.Errorf("trusted %s unavailable; install prerequisites separately", name)
}

func trustedRootFile(path string) error {
	if err := absolutePath(path); err != nil {
		return err
	}
	for p := path; ; p = filepath.Dir(p) {
		i, err := os.Lstat(p)
		if err != nil {
			return err
		}
		if owner(i) != 0 || i.Mode().Perm()&0022 != 0 || i.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("untrusted system path: %s", p)
		}
		if p == path && !i.Mode().IsRegular() || p != path && !i.IsDir() {
			return fmt.Errorf("unexpected system path type")
		}
		if p == "/" {
			return nil
		}
	}
}

var rootStageRE = regexp.MustCompile(`^/run/atm-ai-hermes-[A-Za-z0-9]{10}$`)

func checkSystemStage(path string) error {
	return checkRootStage(path, rootStageRE)
}

func checkRootStage(path string, pattern *regexp.Regexp) error {
	if !pattern.MatchString(path) {
		return fmt.Errorf("unexpected root staging path")
	}
	for p := path; ; p = filepath.Dir(p) {
		i, err := os.Lstat(p)
		if err != nil {
			return err
		}
		if !i.IsDir() || owner(i) != 0 || i.Mode().Perm()&0022 != 0 || p == path && i.Mode().Perm() != 0700 {
			return fmt.Errorf("root-owned private staging required")
		}
		if p == "/" {
			return nil
		}
	}
}

// Keep bounded command output; exceeding the bound fails closed, even on exit 0.
type systemOutput struct {
	buffer   bytes.Buffer
	overflow bool
}

func (w *systemOutput) String() string { return w.buffer.String() }

func (w *systemOutput) Write(p []byte) (int, error) {
	n := len(p)
	left := (64 << 10) - w.buffer.Len()
	if n > left {
		w.overflow = true
		p = p[:left]
	}
	_, _ = w.buffer.Write(p)
	return n, nil
}

func runSystemCommand(ctx context.Context, path string, args []string) (string, error) {
	return runSystemCommandChecked(ctx, path, args, false)
}

// Installed RPM queries must not turn successful exits with diagnostics into absence.
func runQuietSystemCommand(ctx context.Context, path string, args []string) (string, error) {
	return runSystemCommandChecked(ctx, path, args, true)
}

func runSystemCommandChecked(ctx context.Context, path string, args []string, rejectStderr bool) (string, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "DEBIAN_FRONTEND=noninteractive", "TERM=dumb"}
	cmd.Dir = "/"
	cmd.Stdin = nil
	cmd.WaitDelay = time.Second
	var out, stderr systemOutput
	cmd.Stdout, cmd.Stderr = &out, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		return out.String(), fmt.Errorf("%s: %w; %s", filepath.Base(path), err, stderr.String())
	}
	if out.overflow || stderr.overflow {
		return "", fmt.Errorf("system command output exceeded bound")
	}
	if rejectStderr && stderr.String() != "" {
		return "", fmt.Errorf("%s query emitted diagnostics: %s", filepath.Base(path), stderr.String())
	}
	return out.String(), nil
}

func installedHermes() (string, string, error) {
	const path = "/var/lib/dpkg/status"
	if err := trustedRootFile(path); err != nil {
		return "", "", err
	}
	f, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	i, err := f.Stat()
	if err != nil || !i.Mode().IsRegular() || owner(i) != 0 || i.Mode().Perm()&0022 != 0 || i.Size() > 64<<20 {
		return "", "", fmt.Errorf("unsafe or oversized dpkg status")
	}
	data, err := io.ReadAll(io.LimitReader(f, (64<<20)+1))
	if err != nil || len(data) > 64<<20 {
		return "", "", fmt.Errorf("cannot read bounded dpkg status: %w", err)
	}
	return parseInstalledHermes(data)
}

func parseInstalledHermes(data []byte) (string, string, error) {
	var version, arch string
	found := false
	fields := map[string]string{}
	duplicate := false
	flush := func() error {
		if fields["Package"] == "hermes-desktop" {
			if found || duplicate {
				return fmt.Errorf("ambiguous Hermes Desktop package status")
			}
			found = true
			if fields["Status"] != "install ok installed" || !versionRE.MatchString(fields["Version"]) || fields["Architecture"] != "amd64" {
				return fmt.Errorf("existing Hermes Desktop package has unsupported version, architecture or incomplete/held state; inspect manually")
			}
			version, arch = fields["Version"], fields["Architecture"]
		}
		fields, duplicate = map[string]string{}, false
		return nil
	}
	s := bufio.NewScanner(bytes.NewReader(data))
	s.Buffer(make([]byte, 4096), 64<<10)
	for s.Scan() {
		line := s.Text()
		if line == "" {
			if err := flush(); err != nil {
				return "", "", err
			}
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			return "", "", fmt.Errorf("malformed dpkg status")
		}
		switch key {
		case "Package", "Version", "Architecture", "Status":
			if _, ok := fields[key]; ok {
				duplicate = true
			}
			fields[key] = strings.TrimSpace(value)
		}
	}
	if err := s.Err(); err != nil {
		return "", "", err
	}
	if err := flush(); err != nil {
		return "", "", err
	}
	return version, arch, nil
}

// A local DEB can bypass negative package pins. Until we can faithfully evaluate
// administrator preferences, refuse ANY non-comment preference instead of claiming
// that merely leaving the original files in place respects its policy.
func checkAPTPreferences() error {
	paths := []string{"/etc/apt/preferences"}
	const parts = "/etc/apt/preferences.d"
	if i, err := os.Lstat(parts); err == nil {
		if !i.IsDir() || owner(i) != 0 || i.Mode().Perm()&0022 != 0 {
			return fmt.Errorf("untrusted APT preferences directory")
		}
		f, err := os.Open(parts)
		if err != nil {
			return err
		}
		es, readErr := f.ReadDir(129)
		f.Close()
		if readErr != nil && readErr != io.EOF || len(es) > 128 {
			return fmt.Errorf("cannot inspect bounded APT preferences")
		}
		for _, e := range es {
			paths = append(paths, filepath.Join(parts, e.Name()))
		}
	} else if !os.IsNotExist(err) {
		return err
	}
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
			return fmt.Errorf("cannot inspect bounded APT preferences")
		}
		if !emptyAPTPreferences(data) {
			return fmt.Errorf("custom APT preferences detected; local DEB policy evaluation is unsupported in this preview; use your administrator-managed install workflow")
		}
	}
	return nil
}

func emptyAPTPreferences(data []byte) bool {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			return false
		}
	}
	return true
}

func verifyDEBFields(output string, a artifact) error {
	fields := map[string]string{}
	for _, line := range strings.Split(strings.TrimSuffix(output, "\n"), "\n") {
		key, value, ok := strings.Cut(line, ": ")
		if !ok || fields[key] != "" {
			return fmt.Errorf("ambiguous DEB metadata")
		}
		fields[key] = value
	}
	if len(fields) != 3 || fields["Package"] != "hermes-desktop" || fields["Version"] != a.Version || fields["Architecture"] != a.Arch {
		return fmt.Errorf("DEB package name/version/architecture mismatch")
	}
	return nil
}
