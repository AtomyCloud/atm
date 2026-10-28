//go:build linux && (amd64 || arm64)

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
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/AtomyCloud/atm/internal/protocol"
)

const fixtureCommit = "034f571df509819cc10b0c8129f66ef77a542f0e"

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
	req := protocol.Request{Protocol: 1, RequestID: strings.Repeat("a", 32), PluginID: "vscode", Operation: "menu", Home: filepath.Join(t.TempDir(), "runtime"), AppsDir: filepath.Join(t.TempDir(), "Apps")}
	b, err := newBackend(req, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	b.run = func(context.Context, string, []string, []string) error {
		t.Fatal("unexpected editor execution")
		return nil
	}
	return b
}
func action(b *backend, op string, args ...string) error {
	b.req.Operation = op
	b.req.Arguments = args
	_, err := b.execute(context.Background())
	return err
}
func elfBytes(arm bool) []byte {
	data := make([]byte, 64)
	copy(data, []byte{127, 'E', 'L', 'F', 2, 1, 1})
	binary.LittleEndian.PutUint16(data[16:], 2)
	machine := uint16(62)
	if arm {
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

func payloadItems(v, platform string) []tarItem {
	prefix := "VSCode-" + platform + "/"
	return []tarItem{
		{prefix + "code", string(elfBytes(platform == "linux-arm64")), "", tar.TypeReg, 0755},
		{prefix + "bin/code", "#!/bin/sh\nexit 99\n", "", tar.TypeReg, 0755},
		{prefix + "resources/app/package.json", `{"name":"Code","version":"` + v + `"}`, "", tar.TypeReg, 0644},
		{prefix + "resources/app/product.json", `{"applicationName":"code","quality":"stable","commit":"` + fixtureCommit + `"}`, "", tar.TypeReg, 0644},
		{prefix + "resources/app/out/cli.js", "// Never execute fixture", "", tar.TypeReg, 0644},
		{prefix + "resources/app/resources/linux/code.png", "fixture icon", "", tar.TypeReg, 0644},
		{prefix + "chrome-sandbox", string(elfBytes(platform == "linux-arm64")), "", tar.TypeReg, 04755},
	}
}
func archiveItems(t *testing.T, items []tarItem) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
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
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fixture(t *testing.T, b *backend, v string, data []byte, alter func(map[string]any)) {
	t.Helper()
	hash := fmt.Sprintf("%x", sha256.Sum256(data))
	metadata := map[string]any{"url": "https://vscode.download.prss.microsoft.com/dbazure/download/stable/" + fixtureCommit + "/code-stable.tar.gz", "name": v, "productVersion": v, "version": fixtureCommit, "sha256hash": hash}
	if alter != nil {
		alter(metadata)
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/releases/stable":
			_ = json.NewEncoder(w).Encode([]string{v, "1.117.0", "1.116.0", "1.115.0", "1.114.0", "1.113.0", "1.112.0", "1.111.0"})
		case strings.HasPrefix(r.URL.Path, "/api/versions/"):
			_ = json.NewEncoder(w).Encode(metadata)
		case strings.HasPrefix(r.URL.Path, "/dbazure/download/stable/"):
			w.Header().Set("Content-Length", fmt.Sprint(len(data)))
			if r.Method != "HEAD" {
				_, _ = w.Write(data)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	target, _ := url.Parse(srv.URL)
	transport := srv.Client().Transport
	b.client = &http.Client{Timeout: 5 * time.Second, Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		clone := req.Clone(req.Context())
		u := *req.URL
		u.Scheme = target.Scheme
		u.Host = target.Host
		clone.URL = &u
		clone.Host = ""
		return transport.RoundTrip(clone)
	})}
}
func installFixture(t *testing.T, b *backend, v string) {
	t.Helper()
	fixture(t, b, v, archiveItems(t, payloadItems(v, b.platform)), nil)
	if err := action(b, "install", v); err != nil {
		t.Fatal(err)
	}
}
func assertCurrent(t *testing.T, b *backend, want string) {
	t.Helper()
	got, err := b.current()
	if err != nil || got != want {
		t.Fatalf("current=%q want=%q err=%v", got, want, err)
	}
}
func assertAbsent(t *testing.T, p string) {
	t.Helper()
	if _, err := os.Lstat(p); !os.IsNotExist(err) {
		t.Fatalf("expected absent %s: %v", p, err)
	}
}
func assertContents(t *testing.T, p, want string) {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil || string(data) != want {
		t.Fatalf("%s: %q %v", p, data, err)
	}
}
func preserved(t *testing.T, b *backend, v string) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(b.root, "preserved-data", v+"-*", "data"))
	if err != nil {
		t.Fatal(err)
	}
	return paths
}

