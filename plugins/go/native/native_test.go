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
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ATM_DRY_RUN", "")
	req := protocol.Request{Protocol: 1, RequestID: strings.Repeat("a", 32), PluginID: "go", Operation: "menu", Home: filepath.Join(t.TempDir(), "runtime"), AppsDir: filepath.Join(t.TempDir(), "Apps")}
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

// An inert, parseable ELF header: payload validation must never execute it.
func elfBytes(platform string) []byte {
	data := make([]byte, 64)
	copy(data, []byte{127, 'E', 'L', 'F', 2, 1, 1})
	binary.LittleEndian.PutUint16(data[16:], 2)
	machine := uint16(62)
	if platform == "linux-arm64" {
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
func archiveBytes(t *testing.T, v, platform string, extra ...tarItem) []byte {
	items := []tarItem{{"go/bin/go", string(elfBytes(platform)), "", tar.TypeReg, 0755}, {"go/VERSION", "go" + v + "\ntime 2026-01-01T00:00:00Z\n", "", tar.TypeReg, 0644}, {"go/src/hello.go", "package hello\n", "", tar.TypeReg, 0644}}
	return archiveItems(t, append(items, extra...))
}
func rowFor(v, platform string, payload []byte) release {
	return release{Version: "go" + v, Stable: true, Files: []releaseFile{{Filename: "go" + v + "." + platform + ".tar.gz", OS: "linux", Arch: strings.TrimPrefix(platform, "linux-"), Version: "go" + v, SHA256: fmt.Sprintf("%x", sha256.Sum256(payload)), Size: int64(len(payload)), Kind: "archive"}}}
}
func fixture(t *testing.T, b *backend, rows []release, payloads map[string][]byte) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			if r.URL.RawQuery != "mode=json&include=all" {
				t.Errorf("unexpected index query: %s", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(rows)
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
		data := archiveBytes(t, v, b.platform)
		row := rowFor(v, b.platform, data)
		rows = append(rows, row)
		payloads[row.Files[0].Filename] = data
	}
	fixture(t, b, rows, payloads)
}
func legacy(t *testing.T, b *backend, v string) {
	t.Helper()
	write(t, filepath.Join(b.root, v, "bin/go"), elfBytes(b.platform), 0700)
	write(t, filepath.Join(b.root, v, "VERSION"), []byte("go"+v+"\n"), 0600)
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
		t.Fatalf("temporary artifacts remain: %v", names)
	}
}

func TestLifecyclePreservesWorkspaceAndProfiles(t *testing.T) {
	b := newTestBackend(t)
	releasesFixture(t, b, "1.26.1", "1.26.2")
	original := "# user\n# >>> ATM native node PATH >>>\nexport PATH='/node':\"$PATH\"\n# <<< ATM native node PATH <<<\n"
	for _, p := range []string{".profile", ".bashrc", ".zshrc"} {
		write(t, filepath.Join(b.home, p), []byte(original), 0640)
	}
	workspace := filepath.Join(b.root, "workspace/src/user.go")
	write(t, workspace, []byte("precious"), 0600)
	if err := action(b, "install"); err != nil {
		t.Fatal(err)
	}
	assertCurrent(t, b, "1.26.2")
	if err := b.verifyOwned(context.Background(), "1.26.2"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{".profile", ".bashrc", ".zshrc"} {
		data, _ := os.ReadFile(filepath.Join(b.home, p))
		text := string(data)
		if !strings.HasPrefix(text, original) || !strings.Contains(text, "export GOROOT="+shellQuote(filepath.Join(b.root, "current"))) || !strings.Contains(text, "export GOPATH="+shellQuote(filepath.Join(b.root, "workspace"))) || !strings.Contains(text, shellQuote(filepath.Join(b.root, "workspace/bin"))) {
			t.Fatal(text)
		}
	}
	assertAbsent(t, b.req.Home)
	for _, p := range []string{"bin", "pkg", "src"} {
		if err := realDirs(filepath.Join(b.root, "workspace", p), false); err != nil {
			t.Fatal(err)
		}
	}
	if err := action(b, "install", "--version", "go1.26.1"); err != nil {
		t.Fatal(err)
	}
	if err := action(b, "remove", "1.26.1"); err == nil {
		t.Fatal("removed current")
	}
	if err := action(b, "update"); err != nil {
		t.Fatal(err)
	}
	assertCurrent(t, b, "1.26.2")
	if err := action(b, "remove", "1.26.1"); err != nil {
		t.Fatal(err)
	}
	assertAbsent(t, filepath.Join(b.root, "1.26.1"))
	if err := action(b, "integrate"); err != nil {
		t.Fatal(err)
	}
	if err := action(b, "uninstall"); err != nil {
		t.Fatal(err)
	}
	assertCurrent(t, b, "")
	data, _ := os.ReadFile(workspace)
	if string(data) != "precious" {
		t.Fatal("workspace lost")
	}
	for _, p := range []string{".profile", ".bashrc", ".zshrc"} {
		data, _ := os.ReadFile(filepath.Join(b.home, p))
		if string(data) != original {
			t.Fatal("changed unrelated profile content", string(data))
		}
	}
	assertCleanStage(t, b)
}

func TestLegacySelectionNeverAdoptsOrDeletes(t *testing.T) {
	b := newTestBackend(t)
	legacy(t, b, "1.20")
	legacy(t, b, "1.26.1")
	if err := action(b, "use", "1.20"); err != nil {
		t.Fatal(err)
	}
	assertAbsent(t, filepath.Join(b.home, ".zshrc"))
	if err := action(b, "use", "1.26.1"); err != nil {
		t.Fatal(err)
	}
	if err := action(b, "remove", "1.20"); err == nil {
		t.Fatal("legacy deletion accepted")
	}
	releasesFixture(t, b, "1.26.1", "1.26.2")
	if err := action(b, "install", "--version=1.26.1"); err != nil {
		t.Fatal(err)
	}
	assertAbsent(t, filepath.Join(b.root, "1.26.1", receiptName))
	if err := action(b, "install"); err != nil {
		t.Fatal(err)
	}
	if err := action(b, "use", "1.20"); err != nil {
		t.Fatal(err)
	}
	if err := action(b, "uninstall"); err != nil {
		t.Fatal(err)
	}
	assertCurrent(t, b, "1.20")
	if err := b.validInstalled("1.26.1"); err != nil {
		t.Fatal("legacy deleted", err)
	}
	assertAbsent(t, filepath.Join(b.root, "1.26.2"))
}

func TestUpdateCannotDowngradeActiveSelection(t *testing.T) {
	b := newTestBackend(t)
	legacy(t, b, "1.28.1")
	legacy(t, b, "1.27.1")
	if err := action(b, "use", "1.28.1"); err != nil {
		t.Fatal(err)
	}
	releasesFixture(t, b, "1.27.1")
	for _, args := range [][]string{nil, {"--version", "1.27.1"}} {
		if err := action(b, "update", args...); err == nil {
			t.Fatal("downgrade accepted")
		}
		assertCurrent(t, b, "1.28.1")
	}
}

func TestOwnedChangesBlockUseRemovalAndUninstall(t *testing.T) {
	for _, change := range []string{"extra", "edited", "mode", "missing", "VERSION", "binary", "receipt", "symlink"} {
		t.Run(change, func(t *testing.T) {
			b := newTestBackend(t)
			releasesFixture(t, b, "1.26.1")
			if err := action(b, "install"); err != nil {
				t.Fatal(err)
			}
			legacy(t, b, "1.25.1")
			if err := action(b, "use", "1.25.1"); err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(b.root, "1.26.1")
			switch change {
			case "extra":
				write(t, filepath.Join(dir, "user"), []byte("mine"), 0600)
			case "edited":
				write(t, filepath.Join(dir, "src/hello.go"), []byte("edited"), 0600)
			case "mode":
				if err := os.Chmod(filepath.Join(dir, "src/hello.go"), 0700); err != nil {
					t.Fatal(err)
				}
			case "missing":
				if err := os.Remove(filepath.Join(dir, "src/hello.go")); err != nil {
					t.Fatal(err)
				}
			case "VERSION":
				write(t, filepath.Join(dir, "VERSION"), []byte("go1.26.9"), 0600)
			case "binary":
				write(t, filepath.Join(dir, "bin/go"), []byte("not elf"), 0700)
			case "receipt":
				write(t, filepath.Join(dir, receiptName), []byte("{}"), 0600)
			case "symlink":
				if err := os.Symlink("VERSION", filepath.Join(dir, "link")); err != nil {
					t.Fatal(err)
				}
			}
			for _, op := range []string{"use", "remove", "uninstall"} {
				args := []string{"1.26.1"}
				if op == "uninstall" {
					args = nil
				}
				if err := action(b, op, args...); err == nil {
					t.Fatal("changed payload accepted", op)
				}
			}
			assertCurrent(t, b, "1.25.1")
			if _, err := os.Stat(dir); err != nil {
				t.Fatal("changed version deleted")
			}
		})
	}
}

func TestUnsafePathsAndProfiles(t *testing.T) {
	for _, kind := range []string{"root", "ancestor", "current", "version", "bin", "profile", "workspace"} {
		t.Run(kind, func(t *testing.T) {
			b := newTestBackend(t)
			target := t.TempDir()
			legacy(t, b, "1.26.1")
			switch kind {
			case "root":
				if err := os.Rename(b.root, b.root+"-old"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, b.root); err != nil {
					t.Fatal(err)
				}
			case "ancestor":
				if err := os.Rename(b.req.AppsDir, b.req.AppsDir+"-old"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, b.req.AppsDir); err != nil {
					t.Fatal(err)
				}
			case "current":
				if err := os.Symlink(target, filepath.Join(b.root, "current")); err != nil {
					t.Fatal(err)
				}
			case "version":
				if err := os.Symlink(target, filepath.Join(b.root, "1.26.2")); err != nil {
					t.Fatal(err)
				}
			case "bin":
				if err := os.Rename(filepath.Join(b.root, "1.26.1/bin"), filepath.Join(b.root, "1.26.1/oldbin")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, filepath.Join(b.root, "1.26.1/bin")); err != nil {
					t.Fatal(err)
				}
			case "profile":
				if err := os.Symlink(filepath.Join(target, "profile"), filepath.Join(b.home, ".bashrc")); err != nil {
					t.Fatal(err)
				}
			case "workspace":
				if err := os.Symlink(target, filepath.Join(b.root, "workspace")); err != nil {
					t.Fatal(err)
				}
			}
			v := "1.26.1"
			if kind == "version" {
				v = "1.26.2"
			}
			if err := action(b, "use", v); err == nil {
				t.Fatal("unsafe path accepted")
			}
			entries, _ := os.ReadDir(target)
			if len(entries) != 0 {
				t.Fatal("wrote through symlink")
			}
			assertAbsent(t, filepath.Join(b.home, ".profile"))
		})
	}
}

func TestProfileParsingAndRollback(t *testing.T) {
	for _, bad := range []string{profileStart, profileEnd, profileEnd + "\n" + profileStart, profileStart + "\n" + profileStart + "\n" + profileEnd, "x" + profileStart + "\n" + profileEnd} {
		if _, err := profileContent([]byte(bad), ""); err == nil {
			t.Fatal("bad block accepted", bad)
		}
	}
	b := newTestBackend(t)
	write(t, filepath.Join(b.home, ".profile"), []byte("user\n"), 0640)
	plans, err := b.profilePlans(false)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(b.home, ".bashrc"), []byte("concurrent edit"), 0600)
	if _, err := applyProfiles(context.Background(), plans); err == nil {
		t.Fatal("concurrent profile overwrite")
	}
	data, _ := os.ReadFile(filepath.Join(b.home, ".profile"))
	if string(data) != "user\n" {
		t.Fatal("rollback failed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := applyProfiles(ctx, plans); err == nil {
		t.Fatal("ignored cancellation")
	}
}

func TestReleaseSelectionAndMetadata(t *testing.T) {
	b := newTestBackend(t)
	releasesFixture(t, b, "1.26.1", "1.27.0", "1.25.7", "1.26.9", "1.26.10", "1.24.9", "1.26.8")
	choices, err := b.menu(context.Background(), []string{"--latest"})
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, c := range choices[:5] {
		got = append(got, c.ID)
	}
	if !reflect.DeepEqual(got, []string{"1.27.0", "1.26.10", "1.26.9", "1.26.8", "1.26.1"}) {
		t.Fatal(got)
	}
	if err := protocol.ValidateChoices(choices); err != nil {
		t.Fatal(err)
	}
	if err := action(b, "install", "--version", "1.99.9"); err == nil {
		t.Fatal("unknown version accepted")
	}
	for _, kind := range []string{"filename", "hash", "size", "zero", "version", "duplicate", "unstable", "arch"} {
		t.Run(kind, func(t *testing.T) {
			b := newTestBackend(t)
			data := archiveBytes(t, "1.26.1", b.platform)
			row := rowFor("1.26.1", b.platform, data)
			rows := []release{row}
			switch kind {
			case "filename":
				rows[0].Files[0].Filename = "../go.tar.gz"
			case "hash":
				rows[0].Files[0].SHA256 = "not a hash"
			case "size":
				rows[0].Files[0].Size = limits.DownloadBytes + 1
			case "zero":
				rows[0].Files[0].Size = 0
			case "version":
				rows[0].Files[0].Version = "go1.26.2"
			case "duplicate":
				rows = append(rows, row)
			case "unstable":
				rows[0].Stable = false
			case "arch":
				rows[0].Files[0].Arch = "386"
			}
			fixture(t, b, rows, nil)
			if _, err := b.releases(context.Background()); err == nil {
				t.Fatal("invalid metadata accepted")
			}
		})
	}
}

func TestFailedInstallPreservesSelectionAndCleansStage(t *testing.T) {
	for _, kind := range []string{"checksum", "short", "oversize", "truncated", "VERSION", "binary", "architecture", "traversal", "link", "special", "duplicate", "reserved"} {
		t.Run(kind, func(t *testing.T) {
			b := newTestBackend(t)
			legacy(t, b, "1.25.1")
			if err := action(b, "use", "1.25.1"); err != nil {
				t.Fatal(err)
			}
			data := archiveBytes(t, "1.26.1", b.platform)
			switch kind {
			case "VERSION":
				data = archiveBytes(t, "1.26.2", b.platform)
			case "binary":
				data = archiveItems(t, []tarItem{{"go/bin/go", "not elf", "", tar.TypeReg, 0755}, {"go/VERSION", "go1.26.1", "", tar.TypeReg, 0644}})
			case "architecture":
				other := "linux-arm64"
				if b.platform == other {
					other = "linux-amd64"
				}
				data = archiveBytes(t, "1.26.1", other)
			case "traversal":
				data = archiveBytes(t, "1.26.1", b.platform, tarItem{"go/../../escape", "bad", "", tar.TypeReg, 0644})
			case "link":
				data = archiveBytes(t, "1.26.1", b.platform, tarItem{"go/link", "", "VERSION", tar.TypeSymlink, 0777})
			case "special":
				data = archiveBytes(t, "1.26.1", b.platform, tarItem{"go/pipe", "", "", tar.TypeFifo, 0600})
			case "duplicate":
				data = archiveBytes(t, "1.26.1", b.platform, tarItem{"go/VERSION", "go1.26.1", "", tar.TypeReg, 0644})
			case "reserved":
				data = archiveBytes(t, "1.26.1", b.platform, tarItem{"go/" + receiptName, "{}", "", tar.TypeReg, 0644})
			case "truncated":
				data = data[:len(data)-6]
			}
			row := rowFor("1.26.1", b.platform, data)
			switch kind {
			case "checksum":
				row.Files[0].SHA256 = strings.Repeat("0", 64)
			case "short":
				row.Files[0].Size++
			case "oversize":
				row.Files[0].Size--
			}
			fixture(t, b, []release{row}, map[string][]byte{row.Files[0].Filename: data})
			if err := action(b, "install"); err == nil {
				t.Fatal("bad artifact accepted")
			}
			assertCurrent(t, b, "1.25.1")
			assertAbsent(t, filepath.Join(b.root, "1.26.1"))
			assertCleanStage(t, b)
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestDryRunAndOfflineStartupNoIO(t *testing.T) {
	b := newTestBackend(t)
	b.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Error("unexpected network")
		return nil, fmt.Errorf("no network")
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
	assertAbsent(t, filepath.Join(b.home, ".profile"))
	assertAbsent(t, b.req.Home)
	t.Setenv("ATM_DRY_RUN", "")
	b.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, fmt.Errorf("offline") })}
	choices, err := b.menu(context.Background(), []string{"--latest"})
	if err != nil || len(choices) != 2 || choices[1].ID != "back" {
		t.Fatal(choices, err)
	}
}

func TestFetchBoundsRedirectAndCancellation(t *testing.T) {
	b := newTestBackend(t)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/slow":
			<-r.Context().Done()
		case "/redirect":
			http.Redirect(w, r, "https://example.invalid/x", 302)
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
	for _, path := range []string{"/large", "/redirect", "/encoding"} {
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

func TestOfficialRedirectPolicyWithTLS(t *testing.T) {
	for _, target := range []string{"https://dl.google.com/go/go1.26.1.linux-amd64.tar.gz", "http://dl.google.com/go/go1.26.1.linux-amd64.tar.gz", "https://evil.invalid/go/go1.26.1.linux-amd64.tar.gz", "https://dl.google.com/go/different.tar.gz", "https://user@dl.google.com/go/go1.26.1.linux-amd64.tar.gz", "https://go.dev/other.tar.gz"} {
		t.Run(target, func(t *testing.T) {
			b := newTestBackend(t)
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Host == "go.dev" {
					http.Redirect(w, r, target, 302)
					return
				}
				fmt.Fprint(w, "archive")
			}))
			defer srv.Close()
			local, _ := url.Parse(srv.URL)
			transport := srv.Client().Transport
			b.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				clone := r.Clone(r.Context())
				u := *r.URL
				u.Host = local.Host
				clone.URL = &u
				clone.Host = r.URL.Host
				return transport.RoundTrip(clone)
			})}
			var data bytes.Buffer
			err := b.fetch(context.Background(), "/go1.26.1.linux-amd64.tar.gz", &data, 100)
			if target == "https://dl.google.com/go/go1.26.1.linux-amd64.tar.gz" {
				if err != nil || data.String() != "archive" {
					t.Fatal(err, data.String())
				}
			} else if err == nil {
				t.Fatal("unsafe redirect accepted")
			}
		})
	}
}

