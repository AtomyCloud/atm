//go:build linux && amd64

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
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
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/AtomyCloud/atm/internal/config"
	"github.com/AtomyCloud/atm/internal/plugin"
	"github.com/AtomyCloud/atm/internal/protocol"
	"github.com/AtomyCloud/atm/internal/runner"
)

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
	req := protocol.Request{Protocol: 1, RequestID: strings.Repeat("a", 32), PluginID: "android_studio", Operation: "menu", Home: filepath.Join(t.TempDir(), "runtime"), AppsDir: filepath.Join(t.TempDir(), "Apps")}
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

func sdkItems(a artifact, native bool) []tarItem {
	launcher := "bin/studio.sh"
	body := "#!/bin/sh\nexit 99\n"
	icon := "bin/studio.svg"
	if native {
		launcher = "bin/studio"
		body = string(elfBytes(false))
		icon = "bin/studio.png"
	}
	parts := strings.Split(a.Version, ".")
	version := strings.Join(parts[:3], ".")
	p := `{"name":"Android Studio","version":"` + version + `","buildNumber":"253.12345.99","productCode":"AI","launch":[{"os":"Linux","arch":"amd64","launcherPath":"` + launcher + `"}]}`
	return []tarItem{{"android-studio/" + launcher, body, "", tar.TypeReg, 0755}, {"android-studio/jbr/bin/java", string(elfBytes(false)), "", tar.TypeReg, 0755}, {"android-studio/product-info.json", p, "", tar.TypeReg, 0644}, {"android-studio/build.txt", "AI-253.12345.99\n", "", tar.TypeReg, 0644}, {"android-studio/" + icon, "icon", "", tar.TypeReg, 0644}, {"android-studio/lib/source", "sdk source", "", tar.TypeReg, 0644}, {"android-studio/link", "", "lib/source", tar.TypeSymlink, 0777}}
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
func installArgs(a artifact) []string {
	return []string{"--id", a.ID, "--name", a.Name, "--version", a.Version, "--package", a.Package, "--url", a.URL, "--sha256", a.SHA256}
}
func fixture(t *testing.T, b *backend, a artifact, data []byte) artifact {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/archive.tar.gz" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)
	b.client = srv.Client()
	a.URL = srv.URL + "/archive.tar.gz"
	a.SHA256 = fmt.Sprintf("%x", sha256.Sum256(data))
	return a
}
func installFixture(t *testing.T, b *backend, index int, native bool) artifact {
	t.Helper()
	a := catalog[index]
	a = fixture(t, b, a, archiveItems(t, sdkItems(a, native)))
	if err := action(b, "install", installArgs(a)...); err != nil {
		t.Fatal(err)
	}
	return a
}
func legacy(t *testing.T, b *backend, a artifact) {
	t.Helper()
	for _, item := range sdkItems(a, false) {
		name := filepath.Join(b.root, a.dir(), strings.TrimPrefix(item.name, "android-studio/"))
		if item.kind == tar.TypeSymlink {
			if err := os.Symlink(item.link, name); err != nil {
				t.Fatal(err)
			}
		} else {
			write(t, name, []byte(item.body), os.FileMode(item.mode))
		}
	}
	meta := fmt.Sprintf("ATM_ANDROID_STUDIO_ID=%q\nATM_ANDROID_STUDIO_DISPLAY_NAME=%q\nATM_ANDROID_STUDIO_VERSION=%q\n", a.ID, a.Name, a.Version)
	write(t, filepath.Join(b.root, a.dir(), ".atm-meta"), []byte(meta), 0600)
}
func assertCurrent(t *testing.T, b *backend, want string) {
	t.Helper()
	got, err := b.current()
	if err != nil || got != want {
		t.Fatalf("current=%q want=%q err=%v", got, want, err)
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
		t.Fatalf("temporary leftovers %v", names)
	}
}

func TestLifecycleAndDesktopAcrossLauncherLayouts(t *testing.T) {
	b := newTestBackend(t)
	t.Setenv("PATH", t.TempDir())
	original := "# user settings\n"
	for _, p := range []string{".profile", ".bashrc", ".zshrc"} {
		write(t, filepath.Join(b.home, p), []byte(original), 0640)
	}
	a := installFixture(t, b, 0, false)
	for _, p := range []string{".profile", ".bashrc", ".zshrc"} {
		data, _ := os.ReadFile(filepath.Join(b.home, p))
		if string(data) != original {
			t.Fatal("install edited profile")
		}
	}
	assertAbsent(t, filepath.Join(b.home, ".local/share/applications", desktopFileName))
	if err := action(b, "integrate"); err != nil {
		t.Fatal(err)
	}
	desktop := filepath.Join(b.home, ".local/share/applications", desktopFileName)
	data, _ := os.ReadFile(desktop)
	if !bytes.Contains(data, []byte("current/bin/studio.sh")) {
		t.Fatal(string(data))
	}
	a2 := installFixture(t, b, 1, true)
	assertCurrent(t, b, a2.dir())
	data, _ = os.ReadFile(desktop)
	if !bytes.Contains(data, []byte("current/bin/studio\"")) || !bytes.Contains(data, []byte("studio.png")) {
		t.Fatal("stale desktop", string(data))
	}
	if err := action(b, "use", a.target()); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(desktop)
	if !bytes.Contains(data, []byte("studio.sh")) || !bytes.Contains(data, []byte("studio.svg")) {
		t.Fatal("desktop did not follow selection")
	}
	if err := action(b, "remove", a.target()); err == nil {
		t.Fatal("removed active version")
	}
	if err := action(b, "remove", a2.target()); err != nil {
		t.Fatal(err)
	}
	assertAbsent(t, filepath.Join(b.root, a2.dir()))
	keep := filepath.Join(b.home, ".config/Google/AndroidStudio/settings")
	write(t, keep, []byte("settings"), 0600)
	if err := action(b, "uninstall"); err != nil {
		t.Fatal(err)
	}
	assertCurrent(t, b, "")
	assertAbsent(t, desktop)
	assertAbsent(t, filepath.Join(b.root, desktopReceiptName))
	assertAbsent(t, filepath.Join(b.root, a.dir()))
	for _, p := range []string{".profile", ".bashrc", ".zshrc"} {
		data, _ := os.ReadFile(filepath.Join(b.home, p))
		if string(data) != original {
			t.Fatal("unrelated profile changed")
		}
	}
	data, _ = os.ReadFile(keep)
	if string(data) != "settings" {
		t.Fatal("external settings lost")
	}
	assertCleanStage(t, b)
}
func TestMutableSDKSelectsButDeletionRefused(t *testing.T) {
	b := newTestBackend(t)
	a := installFixture(t, b, 0, false)
	a2 := installFixture(t, b, 1, false)
	dir := filepath.Join(b.root, a.dir())
	write(t, filepath.Join(dir, "plugins/user/custom.jar"), []byte("mine"), 0600)
	if err := action(b, "use", a.target()); err != nil {
		t.Fatal(err)
	}
	if err := action(b, "use", a2.target()); err != nil {
		t.Fatal(err)
	}
	for _, op := range []string{"remove", "uninstall"} {
		args := []string{a.target()}
		if op == "uninstall" {
			args = nil
		}
		if err := action(b, op, args...); err == nil {
			t.Fatal("modified SDK deleted")
		}
	}
	assertCurrent(t, b, a2.dir())
	if _, err := os.Stat(dir); err != nil {
		t.Fatal(err)
	}
}
func TestLegacyLiteralMetadataAndNoAdoption(t *testing.T) {
	b := newTestBackend(t)
	a := catalog[0]
	legacy(t, b, a)
	if err := action(b, "use", a.target()); err != nil {
		t.Fatal(err)
	}
	a = fixture(t, b, a, archiveItems(t, sdkItems(a, false)))
	if err := action(b, "install", installArgs(a)...); err != nil {
		t.Fatal(err)
	}
	assertAbsent(t, filepath.Join(b.root, a.dir(), receiptName))
	if err := action(b, "uninstall"); err != nil {
		t.Fatal(err)
	}
	assertCurrent(t, b, a.dir())
	write(t, filepath.Join(b.root, a.dir(), ".atm-meta"), []byte("ATM_ANDROID_STUDIO_ID=\"$(touch /tmp/should-never-run)\"\n"), 0600)
	if _, err := b.spec(a.dir()); err == nil {
		t.Fatal("nonliteral metadata accepted")
	}
	if err := action(b, "status"); err != nil {
		t.Fatal(err)
	}
}
func TestFailedInstallPreservesSelection(t *testing.T) {
	for _, kind := range []string{"hash", "truncated", "version", "build", "code", "arch", "launcher", "traversal", "symlink", "cycle", "link-parent", "duplicate", "special", "reserved"} {
		t.Run(kind, func(t *testing.T) {
			b := newTestBackend(t)
			old := catalog[0]
			legacy(t, b, old)
			if err := action(b, "use", old.target()); err != nil {
				t.Fatal(err)
			}
			a := catalog[1]
			items := sdkItems(a, false)
			switch kind {
			case "version":
				items[2].body = strings.ReplaceAll(items[2].body, "2026.1.1", "2025.1.1")
			case "build":
				items[3].body = "AI-999.999.999"
			case "code":
				items[2].body = strings.ReplaceAll(items[2].body, `"AI"`, `"XX"`)
			case "arch":
				items[1].body = string(elfBytes(true))
			case "launcher":
				items[0].body = "invalid"
			case "traversal":
				items = append(items, tarItem{"android-studio/../../escape", "x", "", tar.TypeReg, 0644})
			case "symlink":
				items = append(items, tarItem{"android-studio/bad", "", "../../escape", tar.TypeSymlink, 0777})
			case "cycle":
				items = append(items, tarItem{"android-studio/a", "", "b", tar.TypeSymlink, 0777}, tarItem{"android-studio/b", "", "a", tar.TypeSymlink, 0777})
			case "link-parent":
				items = append(items, tarItem{"android-studio/dir", "", "lib", tar.TypeSymlink, 0777}, tarItem{"android-studio/dir/file", "x", "", tar.TypeReg, 0644})
			case "duplicate":
				items = append(items, items[0])
			case "special":
				items = append(items, tarItem{"android-studio/fifo", "", "", tar.TypeFifo, 0600})
			case "reserved":
				items = append(items, tarItem{"android-studio/" + receiptName, "{}", "", tar.TypeReg, 0600})
			}
			data := archiveItems(t, items)
			if kind == "truncated" {
				data = data[:len(data)-6]
			}
			a = fixture(t, b, a, data)
			if kind == "hash" {
				a.SHA256 = strings.Repeat("0", 64)
			}
			if err := action(b, "install", installArgs(a)...); err == nil {
				t.Fatal("bad archive accepted")
			}
			assertCurrent(t, b, old.dir())
			assertAbsent(t, filepath.Join(b.root, a.dir()))
			assertCleanStage(t, b)
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestOfflineMenusDryRunAndCustomWizard(t *testing.T) {
	b := newTestBackend(t)
	t.Setenv("PATH", t.TempDir())
	b.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { t.Error("network"); return nil, fmt.Errorf("offline") })}
	for _, op := range []string{"menu", "status"} {
		if err := action(b, op); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("ATM_DRY_RUN", "1")
	for _, op := range []string{"menu", "status", "install", "use", "remove", "uninstall", "integrate"} {
		if err := action(b, op); err != nil {
			t.Fatal(err)
		}
	}
	assertAbsent(t, b.root)
	assertAbsent(t, b.req.Home)
	t.Setenv("ATM_DRY_RUN", "")
	args := []string{"--custom", "Custom Studio"}
	for _, v := range []string{"2025.3.4.6", "studio.tar.gz", "https://example.invalid/studio.tar.gz"} {
		choices, err := b.menu(context.Background(), args)
		if err != nil || len(choices) != 2 {
			t.Fatal(choices, err)
		}
		if err := protocol.ValidateChoices(choices); err != nil {
			t.Fatal(err)
		}
		args = append(args, v)
	}
	choices, err := b.menu(context.Background(), args)
	if err != nil || choices[0].Operation != "install" {
		t.Fatal(choices, err)
	}
	a, err := parseOptions(append(choices[0].Arguments, strings.Repeat("a", 64)))
	if err != nil || a.ID != "custom" {
		t.Fatal(a, err)
	}
}
func TestFetchRedirectAndCancellation(t *testing.T) {
	b := newTestBackend(t)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/slow":
			<-r.Context().Done()
		case "/redirect":
			http.Redirect(w, r, "https://evil.invalid/file", 302)
		case "/path":
			http.Redirect(w, r, "/other", 302)
		case "/encoding":
			w.Header().Set("Content-Encoding", "gzip")
			fmt.Fprint(w, "x")
		case "/oversize":
			w.Header().Set("Content-Length", fmt.Sprint(limits.DownloadBytes+1))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	b.client = srv.Client()
	for _, path := range []string{"/redirect", "/path", "/encoding", "/oversize", "/missing"} {
		if err := b.fetch(context.Background(), srv.URL+path, io.Discard); err == nil {
			t.Fatal(path)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := b.fetch(ctx, srv.URL+"/slow", io.Discard); err == nil {
		t.Fatal("ignored cancellation")
	}
}
func TestCancelledInstallCleanup(t *testing.T) {
	b := newTestBackend(t)
	old := catalog[0]
	legacy(t, b, old)
	if err := action(b, "use", old.target()); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	b.client = srv.Client()
	a := catalog[1]
	a.URL = srv.URL + "/archive"
	b.req.Operation = "install"
	b.req.Arguments = installArgs(a)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := b.execute(ctx); err == nil {
		t.Fatal("ignored cancellation")
	}
	assertCurrent(t, b, old.dir())
	assertAbsent(t, filepath.Join(b.root, a.dir()))
	assertCleanStage(t, b)
}
func TestCompiledBackendThroughCoreEmptyPATH(t *testing.T) {
	b := newTestBackend(t)
	root := t.TempDir()
	dir := filepath.Join(root, "plugins/android_studio")
	manifest, err := os.ReadFile("../plugin.json")
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "plugin.json"), manifest, 0600)
	bin := filepath.Join(dir, "bin/linux-amd64/atm-android-studio")
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
	}
	assertAbsent(t, b.root)
	assertAbsent(t, filepath.Join(b.home, ".profile"))
	a := catalog[0]
	legacy(t, b, a)
	for _, op := range []string{"use", "status", "integrate", "uninstall"} {
		args := []string(nil)
		if op == "use" {
			args = []string{a.target()}
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
	assertCurrent(t, b, a.dir())
}
func TestProtocolAndArgumentRefusals(t *testing.T) {
	b := newTestBackend(t)
	for _, op := range []string{"status", "install", "use", "remove", "uninstall", "integrate", "menu", "update"} {
		if err := action(b, op, "--bad"); err == nil {
			t.Fatal(op)
		}
	}
	for _, args := range [][]string{{"--id", "missing"}, {"--id", "panda4", "--id", "panda4"}, {"--url", "http://example.invalid/a"}, {"--name", "foo"}, {"--version"}} {
		if _, err := parseOptions(args); err == nil {
			t.Fatal(args)
		}
	}
	for _, op := range []string{"menu", "status", "use"} {
		b.req.Operation = op
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
		if stream.Result == nil {
			t.Fatal("missing terminal result")
		}
	}
}
