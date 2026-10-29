//go:build linux

package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const codexDesktopSystem = "codex-desktop-system"
const codexDesktopMaxDownload int64 = 512 << 20

var codexDesktopStageRE = regexp.MustCompile(`^/run/atm-ai-codex-desktop-[A-Za-z0-9]{10}$`)
var debVersionRE = regexp.MustCompile(`^(?:[0-9]+:)?[0-9][A-Za-z0-9.+~]*(?:-[A-Za-z0-9.+~]+)*$`)

type codexPackageRecord struct {
	Present               bool
	Status, Version, Arch string
}

// Test injection is process-local only; no environment URL, command or DB overrides.
type codexDesktopTools struct {
	commands      *systemTools
	packageRecord func() (codexPackageRecord, error)
	configFiles   func() error
	executable    func() (*os.File, error)
}

func (b *backend) codexDesktopTools() *codexDesktopTools {
	if b.codexDesktop != nil {
		return b.codexDesktop
	}
	s := defaultSystemTools()
	s.stage = func(p string) error { return checkRootStage(p, codexDesktopStageRE) }
	return &codexDesktopTools{commands: s, packageRecord: installedCodexDesktop, configFiles: checkCodexDpkgConfig, executable: func() (*os.File, error) { return os.Open("/proc/self/exe") }}
}

func codexDesktopURL(arch string) (string, error) {
	if arch != "amd64" && arch != "arm64" {
		return "", fmt.Errorf("ChatGPT Desktop DEB requires Linux amd64/arm64")
	}
	return "https://persistent.oaistatic.com/codex-app-prod/linux/deb/latest/chatgpt_" + arch + ".deb", nil
}

func validDEBVersion(v string) bool { return len(v) <= 128 && debVersionRE.MatchString(v) }

func codexUpdateEligible(p codexPackageRecord, arch string) bool {
	return p.Present && p.Status == "install ok installed" && validDEBVersion(p.Version) && p.Arch == arch
}

func codexBaselineValid(operation string, p codexPackageRecord, arch string) bool {
	return operation == "install" && p == (codexPackageRecord{}) || operation == "update" && codexUpdateEligible(p, arch)
}

// dpkg owns Debian ordering, including epochs, revisions and equal spellings.
func compareCodexVersions(ctx context.Context, s *systemTools, candidate, baseline string) (int, error) {
	if !validDEBVersion(candidate) || !validDEBVersion(baseline) {
		return 0, fmt.Errorf("invalid Debian version comparison")
	}
	ex, err := s.resolve("dpkg")
	if err != nil {
		return 0, err
	}
	for _, test := range []struct {
		op     string
		result int
	}{{"eq", 0}, {"gt", 1}, {"lt", -1}} {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		out, err := s.run(ctx, ex, []string{"--compare-versions", candidate, test.op, baseline})
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		if out != "" {
			return 0, fmt.Errorf("unexpected dpkg comparison output")
		}
		if err == nil {
			return test.result, nil
		}
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 {
			return 0, fmt.Errorf("dpkg comparison failed: %w", err)
		}
	}
	return 0, fmt.Errorf("inconsistent dpkg version comparison")
}

