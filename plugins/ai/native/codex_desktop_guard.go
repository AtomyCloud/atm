//go:build linux

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const codexGuardName = "atm-ai-guard"
const codexGuardFlag = "--internal-codex-apt-guard"
const codexGuardEarlyFlag = "--internal-codex-apt-guard-early"
const codexGuardMaxImage = int64(64 << 20)

type codexAPTGuardMetadata struct {
	Schema                                            int
	Stage, Version, Arch, SHA256, HelperSHA256, Nonce string
	Size                                              int64
}

func (m codexAPTGuardMetadata) validate(stage string) error {
	_, err := codexDesktopURL(m.Arch)
	if m.Schema != 1 || !codexDesktopStageRE.MatchString(stage) || m.Stage != stage || err != nil || !validDEBVersion(m.Version) || !validSHA(m.SHA256) || !validSHA(m.HelperSHA256) || !validSHA(m.Nonce) || m.Size <= 0 || m.Size > codexDesktopMaxDownload {
		return fmt.Errorf("invalid transaction-bound APT guard metadata")
	}
	return nil
}

func codexGuardMarker(m codexAPTGuardMetadata) []byte {
	return []byte(fmt.Sprintf("atm-codex-apt-guard-v1\n%s\n%s\n%s\n%s\n%s\n%d\n%s\n", m.Stage, m.Nonce, m.Version, m.Arch, m.SHA256, m.Size, m.HelperSHA256))
}

func codexGuardEarlyMarker(m codexAPTGuardMetadata) []byte {
	return append([]byte("early-absence\n"), codexGuardMarker(m)...)
}

func codexGuardAPTArgs(stage string) []string {
	// Every character in the hook is fixed or from the strict mktemp path grammar.
	// APT itself uses /bin/sh -c; exec replaces it with this native guarded image.
	hook := "exec " + stage + "/" + codexGuardName + " " + codexGuardFlag
	return append([]string{"-o", "DPkg::Pre-Invoke::=exec " + stage + "/" + codexGuardName + " " + codexGuardEarlyFlag,
		"-o", "DPkg::Pre-Install-Pkgs::=" + hook,
		"-o", "DPkg::Tools::Options::exec::Version=1", "-o", "DPkg::Tools::Options::exec::InfoFD=0"}, systemAPTArgs(stage+"/chatgpt.deb")...)
}

func readCodexRootFile(path string, mode os.FileMode, limit int64) ([]byte, error) {
	var data bytes.Buffer
	_, err := copyCodexRootFile(path, mode, limit, &data)
	if err != nil {
		return nil, err
	}
	return data.Bytes(), nil
}

func hashCodexRootFile(path string, mode os.FileMode, limit int64) (int64, string, error) {
	h := sha256.New()
	n, err := copyCodexRootFile(path, mode, limit, h)
	if err != nil {
		return 0, "", err
	}
	return n, fmt.Sprintf("%x", h.Sum(nil)), nil
}

func copyCodexRootFile(path string, mode os.FileMode, limit int64, dst io.Writer) (int64, error) {
	if err := trustedRootFile(path); err != nil {
		return 0, err
	}
	f, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	i, err := f.Stat()
	if err != nil || !i.Mode().IsRegular() || owner(i) != 0 || i.Mode().Perm() != mode || i.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 || !singleLink(i) || i.Size() > limit {
		return 0, fmt.Errorf("unsafe root guard file")
	}
	n, err := io.Copy(dst, io.LimitReader(f, limit+1))
	if err != nil || n > limit || n != i.Size() {
		return 0, fmt.Errorf("root guard file exceeds bound or cannot be read exactly")
	}
	now, err := os.Lstat(path)
	if err != nil || !os.SameFile(i, now) || i.Size() != now.Size() || !i.ModTime().Equal(now.ModTime()) {
		return 0, fmt.Errorf("root guard file changed")
	}
	return n, nil
}

func codexWholeWriteLock(lk unix.Flock_t) bool {
	return lk.Type == unix.F_WRLCK && lk.Whence == 0 && lk.Start == 0 && lk.Len == 0 && lk.Pid > 1
}

