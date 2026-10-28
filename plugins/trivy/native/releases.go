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
	"strconv"
	"strings"
	"time"

	"github.com/AtomyCloud/atm/internal/install"
	"github.com/AtomyCloud/atm/internal/protocol"
)

type choice = protocol.Choice

const apiBase = "https://api.github.com/repos/aquasecurity/trivy/releases/"
const releaseBase = "https://github.com/aquasecurity/trivy/releases/download/"

var versionRE = regexp.MustCompile(`^(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})$`)
var limits = install.Limits{DownloadBytes: 256 << 20, ExpandedBytes: 1 << 30, FileBytes: 256 << 20, Entries: 10000}

func normalizeVersion(s string) (string, error) {
	s = strings.TrimPrefix(s, "v")
	if !versionRE.MatchString(s) {
		return "", fmt.Errorf("invalid Trivy version; expected X.Y.Z")
	}
	return s, nil
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

type options struct{ method, version string }

func parseOptions(args []string) (options, error) {
	o := options{version: "latest"}
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		key, val, eq := strings.Cut(args[i], "=")
		if (key != "--method" && key != "--version") || seen[key] {
			return o, fmt.Errorf("unknown or duplicate option %s", key)
		}
		seen[key] = true
		if !eq {
			i++
			if i >= len(args) {
				return o, fmt.Errorf("missing value for %s", key)
			}
			val = args[i]
		}
		if key == "--method" {
			if val != "binary" && val != "docker" {
				return o, fmt.Errorf("method must be binary or docker")
			}
			o.method = val
		} else {
			if val != "latest" {
				var err error
				val, err = normalizeVersion(val)
				if err != nil {
					return o, err
				}
			}
			o.version = val
		}
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

func storageHost(host string) bool {
	return host == "release-assets.githubusercontent.com" || host == "objects.githubusercontent.com" || host == "github-releases.githubusercontent.com"
}
func (b *backend) fetch(ctx context.Context, raw string, dst io.Writer, max int64) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.RawQuery != "" || (!strings.HasPrefix(raw, apiBase) && !strings.HasPrefix(raw, releaseBase)) {
		return fmt.Errorf("fixed official Trivy release URL required")
	}
	c := *b.client
	c.Timeout = 5 * time.Minute
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		prev := via[len(via)-1].URL
		allowed := u.Host == "github.com" && storageHost(req.URL.Host) && (prev.Host == "github.com" || storageHost(prev.Host))
		if len(via) >= 5 || req.URL.Scheme != "https" || req.URL.User != nil || req.URL.Fragment != "" || !allowed {
			return fmt.Errorf("release redirect rejected")
		}
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, "GET", raw, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("User-Agent", "ATM-Trivy/0.0.1")
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
type releaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}
type release struct {
	Tag        string         `json:"tag_name"`
	Draft      *bool          `json:"draft"`
	Prerelease *bool          `json:"prerelease"`
	Assets     []releaseAsset `json:"assets"`
}

func checksum(data, name string) (string, error) {
	found := ""
	for _, line := range strings.Split(data, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 2 {
			return "", fmt.Errorf("malformed checksum list")
		}
		sum, err := hex.DecodeString(parts[0])
		if err != nil || len(sum) != sha256.Size {
			return "", fmt.Errorf("invalid SHA-256 checksum")
		}
		if strings.TrimPrefix(parts[1], "*") == name {
			if found != "" {
				return "", fmt.Errorf("duplicate archive checksum")
			}
			found = strings.ToLower(parts[0])
		}
	}
	if found == "" {
		return "", fmt.Errorf("missing archive checksum")
	}
	return found, nil
}
func (b *backend) resolve(ctx context.Context, requested string, binary bool) (artifact, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	endpoint := apiBase + "latest"
	if requested != "latest" {
		if !versionRE.MatchString(requested) {
			return artifact{}, fmt.Errorf("invalid release version")
		}
		endpoint = apiBase + "tags/v" + requested
	}
	var data strings.Builder
	if err := b.fetch(ctx, endpoint, &data, 4<<20); err != nil {
		return artifact{}, err
	}
	var row release
	if err := json.Unmarshal([]byte(data.String()), &row); err != nil {
		return artifact{}, err
	}
	v, err := normalizeVersion(row.Tag)
	if err != nil || row.Tag != "v"+v || row.Draft == nil || *row.Draft || row.Prerelease == nil || *row.Prerelease || (requested != "latest" && v != requested) {
		return artifact{}, fmt.Errorf("expected exact stable official Trivy release")
	}
	a := artifact{Spec: install.Spec{Version: v, Format: "tar.gz", Origins: []string{"https://github.com", "https://release-assets.githubusercontent.com", "https://objects.githubusercontent.com", "https://github-releases.githubusercontent.com"}}}
	if !binary {
		return a, nil
	}
	name := "trivy_" + v + "_" + b.platform + ".tar.gz"
	checks := "trivy_" + v + "_checksums.txt"
	chosen := map[string]releaseAsset{}
	for _, asset := range row.Assets {
		if asset.Name != name && asset.Name != checks {
			continue
		}
		max := limits.DownloadBytes
		if asset.Name == checks {
			max = 1 << 20
		}
		if _, ok := chosen[asset.Name]; ok {
			return artifact{}, fmt.Errorf("duplicate release asset")
		}
		if asset.URL != releaseBase+"v"+v+"/"+asset.Name || asset.Size <= 0 || asset.Size > max {
			return artifact{}, fmt.Errorf("invalid official release asset")
		}
		chosen[asset.Name] = asset
	}
	if len(chosen) != 2 {
		return artifact{}, fmt.Errorf("required archive/checksum assets missing")
	}
	var sums strings.Builder
	if err := b.fetch(ctx, chosen[checks].URL, &sums, chosen[checks].Size); err != nil {
		return artifact{}, err
	}
	if int64(sums.Len()) != chosen[checks].Size {
		return artifact{}, fmt.Errorf("checksum asset size mismatch")
	}
	a.SHA256, err = checksum(sums.String(), name)
	if err != nil {
		return artifact{}, err
	}
	a.URL = chosen[name].URL
	a.Size = chosen[name].Size
	return a, nil
}
