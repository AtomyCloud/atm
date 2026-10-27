// Package catalog reads only the official main/plugins catalog; it never executes
// or installs code. Git blob identities bind metadata to a single main commit.
package catalog

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/AtomyCloud/atm/internal/data"
	"github.com/AtomyCloud/atm/internal/plugin"
	"github.com/AtomyCloud/atm/internal/protocol"
)

const Source = "https://github.com/AtomyCloud/atm/tree/main/plugins"
const api = "https://api.github.com/repos/AtomyCloud/atm"
const maxResponse = 16 << 20
const Timeout = 90 * time.Second

var shaPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

type Entry struct {
	plugin.Manifest
	Paths []string `json:"paths"`
	Local bool     `json:"local"`
}

type Snapshot struct {
	Source  string  `json:"source"`
	Commit  string  `json:"commit"`
	Entries []Entry `json:"entries"`
}

type Client struct {
	http  *http.Client
	token string
}

func New(token string) *Client {
	return &Client{token: token, http: &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return fmt.Errorf("official catalog redirects are not permitted")
		},
	}}
}

func (c *Client) get(ctx context.Context, route string, limit int64, dst any) error {
	for _, r := range c.token {
		if r < 33 || r > 126 {
			return fmt.Errorf("GITHUB_TOKEN contains invalid characters")
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, api+route, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "ATM-native-catalog/0.0.2")
	req.Header.Set("Accept", "application/vnd.github+json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("cannot reach the official GitHub catalog (network, timeout or redirect)")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == 403 || resp.StatusCode == 429 {
			return fmt.Errorf("GitHub access denied or rate limit reached; try later or set GITHUB_TOKEN")
		}
		return fmt.Errorf("official GitHub catalog HTTP status %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return fmt.Errorf("cannot read official catalog response")
	}
	if int64(len(b)) > limit {
		return fmt.Errorf("catalog response exceeds size limit")
	}
	return json.Unmarshal(b, dst)
}

type treeEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
	Size *int64 `json:"size"`
}

func safePath(path string) bool {
	if !utf8.ValidString(path) || len(path) > 4096 || strings.Contains(path, "\\") {
		return false
	}
	for _, p := range strings.Split(path, "/") {
		if p == "" || p == "." || p == ".." || strings.EqualFold(p, ".git") {
			return false
		}
	}
	for _, r := range path {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}

func (c *Client) blob(ctx context.Context, entry treeEntry) ([]byte, error) {
	if entry.Type != "blob" || (entry.Mode != "100644" && entry.Mode != "100755") ||
		!shaPattern.MatchString(entry.SHA) || entry.Size == nil || *entry.Size < 0 || *entry.Size > data.MaxFileSize {
		return nil, fmt.Errorf("invalid catalog metadata blob: %s", entry.Path)
	}
	var value struct {
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
	}
	if err := c.get(ctx, "/git/blobs/"+entry.SHA, 2*data.MaxFileSize, &value); err != nil {
		return nil, err
	}
	if value.Encoding != "base64" {
		return nil, fmt.Errorf("invalid catalog blob encoding")
	}
	b, err := base64.StdEncoding.DecodeString(value.Content)
	if err != nil || int64(len(b)) != *entry.Size {
		return nil, fmt.Errorf("catalog blob size or encoding mismatch")
	}
	// SHA-1 is Git's object identity, not a signature or a release trust root.
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(b))
	h.Write(b)
	if fmt.Sprintf("%x", h.Sum(nil)) != entry.SHA {
		return nil, fmt.Errorf("catalog Git blob checksum mismatch")
	}
	if !utf8.Valid(b) {
		return nil, fmt.Errorf("catalog metadata must be UTF-8")
	}
	return b, nil
}

func parseManifest(b []byte, id string, legacy bool) (plugin.Manifest, error) {
	var m plugin.Manifest
	if legacy {
		values, err := data.ParseAssignments(b, id+"/plugin.metadata")
		if err != nil {
			return m, err
		}
		for key := range values {
			if !strings.HasPrefix(key, "ATM_PLUGIN_") {
				return m, fmt.Errorf("unexpected catalog metadata key")
			}
		}
		m, err = plugin.LegacyManifest(values)
		if err != nil {
			return m, err
		}
	} else {
		d := json.NewDecoder(bytes.NewReader(b))
		d.DisallowUnknownFields()
		if err := d.Decode(&m); err != nil {
			return m, err
		}
		if d.Decode(new(any)) != io.EOF {
			return m, fmt.Errorf("unexpected trailing plugin JSON")
		}
	}
	if err := m.Validate(id); err != nil {
		return m, err
	}
	for _, s := range []string{m.Name, m.Version, m.Description} {
		for _, r := range s {
			if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
				return m, fmt.Errorf("control character in catalog metadata")
			}
		}
	}
	return m, nil
}

