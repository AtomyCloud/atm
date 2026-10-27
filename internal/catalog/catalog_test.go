package catalog

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AtomyCloud/atm/internal/plugin"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type fixture struct {
	client    *Client
	tree      []treeEntry
	blobs     map[string][]byte
	calls     []string
	revision  string
	truncated bool
	status    int
	corrupt   bool
	oversized bool
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{client: New("test-token"), blobs: map[string][]byte{}, revision: strings.Repeat("a", 40)}
	f.client.http.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "api.github.com" || !strings.HasPrefix(r.URL.Path, "/repos/AtomyCloud/atm/") {
			t.Fatalf("non-official request: %s", r.URL)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatal("missing API token")
		}
		f.calls = append(f.calls, r.URL.String())
		var body any
		switch {
		case strings.HasSuffix(r.URL.Path, "/commits/main"):
			body = map[string]any{"sha": f.revision}
		case strings.Contains(r.URL.Path, "/git/trees/"):
			if !strings.HasSuffix(r.URL.Path, "/"+f.revision) || r.URL.RawQuery != "recursive=1" {
				t.Fatal("tree not pinned")
			}
			body = map[string]any{"truncated": f.truncated, "tree": f.tree}
		case strings.Contains(r.URL.Path, "/git/blobs/"):
			sha := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
			b, ok := f.blobs[sha]
			if !ok {
				t.Fatalf("unexpected blob: %s", sha)
			}
			if f.corrupt {
				b = bytes.Repeat([]byte("!"), len(b))
			}
			body = map[string]any{"encoding": "base64", "content": base64.StdEncoding.EncodeToString(b)}
		default:
			t.Fatalf("unexpected route %s", r.URL)
		}
		b, _ := json.Marshal(body)
		if f.oversized {
			b = bytes.Repeat([]byte(" "), (1<<20)+1)
		}
		status := http.StatusOK
		if f.status != 0 {
			status = f.status
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(b)), Header: http.Header{}}, nil
	})
	return f
}

func (f *fixture) add(path string, b []byte) {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(b))
	h.Write(b)
	sha, size := fmt.Sprintf("%x", h.Sum(nil)), int64(len(b))
	f.blobs[sha] = b
	f.tree = append(f.tree, treeEntry{Path: path, SHA: sha, Type: "blob", Mode: "100644", Size: &size})
}

func manifest(id, name string, order int) []byte {
	b, _ := json.Marshal(plugin.Manifest{Schema: 1, ID: id, Name: name, Version: "0.0.1", Description: "Fixture tool",
		Order: order, MinAPI: 1, Platforms: []string{"linux"}, Executor: "legacy-bash", Protocol: "tty-v1", Operations: []string{"menu"}})
	return b
}

