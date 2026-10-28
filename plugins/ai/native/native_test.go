//go:build linux

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/AtomyCloud/atm/internal/protocol"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type fixture struct {
	b         *backend
	responses map[string][]byte
	calls     int
	latest    string
}

func fakeELF(arch, version string, appimage bool) []byte {
	data := make([]byte, 256)
	copy(data, []byte{0x7f, 'E', 'L', 'F', 2, 1, 1})
	binary.LittleEndian.PutUint16(data[16:], 2)
	machine := uint16(62)
	if arch == "arm64" {
		machine = 183
	}
	binary.LittleEndian.PutUint16(data[18:], machine)
	binary.LittleEndian.PutUint32(data[20:], 1)
	binary.LittleEndian.PutUint16(data[52:], 64)
	if appimage {
		copy(data[8:], []byte{'A', 'I', 2})
	}
	copy(data[96:], version)
	return data
}
func archive(t *testing.T, name string, data []byte, kind byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	h := &tar.Header{Name: name, Mode: 0755, Size: int64(len(data)), Typeflag: kind}
	if kind == tar.TypeSymlink {
		h.Linkname = "/tmp/outside"
		h.Size = 0
		data = nil
	}
	if err := tw.WriteHeader(h); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
func newFixture(t *testing.T, arch string) *fixture {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	f := &fixture{responses: map[string][]byte{}}
	f.b = &backend{req: protocol.Request{PluginID: "ai", Home: filepath.Join(root, "atm"), AppsDir: filepath.Join(root, "Apps")}, arch: arch, log: func(string) {}}
	f.b.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		f.calls++
		if err := r.Context().Err(); err != nil {
			return nil, err
		}
		data, ok := f.responses[r.URL.String()]
		if !ok {
			return nil, fmt.Errorf("unexpected fixture request %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(data)), ContentLength: int64(len(data)), Header: make(http.Header), Request: r}, nil
	})}
	return f
}
func (f *fixture) release(t *testing.T, id, v string, body []byte) artifact {
	t.Helper()
	p := providers[id]
	name, entry, format, err := artifactNames(id, v, f.b.arch)
	if err != nil {
		t.Fatal(err)
	}
	if body == nil {
		body = fakeELF(f.b.arch, v, format == "appimage")
		if format == "tar.gz" {
			body = archive(t, entry, body, tar.TypeReg)
		}
	}
	a := artifact{Provider: id, Version: v, Arch: f.b.arch, URL: "https://github.com/" + p.repo + "/releases/download/" + p.tagPrefix + v + "/" + name, SHA256: fmt.Sprintf("%x", sha256.Sum256(body)), Format: format, Entry: entry, Size: int64(len(body))}
	no := false
	r := release{Tag: p.tagPrefix + v, Draft: &no, Prerelease: &no, Assets: []releaseAsset{{Name: name, URL: a.URL, Digest: "sha256:" + a.SHA256, Size: a.Size}}}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	base := "https://api.github.com/repos/" + p.repo + "/releases/"
	f.responses[base+"latest"] = data
	f.responses[base+"tags/"+r.Tag] = data
	f.responses[a.URL] = body
	f.latest = base + "latest"
	return a
}
func (f *fixture) run(ctx context.Context, op string, args ...string) error {
	f.b.req.Operation = op
	f.b.req.Arguments = args
	_, err := f.b.execute(ctx)
	return err
}
func mustRun(t *testing.T, f *fixture, op string, args ...string) {
	t.Helper()
	if err := f.run(context.Background(), op, args...); err != nil {
		t.Fatal(err)
	}
}
func writeFixture(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func TestPortableLifecycleAllProvidersAndArchitectures(t *testing.T) {
	for _, id := range providerOrder {
		for _, arch := range []string{"amd64", "arm64"} {
			t.Run(id+"-"+arch, func(t *testing.T) {
				f := newFixture(t, arch)
				old := "1.2.3"
				next := "1.2.4"
				legacy := filepath.Join(filepath.Dir(f.b.root(id)), "legacy-user-data")
				writeFixture(t, legacy, []byte("preserve"), 0600)
				f.release(t, id, old, nil)
				mustRun(t, f, "install", "--provider", id, "--trust-download")
				if v, err := f.b.current(id); err != nil || v != old {
					t.Fatalf("current %q %v", v, err)
				}
				active := filepath.Join(f.b.root(id), "current")
				info, err := os.Stat(active)
				if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0755 {
					t.Fatalf("unusable active executable: %v", err)
				}
				calls := f.calls
				mustRun(t, f, "status")
				mustRun(t, f, "menu")
				mustRun(t, f, "menu", id)
				if f.calls != calls {
					t.Fatal("read-only view accessed network")
				}
				f.release(t, id, next, nil)
				mustRun(t, f, "update", "--provider", id, "--trust-download")
				mustRun(t, f, "use", "--provider", id, "--version", old)
				if err = f.run(context.Background(), "remove", "--provider", id, "--version", old); err == nil {
					t.Fatal("removed active version")
				}
				mustRun(t, f, "remove", "--provider", id, "--version", next)
				mustRun(t, f, "uninstall", "--provider", id)
				vs, v, err := f.b.inspect(context.Background(), id, true)
				if err != nil || len(vs) != 0 || v != "" {
					t.Fatalf("remaining %v %q %v", vs, v, err)
				}
				data, err := os.ReadFile(legacy)
				if err != nil || string(data) != "preserve" {
					t.Fatal("legacy data changed")
				}
			})
		}
	}
}
func TestOptionsStrict(t *testing.T) {
	cases := []struct {
		op   string
		args []string
	}{
		{"menu", []string{"picoclaw", "extra"}}, {"menu", []string{"hermes-agent"}}, {"menu", []string{"--provider", "picoclaw"}},
		{"status", []string{"picoclaw"}}, {"install", nil}, {"install", []string{"--provider", "ollama", "--trust-download"}},
		{"install", []string{"--provider", "picoclaw", "--provider", "codex-cli"}}, {"install", []string{"--provider", "picoclaw", "--version", "../x"}},
		{"install", []string{"--provider", "picoclaw", "--version", "1.2.3-beta"}}, {"install", []string{"--provider", "picoclaw", "--trust-download=yes"}},
		{"remove", []string{"--provider", "picoclaw"}}, {"use", []string{"--provider", "picoclaw", "--version", "latest"}},
		{"uninstall", []string{"--provider", "picoclaw", "--version", "1.2.3"}}, {"uninstall", []string{"--provider", "picoclaw", "--trust-download"}},
		{"integrate", nil}, {"install", []string{"--provider"}},
	}
	for _, c := range cases {
		if _, err := parseOptions(c.op, c.args); err == nil {
			t.Errorf("accepted %s %v", c.op, c.args)
		}
	}
	for _, op := range []string{"install", "update"} {
		f := newFixture(t, "amd64")
		if err := f.run(context.Background(), op, "--provider", "picoclaw"); err == nil {
			t.Fatal("missing trust accepted")
		}
		if f.calls != 0 {
			t.Fatal("no-consent network")
		}
		if _, err := os.Lstat(f.b.req.AppsDir); !os.IsNotExist(err) {
			t.Fatal("no-consent writes")
		}
	}
}
func TestMenusOfflineStrictChoices(t *testing.T) {
	f := newFixture(t, "amd64")
	for _, id := range append([]string{""}, providerOrder...) {
		choices, err := f.b.menu(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if err = protocol.ValidateChoices(choices); err != nil {
			t.Fatal(err)
		}
		for _, c := range choices {
			args := append([]string{}, c.Arguments...)
			if c.Prompt != "" {
				args = append(args, "1.2.3")
			}
			if _, err = parseOptions(c.Operation, args); err != nil {
				t.Fatalf("invalid generated choice %+v: %v", c, err)
			}
		}
	}
	if f.calls != 0 {
		t.Fatal("menu network")
	}
	if _, err := os.Lstat(f.b.req.AppsDir); !os.IsNotExist(err) {
		t.Fatal("menu writes")
	}
}
func TestReleaseIdentityAndDigestValidation(t *testing.T) {
	for _, tc := range []string{"draft", "prerelease", "missing-flag", "wrong-tag", "missing-digest", "wrong-origin", "duplicate", "missing-asset", "wrong-size", "bad-digest", "wrong-arch"} {
		t.Run(tc, func(t *testing.T) {
			f := newFixture(t, "amd64")
			f.release(t, "picoclaw", "1.2.3", nil)
			var r release
			if err := json.Unmarshal(f.responses[f.latest], &r); err != nil {
				t.Fatal(err)
			}
			yes := true
			switch tc {
			case "draft":
				r.Draft = &yes
			case "prerelease":
				r.Prerelease = &yes
			case "missing-flag":
				r.Draft = nil
			case "wrong-tag":
				r.Tag = "x1.2.3"
			case "missing-digest":
				r.Assets[0].Digest = ""
			case "wrong-origin":
				r.Assets[0].URL = "https://evil.invalid/tool"
			case "duplicate":
				r.Assets = append(r.Assets, r.Assets[0])
			case "missing-asset":
				r.Assets = nil
			case "wrong-size":
				r.Assets[0].Size = maxDownload + 1
			case "bad-digest":
				r.Assets[0].Digest = "sha256:bad"
			case "wrong-arch":
				r.Assets[0].Name = "picoclaw_Linux_arm64.tar.gz"
			}
			data, _ := json.Marshal(r)
			f.responses[f.latest] = data
			if _, err := f.b.resolve(context.Background(), "picoclaw", "latest"); err == nil {
				t.Fatal("accepted invalid release")
			}
		})
	}
}
func TestDuplicateJSONRejected(t *testing.T) {
	for _, data := range []string{`{"tag_name":"v1.2.3","tag_name":"v9.9.9"}`, `{"assets":[{"name":"x","name":"y"}]}`, `{} {}`, `null`} {
		var r release
		if err := decode([]byte(data), &r, false); err == nil {
			t.Fatalf("accepted %s", data)
		}
	}
}
func TestCorruptDownloadPreservesCurrent(t *testing.T) {
	for _, id := range providerOrder {
		t.Run(id, func(t *testing.T) {
			f := newFixture(t, "amd64")
			f.release(t, id, "1.2.3", nil)
			mustRun(t, f, "install", "--provider", id, "--trust-download")
			a := f.release(t, id, "1.2.4", nil)
			f.responses[a.URL] = []byte("wrong bytes")
			if err := f.run(context.Background(), "update", "--provider", id, "--trust-download"); err == nil {
				t.Fatal("corruption accepted")
			}
			_, v, err := f.b.inspect(context.Background(), id, true)
			if err != nil || v != "1.2.3" {
				t.Fatalf("old version lost %s %v", v, err)
			}
		})
	}
}
func TestInvalidPayloadsAreNeverPublished(t *testing.T) {
	cases := []struct {
		id   string
		data func(*testing.T) []byte
	}{
		{"picoclaw", func(t *testing.T) []byte { return archive(t, "../picoclaw", fakeELF("amd64", "x", false), tar.TypeReg) }},
		{"picoclaw", func(t *testing.T) []byte { return archive(t, "picoclaw", nil, tar.TypeSymlink) }},
		{"picoclaw", func(t *testing.T) []byte { return archive(t, "picoclaw", fakeELF("arm64", "x", false), tar.TypeReg) }},
		{"codex-cli", func(t *testing.T) []byte { return archive(t, "wrong-name", fakeELF("amd64", "x", false), tar.TypeReg) }},
		{"hermes-desktop", func(t *testing.T) []byte { return fakeELF("amd64", "x", false) }},
		{"hermes-desktop", func(t *testing.T) []byte { return []byte("#!/bin/sh\necho never run\n") }},
	}
	for n, c := range cases {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			f := newFixture(t, "amd64")
			f.release(t, c.id, "1.2.3", c.data(t))
			if err := f.run(context.Background(), "install", "--provider", c.id, "--trust-download"); err == nil {
				t.Fatal("invalid payload installed")
			}
			if _, err := os.Lstat(f.b.root(c.id)); !os.IsNotExist(err) {
				t.Fatal("invalid payload published")
			}
		})
	}
}
func TestTransactionFaultsPreservePreviousSelection(t *testing.T) {
	for _, at := range []string{"verified", "published", "before-activation"} {
		t.Run(at, func(t *testing.T) {
			f := newFixture(t, "amd64")
			f.release(t, "picoclaw", "1.2.3", nil)
			mustRun(t, f, "install", "--provider", "picoclaw", "--trust-download")
			f.release(t, "picoclaw", "1.2.4", nil)
			f.b.checkpoint = func(name string) error {
				if name == at {
					return errors.New("fixture fault")
				}
				return nil
			}
			if err := f.run(context.Background(), "update", "--provider", "picoclaw", "--trust-download"); err == nil {
				t.Fatal("fault ignored")
			}
			if v, err := f.b.current("picoclaw"); err != nil || v != "1.2.3" {
				t.Fatalf("previous selection changed %q %v", v, err)
			}
			if _, _, err := f.b.inspect(context.Background(), "picoclaw", true); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestCancellationDoesNotActivate(t *testing.T) {
	f := newFixture(t, "amd64")
	f.release(t, "picoclaw", "1.2.3", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.b.checkpoint = func(name string) error {
		if name == "verified" {
			cancel()
		}
		return nil
	}
	if err := f.run(ctx, "install", "--provider", "picoclaw", "--trust-download"); !errors.Is(err, context.Canceled) {
		t.Fatalf("wanted cancellation, got %v", err)
	}
	if _, err := os.Lstat(f.b.root("picoclaw")); !os.IsNotExist(err) {
		t.Fatal("cancelled first installation published")
	}
}
func TestModifiedOrUnknownVersionPreserved(t *testing.T) {
	for _, kind := range []string{"binary", "extra", "receipt", "mode", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t, "amd64")
			f.release(t, "picoclaw", "1.2.3", nil)
			mustRun(t, f, "install", "--provider", "picoclaw", "--trust-download")
			dir := filepath.Join(f.b.root("picoclaw"), "versions", "1.2.3")
			switch kind {
			case "binary":
				writeFixture(t, filepath.Join(dir, "tool"), fakeELF("amd64", "altered", false), 0755)
			case "extra":
				writeFixture(t, filepath.Join(dir, "user-data"), []byte("keep"), 0600)
			case "receipt":
				writeFixture(t, filepath.Join(dir, "receipt.json"), []byte("{}"), 0600)
			case "mode":
				if err := os.Chmod(filepath.Join(dir, "tool"), 0777); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Remove(filepath.Join(dir, "tool")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("/etc/passwd", filepath.Join(dir, "tool")); err != nil {
					t.Fatal(err)
				}
			}
			if err := f.run(context.Background(), "uninstall", "--provider", "picoclaw"); err == nil {
				t.Fatal("unowned mutation accepted")
			}
			if _, err := os.Lstat(dir); err != nil {
				t.Fatal("version deleted")
			}
			if v, err := f.b.current("picoclaw"); err != nil || v != "1.2.3" {
				t.Fatal("selection removed")
			}
		})
	}
}
func TestLockAndUnownedRootRefused(t *testing.T) {
	for _, kind := range []string{"lock", "root", "link", "ancestor"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t, "amd64")
			root := f.b.root("picoclaw")
			if err := os.MkdirAll(filepath.Dir(root), 0700); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "lock":
				if err := os.Mkdir(root+".lock", 0700); err != nil {
					t.Fatal(err)
				}
			case "root":
				if err := os.Mkdir(root, 0700); err != nil {
					t.Fatal(err)
				}
			case "link":
				target := t.TempDir()
				if err := os.Symlink(target, root); err != nil {
					t.Fatal(err)
				}
			case "ancestor":
				if err := os.Chmod(filepath.Dir(root), 0777); err != nil {
					t.Fatal(err)
				}
			}
			if err := f.run(context.Background(), "install", "--provider", "picoclaw", "--trust-download"); err == nil {
				t.Fatal("unsafe root accepted")
			}
			if f.calls != 0 {
				t.Fatal("unsafe root accessed network")
			}
		})
	}
}
func TestUpdatesCannotAdoptOrDowngrade(t *testing.T) {
	f := newFixture(t, "amd64")
	if err := f.run(context.Background(), "update", "--provider", "picoclaw", "--trust-download"); err == nil {
		t.Fatal("update adopted missing installation")
	}
	if f.calls != 0 {
		t.Fatal("update fetched without installation")
	}
	f.release(t, "picoclaw", "2.0.0", nil)
	mustRun(t, f, "install", "--provider", "picoclaw", "--trust-download")
	f.release(t, "picoclaw", "1.9.9", nil)
	if err := f.run(context.Background(), "update", "--provider", "picoclaw", "--trust-download"); err == nil {
		t.Fatal("downgrade accepted")
	}
	if v, _ := f.b.current("picoclaw"); v != "2.0.0" {
		t.Fatal("downgrade changed selection")
	}
}
func TestURLRedirectAndResponseLimits(t *testing.T) {
	f := newFixture(t, "amd64")
	a := f.release(t, "picoclaw", "1.2.3", nil)
	if err := f.b.fetch(context.Background(), "picoclaw", "https://evil.invalid/", io.Discard, 1024); err == nil {
		t.Fatal("external origin")
	}
	if err := f.b.fetch(context.Background(), "picoclaw", a.URL, io.Discard, 1); err == nil {
		t.Fatal("oversized response")
	}
	f.b.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 302, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{"Location": []string{"https://evil.invalid/payload"}}, Request: r}, nil
	})
	if err := f.b.fetch(context.Background(), "picoclaw", a.URL, io.Discard, maxDownload); err == nil {
		t.Fatal("untrusted redirect")
	}
}
func TestServeProtocolOffline(t *testing.T) {
	f := newFixture(t, "amd64")
	req := f.b.req
	req.Protocol = 1
	req.RequestID = strings.Repeat("a", 32)
	req.Operation = "menu"
	data, _ := json.Marshal(req)
	var out bytes.Buffer
	if err := serve(context.Background(), bytes.NewReader(data), &out); err != nil {
		t.Fatal(err)
	}
	stream := protocol.Stream{RequestID: req.RequestID}
	for _, line := range bytes.Split(bytes.TrimSpace(out.Bytes()), []byte{'\n'}) {
		if _, err := stream.Accept(line); err != nil {
			t.Fatal(err)
		}
	}
	if stream.Result == nil || stream.Result.Status != "success" || len(stream.Result.Choices) != 4 {
		t.Fatalf("unexpected result %+v", stream.Result)
	}
	if _, err := os.Lstat(req.AppsDir); !os.IsNotExist(err) {
		t.Fatal("protocol menu writes")
	}
	before := append([]byte{}, out.Bytes()...)
	out.Reset()
	t.Setenv("ATM_DRY_RUN", "1")
	req.Operation = "install"
	data, _ = json.Marshal(req)
	if err := serve(context.Background(), bytes.NewReader(data), &out); err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(before, out.Bytes()) {
		t.Fatal("dry-run not handled")
	}
}
