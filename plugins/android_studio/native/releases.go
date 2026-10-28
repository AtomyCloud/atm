package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/AtomyCloud/atm/internal/install"
	"github.com/AtomyCloud/atm/internal/protocol"
)

type choice = protocol.Choice

var limits = install.Limits{DownloadBytes: 3 << 30, ExpandedBytes: 8 << 30, FileBytes: 2 << 30, Entries: 150000}
var versionRE = regexp.MustCompile(`^[0-9]{4}(\.[0-9]{1,6}){3}$`)
var idRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
var slotRE = regexp.MustCompile(`^studio_[a-z0-9._-]{1,150}_[0-9]{4}(\.[0-9]{1,6}){3}$`)
var slugReplace = regexp.MustCompile(`[^a-z0-9._-]+`)

const officialBase = "https://edgedl.me.gvt1.com/android/studio/ide-zips/"

type artifact struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
	Package string `json:"package"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
}

func slug(s string) string {
	return strings.Trim(slugReplace.ReplaceAllString(strings.ToLower(s), "_"), "_")
}
func packageName(s string) bool   { return path.Base(s) == s && strings.HasSuffix(s, ".tar.gz") }
func (a artifact) dir() string    { return "studio_" + slug(a.Name) + "_" + a.Version }
func (a artifact) target() string { return a.ID + "/" + a.Version }
func safeText(s string, max int) bool {
	if strings.TrimSpace(s) != s || len(s) == 0 || len(s) > max {
		return false
	}
	for _, r := range s {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}
func httpsURL(s string) (*url.URL, error) {
	u, e := url.Parse(s)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" || strings.ContainsAny(s, "\r\n\x00") {
		return nil, fmt.Errorf("plain HTTPS archive URL required (no credentials/query/fragment)")
	}
	return u, nil
}
func (a artifact) validate() error {
	if !idRE.MatchString(a.ID) || !versionRE.MatchString(a.Version) || !safeText(a.Name, 120) || !slotRE.MatchString(a.dir()) || slug(a.Name) == "" {
		return fmt.Errorf("invalid Android Studio id/name/version")
	}
	if path.Base(a.Package) != a.Package || !safeArchivePath(a.Package) || !strings.HasSuffix(a.Package, ".tar.gz") {
		return fmt.Errorf("safe tar.gz package filename required")
	}
	if len(a.SHA256) != 64 || strings.Trim(a.SHA256, "0123456789abcdef") != "" {
		return fmt.Errorf("explicit 64-character SHA-256 required")
	}
	_, err := httpsURL(a.URL)
	return err
}

// Pinned legacy selectors; hashes verified against the official archive on
// 2026-10-28. These are not presented as the latest Android Studio releases.
var catalog = []artifact{
	{ID: "panda4", Name: "Android Studio Panda 4", Version: "2025.3.4.6", Package: "android-studio-panda4-linux.tar.gz", SHA256: "32a7ff09acaa38b48d61c8882bee7e213022a8ba0d1f16c0073380facb509fd3"},
	{ID: "quail1_canary2", Name: "Android Studio Quail 1 Canary 2", Version: "2026.1.1.2", Package: "android-studio-quail1-canary2-linux.tar.gz", SHA256: "8dd6f1c8aed5e04248e6c0a4df5a28efb947eae5d33344239635205a7e419773"},
	{ID: "quail1_canary1", Name: "Android Studio Quail 1 Canary 1", Version: "2026.1.1.1", Package: "android-studio-quail1-canary1-linux.tar.gz", SHA256: "36bed5e81e2c119adcc2431d2a87af2c49322076b0c54c7f7a33749c12f8296d"},
	{ID: "panda4_rc1", Name: "Android Studio Panda 4 RC 1", Version: "2025.3.4.5", Package: "android-studio-panda4-rc1-linux.tar.gz", SHA256: "36236ad63fabccf5382ac2ca63299f8b9fd5c76067de12bc237395214165a17d"},
	{ID: "panda3_patch1", Name: "Android Studio Panda 3 Patch 1", Version: "2025.3.3.7", Package: "android-studio-panda3-patch1-linux.tar.gz", SHA256: "153009f6b64fc0b80803fb8f2a5e1df61681c4b2f83b667e1fcb18e91a9a78e0"},
}

type options = artifact

func parseOptions(args []string) (options, error) {
	values := map[string]string{}
	for i := 0; i < len(args); i++ {
		k, v, eq := strings.Cut(args[i], "=")
		switch k {
		case "--id", "--name", "--version", "--package", "--url", "--sha256":
		default:
			return artifact{}, fmt.Errorf("unknown install option %s", k)
		}
		if _, ok := values[k]; ok {
			return artifact{}, fmt.Errorf("duplicate install option")
		}
		if !eq {
			i++
			if i == len(args) {
				return artifact{}, fmt.Errorf("missing %s value", k)
			}
			v = args[i]
		}
		if v == "" {
			return artifact{}, fmt.Errorf("empty install value")
		}
		values[k] = v
	}
	id := values["--id"]
	if id == "" {
		id = "panda4"
	}
	if len(values) == 0 || (len(values) == 1 && values["--id"] != "") {
		for _, a := range catalog {
			if a.ID == id {
				a.URL = officialBase + a.Version + "/" + a.Package
				return a, a.validate()
			}
		}
		return artifact{}, fmt.Errorf("unknown selector; provide complete custom archive options")
	}
	if values["--id"] == "" {
		id = "custom"
	}
	a := artifact{ID: id, Name: values["--name"], Version: values["--version"], Package: values["--package"], URL: values["--url"], SHA256: strings.ToLower(values["--sha256"])}
	if a.Package == "" {
		a.Package = slug(a.Name) + "-" + a.Version + "-linux.tar.gz"
	}
	return a, a.validate()
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
func (b *backend) fetch(ctx context.Context, address string, dst io.Writer) error {
	u, err := httpsURL(address)
	if err != nil {
		return err
	}
	c := *b.client
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		next, e := httpsURL(req.URL.String())
		if e != nil || len(via) >= 5 || next.Host != u.Host || next.EscapedPath() != u.EscapedPath() {
			return fmt.Errorf("archive redirect rejected")
		}
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || resp.ContentLength > limits.DownloadBytes {
		return fmt.Errorf("archive response refused (%d)", resp.StatusCode)
	}
	if v := resp.Header.Get("Content-Encoding"); v != "" && v != "identity" {
		return fmt.Errorf("encoded archive response refused")
	}
	n, err := io.Copy(dst, io.LimitReader(contextReader{ctx, resp.Body}, limits.DownloadBytes+1))
	if err != nil {
		return err
	}
	if n <= 0 || n > limits.DownloadBytes {
		return fmt.Errorf("archive download limit exceeded or empty")
	}
	return ctx.Err()
}