func TestPinnedSearchFilterSortAndLocal(t *testing.T) {
	f := newFixture(t)
	f.add("plugins/alpha/plugin.json", manifest("alpha", "Zulu", 2))
	f.add("plugins/beta/plugin.json", manifest("beta", "Alpha", 1))
	f.add("plugins/alpha/unique-file.txt", []byte("not fetched or searched"))
	s, err := f.client.Search(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if s.Source != Source || s.Commit != f.revision || len(f.calls) != 4 {
		t.Fatal(s, f.calls)
	}
	if s.Entries[0].ID != "beta" || len(s.Filter("UNIQUE-FILE", "az")) != 1 || len(s.Filter("not fetched", "az")) != 0 {
		t.Fatal(s)
	}
	if s.Filter("", "za")[0].ID != "alpha" {
		t.Fatal("wrong reverse order")
	}
	s.MarkLocal([]plugin.Plugin{{Manifest: plugin.Manifest{ID: "alpha"}, Enabled: false}})
	if !s.Filter("Zulu", "default")[0].Local || s.Entries[0].Local {
		t.Fatal("incorrect local/disabled presence")
	}
	if len(s.Filter("does-not-exist", "default")) != 0 {
		t.Fatal("unexpected match")
	}
}

func TestRejectBadRepositoryResponses(t *testing.T) {
	for _, name := range []string{"invalid-commit", "truncated", "duplicate", "case-collision", "traversal", "controls", "bad-hash", "symlink", "missing-size", "negative-size", "wrong-id", "unknown-field", "trailing-json", "bad-utf8", "rate-limit", "not-found", "oversized"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			b := manifest("alpha", "Alpha", 1)
			switch name {
			case "wrong-id":
				b = manifest("wrong", "Alpha", 1)
			case "unknown-field":
				b = append(b[:len(b)-1], []byte(`,"unknown":true}`)...)
			case "trailing-json":
				b = append(b, []byte(` {}`)...)
			case "bad-utf8":
				b = []byte{0xff}
			}
			f.add("plugins/alpha/plugin.json", b)
			switch name {
			case "invalid-commit":
				f.revision = "main"
			case "truncated":
				f.truncated = true
			case "duplicate":
				f.tree = append(f.tree, f.tree[0])
			case "case-collision":
				e := f.tree[0]
				e.Path = "plugins/alpha/PLUGIN.JSON"
				f.tree = append(f.tree, e)
			case "traversal":
				f.tree[0].Path = "plugins/alpha/../plugin.json"
			case "controls":
				f.tree[0].Path = "plugins/alpha/\x1bplugin.json"
			case "bad-hash":
				f.corrupt = true
			case "symlink":
				f.tree[0].Mode = "120000"
			case "missing-size":
				f.tree[0].Size = nil
			case "negative-size":
				n := int64(-1)
				f.tree[0].Size = &n
			case "rate-limit":
				f.status = 429
			case "not-found":
				f.status = 404
			case "oversized":
				f.oversized = true
			}
			s, err := f.client.Search(context.Background(), "")
			if err == nil || s.Commit != "" || len(s.Entries) != 0 {
				t.Fatal("accepted invalid/partial snapshot", s, err)
			}
			if strings.Contains(err.Error(), "test-token") {
				t.Fatal("credential leak")
			}
		})
	}
}

func TestLegacyMetadataNeverExecutes(t *testing.T) {
	for _, suffix := range []string{"", "\necho unsafe", "\nBAD=$(touch marker)", "\nBAD=`touch marker`"} {
		f := newFixture(t)
		f.add("plugins/alpha/plugin.metadata", []byte("ATM_PLUGIN_ID=alpha\nATM_PLUGIN_NAME_VALUE='Alpha tool'\nATM_PLUGIN_VERSION_VALUE=0.0.1\n"+suffix))
		s, err := f.client.Search(context.Background(), "")
		if suffix == "" {
			if err != nil || len(s.Entries) != 1 || s.Entries[0].Name != "Alpha tool" {
				t.Fatal(s, err)
			}
		} else if err == nil {
			t.Fatal("accepted executable metadata")
		}
	}
}

func TestRepositoryManifestsAndMetadata(t *testing.T) {
	dirs, err := os.ReadDir("../../plugins")
	if err != nil {
		t.Fatal(err)
	}
	f := newFixture(t)
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		for _, file := range []string{"plugin.json", "plugin.metadata"} {
			b, err := os.ReadFile(filepath.Join("../../plugins", dir.Name(), file))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := parseManifest(b, dir.Name(), file == "plugin.metadata"); err != nil {
				t.Fatal(dir.Name(), file, err)
			}
			f.add("plugins/"+dir.Name()+"/"+file, b)
		}
	}
	s, err := f.client.Search(context.Background(), "")
	if err != nil || len(s.Entries) != 16 {
		t.Fatal("real repository contracts", len(s.Entries), err)
	}
}

func TestCancellationAndTokenAndRedirect(t *testing.T) {
	c := New("bad\ntoken")
	c.http.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		t.Fatal("invalid token contacted network")
		return nil, nil
	})
	if _, err := c.Search(context.Background(), ""); err == nil {
		t.Fatal("accepted bad token")
	}
	c = New("secret")
	c.http.Transport = transportFunc(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Search(ctx, ""); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	c.http.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.github.com" {
			t.Fatal("followed external redirect")
		}
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"https://example.invalid/steal"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	if _, err := c.Search(context.Background(), ""); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal("redirect or token handling", err)
	}
}