// This reader reports package database evidence, never application ownership or health.
func installedCodexDesktop() (codexPackageRecord, error) {
	const path = "/var/lib/dpkg/status"
	if err := trustedRootFile(path); err != nil {
		return codexPackageRecord{}, err
	}
	if err := codexUpdatesEmpty("/var/lib/dpkg/updates", 0); err != nil {
		return codexPackageRecord{}, err
	}
	f, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return codexPackageRecord{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || owner(info) != 0 || info.Mode().Perm()&0022 != 0 || info.Size() > 64<<20 {
		return codexPackageRecord{}, fmt.Errorf("unsafe or oversized dpkg status; package state unknown")
	}
	data, err := io.ReadAll(io.LimitReader(f, (64<<20)+1))
	if err != nil || len(data) > 64<<20 {
		return codexPackageRecord{}, fmt.Errorf("cannot read bounded dpkg status; package state unknown")
	}
	after, err := f.Stat()
	now, pathErr := os.Lstat(path)
	if err != nil || pathErr != nil || !os.SameFile(info, now) || info.Size() != after.Size() || !info.ModTime().Equal(after.ModTime()) {
		return codexPackageRecord{}, fmt.Errorf("dpkg status changed while reading; package state unknown")
	}
	if err := codexUpdatesEmpty("/var/lib/dpkg/updates", 0); err != nil {
		return codexPackageRecord{}, err
	}
	return parseCodexPackageRecord(data)
}

func codexUpdatesEmpty(path string, uid uint32) error {
	f, err := os.OpenFile(path, os.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return fmt.Errorf("dpkg updates unavailable; package state unknown: %w", err)
	}
	defer f.Close()
	i, err := f.Stat()
	if err != nil || !i.IsDir() || owner(i) != uid || i.Mode().Perm()&0022 != 0 {
		return fmt.Errorf("unsafe dpkg updates; package state unknown")
	}
	entries, err := f.ReadDir(1)
	if len(entries) != 0 || err != io.EOF {
		return fmt.Errorf("pending or unreadable dpkg updates; package state unknown")
	}
	now, err := os.Lstat(path)
	if err != nil || !os.SameFile(i, now) || !i.ModTime().Equal(now.ModTime()) {
		return fmt.Errorf("dpkg updates changed; package state unknown")
	}
	return nil
}

func parseCodexPackageRecord(data []byte) (codexPackageRecord, error) {
	var result codexPackageRecord
	if len(data) > 64<<20 {
		return result, fmt.Errorf("oversized dpkg status")
	}
	fields := map[string]string{}
	last := ""
	flush := func() error {
		if len(fields) == 0 {
			return nil
		}
		if fields["Package"] == "" || fields["Status"] == "" {
			return fmt.Errorf("incomplete dpkg paragraph; state unknown")
		}
		if fields["Package"] == "chatgpt" {
			if result.Present {
				return fmt.Errorf("ambiguous chatgpt package records")
			}
			result = codexPackageRecord{true, fields["Status"], fields["Version"], fields["Architecture"]}
		}
		fields, last = map[string]string{}, ""
		return nil
	}
	s := bufio.NewScanner(bytes.NewReader(data))
	s.Buffer(make([]byte, 4096), 64<<10)
	for s.Scan() {
		line := s.Text()
		if line == "" {
			if err := flush(); err != nil {
				return codexPackageRecord{}, err
			}
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			if last == "" || last == "Package" || last == "Status" || last == "Version" || last == "Architecture" {
				return codexPackageRecord{}, fmt.Errorf("ambiguous dpkg continuation")
			}
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok || key == "" || strings.TrimSpace(key) != key {
			return codexPackageRecord{}, fmt.Errorf("malformed dpkg status")
		}
		if _, exists := fields[key]; exists {
			return codexPackageRecord{}, fmt.Errorf("duplicate dpkg field")
		}
		fields[key], last = strings.TrimSpace(value), key
	}
	if err := s.Err(); err != nil {
		return codexPackageRecord{}, err
	}
	if err := flush(); err != nil {
		return codexPackageRecord{}, err
	}
	return result, nil
}

func codexDEBFields(output, arch string) (string, error) {
	fields := map[string]string{}
	for _, line := range strings.Split(strings.TrimSuffix(output, "\n"), "\n") {
		key, value, ok := strings.Cut(line, ": ")
		if !ok {
			return "", fmt.Errorf("malformed ChatGPT DEB metadata")
		}
		if _, exists := fields[key]; exists {
			return "", fmt.Errorf("duplicate ChatGPT DEB field")
		}
		fields[key] = value
	}
	if len(fields) != 3 || fields["Package"] != "chatgpt" || fields["Architecture"] != arch || !validDEBVersion(fields["Version"]) {
		return "", fmt.Errorf("ChatGPT DEB package/version/architecture mismatch")
	}
	return fields["Version"], nil
}

// No upstream checksum is available. SHA-256 proves only continuity of these bytes.
func (b *backend) downloadCodexDesktop(ctx context.Context, rawURL string, dst io.Writer) (int64, string, error) {
	want, err := codexDesktopURL(b.arch)
	if err != nil || rawURL != want {
		return 0, "", fmt.Errorf("only the exact official latest ChatGPT DEB URL is allowed")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Accept-Encoding", "identity")
	client := *b.client
	client.Jar = nil
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return fmt.Errorf("ChatGPT DEB redirects refused") }
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.ContentLength <= 0 || resp.ContentLength > codexDesktopMaxDownload || resp.Uncompressed || (resp.Header.Get("Content-Encoding") != "" && resp.Header.Get("Content-Encoding") != "identity") || len(resp.TransferEncoding) != 0 {
		return 0, "", fmt.Errorf("ChatGPT DEB requires HTTP 200 and positive exact Content-Length <= 512 MiB, without transfer/content encoding")
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(dst, h), io.LimitReader(resp.Body, resp.ContentLength+1))
	if ctx.Err() != nil {
		return 0, "", ctx.Err()
	}
	if err != nil {
		return 0, "", err
	}
	if n != resp.ContentLength {
		return 0, "", fmt.Errorf("ChatGPT DEB Content-Length mismatch")
	}
	return n, hex.EncodeToString(h.Sum(nil)), nil
}