func (c *Client) Search(ctx context.Context, query string) (Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	var tip struct {
		SHA string `json:"sha"`
	}
	if err := c.get(ctx, "/commits/main", data.MaxFileSize, &tip); err != nil {
		return Snapshot{}, err
	}
	if !shaPattern.MatchString(tip.SHA) {
		return Snapshot{}, fmt.Errorf("invalid official main commit")
	}
	var tree struct {
		Truncated *bool       `json:"truncated"`
		Tree      []treeEntry `json:"tree"`
	}
	if err := c.get(ctx, "/git/trees/"+tip.SHA+"?recursive=1", maxResponse, &tree); err != nil {
		return Snapshot{}, err
	}
	if tree.Truncated == nil || *tree.Truncated || tree.Tree == nil || len(tree.Tree) > 50000 {
		return Snapshot{}, fmt.Errorf("incomplete or oversized official repository tree")
	}
	files := map[string]map[string]treeEntry{}
	seen := map[string]bool{}
	for _, entry := range tree.Tree {
		if !strings.HasPrefix(entry.Path, "plugins/") {
			continue
		}
		if !safePath(entry.Path) || seen[strings.ToLower(entry.Path)] {
			return Snapshot{}, fmt.Errorf("unsafe or duplicate catalog path")
		}
		seen[strings.ToLower(entry.Path)] = true
		parts := strings.Split(entry.Path, "/")
		if len(parts) < 3 || !plugin.IDPattern.MatchString(parts[1]) || entry.Type == "tree" {
			continue
		}
		id, rel := parts[1], strings.Join(parts[2:], "/")
		if files[id] == nil {
			files[id] = map[string]treeEntry{}
		}
		files[id][rel] = entry
	}
	ids := []string{}
	for id, group := range files {
		_, jsonOK := group["plugin.json"]
		_, legacyOK := group["plugin.metadata"]
		if jsonOK || legacyOK {
			ids = append(ids, id)
		}
	}
	if len(ids) > 200 {
		return Snapshot{}, fmt.Errorf("catalog exceeds 200 plugins")
	}
	sort.Strings(ids)
	result := Snapshot{Source: Source, Commit: tip.SHA, Entries: []Entry{}}
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, err
		}
		entry, hasJSON := files[id]["plugin.json"]
		if !hasJSON {
			entry = files[id]["plugin.metadata"]
		}
		b, err := c.blob(ctx, entry)
		if err != nil {
			return Snapshot{}, err
		}
		m, err := parseManifest(b, id, !hasJSON)
		if err != nil {
			return Snapshot{}, fmt.Errorf("catalog plugin %s: %s", id, protocol.SafeText(err.Error()))
		}
		paths := []string{}
		for _, file := range files[id] {
			paths = append(paths, file.Path)
		}
		sort.Strings(paths)
		result.Entries = append(result.Entries, Entry{Manifest: m, Paths: paths})
	}
	result.Entries = result.Filter(query, "default")
	return result, nil
}

// Filter searches metadata and file names, never script/file bodies.
func (s Snapshot) Filter(query, mode string) []Entry {
	query = strings.ToLower(query)
	entries := []Entry{}
	for _, e := range s.Entries {
		if strings.Contains(strings.ToLower(e.ID+" "+e.Name+" "+e.Description+" "+strings.Join(e.Paths, " ")), query) {
			entries = append(entries, e)
		}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if mode == "default" && a.Order != b.Order {
			return a.Order < b.Order
		}
		if mode == "az" || mode == "za" {
			x, y := strings.ToLower(a.Name), strings.ToLower(b.Name)
			if x != y {
				if mode == "za" {
					return x > y
				}
				return x < y
			}
		}
		return a.ID < b.ID
	})
	return entries
}

func (s *Snapshot) MarkLocal(plugins []plugin.Plugin) {
	ids := map[string]bool{}
	for _, p := range plugins {
		ids[p.ID] = true
	}
	for i := range s.Entries {
		s.Entries[i].Local = ids[s.Entries[i].ID]
	}
}
