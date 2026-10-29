//go:build linux

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const codexDesktopRPM = "codex-desktop-rpm"
const codexRPMQueryFormat = "%{NAME}\t%{EPOCHNUM}\t%{VERSION}\t%{RELEASE}\t%{ARCH}\n"

type codexRPMMetadata struct {
	Name, Epoch, Version, Release, Arch string
}

var codexRPMComponentRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+~^]*$`)

func codexRPMArch(goarch string) (string, error) {
	switch goarch {
	case "amd64":
		return "x86_64", nil
	case "arm64":
		return "aarch64", nil
	default:
		return "", fmt.Errorf("ChatGPT RPM requires Linux amd64/arm64")
	}
}

func codexRPMURL(goarch string) (string, error) {
	arch, err := codexRPMArch(goarch)
	if err != nil {
		return "", err
	}
	return "https://persistent.oaistatic.com/codex-app-prod/linux/rpm/latest/chatgpt." + arch + ".rpm", nil
}

func parseCodexRPMMetadata(output, goarch string) (codexRPMMetadata, error) {
	arch, err := codexRPMArch(goarch)
	if err != nil {
		return codexRPMMetadata{}, err
	}
	if len(output) > 1024 || !strings.HasSuffix(output, "\n") || strings.Count(output, "\n") != 1 {
		return codexRPMMetadata{}, fmt.Errorf("one bounded newline-terminated RPM metadata record required")
	}
	fields := strings.Split(strings.TrimSuffix(output, "\n"), "\t")
	if len(fields) != 5 || fields[0] != "chatgpt" || fields[4] != arch {
		return codexRPMMetadata{}, fmt.Errorf("ChatGPT RPM identity/architecture mismatch")
	}
	epoch, err := strconv.ParseUint(fields[1], 10, 32)
	if err != nil || strconv.FormatUint(epoch, 10) != fields[1] {
		return codexRPMMetadata{}, fmt.Errorf("canonical nonnegative RPM epoch required")
	}
	// Validate components, not Debian/semantic versions; RPM owns their ordering.
	for _, value := range fields[2:4] {
		if len(value) > 256 || !codexRPMComponentRE.MatchString(value) {
			return codexRPMMetadata{}, fmt.Errorf("invalid or oversized RPM version/release")
		}
	}
	return codexRPMMetadata{fields[0], fields[1], fields[2], fields[3], fields[4]}, nil
}

// The caller must pin and recheck the package file around this metadata query.
func queryCodexRPM(ctx context.Context, s *systemTools, path, arch string) (codexRPMMetadata, error) {
	if _, err := codexRPMArch(arch); err != nil {
		return codexRPMMetadata{}, err
	}
	if err := absolutePath(path); err != nil {
		return codexRPMMetadata{}, err
	}
	if !strings.HasSuffix(path, ".rpm") {
		return codexRPMMetadata{}, fmt.Errorf("RPM package path required")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return codexRPMMetadata{}, err
	}
	ex, err := s.resolve("rpm")
	if err != nil {
		return codexRPMMetadata{}, err
	}
	if err := ctx.Err(); err != nil {
		return codexRPMMetadata{}, err
	}
	out, err := s.run(ctx, ex, []string{"-qp", "--queryformat", codexRPMQueryFormat, "--", path})
	if ctx.Err() != nil {
		return codexRPMMetadata{}, ctx.Err()
	}
	if err != nil {
		return codexRPMMetadata{}, err
	}
	return parseCodexRPMMetadata(out, arch)
}

// Only explicit installation calls this; query failures never establish absence.
func installedCodexRPM(ctx context.Context, s *systemTools, arch string) (codexRPMMetadata, bool, error) {
	if _, err := codexRPMArch(arch); err != nil {
		return codexRPMMetadata{}, false, err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return codexRPMMetadata{}, false, err
	}
	ex, err := s.resolve("rpm")
	if err != nil {
		return codexRPMMetadata{}, false, err
	}
	if err := ctx.Err(); err != nil {
		return codexRPMMetadata{}, false, err
	}
	if s.quietRun == nil {
		return codexRPMMetadata{}, false, fmt.Errorf("RPM installed query requires diagnostic-aware execution")
	}
	out, err := s.quietRun(ctx, ex, []string{"-qa", "--queryformat", codexRPMQueryFormat, "--", "chatgpt"})
	if ctx.Err() != nil {
		return codexRPMMetadata{}, false, ctx.Err()
	}
	if err != nil {
		return codexRPMMetadata{}, false, err
	}
	if out == "" {
		return codexRPMMetadata{}, false, nil
	}
	m, err := parseCodexRPMMetadata(out, arch)
	if err != nil {
		return codexRPMMetadata{}, false, err
	}
	return m, true, nil
}

// SHA-256 establishes byte continuity only, not publisher authentication.
func (b *backend) downloadCodexRPM(ctx context.Context, dst io.Writer) (int64, string, error) {
	rawURL, err := codexRPMURL(b.arch)
	if err != nil {
		return 0, "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return 0, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Accept-Encoding", "identity")
	client := *b.client
	client.Jar = nil
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return fmt.Errorf("ChatGPT RPM redirects refused") }
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.ContentLength <= 0 || resp.ContentLength > codexDesktopMaxDownload || resp.Uncompressed || (resp.Header.Get("Content-Encoding") != "" && resp.Header.Get("Content-Encoding") != "identity") || len(resp.TransferEncoding) != 0 {
		return 0, "", fmt.Errorf("ChatGPT RPM requires HTTP 200 and positive exact Content-Length <= 512 MiB, without transfer/content encoding")
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
		return 0, "", fmt.Errorf("ChatGPT RPM Content-Length mismatch")
	}
	return n, hex.EncodeToString(h.Sum(nil)), nil
}