// The environment flag alone is NOT proof of a lock. Check the kernel's POSIX
// write-lock owner and a bounded root ancestor chain to trusted apt-get. The
// Pre-Invoke runner forks an intermediate process before its shell exec.
func checkCodexAPTFrontendLock() error {
	if os.Geteuid() != 0 || os.Getuid() != 0 || os.Getenv("DPKG_FRONTEND_LOCKED") != "true" {
		return fmt.Errorf("native guard requires locked root APT hook context")
	}
	for _, item := range codexSystemEnvironment() {
		key, value, _ := strings.Cut(item, "=")
		if os.Getenv(key) != value {
			return fmt.Errorf("native guard requires the fixed post-sudo environment")
		}
	}
	const path = "/var/lib/dpkg/lock-frontend"
	if err := trustedRootFile(path); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	i, err := f.Stat()
	if err != nil || !i.Mode().IsRegular() || owner(i) != 0 || i.Mode().Perm()&0022 != 0 || !singleLink(i) {
		return fmt.Errorf("untrusted frontend lock")
	}
	lk := unix.Flock_t{Type: unix.F_WRLCK, Whence: 0, Start: 0, Len: 0}
	if err := unix.FcntlFlock(f.Fd(), unix.F_GETLK, &lk); err != nil {
		return err
	}
	if !codexWholeWriteLock(lk) {
		return fmt.Errorf("frontend whole-file write lock is not held")
	}
	pid, found := os.Getppid(), false
	for depth := 0; depth < 3 && pid > 1; depth++ {
		ppid, err := codexRootParent(pid)
		if err != nil {
			return err
		}
		if pid == int(lk.Pid) {
			found = true
			break
		}
		pid = ppid
	}
	if !found {
		return fmt.Errorf("frontend write lock owner is not a bounded root APT ancestor")
	}
	apt, err := systemCommand("apt-get")
	if err != nil {
		return err
	}
	parent := "/proc/" + strconv.Itoa(int(lk.Pid))
	pi, err := os.Stat(parent)
	if err != nil || owner(pi) != 0 {
		return fmt.Errorf("APT lock owner is not root")
	}
	exe, err := os.Stat(parent + "/exe")
	if err != nil {
		return err
	}
	trusted, err := os.Stat(apt)
	if err != nil || !os.SameFile(exe, trusted) {
		return fmt.Errorf("frontend lock owner is not the trusted apt-get executable")
	}
	now, err := os.Lstat(path)
	if err != nil || !os.SameFile(i, now) {
		return fmt.Errorf("frontend lock file changed")
	}
	return nil
}

func codexRootParent(pid int) (int, error) {
	f, err := os.OpenFile("/proc/"+strconv.Itoa(pid)+"/status", os.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (64<<10)+1))
	if err != nil || len(data) > 64<<10 {
		return 0, fmt.Errorf("cannot inspect bounded APT ancestry")
	}
	return parseCodexRootParent(string(data))
}

func parseCodexRootParent(data string) (int, error) {
	parent, root := 0, false
	for _, line := range strings.Split(data, "\n") {
		key, value, _ := strings.Cut(line, ":")
		if key == "PPid" {
			if parent != 0 {
				return 0, fmt.Errorf("duplicate parent PID")
			}
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || n < 1 {
				return 0, fmt.Errorf("invalid parent PID")
			}
			parent = n
		}
		if key == "Uid" {
			fields := strings.Fields(value)
			if root || len(fields) != 4 {
				return 0, fmt.Errorf("invalid ancestor UID")
			}
			for _, v := range fields {
				if v != "0" {
					return 0, fmt.Errorf("nonroot APT ancestor")
				}
			}
			root = true
		}
	}
	if !root || parent == 0 {
		return 0, fmt.Errorf("incomplete root ancestry")
	}
	return parent, nil
}

type codexAPTGuardTools struct {
	stage         func(string) error
	lock          func() error
	policy        func(context.Context) error
	packageRecord func() (codexPackageRecord, error)
	read          func(string, os.FileMode, int64) ([]byte, error)
	hash          func(string, os.FileMode, int64) (int64, string, error)
	fields        func(context.Context, string) (string, error)
	marker        func(string, []byte) error
}

func defaultCodexAPTGuardTools() codexAPTGuardTools {
	b := &backend{}
	t := b.codexDesktopTools()
	return codexAPTGuardTools{
		stage: t.commands.stage, lock: checkCodexAPTFrontendLock,
		policy:        func(ctx context.Context) error { return checkCodexSystemPolicy(ctx, t) },
		packageRecord: installedCodexDesktop, read: readCodexRootFile, hash: hashCodexRootFile,
		fields: func(ctx context.Context, path string) (string, error) {
			ex, err := systemCommand("dpkg-deb")
			if err != nil {
				return "", err
			}
			return runSystemCommand(ctx, ex, []string{"--field", path, "Package", "Version", "Architecture"})
		},
		marker: func(path string, data []byte) error {
			f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY|unix.O_NOFOLLOW, 0600)
			if err != nil {
				return fmt.Errorf("APT guard already ran or marker is unsafe: %w", err)
			}
			_, err = f.Write(data)
			if err == nil {
				err = f.Sync()
			}
			closeErr := f.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
			dir, err := os.Open(filepath.Dir(path))
			if err != nil {
				return err
			}
			defer dir.Close()
			return dir.Sync()
		},
	}
}

func runCodexAPTGuard(ctx context.Context, stage string, input io.Reader, t codexAPTGuardTools) error {
	return runCodexAPTGuardPhase(ctx, stage, input, t, false)
}

