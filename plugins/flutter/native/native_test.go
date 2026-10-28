//go:build linux

package main

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/AtomyCloud/atm/internal/config"
	"github.com/AtomyCloud/atm/internal/plugin"
	"github.com/AtomyCloud/atm/internal/protocol"
	"github.com/AtomyCloud/atm/internal/runner"
	"github.com/ulikunitz/xz"
)

const revision = "0123456789abcdef0123456789abcdef01234567"

func write(t *testing.T, name string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, data, mode); err != nil {
		t.Fatal(err)
	}
}
func newTestBackend(t *testing.T) *backend {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ATM_DRY_RUN", "")
	req := protocol.Request{Protocol: 1, RequestID: strings.Repeat("a", 32), PluginID: "flutter", Operation: "menu", Home: filepath.Join(t.TempDir(), "runtime"), AppsDir: filepath.Join(t.TempDir(), "Apps")}
	b, err := newBackend(req, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func action(b *backend, op string, args ...string) error {
	b.req.Operation = op
	b.req.Arguments = args
	_, err := b.execute(context.Background())
	return err
}
func elfBytes(arch string) []byte {
	data := make([]byte, 64)
	copy(data, []byte{127, 'E', 'L', 'F', 2, 1, 1})
	binary.LittleEndian.PutUint16(data[16:], 2)
	machine := uint16(62)
	if arch == "arm64" {
		machine = 183
	}
	binary.LittleEndian.PutUint16(data[18:], machine)
	binary.LittleEndian.PutUint32(data[20:], 1)
	binary.LittleEndian.PutUint16(data[52:], 64)
	return data
}

type tarItem struct {
	name, body, link string
	kind             byte
	mode             int64
}

func sdkItems(v, arch string) []tarItem {
	return []tarItem{
		{"flutter/bin/flutter", "#!/bin/sh\nexit 99\n", "", tar.TypeReg, 0755},
		{"flutter/bin/cache/dart-sdk/bin/dart", string(elfBytes(arch)), "", tar.TypeReg, 0755},
		{"flutter/bin/cache/flutter.version.json", `{"frameworkVersion":"` + v + `"}`, "", tar.TypeReg, 0644},
		{"flutter/.git/HEAD", "ref: refs/heads/stable\n", "", tar.TypeReg, 0644},
		{"flutter/.git/refs/heads/stable", revision + "\n", "", tar.TypeReg, 0644},
		{"flutter/packages/flutter/README.md", "sdk source", "", tar.TypeReg, 0644},
		{"flutter/link", "", "packages/flutter/README.md", tar.TypeSymlink, 0777},
	}
}
func archiveItems(t *testing.T, items []tarItem) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw, err := xz.NewWriter(&buf)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(zw)
	for _, item := range items {
		size := int64(len(item.body))
		if item.kind != tar.TypeReg {
			size = 0
		}
		if err := tw.WriteHeader(&tar.Header{Name: item.name, Linkname: item.link, Typeflag: item.kind, Mode: item.mode, Size: size}); err != nil {
			t.Fatal(err)
		}
		if size > 0 {
			if _, err := tw.Write([]byte(item.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
func rowFor(v, arch string, payload []byte) release {
	part := ""
	if arch == "arm64" {
		part = "arm64_"
	}
	return release{Version: v, Channel: "stable", Arch: arch, Archive: "stable/linux/flutter_linux_" + part + v + "-stable.tar.xz", SHA256: fmt.Sprintf("%x", sha256.Sum256(payload)), Revision: revision}
}
func fixture(t *testing.T, b *backend, rows []release, payloads map[string][]byte) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/releases_linux.json" {
			_ = json.NewEncoder(w).Encode(map[string]any{"base_url": officialBase, "releases": rows})
			return
		}
		if data, ok := payloads[strings.TrimPrefix(r.URL.Path, "/")]; ok {
			_, _ = w.Write(data)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	b.base = srv.URL
	b.client = srv.Client()
}
func releasesFixture(t *testing.T, b *backend, versions ...string) {
	t.Helper()
	rows := []release{}
	payloads := map[string][]byte{}
	for _, v := range versions {
		data := archiveItems(t, sdkItems(v, b.platform))
		r := rowFor(v, b.platform, data)
		rows = append(rows, r)
		payloads[r.Archive] = data
	}
	fixture(t, b, rows, payloads)
}
func legacy(t *testing.T, b *backend, v string) {
	t.Helper()
	for _, item := range sdkItems(v, b.platform) {
		name := filepath.Join(b.root, v, strings.TrimPrefix(item.name, "flutter/"))
		if item.kind == tar.TypeSymlink {
			if err := os.Symlink(item.link, name); err != nil {
				t.Fatal(err)
			}
		} else {
			write(t, name, []byte(item.body), os.FileMode(item.mode))
		}
	}
}
func assertCurrent(t *testing.T, b *backend, want string) {
	t.Helper()
	got, err := b.current()
	if err != nil || got != want {
		t.Fatalf("current %q want %q: %v", got, want, err)
	}
}
func assertAbsent(t *testing.T, name string) {
	t.Helper()
	if _, err := os.Lstat(name); !os.IsNotExist(err) {
		t.Fatalf("expected absent %s: %v", name, err)
	}
}
func assertCleanStage(t *testing.T, b *backend) {
	t.Helper()
	names, _ := filepath.Glob(filepath.Join(b.root, ".atm-*-*"))
	if len(names) != 0 {
		t.Fatalf("stage/lock remains: %v", names)
	}
}

func TestLifecycleExplicitIntegration(t *testing.T) {
	b := newTestBackend(t)
	releasesFixture(t, b, "3.47.7", "3.47.8")
	t.Setenv("PATH", t.TempDir())
	original := "# my unrelated configuration\n"
	for _, name := range []string{".profile", ".bashrc", ".zshrc"} {
		write(t, filepath.Join(b.home, name), []byte(original), 0640)
	}
	for _, v := range []string{"3.47.7", "3.47.8"} {
		if err := action(b, "install", "--version", v); err != nil {
			t.Fatal(err)
		}
		if err := b.verifyOwned(context.Background(), v); err != nil {
			t.Fatal(err)
		}
	}
	assertCurrent(t, b, "3.47.8")
	for _, name := range []string{".profile", ".bashrc", ".zshrc"} {
		data, _ := os.ReadFile(filepath.Join(b.home, name))
		if string(data) != original {
			t.Fatal("install changed profiles")
		}
	}
	if err := action(b, "remove", "3.47.8"); err == nil {
		t.Fatal("removed current")
	}
	if err := action(b, "use", "3.47.7"); err != nil {
		t.Fatal(err)
	}
	if err := action(b, "update"); err != nil {
		t.Fatal(err)
	}
	assertCurrent(t, b, "3.47.8")
	if err := action(b, "update", "--version", "3.47.7"); err == nil {
		t.Fatal("downgrade accepted")
	}
	if err := action(b, "integrate"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".profile", ".bashrc", ".zshrc"} {
		data, _ := os.ReadFile(filepath.Join(b.home, name))
		if !strings.HasPrefix(string(data), original) || !strings.Contains(string(data), profileStart) {
			t.Fatal("integration missing")
		}
	}
	if err := action(b, "remove", "3.47.7"); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(b.root, "my-project/keep")
	write(t, keep, []byte("mine"), 0600)
	if err := action(b, "uninstall"); err != nil {
		t.Fatal(err)
	}
	assertCurrent(t, b, "")
	assertAbsent(t, filepath.Join(b.root, "3.47.8"))
	for _, name := range []string{".profile", ".bashrc", ".zshrc"} {
		data, _ := os.ReadFile(filepath.Join(b.home, name))
		if string(data) != original {
			t.Fatal("uninstall changed unrelated profile")
		}
	}
	if data, _ := os.ReadFile(keep); string(data) != "mine" {
		t.Fatal("unrelated data lost")
	}
	assertAbsent(t, b.req.Home)
	assertCleanStage(t, b)
}
func TestCacheAndGitChangesPreservedButSelectionWorks(t *testing.T) {
	b := newTestBackend(t)
	releasesFixture(t, b, "3.47.8")
	if err := action(b, "install"); err != nil {
		t.Fatal(err)
	}
	legacy(t, b, "3.40.0")
	dir := filepath.Join(b.root, "3.47.8")
	for _, name := range []string{"bin/cache/new-artifact", ".git/logs/HEAD", "packages/flutter/.dart_tool/package_config.json"} {
		write(t, filepath.Join(dir, name), []byte("generated"), 0600)
	}
	for _, v := range []string{"3.40.0", "3.47.8", "3.40.0"} {
		if err := action(b, "use", v); err != nil {
			t.Fatal(err)
		}
	}
	for _, op := range []string{"remove", "uninstall"} {
		args := []string{"3.47.8"}
		if op == "uninstall" {
			args = nil
		}
		if err := action(b, op, args...); err == nil {
			t.Fatal("modified SDK deleted")
		}
	}
	if err := action(b, "remove", "3.40.0"); err == nil {
		t.Fatal("legacy/current removed")
	}
	if err := action(b, "use", "3.47.8"); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, ".git/refs/heads/stable"), []byte(strings.Repeat("a", 40)), 0600)
	if err := action(b, "use", "3.47.8"); err == nil {
		t.Fatal("different SDK revision selected")
	}
	if err := action(b, "status"); err != nil {
		t.Fatal(err)
	}
	if err := action(b, "use", "3.40.0"); err != nil {
		t.Fatal("could not switch away from changed SDK", err)
	}
	assertAbsent(t, filepath.Join(b.home, ".profile"))
	assertCleanStage(t, b)
}
func TestLegacyNeverAdopted(t *testing.T) {
	b := newTestBackend(t)
	legacy(t, b, "3.47.7")
	legacy(t, b, "3.47.8")
	releasesFixture(t, b, "3.47.8")
	if err := action(b, "install"); err != nil {
		t.Fatal(err)
	}
	assertAbsent(t, filepath.Join(b.root, "3.47.8", receiptName))
	if err := action(b, "remove", "3.47.7"); err == nil {
		t.Fatal("unowned SDK removed")
	}
	if err := action(b, "uninstall"); err != nil {
		t.Fatal(err)
	}
	assertCurrent(t, b, "3.47.8")
}

func TestReleaseListingDuplicatesArchitectureAndCustom(t *testing.T) {
	b := newTestBackend(t)
	releasesFixture(t, b, "3.47.1", "3.47.10", "3.47.9", "3.47.8", "3.47.7", "3.47.6")
	choices, err := b.menu(context.Background(), []string{"--latest"})
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, c := range choices[:5] {
		got = append(got, c.ID)
	}
	if !reflect.DeepEqual(got, []string{"3.47.10", "3.47.9", "3.47.8", "3.47.7", "3.47.6"}) {
		t.Fatal(got)
	}
	if err := protocol.ValidateChoices(choices); err != nil {
		t.Fatal(err)
	}
	if err := action(b, "install", "--version=3.99.99"); err == nil {
		t.Fatal("unlisted custom version accepted")
	}
	b = newTestBackend(t)
	data := archiveItems(t, sdkItems("3.47.8", "x64"))
	r := rowFor("3.47.8", "x64", data)
	old := rowFor("3.13.3", "x64", data)
	conflict := old
	conflict.SHA256 = strings.Repeat("f", 64)
	fixture(t, b, []release{r, r, old, conflict}, map[string][]byte{r.Archive: data})
	b.platform = "x64"
	rows, err := b.releases(context.Background(), "stable")
	if err != nil || len(rows) != 2 || !rows[1].Ambiguous {
		t.Fatal(rows, err)
	}
	if err := action(b, "install"); err != nil {
		t.Fatal(err)
	}
	if err := action(b, "install", "--version", "3.13.3"); err == nil {
		t.Fatal("ambiguous artifact accepted")
	}
	b.platform = "arm64"
	if _, err := b.releases(context.Background(), "stable"); err == nil {
		t.Fatal("wrong architecture fallback")
	}
}
func TestMetadataRefusals(t *testing.T) {
	for _, kind := range []string{"hash", "revision", "path", "arch", "channel"} {
		t.Run(kind, func(t *testing.T) {
			b := newTestBackend(t)
			r := rowFor("3.47.8", b.platform, nil)
			switch kind {
			case "hash":
				r.SHA256 = "bad"
			case "revision":
				r.Revision = "bad"
			case "path":
				r.Archive = "../../escape"
			case "arch":
				r.Arch = "alien"
			case "channel":
				r.Channel = "beta"
			}
			fixture(t, b, []release{r}, nil)
			if _, err := b.releases(context.Background(), "stable"); err == nil {
				t.Fatal("bad metadata accepted")
			}
		})
	}
}

func TestFailedInstallKeepsCurrent(t *testing.T) {
	for _, kind := range []string{"hash", "truncated", "version", "arch", "revision", "traversal", "symlink", "cycle", "link-parent", "duplicate", "special", "reserved"} {
		t.Run(kind, func(t *testing.T) {
			b := newTestBackend(t)
			legacy(t, b, "3.40.0")
			if err := action(b, "use", "3.40.0"); err != nil {
				t.Fatal(err)
			}
			items := sdkItems("3.47.8", b.platform)
			switch kind {
			case "version":
				items = sdkItems("3.47.9", b.platform)
			case "arch":
				other := "arm64"
				if b.platform == other {
					other = "x64"
				}
				items = sdkItems("3.47.8", other)
			case "revision":
				items[4].body = strings.Repeat("a", 40)
			case "traversal":
				items = append(items, tarItem{"flutter/../../escape", "x", "", tar.TypeReg, 0644})
			case "symlink":
				items = append(items, tarItem{"flutter/bad", "", "../../escape", tar.TypeSymlink, 0777})
			case "cycle":
				items = append(items, tarItem{"flutter/a", "", "b", tar.TypeSymlink, 0777}, tarItem{"flutter/b", "", "a", tar.TypeSymlink, 0777})
			case "link-parent":
				items = append(items, tarItem{"flutter/dirlink", "", "packages", tar.TypeSymlink, 0777}, tarItem{"flutter/dirlink/file", "x", "", tar.TypeReg, 0644})
			case "duplicate":
				items = append(items, items[0])
			case "special":
				items = append(items, tarItem{"flutter/fifo", "", "", tar.TypeFifo, 0600})
			case "reserved":
				items = append(items, tarItem{"flutter/" + receiptName, "{}", "", tar.TypeReg, 0600})
			}
			data := archiveItems(t, items)
			if kind == "truncated" {
				data = data[:len(data)-7]
			}
			r := rowFor("3.47.8", b.platform, data)
			if kind == "hash" {
				r.SHA256 = strings.Repeat("0", 64)
			}
			fixture(t, b, []release{r}, map[string][]byte{r.Archive: data})
			if err := action(b, "install"); err == nil {
				t.Fatal("bad artifact accepted")
			}
			assertCurrent(t, b, "3.40.0")
			assertAbsent(t, filepath.Join(b.root, "3.47.8"))
			assertAbsent(t, filepath.Join(b.home, ".profile"))
			assertCleanStage(t, b)
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestOfflineStartupDryRunAndArguments(t *testing.T) {
	b := newTestBackend(t)
	t.Setenv("PATH", t.TempDir())
	b.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Error("unexpected network")
		return nil, fmt.Errorf("offline")
	})}
	for _, op := range []string{"menu", "status"} {
		if err := action(b, op); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("ATM_DRY_RUN", "1")
	for _, op := range []string{"menu", "status", "install", "update", "use", "remove", "uninstall", "integrate"} {
		if err := action(b, op); err != nil {
			t.Fatal(err)
		}
	}
	assertAbsent(t, b.root)
	assertAbsent(t, b.req.Home)
	assertAbsent(t, filepath.Join(b.home, ".profile"))
	t.Setenv("ATM_DRY_RUN", "")
	for _, op := range []string{"status", "install", "update", "use", "remove", "uninstall", "integrate", "menu", "unknown"} {
		if err := action(b, op, "--bad"); err == nil {
			t.Fatal(op)
		}
	}
	for _, args := range [][]string{{"--version"}, {"--channel", "nightly"}, {"--version", "../escape"}, {"--version", "3.1.0", "--version", "3.2.0"}} {
		if _, err := parseOptions(args); err == nil {
			t.Fatal(args)
		}
	}
	b.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, fmt.Errorf("offline") })}
	choices, err := b.menu(context.Background(), []string{"--latest"})
	if err != nil || len(choices) != 2 {
		t.Fatal(choices, err)
	}
}
func TestFetchAndCancellation(t *testing.T) {
	b := newTestBackend(t)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/slow":
			<-r.Context().Done()
		case "/redirect":
			http.Redirect(w, r, "https://example.invalid/archive", 302)
		case "/sameorigin":
			http.Redirect(w, r, "/other", 302)
		case "/encoding":
			w.Header().Set("Content-Encoding", "gzip")
			fmt.Fprint(w, "x")
		default:
			fmt.Fprint(w, strings.Repeat("x", 100))
		}
	}))
	defer srv.Close()
	b.base = srv.URL
	b.client = srv.Client()
	for _, path := range []string{"/large", "/redirect", "/sameorigin", "/encoding"} {
		if err := b.fetch(context.Background(), path, io.Discard, 10); err == nil {
			t.Fatal(path)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := b.fetch(ctx, "/slow", io.Discard, 100); err == nil {
		t.Fatal("ignored cancellation")
	}
}
func TestCompiledBackendEmptyPATH(t *testing.T) {
	b := newTestBackend(t)
	root := t.TempDir()
	dir := filepath.Join(root, "plugins/flutter")
	manifest, err := os.ReadFile("../plugin.json")
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "plugin.json"), manifest, 0600)
	bin := filepath.Join(dir, "bin/linux-"+runtime.GOARCH+"/atm-flutter")
	if err := os.MkdirAll(filepath.Dir(bin), 0700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin/go"), "build", "-o", bin, ".")
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, data)
	}
	c := config.Config{Home: root, PluginsDir: filepath.Join(root, "plugins"), AppsDir: b.req.AppsDir, Language: "en-us"}
	ps, err := plugin.Discover(c, "linux")
	if err != nil || len(ps) != 1 {
		t.Fatal(ps, err)
	}
	t.Setenv("PATH", t.TempDir())
	for _, op := range []string{"menu", "status"} {
		plan, err := runner.Plugin(c, ps[0], op, nil)
		if err != nil {
			t.Fatal(err)
		}
		out := runner.RunNative(context.Background(), c, plan, runner.NativeOptions{Timeout: 5 * time.Second})
		if out.Status != "success" {
			t.Fatal(out)
		}
		if op == "menu" && (len(out.Choices) != 8 || !strings.Contains(out.Message, "https://docs.flutter.dev")) {
			t.Fatal(out)
		}
	}
	assertAbsent(t, b.root)
	assertAbsent(t, filepath.Join(b.home, ".profile"))
	legacy(t, b, "3.47.8")
	for _, op := range []string{"use", "status", "integrate", "uninstall"} {
		args := []string(nil)
		if op == "use" {
			args = []string{"3.47.8"}
		}
		plan, err := runner.Plugin(c, ps[0], op, args)
		if err != nil {
			t.Fatal(err)
		}
		out := runner.RunNative(context.Background(), c, plan, runner.NativeOptions{Timeout: 5 * time.Second})
		if out.Status != "success" {
			t.Fatal(op, out)
		}
	}
	assertCurrent(t, b, "3.47.8")
}

func TestSavedOfficialMetadata(t *testing.T) {
	name := os.Getenv("ATM_TEST_FLUTTER_METADATA")
	if name == "" {
		t.Skip("optional saved official metadata snapshot")
	}
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	b := newTestBackend(t)
	b.platform = "x64"
	b.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != officialBase+"/releases_linux.json" {
			t.Fatal("unexpected artifact download", r.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(data)), Header: http.Header{}, ContentLength: int64(len(data))}, nil
	})}
	choices, err := b.menu(context.Background(), []string{"--latest"})
	if err != nil || len(choices) != 7 {
		t.Fatal(choices, err)
	}
	if choices[0].ID != "3.47.8" {
		t.Fatal(choices)
	}
	rows, err := b.releases(context.Background(), "stable")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rows {
		if r.Version == "3.13.3" {
			found = r.Ambiguous
		}
	}
	if !found {
		t.Fatal("historic ambiguous version not isolated")
	}
}
