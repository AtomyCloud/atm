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

var versionRE = regexp.MustCompile(`^(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})$`)
var limits = install.Limits{DownloadBytes: 256 << 20, ExpandedBytes: 1 << 30, FileBytes: 256 << 20, Entries: 30000}

func normalizeVersion(s string) (string, error) {
	s = strings.TrimPrefix(strings.TrimPrefix(s, "node-"), "v")
	if !versionRE.MatchString(s) {
		return "", fmt.Errorf("invalid Node version; expected e.g. 24.16.0")
	}
	return s, nil
}
func newer(a, b string) bool {
	aa, bb := strings.Split(a, "."), strings.Split(b, ".")
	for i := range aa {
		x, _ := strconv.Atoi(aa[i])
		y, _ := strconv.Atoi(bb[i])
		if x != y {
			return x > y
		}
	}
	return false
}
func validMajor(s string) bool { return s == "24" || s == "25" || s == "26" }

type options struct {
	version, major, location string
	accept                   bool
}

func parseOptions(args []string) (options, error) {
	o := options{location: "atm"}
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		key, val, eq := strings.Cut(args[i], "=")
		if seen[key] {
			return o, fmt.Errorf("duplicate option %s", key)
		}
		seen[key] = true
		if key == "--accept-system-packages" && !eq {
			o.accept = true
			continue
		}
		if key != "--version" && key != "--major" && key != "--location" {
			return o, fmt.Errorf("unknown option %s", key)
		}
		if !eq {
			i++
			if i >= len(args) {
				return o, fmt.Errorf("missing value for %s", key)
			}
			val = args[i]
		}
		switch key {
		case "--version":
			v, err := normalizeVersion(val)
			if err != nil {
				return o, err
			}
			o.version = v
		case "--major":
			if !validMajor(val) {
				return o, fmt.Errorf("major must be 24, 25 or 26")
			}
			o.major = val
		case "--location":
			o.location = val
		}
	}
	if o.location != "atm" && o.location != "system" {
		return o, fmt.Errorf("location must be atm or system")
	}
	if o.version != "" && o.major != "" {
		return o, fmt.Errorf("choose version or major, not both")
	}
	if o.location == "system" && (o.version != "" || o.major != "") {
		return o, fmt.Errorf("APT does not accept portable version/major selection")
	}
	if o.location == "atm" && o.accept {
		return o, fmt.Errorf("system consent flag requires system location")
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

func (b *backend) fetch(ctx context.Context, suffix string, dst io.Writer, max int64) error {
	base, err := url.Parse(b.base)
	if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil {
		return fmt.Errorf("HTTPS release origin required")
	}
	c := *b.client
	c.Timeout = 5 * time.Minute
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || req.URL.Scheme != "https" || req.URL.Host != base.Host || req.URL.User != nil {
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
func (b *backend) latest(ctx context.Context, major string) ([]string, error) {
	if !validMajor(major) {
		return nil, fmt.Errorf("unsupported major")
	}
	var data strings.Builder
	if err := b.fetch(ctx, "/index.json", &data, 8<<20); err != nil {
		return nil, err
	}
	var rows []struct {
		Version string   `json:"version"`
		Files   []string `json:"files"`
	}
	if err := json.Unmarshal([]byte(data.String()), &rows); err != nil {
		return nil, err
	}
	versions := []string{}
	seen := map[string]bool{}
	for _, row := range rows {
		v, err := normalizeVersion(row.Version)
		if err != nil || !strings.HasPrefix(v, major+".") || seen[v] {
			continue
		}
		available := false
		for _, f := range row.Files {
			if f == b.platform {
				available = true
			}
		}
		if !available {
			continue
		}
		seen[v] = true
		versions = append(versions, v)
	}
	sort.Slice(versions, func(i, j int) bool { return newer(versions[i], versions[j]) })
	if len(versions) == 0 {
		return nil, fmt.Errorf("no official releases for major %s on %s", major, b.platform)
	}
	return versions[:min(3, len(versions))], nil
}
func checksum(data, filename string) (string, error) {
	found := ""
	for _, line := range strings.Split(data, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return "", fmt.Errorf("malformed checksum list")
		}
		raw, err := hex.DecodeString(fields[0])
		if err != nil || len(raw) != sha256.Size {
			return "", fmt.Errorf("malformed SHA-256")
		}
		name := strings.TrimPrefix(fields[1], "*")
		if name == filename {
			if found != "" {
				return "", fmt.Errorf("duplicate artifact checksum")
			}
			found = strings.ToLower(fields[0])
		}
	}
	if found == "" {
		return "", fmt.Errorf("artifact missing from official checksums")
	}
	return found, nil
}
func (b *backend) spec(ctx context.Context, v string) (install.Spec, error) {
	name := "node-v" + v + "-" + b.platform + ".tar.gz"
	var data strings.Builder
	err := b.fetch(ctx, "/v"+v+"/SHASUMS256.txt", &data, 1<<20)
	if err != nil {
		return install.Spec{}, err
	}
	sum, err := checksum(data.String(), name)
	if err != nil {
		return install.Spec{}, err
	}
	origin, _ := url.Parse(b.base)
	return install.Spec{Version: v, URL: b.base + "/v" + v + "/" + name, SHA256: sum, Format: "tar.gz", StripPrefix: strings.TrimSuffix(name, ".tar.gz"), Origins: []string{origin.Scheme + "://" + origin.Host}}, nil
}

func (b *backend) menu(ctx context.Context, args []string) ([]choice, error) {
	add := func(id, label, op string, args ...string) choice {
		return choice{ID: id, Label: label, Operation: op, Arguments: args}
	}
	custom := choice{ID: "custom", Label: label("custom"), Operation: "install", Arguments: []string{"--version"}, Prompt: label("version_prompt")}
	if len(args) == 2 && args[0] == "--major" {
		vs, err := b.latest(ctx, args[1])
		if err != nil {
			return nil, err
		}
		out := []choice{}
		for _, v := range vs {
			out = append(out, add(v, label("install_version", v), "install", "--version", v))
		}
		return append(out, custom), nil
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
			if len(out) == 100 {
				return nil, fmt.Errorf("more than 100 versions; use CLI exact version")
			}
			out = append(out, add(v, label(args[1]+"_version", v), args[1], v))
		}
		if len(out) == 0 {
			b.log(label("empty"))
		}
		return out, nil
	}
	if len(args) == 1 && args[0] == "--locations" {
		return []choice{add("atm", label("atm"), "install", "--location", "atm"), add("system", label("system"), "install", "--location", "system", "--accept-system-packages")}, nil
	}
	if len(args) != 0 {
		return nil, fmt.Errorf("invalid menu page")
	}
	return []choice{add("24", label("major", "24"), "menu", "--major", "24"), add("25", label("major", "25"), "menu", "--major", "25"), add("26", label("major", "26"), "menu", "--major", "26"), custom, add("locations", label("locations"), "menu", "--locations"), add("status", label("status"), "status"), add("use", label("use"), "menu", "--installed", "use"), add("remove", label("remove"), "menu", "--installed", "remove"), add("uninstall", label("uninstall"), "uninstall"), add("integrate", label("integrate"), "integrate")}, nil
}
