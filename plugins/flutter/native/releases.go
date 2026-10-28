package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/AtomyCloud/atm/internal/install"
	"github.com/AtomyCloud/atm/internal/protocol"
)

const officialBase = "https://storage.googleapis.com/flutter_infra_release/releases"

type choice = protocol.Choice

var versionRE = regexp.MustCompile(`^(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})(-[A-Za-z0-9][A-Za-z0-9.-]{0,60})?$`)
var limits = install.Limits{DownloadBytes: 3 << 30, ExpandedBytes: 8 << 30, FileBytes: 2 << 30, Entries: 150000}

func normalizeVersion(s string) (string, error) {
	s = strings.TrimPrefix(s, "flutter")
	if !versionRE.MatchString(s) {
		return "", fmt.Errorf("invalid Flutter version")
	}
	return s, nil
}
func newer(a, b string) bool {
	ac, as, _ := strings.Cut(a, "-")
	bc, bs, _ := strings.Cut(b, "-")
	aa, bb := strings.Split(ac, "."), strings.Split(bc, ".")
	for i := 0; i < 3; i++ {
		x, _ := strconv.Atoi(aa[i])
		y, _ := strconv.Atoi(bb[i])
		if x != y {
			return x > y
		}
	}
	if as == "" || bs == "" {
		return as == "" && bs != ""
	}
	// Flutter prerelease components are compared numerically when possible.
	ap, bp := strings.Split(as, "."), strings.Split(bs, ".")
	for i := 0; i < min(len(ap), len(bp)); i++ {
		if ap[i] == bp[i] {
			continue
		}
		x, xe := strconv.Atoi(ap[i])
		y, ye := strconv.Atoi(bp[i])
		if xe == nil && ye == nil {
			return x > y
		}
		return ap[i] > bp[i]
	}
	return len(ap) > len(bp)
}

type options struct {
	version, channel string
	channelExplicit  bool
}

func parseOptions(args []string) (options, error) {
	o := options{channel: "stable"}
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		k, v, equals := strings.Cut(args[i], "=")
		if (k != "--version" && k != "--channel") || seen[k] {
			return o, fmt.Errorf("expected --version VERSION and/or --channel stable|beta|dev")
		}
		seen[k] = true
		if !equals {
			i++
			if i >= len(args) {
				return o, fmt.Errorf("missing %s value", k)
			}
			v = args[i]
		}
		if k == "--version" {
			n, err := normalizeVersion(v)
			if err != nil {
				return o, err
			}
			o.version = n
		} else {
			o.channel = v
			o.channelExplicit = true
		}
	}
	if o.channel != "stable" && o.channel != "beta" && o.channel != "dev" {
		return o, fmt.Errorf("unsupported Flutter channel")
	}
	return o, nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

func (b *backend) fetch(ctx context.Context, suffix string, dst io.Writer, maxBytes int64) error {
	base, err := url.Parse(b.base)
	if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return fmt.Errorf("HTTPS release origin required")
	}
	target, err := url.Parse(b.base + suffix)
	if err != nil || target.Scheme != base.Scheme || target.Host != base.Host || target.User != nil || target.RawQuery != "" || target.Fragment != "" || !strings.HasPrefix(target.Path, base.Path+"/") {
		return fmt.Errorf("invalid release URL")
	}
	client := *b.client
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || req.URL.Scheme != "https" || req.URL.Host != base.Host || req.URL.User != nil || req.URL.RawQuery != "" || req.URL.Fragment != "" || req.URL.EscapedPath() != target.EscapedPath() {
			return fmt.Errorf("release redirect rejected")
		}
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || resp.ContentLength > maxBytes {
		return fmt.Errorf("release response rejected (status %d)", resp.StatusCode)
	}
	if ce := resp.Header.Get("Content-Encoding"); ce != "" && ce != "identity" {
		return fmt.Errorf("encoded release response rejected")
	}
	n, err := io.Copy(dst, io.LimitReader(contextReader{ctx, resp.Body}, maxBytes+1))
	if err != nil {
		return err
	}
	if n > maxBytes {
		return fmt.Errorf("release download exceeds limit")
	}
	return ctx.Err()
}

type artifact struct {
	Ambiguous   bool   `json:"ambiguous,omitempty"`
	Version     string `json:"version"`
	URL         string `json:"url"`
	SHA256      string `json:"sha256"`
	Channel     string `json:"channel"`
	Arch        string `json:"arch"`
	Revision    string `json:"revision"`
	StripPrefix string `json:"strip_prefix"`
}
type release struct {
	Version  string `json:"version"`
	Channel  string `json:"channel"`
	Arch     string `json:"dart_sdk_arch"`
	Archive  string `json:"archive"`
	SHA256   string `json:"sha256"`
	Revision string `json:"hash"`
}

func (b *backend) releases(ctx context.Context, channel string) ([]artifact, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var data strings.Builder
	if err := b.fetch(ctx, "/releases_linux.json", &data, 16<<20); err != nil {
		return nil, err
	}
	var doc struct {
		Base     string    `json:"base_url"`
		Releases []release `json:"releases"`
	}
	if err := json.Unmarshal([]byte(data.String()), &doc); err != nil {
		return nil, err
	}
	if doc.Base != officialBase {
		return nil, fmt.Errorf("unexpected official release base_url")
	}
	out := []artifact{}
	seen := map[string]int{}
	for _, r := range doc.Releases {
		// Historic metadata omitted dart_sdk_arch for x64 only; the exact legacy filename must match too.
		arch := r.Arch
		if arch == "" {
			arch = "x64"
		}
		if arch != b.platform || r.Channel != channel {
			continue
		}
		v, err := normalizeVersion(r.Version)
		if err != nil || v != r.Version {
			continue
		}
		archPart := ""
		if arch == "arm64" {
			archPart = "arm64_"
		}
		expected := channel + "/linux/flutter_linux_" + archPart + v + "-" + channel + ".tar.xz"
		hash, e := hex.DecodeString(r.SHA256)
		rev, re := hex.DecodeString(r.Revision)
		if r.Arch == "" && r.Archive != expected {
			continue
		}
		if r.Archive != expected || e != nil || len(hash) != sha256.Size || re != nil || len(rev) != 20 {
			return nil, fmt.Errorf("invalid official artifact metadata for %s", v)
		}
		a := artifact{Version: v, URL: b.base + "/" + r.Archive, SHA256: strings.ToLower(r.SHA256), Channel: channel, Arch: arch, Revision: strings.ToLower(r.Revision), StripPrefix: "flutter"}
		if index, exists := seen[v]; exists {
			prior := out[index]
			if prior.SHA256 != a.SHA256 || prior.Revision != a.Revision || prior.URL != a.URL {
				out[index].Ambiguous = true
			}
			continue
		}
		seen[v] = len(out)
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return newer(out[i].Version, out[j].Version) })
	if len(out) == 0 {
		return nil, fmt.Errorf("no official Linux %s %s archives available; no architecture fallback", b.platform, channel)
	}
	return out, nil
}
