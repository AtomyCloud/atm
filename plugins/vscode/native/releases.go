package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const apiBase = "https://update.code.visualstudio.com"

var versionRE = regexp.MustCompile(`^[0-9]{1,4}\.[0-9]{1,4}\.[0-9]{1,4}$`)
var slotRE = regexp.MustCompile(`^code_[0-9]{1,4}\.[0-9]{1,4}\.[0-9]{1,4}$`)
var hashRE = regexp.MustCompile(`^[0-9a-f]{64}$`)
var commitRE = regexp.MustCompile(`^[0-9a-f]{40}$`)
var limits = struct {
	ArchiveBytes, ExpandedBytes, FileBytes int64
	Entries                                int
}{1 << 30, 4 << 30, 1 << 30, 100000}

type artifact struct {
	Version  string `json:"version"`
	Commit   string `json:"commit"`
	Platform string `json:"platform"`
	URL      string `json:"url"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
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
func normalizeVersion(v string) (string, error) {
	v = strings.TrimPrefix(v, "v")
	if strings.Count(v, ".") == 1 {
		v += ".0"
	}
	if !versionRE.MatchString(v) {
		return "", fmt.Errorf("expected stable version such as 1.118.1")
	}
	for _, p := range strings.Split(v, ".") {
		if len(p) > 1 && p[0] == '0' {
			return "", fmt.Errorf("noncanonical version")
		}
	}
	return v, nil
}
func slot(v string) (string, error) {
	v = strings.TrimPrefix(v, "code_")
	v, err := normalizeVersion(v)
	return "code_" + v, err
}
func parseVersionOptions(a []string) (string, error) {
	if len(a) == 0 {
		return "", nil
	}
	if len(a) == 1 {
		return normalizeVersion(strings.TrimPrefix(a[0], "--version="))
	}
	if len(a) == 2 && a[0] == "--version" {
		return normalizeVersion(a[1])
	}
	return "", fmt.Errorf("expected optional --version VERSION")
}
func newer(a, b string) bool {
	aa, bb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < 3; i++ {
		x, _ := strconv.Atoi(aa[i])
		y, _ := strconv.Atoi(bb[i])
		if x != y {
			return x > y
		}
	}
	return false
}
func approvedURL(raw string, artifact bool) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" {
		return false
	}
	if artifact {
		return u.Host == "vscode.download.prss.microsoft.com" && strings.HasPrefix(u.Path, "/dbazure/download/stable/") && strings.HasSuffix(u.Path, ".tar.gz")
	}
	return u.Host == "update.code.visualstudio.com" && strings.HasPrefix(u.Path, "/api/")
}
func (b *backend) request(ctx context.Context, method, raw string, artifact bool) (*http.Response, error) {
	if !approvedURL(raw, artifact) {
		return nil, fmt.Errorf("unapproved official URL")
	}
	client := *b.client
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 3 || !approvedURL(req.URL.String(), artifact) {
			return fmt.Errorf("unapproved redirect")
		}
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, method, raw, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ATM/0.0.2")
	req.Header.Set("Accept-Encoding", "identity")
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != 200 || res.Header.Get("Content-Encoding") != "" {
		res.Body.Close()
		return nil, fmt.Errorf("official endpoint returned status %d or encoded content", res.StatusCode)
	}
	return res, nil
}
func (b *backend) json(ctx context.Context, raw string, value any) error {
	res, err := b.request(ctx, "GET", raw, false)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(contextReader{ctx, res.Body}, (2<<20)+1))
	if err != nil {
		return err
	}
	if len(data) > 2<<20 {
		return fmt.Errorf("official metadata too large")
	}
	return json.Unmarshal(data, value)
}
func (b *backend) releases(ctx context.Context) ([]string, error) {
	var vs []string
	if err := b.json(ctx, apiBase+"/api/releases/stable", &vs); err != nil {
		return nil, err
	}
	if len(vs) > 10000 {
		return nil, fmt.Errorf("too many release entries")
	}
	seen := map[string]bool{}
	out := []string{}
	for _, v := range vs {
		n, err := normalizeVersion(v)
		if err == nil && n == v && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no stable releases found")
	}
	sort.Slice(out, func(i, j int) bool { return newer(out[i], out[j]) })
	return out, nil
}
func (b *backend) resolve(ctx context.Context, v string) (artifact, error) {
	if v == "" {
		vs, err := b.releases(ctx)
		if err != nil {
			return artifact{}, err
		}
		v = vs[0]
	}
	var m struct {
		URL, Name, Version, ProductVersion, SHA256Hash string
		Size                                           int64
	}
	if err := b.json(ctx, apiBase+"/api/versions/"+v+"/"+b.platform+"/stable", &m); err != nil {
		return artifact{}, err
	}
	if m.Name != v || m.ProductVersion != v || !commitRE.MatchString(m.Version) || !hashRE.MatchString(m.SHA256Hash) || !approvedURL(m.URL, true) {
		return artifact{}, fmt.Errorf("invalid official release identity/hash/URL")
	}
	if !strings.Contains(m.URL, "/stable/"+m.Version+"/") {
		return artifact{}, fmt.Errorf("artifact URL build mismatch")
	}
	// The official version API currently omits size. HEAD binds the transfer size;
	// the API's SHA256 remains the authoritative integrity check.
	res, err := b.request(ctx, "HEAD", m.URL, true)
	if err != nil {
		return artifact{}, err
	}
	res.Body.Close()
	size := res.ContentLength
	if size <= 0 || size > limits.ArchiveBytes || (m.Size != 0 && m.Size != size) {
		return artifact{}, fmt.Errorf("invalid official artifact size")
	}
	return artifact{v, m.Version, b.platform, m.URL, m.SHA256Hash, size}, nil
}