func TestCancellationDuringInstall(t *testing.T) {
	b := newTestBackend(t)
	legacy(t, b, "1.25.1")
	if err := action(b, "use", "1.25.1"); err != nil {
		t.Fatal(err)
	}
	data := archiveBytes(t, "1.26.1", b.platform)
	row := rowFor("1.26.1", b.platform, data)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			_ = json.NewEncoder(w).Encode([]release{row})
			return
		}
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	b.base = srv.URL
	b.client = srv.Client()
	b.req.Operation = "install"
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := b.execute(ctx); err == nil {
		t.Fatal("cancelled install succeeded")
	}
	assertCurrent(t, b, "1.25.1")
	assertAbsent(t, filepath.Join(b.root, "1.26.1"))
	assertCleanStage(t, b)
}

func TestCompiledBackendThroughCoreEmptyPATH(t *testing.T) {
	b := newTestBackend(t)
	root := t.TempDir()
	dir := filepath.Join(root, "plugins/go")
	manifest, err := os.ReadFile("../plugin.json")
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "plugin.json"), manifest, 0600)
	bin := filepath.Join(dir, "bin/linux-"+runtime.GOARCH+"/atm-go")
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
	plan, err := runner.Plugin(c, ps[0], "install", nil)
	if err != nil {
		t.Fatal(err)
	}
	dry := c
	dry.DryRun = true
	if out := runner.RunNative(context.Background(), dry, plan, runner.NativeOptions{}); out.Code != "dry_run" {
		t.Fatal(out)
	}
	assertAbsent(t, b.root)
	t.Setenv("ATM_DRY_RUN", "1")
	if out := runner.RunNative(context.Background(), c, plan, runner.NativeOptions{}); out.Status != "success" {
		t.Fatal(out)
	}
	assertAbsent(t, b.root)
	t.Setenv("ATM_DRY_RUN", "")
	for _, op := range []string{"menu", "status", "integrate"} {
		plan, err := runner.Plugin(c, ps[0], op, nil)
		if err != nil {
			t.Fatal(err)
		}
		out := runner.RunNative(context.Background(), c, plan, runner.NativeOptions{Timeout: 5 * time.Second})
		want := "success"
		if op == "integrate" {
			want = "error"
		}
		if out.Status != want {
			t.Fatalf("%s: %+v", op, out)
		}
		if op == "menu" && len(out.Choices) != 8 {
			t.Fatal(out)
		}
		if op == "menu" && !strings.Contains(out.Message, "https://go.dev/") {
			t.Fatal("menu lost official site", out)
		}
	}
	legacy(t, b, "1.26.1")
	for _, op := range []string{"use", "integrate", "status", "uninstall"} {
		args := []string(nil)
		if op == "use" {
			args = []string{"1.26.1"}
		}
		plan, err := runner.Plugin(c, ps[0], op, args)
		if err != nil {
			t.Fatal(err)
		}
		out := runner.RunNative(context.Background(), c, plan, runner.NativeOptions{Timeout: 5 * time.Second})
		if out.Status != "success" {
			t.Fatalf("%s: %+v", op, out)
		}
	}
	assertCurrent(t, b, "1.26.1")
	assertAbsent(t, filepath.Join(root, ".profile"))
	// Prepare native-owned versions using TLS fixture installs, then exercise the
	// compiled backend's owned lifecycle through the real generic CORE runner.
	releasesFixture(t, b, "1.26.2", "1.26.3")
	for _, v := range []string{"1.26.2", "1.26.3"} {
		if err := action(b, "install", "--version", v); err != nil {
			t.Fatal(err)
		}
	}
	workspace := filepath.Join(b.root, "workspace/src/keep")
	write(t, workspace, []byte("workspace"), 0600)
	for _, step := range []struct {
		op   string
		args []string
	}{{"use", []string{"1.26.2"}}, {"remove", []string{"1.26.3"}}, {"integrate", nil}, {"uninstall", nil}} {
		plan, err := runner.Plugin(c, ps[0], step.op, step.args)
		if err != nil {
			t.Fatal(err)
		}
		out := runner.RunNative(context.Background(), c, plan, runner.NativeOptions{Timeout: 5 * time.Second})
		if out.Status != "success" {
			t.Fatalf("%s: %+v", step.op, out)
		}
	}
	assertCurrent(t, b, "")
	assertAbsent(t, filepath.Join(b.root, "1.26.2"))
	assertAbsent(t, filepath.Join(b.root, "1.26.3"))
	if err := b.validInstalled("1.26.1"); err != nil {
		t.Fatal("legacy lost", err)
	}
	data, err := os.ReadFile(workspace)
	if err != nil || string(data) != "workspace" {
		t.Fatal("workspace lost", err)
	}
}

func TestProtocolAndArgumentRefusals(t *testing.T) {
	b := newTestBackend(t)
	for _, op := range []string{"status", "integrate", "uninstall", "use", "remove", "install", "update", "menu", "unknown"} {
		if err := action(b, op, "--bad"); err == nil {
			t.Fatal(op)
		}
	}
	for _, v := range []string{"../1.2.3", "1.2.3beta1", "01.2.3", "1.2.3/other", "1.2.3\n", "v1.2.3"} {
		if _, err := normalizeVersion(v); err == nil {
			t.Fatal(v)
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
