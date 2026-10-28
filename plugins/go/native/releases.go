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

type choice = protocol.Choice

var versionRE = regexp.MustCompile(`^(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})(\.(0|[1-9][0-9]{0,5}))?$`)
var limits = install.Limits{DownloadBytes: 256 << 20, ExpandedBytes: 1 << 30, FileBytes: 256 << 20, Entries: 50000}

func normalizeVersion(s string) (string, error) {
	s = strings.TrimPrefix(s, "go")
	if !versionRE.MatchString(s) {
		return "", fmt.Errorf("invalid Go version; expected e.g. 1.26.2")
	}
	return s, nil
}
func newer(a, b string) bool {
	aa, bb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < 3; i++ {
		x, y := 0, 0
		if i < len(aa) {
			x, _ = strconv.Atoi(aa[i])
		}
		if i < len(bb) {
			y, _ = strconv.Atoi(bb[i])
		}
		if x != y {
			return x > y
		}
	}
	return false
}

type options struct{ version string }

func parseOptions(args []string) (options, error) {
	if len(args) == 0 {
		return options{}, nil
	}
	var v string
	if len(args) == 2 && args[0] == "--version" {
		v = args[1]
	} else if len(args) == 1 && strings.HasPrefix(args[0], "--version=") {
		v = strings.TrimPrefix(args[0], "--version=")
	} else {
		return options{}, fmt.Errorf("expected --version <exact numeric version>")
	}
	v, err := normalizeVersion(v)
	return options{v}, err
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

// Production uses a fixed origin. The unexported base/client are injectable only
// by in-process tests, never from requests, configuration or environment variables.
func (b *backend) fetch(ctx context.Context, suffix string, dst io.Writer, max int64) error {
	base, err := url.Parse(b.base)
	if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil {
		return fmt.Errorf("HTTPS release origin required")
	}
	c := *b.client
	c.Timeout = 5 * time.Minute
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		previous := via[len(via)-1].URL
		same := req.URL.Host == previous.Host && req.URL.Path == previous.Path && req.URL.RawQuery == previous.RawQuery
		google := previous.Host == "go.dev" && req.URL.Host == "dl.google.com" && base.Host == "go.dev" && strings.HasPrefix(previous.Path, "/dl/go") && req.URL.Path == "/go/"+strings.TrimPrefix(previous.Path, "/dl/") && req.URL.RawQuery == ""
		if len(via) >= 5 || req.URL.Scheme != "https" || req.URL.User != nil || req.URL.Fragment != "" || (!same && !google) {
			return fmt.Errorf("release redirect rejected")
		}
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, "GET", b.base+suffix, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || resp.ContentLength > max {
		return fmt.Errorf("release response rejected (status %d)", resp.StatusCode)
	}
	if ce := resp.Header.Get("Content-Encoding"); ce != "" && ce != "identity" {
		return fmt.Errorf("encoded release response rejected")
	}
	n, err := io.Copy(dst, io.LimitReader(contextReader{ctx, resp.Body}, max+1))
	if err != nil {
		return err
	}
	if n > max {
		return fmt.Errorf("release download exceeds limit")
	}
	return ctx.Err()
}

type artifact struct {
	install.Spec
	Size int64 `json:"size"`
}
type releaseFile struct {
	Filename string `json:"filename"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Version  string `json:"version"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
	Kind     string `json:"kind"`
}
type release struct {
	Version string        `json:"version"`
	Stable  bool          `json:"stable"`
	Files   []releaseFile `json:"files"`
}

func (b *backend) releases(ctx context.Context) ([]artifact, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var data strings.Builder
	if err := b.fetch(ctx, "/?mode=json&include=all", &data, 16<<20); err != nil {
		return nil, err
	}
	var rows []release
	if err := json.Unmarshal([]byte(data.String()), &rows); err != nil {
		return nil, err
	}
	out := []artifact{}
	seen := map[string]bool{}
	for _, row := range rows {
		v, err := normalizeVersion(row.Version)
		if err != nil || row.Version != "go"+v || !row.Stable {
			continue
		}
		name := "go" + v + "." + b.platform + ".tar.gz"
		for _, f := range row.Files {
			if f.OS != "linux" || "linux-"+f.Arch != b.platform || f.Kind != "archive" {
				continue
			}
			hash, err := hex.DecodeString(f.SHA256)
			if f.Filename != name || f.Version != row.Version || err != nil || len(hash) != sha256.Size || f.Size <= 0 || f.Size > limits.DownloadBytes {
				return nil, fmt.Errorf("invalid official artifact metadata for %s", v)
			}
			if seen[v] {
				return nil, fmt.Errorf("duplicate official artifact for %s", v)
			}
			seen[v] = true
			out = append(out, artifact{install.Spec{Version: v, URL: b.base + "/" + name, SHA256: strings.ToLower(f.SHA256), Format: "tar.gz", StripPrefix: "go", Origins: []string{"https://go.dev", "https://dl.google.com"}}, f.Size})
		}
	}
	sort.Slice(out, func(i, j int) bool { return newer(out[i].Version, out[j].Version) })
	if len(out) == 0 {
		return nil, fmt.Errorf("no compatible stable official Go releases")
	}
	return out, nil
}

func (b *backend) menu(ctx context.Context, args []string) ([]choice, error) {
	add := func(id, key, op string, args ...string) choice {
		return choice{ID: id, Label: label(key), Operation: op, Arguments: args}
	}
	back := add("back", "back", "menu")
	custom := choice{ID: "custom", Label: label("custom"), Operation: "install", Arguments: []string{"--version"}, Prompt: label("version_prompt")}
	if len(args) == 1 && args[0] == "--latest" {
		releases, err := b.releases(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			b.log(label("offline") + ": " + err.Error())
			return []choice{custom, back}, nil
		}
		out := []choice{}
		for _, a := range releases[:min(5, len(releases))] {
			out = append(out, choice{ID: a.Version, Label: label("install_version", a.Version), Operation: "install", Arguments: []string{"--version", a.Version}})
		}
		return append(out, custom, back), nil
	}
	if len(args) == 2 && args[0] == "--installed" && (args[1] == "use" || args[1] == "remove") {
		vs, err := b.installed()
		if err != nil {
			return nil, err
		}
		current, err := b.current()
		if err != nil {
			return nil, err
		}
		out := []choice{}
		for _, v := range vs {
			if args[1] == "remove" && (!b.hasReceipt(v) || v == current) {
				continue
			}
			if len(out) == 99 {
				return nil, fmt.Errorf("too many versions; use CLI exact version")
			}
			text := label(args[1]+"_version", v)
			if v == current {
				text += " " + label("current")
			}
			out = append(out, choice{ID: v, Label: text, Operation: args[1], Arguments: []string{v}})
		}
		if len(out) == 0 {
			b.log(label("empty"))
		}
		return append(out, back), nil
	}
	if len(args) != 0 {
		return nil, fmt.Errorf("invalid menu page")
	}
	return []choice{add("latest", "latest", "menu", "--latest"), custom, add("update", "update", "update"), add("status", "status", "status"), add("use", "use", "menu", "--installed", "use"), add("remove", "remove", "menu", "--installed", "remove"), add("uninstall", "uninstall", "uninstall"), add("integrate", "integrate", "integrate")}, nil
}
