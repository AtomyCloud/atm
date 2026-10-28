//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const maxDownload = int64(512 << 20)

var versionRE = regexp.MustCompile(`^(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})$`)

type provider struct{ name, path, repo, tagPrefix, docs string }

var providers = map[string]provider{
	"picoclaw":       {"PicoClaw CLI", "ai/picoclaw", "sipeed/picoclaw", "v", "https://docs.picoclaw.io/docs/installation/"},
	"hermes-desktop": {"Hermes Desktop (AppImage)", "hermes/hermes-desktop", "fathah/hermes-desktop", "v", "https://github.com/fathah/hermes-desktop/releases/"},
	"codex-cli":      {"Codex CLI (standalone)", "ai/codex/cli", "openai/codex", "rust-v", "https://learn.chatgpt.com/docs/codex/cli"},
}

type artifact struct {
	Provider, Version, Arch, URL, SHA256, Format, Entry string
	Size                                                int64
}

func artifactNames(id, v, arch string) (name, entry, format string, error error) {
	if arch != "amd64" && arch != "arm64" {
		return "", "", "", fmt.Errorf("unsupported architecture")
	}
	if !versionRE.MatchString(v) {
		return "", "", "", fmt.Errorf("invalid stable version")
	}
	switch id {
	case "picoclaw":
		a := "x86_64"
		if arch == "arm64" {
			a = "arm64"
		}
		return "picoclaw_Linux_" + a + ".tar.gz", "picoclaw", "tar.gz", nil
	case "codex-cli":
		a := "x86_64"
		if arch == "arm64" {
			a = "aarch64"
		}
		entry = "codex-" + a + "-unknown-linux-musl"
		return entry + ".tar.gz", entry, "tar.gz", nil
	case "hermes-desktop":
		a := "x86_64"
		if arch == "arm64" {
			a = "arm64"
		}
		return "hermes-desktop-" + v + "-" + a + ".AppImage", "tool", "appimage", nil
	}
	return "", "", "", fmt.Errorf("unknown provider")
}
func validSHA(s string) bool {
	h, e := hex.DecodeString(s)
	return e == nil && len(h) == 32 && strings.ToLower(s) == s
}
func (a artifact) validate() error {
	p, ok := providers[a.Provider]
	if !ok {
		return fmt.Errorf("invalid artifact provider")
	}
	name, entry, format, err := artifactNames(a.Provider, a.Version, a.Arch)
	if err != nil {
		return err
	}
	want := "https://github.com/" + p.repo + "/releases/download/" + p.tagPrefix + a.Version + "/" + name
	if a.URL != want || a.Entry != entry || a.Format != format || !validSHA(a.SHA256) || a.Size <= 0 || a.Size > maxDownload {
		return fmt.Errorf("invalid pinned artifact")
	}
	return nil
}

type releaseAsset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
}
type release struct {
	Tag        string         `json:"tag_name"`
	Draft      *bool          `json:"draft"`
	Prerelease *bool          `json:"prerelease"`
	Assets     []releaseAsset `json:"assets"`
}

func (b *backend) resolve(ctx context.Context, id, version string) (artifact, error) {
	p := providers[id]
	endpoint := "https://api.github.com/repos/" + p.repo + "/releases/latest"
	if version != "latest" {
		if !versionRE.MatchString(version) {
			return artifact{}, fmt.Errorf("invalid version")
		}
		endpoint = "https://api.github.com/repos/" + p.repo + "/releases/tags/" + p.tagPrefix + version
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var buf bytes.Buffer
	if err := b.fetch(ctx, id, endpoint, &buf, 4<<20); err != nil {
		return artifact{}, err
	}
	var r release
	if err := decode(buf.Bytes(), &r, false); err != nil {
		return artifact{}, err
	}
	v := strings.TrimPrefix(r.Tag, p.tagPrefix)
	if !versionRE.MatchString(v) || r.Tag != p.tagPrefix+v || r.Draft == nil || *r.Draft || r.Prerelease == nil || *r.Prerelease || (version != "latest" && version != v) {
		return artifact{}, fmt.Errorf("stable exact release required")
	}
	name, entry, format, err := artifactNames(id, v, b.arch)
	if err != nil {
		return artifact{}, err
	}
	found := 0
	a := artifact{Provider: id, Version: v, Arch: b.arch, Entry: entry, Format: format}
	for _, asset := range r.Assets {
		if asset.Name != name {
			continue
		}
		found++
		a.URL, a.Size = asset.URL, asset.Size
		if !strings.HasPrefix(asset.Digest, "sha256:") {
			return artifact{}, fmt.Errorf("official SHA-256 asset digest required; no checksum-free fallback")
		}
		a.SHA256 = strings.TrimPrefix(asset.Digest, "sha256:")
	}
	if found != 1 {
		return artifact{}, fmt.Errorf("expected one exact architecture asset; found %d", found)
	}
	return a, a.validate()
}
func storageHost(h string) bool {
	return h == "release-assets.githubusercontent.com" || h == "objects.githubusercontent.com" || h == "github-releases.githubusercontent.com"
}
func (b *backend) fetch(ctx context.Context, id, raw string, dst io.Writer, max int64) error {
	p, ok := providers[id]
	if !ok {
		return fmt.Errorf("unknown provider")
	}
	u, err := url.Parse(raw)
	api := "https://api.github.com/repos/" + p.repo + "/releases/"
	download := "https://github.com/" + p.repo + "/releases/download/"
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.RawQuery != "" || (!strings.HasPrefix(raw, api) && !strings.HasPrefix(raw, download)) {
		return fmt.Errorf("fixed official release origin required")
	}
	c := *b.client
	c.Timeout = 5 * time.Minute
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || u.Host != "github.com" || !storageHost(req.URL.Host) || req.URL.Scheme != "https" || req.URL.User != nil || req.URL.Fragment != "" {
			return fmt.Errorf("release redirect rejected")
		}
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("User-Agent", "ATM-AI-Preview/0.0.1")
	if u.Host == "api.github.com" {
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || resp.ContentLength > max {
		return fmt.Errorf("release response rejected: status=%d length=%d", resp.StatusCode, resp.ContentLength)
	}
	if ce := resp.Header.Get("Content-Encoding"); ce != "" && ce != "identity" {
		return fmt.Errorf("encoded response rejected")
	}
	n, err := io.Copy(dst, io.LimitReader(contextReader{ctx, resp.Body}, max+1))
	if err != nil {
		return err
	}
	if n == 0 || n > max {
		return fmt.Errorf("empty or oversized response")
	}
	return ctx.Err()
}

// Reject duplicate keys at every depth, not only the top-level receipt.
func decode(data []byte, out any, strict bool) error {
	d := json.NewDecoder(bytes.NewReader(data))
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 32 {
			return fmt.Errorf("JSON depth exceeded")
		}
		tok, err := d.Token()
		if err != nil {
			return err
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				key, e := d.Token()
				if e != nil {
					return e
				}
				s, ok := key.(string)
				if !ok || seen[s] {
					return fmt.Errorf("duplicate JSON field")
				}
				seen[s] = true
				if e = walk(depth + 1); e != nil {
					return e
				}
			}
		case '[':
			for d.More() {
				if e := walk(depth + 1); e != nil {
					return e
				}
			}
		default:
			return fmt.Errorf("invalid JSON delimiter")
		}
		_, err = d.Token()
		return err
	}
	if err := walk(0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("trailing JSON data")
	}
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return fmt.Errorf("null JSON object")
	}
	d = json.NewDecoder(bytes.NewReader(data))
	if strict {
		d.DisallowUnknownFields()
	}
	return d.Decode(out)
}