func runCodexAPTGuardPhase(ctx context.Context, stage string, input io.Reader, t codexAPTGuardTools, early bool) error {
	if !codexDesktopStageRE.MatchString(stage) {
		return fmt.Errorf("invalid native guard stage")
	}
	if err := t.stage(stage); err != nil {
		return err
	}
	if err := t.lock(); err != nil {
		return err
	}
	data, err := t.read(stage+"/guard.json", 0600, 64<<10)
	if err != nil {
		return err
	}
	var m codexAPTGuardMetadata
	if err := decode(data, &m, true); err != nil {
		return err
	}
	if err := m.validate(stage); err != nil {
		return err
	}
	if !early {
		ack, err := t.read(stage+"/guard-early", 0600, 4096)
		if err != nil {
			return err
		}
		if string(ack) != string(codexGuardEarlyMarker(m)) {
			return fmt.Errorf("missing/mismatched early absence guard acknowledgement")
		}
	}
	_, imageHash, err := t.hash(stage+"/"+codexGuardName, 0500, codexGuardMaxImage)
	if err != nil {
		return err
	}
	if imageHash != m.HelperSHA256 {
		return fmt.Errorf("native guard image mismatch")
	}
	if err := t.policy(ctx); err != nil {
		return err
	}
	target := stage + "/chatgpt.deb"
	if !early {
		list, err := io.ReadAll(io.LimitReader(input, (1<<20)+1))
		if err != nil || len(list) > 1<<20 {
			return fmt.Errorf("APT hook input exceeds bound or is unreadable")
		}
		if err := validateCodexAPTInput(string(list), target, func(path string) error {
			if err := trustedRootFile(path); err != nil {
				return err
			}
			out, err := t.fields(ctx, path)
			if err != nil {
				return err
			}
			for _, line := range strings.Split(out, "\n") {
				if line == "Package: chatgpt" {
					return fmt.Errorf("additional chatgpt DEB in APT plan")
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}
	size, digest, err := t.hash(target, 0600, codexDesktopMaxDownload)
	if err != nil {
		return err
	}
	if size != m.Size || digest != m.SHA256 {
		return fmt.Errorf("guard DEB length/hash mismatch")
	}
	out, err := t.fields(ctx, target)
	if err != nil {
		return err
	}
	v, err := codexDEBFields(out, m.Arch)
	if err != nil || v != m.Version {
		return fmt.Errorf("guard DEB identity/version/architecture mismatch")
	}
	pkg, err := t.packageRecord()
	if err != nil {
		return err
	}
	if pkg.Present {
		return fmt.Errorf("chatgpt appeared before APT acquired its frontend lock; existing record preserved")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := t.lock(); err != nil {
		return err
	}
	if err := t.stage(stage); err != nil {
		return err
	}
	if early {
		return t.marker(stage+"/guard-early", codexGuardEarlyMarker(m))
	}
	return t.marker(stage+"/guard-ok", codexGuardMarker(m))
}

func validateCodexAPTInput(input, target string, dependency func(string) error) error {
	if input == "" || !strings.HasSuffix(input, "\n") || len(input) > 1<<20 {
		return fmt.Errorf("missing/invalid APT protocol-v1 input")
	}
	lines := strings.Split(strings.TrimSuffix(input, "\n"), "\n")
	if len(lines) > 4096 {
		return fmt.Errorf("too many APT package files")
	}
	seen := map[string]bool{}
	found := false
	for _, path := range lines {
		if absolutePath(path) != nil || !strings.HasSuffix(path, ".deb") || seen[path] {
			return fmt.Errorf("unsafe/duplicate APT package path")
		}
		seen[path] = true
		if path == target {
			found = true
			continue
		}
		if filepath.Dir(path) != "/var/cache/apt/archives" {
			return fmt.Errorf("APT dependency outside fixed system cache")
		}
		if err := dependency(path); err != nil {
			return err
		}
	}
	if !found {
		return fmt.Errorf("APT plan lacks exact staged ChatGPT DEB")
	}
	return nil
}

func codexAPTGuardMain(args []string) error {
	if len(args) != 1 || args[0] != codexGuardFlag && args[0] != codexGuardEarlyFlag || os.Geteuid() != 0 || os.Getuid() != 0 {
		return fmt.Errorf("internal root APT hook only")
	}
	if args[0] == codexGuardFlag && os.Getenv("APT_HOOK_INFO_FD") != "0" {
		return fmt.Errorf("APT protocol-v1 input on stdin required")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	stage := filepath.Dir(exe)
	if filepath.Base(exe) != codexGuardName || !codexDesktopStageRE.MatchString(stage) {
		return fmt.Errorf("guard must execute from its root-owned transaction stage")
	}
	if err := checkRootStage(stage, codexDesktopStageRE); err != nil {
		return err
	}
	if err := trustedRootFile(exe); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return runCodexAPTGuardPhase(ctx, stage, os.Stdin, defaultCodexAPTGuardTools(), args[0] == codexGuardEarlyFlag)
}

func encodeCodexGuardMetadata(m codexAPTGuardMetadata) ([]byte, error) { return json.Marshal(m) }