func TestLifecyclePreservesAllPortableData(t *testing.T) {
	b := newTestBackend(t)
	t.Setenv("PATH", t.TempDir())
	for _, p := range []string{".profile", ".bashrc", ".zshrc"} {
		write(t, filepath.Join(b.home, p), []byte("# personal\n"), 0640)
	}
	installFixture(t, b, "1.118.0")
	old := "code_1.118.0"
	write(t, filepath.Join(b.root, old, "data/user-data/User/settings.json"), []byte("old private settings"), 0600)
	assertAbsent(t, b.cliPath())
	assertAbsent(t, filepath.Join(b.home, ".local/share/applications/code.desktop"))
	for _, p := range []string{".profile", ".bashrc", ".zshrc"} {
		assertContents(t, filepath.Join(b.home, p), "# personal\n")
	}
	installFixture(t, b, "1.118.1")
	current := "code_1.118.1"
	if err := action(b, "integrate"); err != nil {
		t.Fatal(err)
	}
	if err := action(b, "use", "v1.118"); err != nil {
		t.Fatal(err)
	}
	assertCurrent(t, b, old)
	if err := action(b, "remove", old); err == nil {
		t.Fatal("removed active")
	}
	if err := action(b, "use", current); err != nil {
		t.Fatal(err)
	}
	if err := action(b, "remove", old); err != nil {
		t.Fatal(err)
	}
	assertAbsent(t, filepath.Join(b.root, old))
	paths := preserved(t, b, old)
	if len(paths) != 1 {
		t.Fatal(paths)
	}
	assertContents(t, filepath.Join(paths[0], "user-data/User/settings.json"), "old private settings")
	write(t, filepath.Join(b.root, current, "data/extensions/personal/file"), []byte("extension data"), 0600)
	if err := action(b, "uninstall"); err != nil {
		t.Fatal(err)
	}
	assertCurrent(t, b, "")
	assertAbsent(t, filepath.Join(b.root, current))
	assertAbsent(t, b.cliPath())
	assertAbsent(t, filepath.Join(b.home, ".local/share/applications/code.desktop"))
	for _, p := range []string{".profile", ".bashrc", ".zshrc"} {
		assertContents(t, filepath.Join(b.home, p), "# personal\n")
	}
	paths = preserved(t, b, current)
	if len(paths) != 1 {
		t.Fatal(paths)
	}
	assertContents(t, filepath.Join(paths[0], "extensions/personal/file"), "extension data")
}
func TestLegacySelectionNoAdoptionAndModifiedRemovalRefusal(t *testing.T) {
	b := newTestBackend(t)
	installFixture(t, b, "1.118.0")
	old := "code_1.118.0"
	if err := os.Remove(filepath.Join(b.root, old, receiptName)); err != nil {
		t.Fatal(err)
	}
	if err := action(b, "use", old); err != nil {
		t.Fatal(err)
	}
	installFixture(t, b, "1.118.1")
	if err := action(b, "remove", old); err == nil {
		t.Fatal("deleted legacy")
	}
	if err := action(b, "uninstall"); err != nil {
		t.Fatal(err)
	}
	if err := b.validInstalled(old); err != nil {
		t.Fatal(err)
	}
	installFixture(t, b, "1.118.1")
	write(t, filepath.Join(b.root, "code_1.118.1", "user-added"), []byte("mine"), 0600)
	if err := action(b, "use", old); err != nil {
		t.Fatal(err)
	}
	if err := action(b, "remove", "1.118.1"); err == nil {
		t.Fatal("deleted added data")
	}
	if err := action(b, "uninstall"); err == nil {
		t.Fatal("uninstall ignored changed payload")
	}
}
func TestMetadataLatestSevenAndCustomNormalization(t *testing.T) {
	b := newTestBackend(t)
	fixture(t, b, "1.118.1", archiveItems(t, payloadItems("1.118.1", b.platform)), nil)
	choices, err := b.menu(context.Background(), []string{"releases"})
	if err != nil {
		t.Fatal(err)
	}
	if len(choices) != 9 || choices[0].Arguments[0] != "1.118.1" || choices[6].Arguments[0] != "1.112.0" || choices[7].Prompt == "" {
		t.Fatal(choices)
	}
	for _, v := range []string{"1.118", "v1.118", "1.118.0"} {
		got, err := normalizeVersion(v)
		if err != nil || got != "1.118.0" {
			t.Fatal(v, got, err)
		}
	}
	for _, v := range []string{"../../evil", "--bad", "1.02.3", "1.118.0-beta", "$(touch x)", "1.2.3\n"} {
		if _, err := normalizeVersion(v); err == nil {
			t.Fatal(v)
		}
	}
	if err := protocol.ValidateChoices(choices); err != nil {
		t.Fatal(err)
	}
}
func TestBadArchiveAndMetadataNeverPublish(t *testing.T) {
	for _, kind := range []string{"hash", "size", "commit", "url", "name", "version", "arch", "package-name", "product", "traversal", "link-escape", "link-cycle", "duplicate", "reserved", "special", "setuid", "truncated"} {
		t.Run(kind, func(t *testing.T) {
			b := newTestBackend(t)
			installFixture(t, b, "1.118.0")
			items := payloadItems("1.118.1", b.platform)
			prefix := "VSCode-" + b.platform + "/"
			alter := func(m map[string]any) {}
			switch kind {
			case "hash":
				alter = func(m map[string]any) { m["sha256hash"] = strings.Repeat("0", 64) }
			case "size":
				alter = func(m map[string]any) { m["size"] = 1 }
			case "commit":
				alter = func(m map[string]any) { m["version"] = strings.Repeat("a", 40) }
			case "url":
				alter = func(m map[string]any) { m["url"] = "https://evil.invalid/file.tar.gz" }
			case "name":
				alter = func(m map[string]any) { m["name"] = "1.100.0" }
			case "version":
				items[2].body = strings.ReplaceAll(items[2].body, "1.118.1", "1.118.0")
			case "arch":
				items[0].body = string(elfBytes(b.platform != "linux-arm64"))
			case "package-name":
				items[2].body = strings.ReplaceAll(items[2].body, `"Code"`, `"code"`)
			case "product":
				items[3].body = strings.ReplaceAll(items[3].body, fixtureCommit, strings.Repeat("b", 40))
			case "traversal":
				items = append(items, tarItem{prefix + "../../escape", "x", "", tar.TypeReg, 0600})
			case "link-escape":
				items = append(items, tarItem{prefix + "escape", "", "../../outside", tar.TypeSymlink, 0777})
			case "link-cycle":
				items = append(items, tarItem{prefix + "a", "", "b", tar.TypeSymlink, 0777}, tarItem{prefix + "b", "", "a", tar.TypeSymlink, 0777})
			case "duplicate":
				items = append(items, items[0])
			case "reserved":
				items = append(items, tarItem{prefix + "data/settings", "x", "", tar.TypeReg, 0600})
			case "special":
				items = append(items, tarItem{prefix + "fifo", "", "", tar.TypeFifo, 0600})
			case "setuid":
				items[0].mode = 04755
			}
			data := archiveItems(t, items)
			if kind == "truncated" {
				data = data[:len(data)-6]
			}
			fixture(t, b, "1.118.1", data, alter)
			if err := action(b, "install", "1.118.1"); err == nil {
				t.Fatal("unsafe install succeeded")
			}
			assertCurrent(t, b, "code_1.118.0")
			assertAbsent(t, filepath.Join(b.root, "code_1.118.1"))
			stages, _ := filepath.Glob(filepath.Join(b.root, ".atm-stage-*"))
			if len(stages) > 0 {
				t.Fatal(stages)
			}
		})
	}
}
func TestOfflineRootStatusAndDryRun(t *testing.T) {
	b := newTestBackend(t)
	b.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("unexpected network"); return nil, nil })}
	t.Setenv("PATH", t.TempDir())
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
}
func TestNativeProtocolAndArgumentRefusals(t *testing.T) {
	b := newTestBackend(t)
	for _, op := range []string{"menu", "status", "install", "update", "use", "remove", "uninstall", "integrate"} {
		if err := action(b, op, "--bad"); err == nil {
			t.Fatal(op)
		}
	}
	if err := action(b, "integrate", "migrate-data", "1.1.0", "1.2.0"); err == nil {
		t.Fatal("missing closed acknowledgement accepted")
	}
	b.req.Operation = "menu"
	b.req.Arguments = nil
	data, _ := json.Marshal(b.req)
	var out bytes.Buffer
	if err := serve(context.Background(), bytes.NewReader(data), &out); err != nil {
		t.Fatal(err)
	}
	stream := protocol.Stream{RequestID: b.req.RequestID}
	for _, line := range bytes.Split(bytes.TrimSpace(out.Bytes()), []byte("\n")) {
		if _, err := stream.Accept(line); err != nil {
			t.Fatal(err)
		}
	}
	if stream.Result == nil || stream.Result.Status != "success" || len(stream.Result.Choices) < 8 {
		t.Fatal(stream.Result)
	}
}
func TestDataMigrationPreservesSourceAndDestination(t *testing.T) {
	b := newTestBackend(t)
	installFixture(t, b, "1.118.0")
	installFixture(t, b, "1.118.1")
	source := filepath.Join(b.root, "code_1.118.0/data")
	target := filepath.Join(b.root, "code_1.118.1/data")
	write(t, filepath.Join(source, "user-data/User/settings.json"), []byte("personal"), 0640)
	write(t, filepath.Join(target, "old"), []byte("previous target"), 0600)
	before, err := dataSnapshot(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	if err := action(b, "integrate", "migrate-data", "v1.118", "1.118.1", "--closed"); err != nil {
		t.Fatal(err)
	}
	after, err := dataSnapshot(context.Background(), source)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("source changed", err)
	}
	copied, err := dataSnapshot(context.Background(), target)
	if err != nil || !reflect.DeepEqual(before, copied) {
		t.Fatal("copy differs", err)
	}
	paths := preserved(t, b, "code_1.118.1")
	if len(paths) != 1 {
		t.Fatal(paths)
	}
	assertContents(t, filepath.Join(paths[0], "old"), "previous target")
	if err := action(b, "uninstall"); err != nil {
		t.Fatal(err)
	}
	assertContents(t, filepath.Join(paths[0], "old"), "previous target")
}
func TestDataMigrationCancellationRollsBack(t *testing.T) {
	b := newTestBackend(t)
	installFixture(t, b, "1.118.0")
	installFixture(t, b, "1.118.1")
	source := filepath.Join(b.root, "code_1.118.0/data")
	target := filepath.Join(b.root, "code_1.118.1/data")
	write(t, filepath.Join(source, "new"), []byte("new"), 0600)
	write(t, filepath.Join(target, "old"), []byte("old"), 0600)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b.log = func(s string) {
		if strings.HasPrefix(s, "Previous destination") {
			cancel()
		}
	}
	err := b.withLock(func() error { return b.migrateData(ctx, "code_1.118.0", "code_1.118.1") })
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	assertContents(t, filepath.Join(target, "old"), "old")
	assertAbsent(t, filepath.Join(target, "new"))
	assertContents(t, filepath.Join(source, "new"), "new")
	stages, _ := filepath.Glob(filepath.Join(b.root, ".atm-data-stage-*"))
	if len(stages) > 0 {
		t.Fatal(stages)
	}
}
func TestDataSnapshotRejectsLinksAndSpecialModes(t *testing.T) {
	for _, kind := range []string{"symlink", "dir-symlink", "setuid", "setgid", "sticky-dir"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			file := filepath.Join(root, "entry")
			switch kind {
			case "symlink":
				if err := os.Symlink("/tmp", file); err != nil {
					t.Fatal(err)
				}
			case "dir-symlink":
				if err := os.Symlink(root, filepath.Join(root, "data")); err != nil {
					t.Fatal(err)
				}
				root = filepath.Join(root, "data")
			case "sticky-dir":
				if err := os.Mkdir(file, 0700|os.ModeSticky); err != nil {
					t.Fatal(err)
				}
			default:
				write(t, file, []byte("private"), 0600)
				mode := os.FileMode(0600) | os.ModeSetuid
				if kind == "setgid" {
					mode = 0600 | os.ModeSetgid
				}
				if err := os.Chmod(file, mode); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := dataSnapshot(context.Background(), root); err == nil {
				t.Fatal("unsafe copy accepted")
			}
		})
	}
}
func TestExtensionsLiteralExplicitAndPartialFailure(t *testing.T) {
	b := newTestBackend(t)
	installFixture(t, b, "1.118.1")
	write(t, filepath.Join(b.req.Home, "plugins/vscode/extensions.txt"), []byte("# note\nfoo.bar\nFoo.Bar\nother.extension\n"), 0600)
	t.Setenv("NODE_OPTIONS", "--require /bad")
	t.Setenv("VSCODE_IPC_HOOK_CLI", "/bad")
	calls := 0
	b.run = func(ctx context.Context, exe string, args, env []string) error {
		calls++
		if exe != filepath.Join(b.root, "code_1.118.1/code") {
			t.Fatal(exe)
		}
		if len(args) != 4 || args[1] != "--install-extension" || args[3] != "--force" {
			t.Fatal(args)
		}
		joined := strings.Join(env, "\n")
		if strings.Contains(joined, "NODE_OPTIONS") || strings.Contains(joined, "VSCODE_IPC_HOOK_CLI") || !strings.Contains(joined, "ELECTRON_RUN_AS_NODE=1") {
			t.Fatal(env)
		}
		if calls == 2 {
			return fmt.Errorf("mock exit 1")
		}
		return nil
	}
	if err := action(b, "integrate", "extensions"); err == nil || calls != 2 {
		t.Fatal(err, calls)
	}
	write(t, filepath.Join(b.req.Home, "plugins/vscode/extensions.txt"), []byte("foo.bar\n--install-extension=evil\n"), 0600)
	calls = 0
	if err := action(b, "integrate", "extensions"); err == nil || calls != 0 {
		t.Fatal("bad list executed", err, calls)
	}
}
func TestIntegrationRefusesUnownedAndEditedPaths(t *testing.T) {
	for _, kind := range []string{"desktop", "cli", "profile", "owned-desktop", "owned-cli"} {
		t.Run(kind, func(t *testing.T) {
			b := newTestBackend(t)
			installFixture(t, b, "1.118.1")
			switch kind {
			case "desktop":
				write(t, filepath.Join(b.home, ".local/share/applications/code.desktop"), []byte("mine"), 0644)
			case "cli":
				write(t, b.cliPath(), []byte("mine"), 0700)
			case "profile":
				write(t, filepath.Join(b.home, ".profile"), []byte(profileStart+"\ncustom\n"+profileEnd+"\n"), 0600)
			case "owned-desktop", "owned-cli":
				if err := action(b, "integrate"); err != nil {
					t.Fatal(err)
				}
				if kind == "owned-desktop" {
					write(t, filepath.Join(b.home, ".local/share/applications/code.desktop"), []byte("changed"), 0644)
				} else {
					if err := os.Remove(b.cliPath()); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink("/user/custom", b.cliPath()); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := action(b, "integrate"); err == nil {
				t.Fatal("overwrote unowned/edited integration")
			}
			if strings.HasPrefix(kind, "owned-") {
				if err := action(b, "uninstall"); err == nil {
					t.Fatal("deleted edited integration")
				}
				assertCurrent(t, b, "code_1.118.1")
			}
		})
	}
}
func TestOfficialPackageNameRegression(t *testing.T) {
	// Microsoft build/gulpfile.vscode.ts stamps package.name from product.nameShort
	// (Code), whereas product.applicationName is the lowercase executable name.
	b := newTestBackend(t)
	installFixture(t, b, "1.118.1")
	assertCurrent(t, b, "code_1.118.1")
	if runtime.GOARCH == "amd64" {
		i, err := os.Stat(filepath.Join(b.root, "code_1.118.1/chrome-sandbox"))
		if err != nil || i.Mode()&os.ModeSetuid != 0 {
			t.Fatal("installer retained setuid", err)
		}
	}
}

func TestHTTPRefusalsAndCancelledDownload(t *testing.T) {
	for _, raw := range []string{"http://update.code.visualstudio.com/api/releases/stable", "https://update.code.visualstudio.com.evil/api/releases/stable", "https://user@update.code.visualstudio.com/api/releases/stable", "https://update.code.visualstudio.com:443/api/releases/stable", "https://update.code.visualstudio.com/api/releases/stable?bad=1"} {
		if approvedURL(raw, false) {
			t.Fatal(raw)
		}
	}
	b := newTestBackend(t)
	requests := 0
	b.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://evil.invalid/archive.tar.gz"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})}
	if _, err := b.releases(context.Background()); err == nil || requests != 1 {
		t.Fatal("followed unapproved redirect", err, requests)
	}
	for _, kind := range []string{"http-error", "encoded", "oversize"} {
		b.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			header := http.Header{}
			status := 200
			body := "[]"
			if kind == "http-error" {
				status = 500
			}
			if kind == "encoded" {
				header.Set("Content-Encoding", "gzip")
			}
			if kind == "oversize" {
				body = strings.Repeat(" ", (2<<20)+1)
			}
			return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		})}
		if _, err := b.releases(context.Background()); err == nil {
			t.Fatal(kind)
		}
	}
	installFixture(t, b, "1.118.0")
	data := archiveItems(t, payloadItems("1.118.1", b.platform))
	fixture(t, b, "1.118.1", data, nil)
	original := b.client.Transport
	b.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == "GET" && r.URL.Host == "vscode.download.prss.microsoft.com" {
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		return original.RoundTrip(r)
	})
	b.req.Operation = "install"
	b.req.Arguments = []string{"1.118.1"}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := b.execute(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	assertCurrent(t, b, "code_1.118.0")
	assertAbsent(t, filepath.Join(b.root, "code_1.118.1"))
	stages, _ := filepath.Glob(filepath.Join(b.root, ".atm-stage-*"))
	if len(stages) > 0 {
		t.Fatal(stages)
	}
}
