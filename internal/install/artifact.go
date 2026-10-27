// Package install provides opt-in native installation services, not application policy.
package install

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type Limits struct {
	DownloadBytes int64
	ExpandedBytes int64
	FileBytes     int64
	Entries       int
}

func (l Limits) defaults() Limits {
	if l.DownloadBytes <= 0 {
		l.DownloadBytes = 256 << 20
	}
	if l.ExpandedBytes <= 0 {
		l.ExpandedBytes = 1 << 30
	}
	if l.FileBytes <= 0 {
		l.FileBytes = 256 << 20
	}
	if l.Entries <= 0 {
		l.Entries = 10000
	}
	return l
}

type Spec struct {
	Version     string   `json:"version"`
	URL         string   `json:"url"`
	SHA256      string   `json:"sha256"`
	Format      string   `json:"format"`
	StripPrefix string   `json:"strip_prefix,omitempty"`
	Origins     []string `json:"origins"`
}

// Archive paths intentionally use an ASCII portable subset. Root paths can be Unicode.
func portablePath(name string) bool {
	if !fs.ValidPath(name) || name == "." || len(name) > 240 || strings.Count(name, "/") >= 16 {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
			return false
		}
		base, _, _ := strings.Cut(strings.ToUpper(part), ".")
		if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || (len(base) == 4 && (base[:3] == "COM" || base[:3] == "LPT") && base[3] >= '1' && base[3] <= '9') {
			return false
		}
		for _, c := range part {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("._-+@() ", c)) {
				return false
			}
		}
	}
	return true
}

func validVersion(v string) bool {
	if len(v) == 0 || len(v) > 64 || !portablePath(v) {
		return false
	}
	for i, c := range v {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || i > 0 && strings.ContainsRune("._+-", c)) {
			return false
		}
	}
	return true
}

func origin(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return "", fmt.Errorf("artifact URLs require HTTPS without credentials or fragments")
	}
	return "https://" + strings.ToLower(u.Host), nil
}

func (s Spec) validate() error {
	if !validVersion(s.Version) || (s.Format != "tar.gz" && s.Format != "zip") || (s.StripPrefix != "" && !portablePath(s.StripPrefix)) {
		return fmt.Errorf("invalid version, archive format or prefix")
	}
	b, err := hex.DecodeString(s.SHA256)
	if err != nil || len(b) != 32 || strings.ToLower(s.SHA256) != s.SHA256 {
		return fmt.Errorf("a pinned lowercase SHA-256 is required")
	}
	if len(s.Origins) == 0 {
		return fmt.Errorf("explicit trusted HTTPS origins required")
	}
	for _, o := range s.Origins {
		canonical, err := origin(o)
		if err != nil || canonical != o {
			return fmt.Errorf("origins must be canonical https://host[:port] values")
		}
	}
	return s.allow(s.URL)
}

func (s Spec) allow(raw string) error {
	o, err := origin(raw)
	if err != nil {
		return err
	}
	for _, allowed := range s.Origins {
		if o == allowed {
			return nil
		}
	}
	return fmt.Errorf("artifact origin is not permitted: %s", o)
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

func download(ctx context.Context, r *os.Root, dest string, s Spec, l Limits, client *http.Client) error {
	c := http.Client{Timeout: 5 * time.Minute}
	if client != nil {
		c = *client
		if c.Timeout <= 0 || c.Timeout > 5*time.Minute {
			c.Timeout = 5 * time.Minute
		}
	}
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("too many artifact redirects")
		}
		return s.allow(req.URL.String())
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.ContentLength > l.DownloadBytes {
		return fmt.Errorf("artifact response rejected: status=%d length=%d", resp.StatusCode, resp.ContentLength)
	}
	if ce := resp.Header.Get("Content-Encoding"); ce != "" && ce != "identity" {
		return fmt.Errorf("encoded HTTP artifact response refused")
	}
	f, err := r.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(f, h), io.LimitReader(contextReader{ctx, resp.Body}, l.DownloadBytes+1))
	if copyErr == nil {
		copyErr = f.Sync()
	}
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if n > l.DownloadBytes || n == 0 || hex.EncodeToString(h.Sum(nil)) != s.SHA256 {
		return fmt.Errorf("artifact length or SHA-256 mismatch")
	}
	return ctx.Err()
}
