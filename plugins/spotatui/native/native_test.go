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
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AtomyCloud/atm/internal/protocol"
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
func absent(t *testing.T, name string) {
	t.Helper()
	if _, err := os.Lstat(name); !os.IsNotExist(err) {
		t.Fatalf("expected absent %s: %v", name, err)
	}
}
func newTestBackend(t *testing.T) *backend {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ATM_DRY_RUN", "")
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	for _, key := range []string{"ATM_SPOTATUI_INSTALL_ROOT", "ATM_SPOTATUI_LOCAL_ROOT", "ATM_SPOTATUI_LOCAL_ENTRY"} {
		t.Setenv(key, "")
	}
	b, err := newBackend(protocol.Request{Protocol: 1, RequestID: strings.Repeat("a", 32), PluginID: "spotatui", Operation: "menu", Home: t.TempDir(), AppsDir: filepath.Join(t.TempDir(), "Apps with spaces")}, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	b.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Error("unexpected network")
		return nil, fmt.Errorf("offline")
	})}
	b.look = func(string) (string, error) { return "", fmt.Errorf("absent") }
	b.run = func(context.Context, string, []string) (probeResult, error) {
		t.Fatal("unexpected process")
		return probeResult{}, nil
	}
	return b
}
func action(b *backend, op string, args ...string) error {
	b.req.Operation = op
	b.req.Arguments = args
	_, err := b.execute(context.Background())
	return err
}
func current(t *testing.T, b *backend, want string) {
	t.Helper()
	got, err := b.current()
	if err != nil || got != want {
		t.Fatalf("current=%q want=%q err=%v", got, want, err)
	}
}
func clean(t *testing.T, b *backend) {
	t.Helper()
	for _, p := range []string{".atm-stage-*", ".atm-switch-*", ".atm-native-lock", ".atm-spotatui-profile-*"} {
		matches, _ := filepath.Glob(filepath.Join(b.root, p))
		if len(matches) > 0 {
			t.Fatal(matches)
		}
	}
}
func mockDeps(b *backend) {
	b.look = func(name string) (string, error) {
		if name == "deno" {
			return "", fmt.Errorf("absent")
		}
		return "/fixture tools/" + name, nil
	}
	b.run = func(ctx context.Context, name string, args []string) (probeResult, error) {
		if filepath.Base(name) == "node" {
			return probeResult{stdout: "v22.0.0\n"}, nil
		}
		if args[len(args)-1] == "--version" {
			return probeResult{stdout: "2025.11.12\n"}, nil
		}
		return probeResult{code: 2, stderr: "[debug] Optional libraries: yt_dlp_ejs-0.3.1\n[debug] JS runtimes: node-22.0.0\n"}, nil
	}
}
func elfBytes(platform string) []byte {
	data := make([]byte, 64)
	copy(data, []byte{127, 'E', 'L', 'F', 2, 1, 1})
	binary.LittleEndian.PutUint16(data[16:], 2)
	machine := uint16(62)
	if platform == "linux-aarch64" {
		machine = 183
	}
	binary.LittleEndian.PutUint16(data[18:], machine)
	binary.LittleEndian.PutUint32(data[20:], 1)
	binary.LittleEndian.PutUint16(data[52:], 64)
	return data
}

type tarItem struct {
	name string
	body []byte
	kind byte
	mode int64
	link string
}

func archiveItems(t *testing.T, items ...tarItem) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, x := range items {
		n := int64(len(x.body))
		if x.kind != tar.TypeReg {
			n = 0
		}
		if err := tw.WriteHeader(&tar.Header{Name: x.name, Size: n, Typeflag: x.kind, Mode: x.mode, Linkname: x.link}); err != nil {
			t.Fatal(err)
		}
		if n > 0 {
			if _, err := tw.Write(x.body); err != nil {
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
func archiveBytes(t *testing.T, platform string, extra ...tarItem) []byte {
	return archiveItems(t, append([]tarItem{{"spotatui", elfBytes(platform), tar.TypeReg, 0755, ""}}, extra...)...)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func tlsFixture(t *testing.T, b *backend, handler http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	transport := srv.Client().Transport
	b.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		clone := r.Clone(r.Context())
		local := *r.URL
		local.Host = u.Host
		clone.URL = &local
		clone.Host = r.URL.Host
		return transport.RoundTrip(clone)
	})}
}
func releaseData(v, platform string, data []byte) (release, string) {
	stable := false
	name := "spotatui-" + platform + ".tar.gz"
	sums := fmt.Sprintf("%x  %s\n", sha256.Sum256(data), name)
	return release{Tag: "v" + v, Draft: &stable, Prerelease: &stable, Assets: []releaseAsset{{name, releaseBase + "v" + v + "/" + name, int64(len(data))}, {name + ".sha256", releaseBase + "v" + v + "/" + name + ".sha256", int64(len(sums))}}}, sums
}
func fixtureRelease(t *testing.T, b *backend, row release, data []byte, sums string) {
	tlsFixture(t, b, func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "api.github.com" {
			_ = json.NewEncoder(w).Encode(row)
			return
		}
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			fmt.Fprint(w, sums)
			return
		}
		_, _ = w.Write(data)
	})
}
func releasesFixture(t *testing.T, b *backend, latest string) {
	data := archiveBytes(t, b.platform)
	tlsFixture(t, b, func(w http.ResponseWriter, r *http.Request) {
		v := latest
		if r.Host == "api.github.com" {
			if strings.Contains(r.URL.Path, "/tags/") {
				v = strings.TrimPrefix(filepath.Base(r.URL.Path), "v")
			}
			row, _ := releaseData(v, b.platform, data)
			_ = json.NewEncoder(w).Encode(row)
			return
		}
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			_, sums := releaseData(v, b.platform, data)
			fmt.Fprint(w, sums)
			return
		}
		_, _ = w.Write(data)
	})
}

func TestNativeLifecycleBothLocationsAndLegacyPreservation(t *testing.T) {
	b := newTestBackend(t)
	mockDeps(b)
	releasesFixture(t, b, "0.47.0")
	preserved := []string{filepath.Join(b.atmRoot, "0.1.0/bin/spotatui"), filepath.Join(b.localRoot, "0.1.0/personal"), filepath.Join(b.home, ".config/spotatui/credentials.json"), filepath.Join(b.home, ".cache/spotatui/personal")}
	for _, p := range preserved {
		write(t, p, []byte("private"), 0600)
	}
	if err := os.Symlink("0.1.0", filepath.Join(b.atmRoot, "current")); err != nil {
		t.Fatal(err)
	}
	original := "# user settings\n"
	for _, name := range []string{".profile", ".bashrc", ".zshrc"} {
		write(t, filepath.Join(b.home, name), []byte(original), 0640)
	}
	for _, loc := range []string{"atm", "local"} {
		for _, v := range []string{"0.46.0", "0.47.0"} {
			if err := action(b, "install", "--location", loc, "--version", v, "--trust-dependencies"); err != nil {
				t.Fatal(loc, err)
			}
		}
		current(t, b, "0.47.0")
		if err := action(b, "remove", "0.47.0", "--location", loc); err == nil {
			t.Fatal("removed current")
		}
		for _, name := range []string{".profile", ".bashrc", ".zshrc"} {
			data, _ := os.ReadFile(filepath.Join(b.home, name))
			if string(data) != original {
				t.Fatal("implicit integration")
			}
		}
	}
	for _, loc := range []string{"atm", "local"} {
		if err := action(b, "use", "0.46.0", "--location", loc, "--trust-dependencies"); err != nil {
			t.Fatal(err)
		}
		if err := action(b, "update", "--location", loc, "--trust-dependencies"); err != nil {
			t.Fatal(err)
		}
		if err := action(b, "update", "--location", loc, "--version", "0.46.0", "--trust-dependencies"); err == nil {
			t.Fatal("downgrade update")
		}
		if err := action(b, "remove", "0.46.0", "--location", loc); err != nil {
			t.Fatal(err)
		}
		if err := action(b, "integrate", "--location", loc); err != nil {
			t.Fatal(err)
		}
	}
	for _, loc := range []string{"atm", "local"} {
		if err := action(b, "uninstall", "--location", loc); err != nil {
			t.Fatal(err)
		}
		current(t, b, "")
		clean(t, b)
	}
	for _, p := range preserved {
		data, _ := os.ReadFile(p)
		if string(data) != "private" {
			t.Fatal("lost", p)
		}
	}
	for _, name := range []string{".profile", ".bashrc", ".zshrc"} {
		data, _ := os.ReadFile(filepath.Join(b.home, name))
		if string(data) != original {
			t.Fatal("profile changed", string(data))
		}
		i, _ := os.Stat(filepath.Join(b.home, name))
		if i.Mode().Perm() != 0640 {
			t.Fatal("profile mode changed")
		}
	}
	absent(t, b.localEntry)
}

func TestOfflineMenusTrustRefusalsAndDryRun(t *testing.T) {
	b := newTestBackend(t)
	for _, op := range []string{"menu", "status"} {
		if err := action(b, op); err != nil {
			t.Fatal(err)
		}
	}
	for _, sub := range []string{"actions", "versions", "dependencies", "usage"} {
		b.req.Operation = "menu"
		b.req.Arguments = []string{sub}
		choices, err := b.execute(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if err = protocol.ValidateChoices(choices); err != nil {
			t.Fatal(err)
		}
	}
	for _, op := range []string{"install", "update", "use"} {
		args := []string{}
		if op == "use" {
			args = append(args, "0.47.0")
		}
		if err := action(b, op, args...); err == nil || !strings.Contains(err.Error(), "trust") {
			t.Fatal("missing trust accepted", err)
		}
	}
	absent(t, b.root)
	t.Setenv("ATM_DRY_RUN", "1")
	b.look = func(string) (string, error) { t.Fatal("dry-run inspected PATH"); return "", nil }
	for _, op := range []string{"menu", "status", "install", "update", "use", "remove", "uninstall", "integrate"} {
		if err := action(b, op); err != nil {
			t.Fatal(err)
		}
	}
	// Missing HOME and malformed paths prove dry-run exits before backend setup.
	t.Setenv("HOME", "")
	b.req.AppsDir = "relative"
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
	if stream.Result == nil || stream.Result.Status != "success" {
		t.Fatal(stream.Result)
	}
	absent(t, b.root)
}

func TestForeignLocalEntryAndLegacyUseRefusedBeforeProbes(t *testing.T) {
	b := newTestBackend(t)
	write(t, b.localEntry, []byte("foreign"), 0700)
	for _, op := range []string{"install", "update", "use"} {
		args := []string{"--location", "local", "--trust-dependencies"}
		if op == "use" {
			args = append(args, "0.47.0")
		}
		if err := action(b, op, args...); err == nil {
			t.Fatal(op)
		}
	}
	data, _ := os.ReadFile(b.localEntry)
	if string(data) != "foreign" {
		t.Fatal("foreign entry changed")
	}
	absent(t, filepath.Join(b.localRoot, "native-v1"))
	write(t, filepath.Join(b.atmRoot, "0.47.0/bin/spotatui"), []byte("legacy"), 0700)
	if err := action(b, "use", "0.47.0", "--trust-dependencies"); err == nil {
		t.Fatal("legacy adopted")
	}
	absent(t, filepath.Join(b.atmRoot, "native-v1"))
}

func TestPayloadAndReceiptChangesRefused(t *testing.T) {
	for _, kind := range []string{"extra", "edited", "mode", "missing", "receipt", "link", "dependency"} {
		t.Run(kind, func(t *testing.T) {
			b := newTestBackend(t)
			mockDeps(b)
			releasesFixture(t, b, "0.47.0")
			if err := action(b, "install", "--trust-dependencies"); err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(b.root, "0.47.0")
			switch kind {
			case "extra":
				write(t, filepath.Join(dir, "personal"), []byte("keep"), 0600)
			case "edited":
				write(t, filepath.Join(dir, "libexec/spotatui"), []byte("changed"), 0700)
			case "mode":
				_ = os.Chmod(filepath.Join(dir, "bin/spotatui"), 0755)
			case "missing":
				_ = os.Remove(filepath.Join(dir, "shims/yt-dlp"))
			case "receipt":
				write(t, filepath.Join(dir, receiptName), []byte("{}"), 0600)
			case "link":
				_ = os.Symlink("bin/spotatui", filepath.Join(dir, "alias"))
			case "dependency":
				write(t, filepath.Join(dir, dependencyFile), []byte("{}"), 0600)
			}
			for _, op := range []string{"use", "remove", "uninstall", "integrate"} {
				args := []string{}
				if op == "use" || op == "remove" {
					args = append(args, "0.47.0")
				}
				if op == "use" {
					args = append(args, "--trust-dependencies")
				}
				if err := action(b, op, args...); err == nil {
					t.Fatal("accepted changed files", op)
				}
			}
			if _, err := os.Stat(dir); err != nil {
				t.Fatal("deleted changed version")
			}
		})
	}
}

func TestRefreshSameVersionAndFailureRollback(t *testing.T) {
	b := newTestBackend(t)
	mockDeps(b)
	releasesFixture(t, b, "0.47.0")
	if err := action(b, "install", "--trust-dependencies"); err != nil {
		t.Fatal(err)
	}
	oldLook := b.look
	b.look = func(name string) (string, error) {
		p, e := oldLook(name)
		return strings.ReplaceAll(p, "fixture tools", "new tools"), e
	}
	if err := action(b, "install", "--trust-dependencies"); err != nil {
		t.Fatal(err)
	}
	d, err := readDependencies(filepath.Join(b.root, "0.47.0"))
	if err != nil || !strings.Contains(d.YTDLP, "new tools") {
		t.Fatal(d, err)
	}
	if err = b.verifyOwned(context.Background(), "0.47.0"); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(b.root, "0.47.0", dependencyFile)
	before, mode, exists, err := readProfile(p)
	if err != nil {
		t.Fatal(err)
	}
	fail := filepath.Join(t.TempDir(), "foreign")
	write(t, fail, []byte("foreign"), 0600)
	_, err = applyProfiles(context.Background(), []profilePlan{{p, before, []byte("temporary"), mode, exists}, {fail, []byte("expected"), []byte("bad"), 0600, true}})
	if err == nil {
		t.Fatal("failed refresh not reported")
	}
	after, _ := os.ReadFile(p)
	if !bytes.Equal(before, after) {
		t.Fatal("refresh rollback failed")
	}
	if err = b.verifyOwned(context.Background(), "0.47.0"); err != nil {
		t.Fatal(err)
	}
}

func TestReleaseAndArchiveFailures(t *testing.T) {
	for _, kind := range []string{"draft", "prerelease", "missing-bool", "wrong-tag", "missing-asset", "duplicate", "wrong-url", "checksum", "duplicate-checksum", "wrong-size", "truncated", "traversal", "link", "fifo", "missing", "wrong-arch", "special-mode"} {
		t.Run(kind, func(t *testing.T) {
			b := newTestBackend(t)
			mockDeps(b)
			releasesFixture(t, b, "0.46.0")
			if err := action(b, "install", "--trust-dependencies"); err != nil {
				t.Fatal(err)
			}
			data := archiveBytes(t, b.platform)
			switch kind {
			case "truncated":
				data = data[:len(data)-5]
			case "traversal":
				data = archiveBytes(t, b.platform, tarItem{"../escape", []byte("bad"), tar.TypeReg, 0600, ""})
			case "link":
				data = archiveBytes(t, b.platform, tarItem{"link", nil, tar.TypeSymlink, 0777, "spotatui"})
			case "fifo":
				data = archiveBytes(t, b.platform, tarItem{"pipe", nil, tar.TypeFifo, 0600, ""})
			case "missing":
				data = archiveItems(t, tarItem{"other", []byte("bad"), tar.TypeReg, 0700, ""})
			case "wrong-arch":
				other := "linux-aarch64"
				if b.platform == other {
					other = "linux-x86_64"
				}
				data = archiveBytes(t, other)
			case "special-mode":
				data = archiveBytes(t, b.platform, tarItem{"suid", []byte("bad"), tar.TypeReg, 04755, ""})
			}
			row, sums := releaseData("0.47.0", b.platform, data)
			yes := true
			switch kind {
			case "draft":
				row.Draft = &yes
			case "prerelease":
				row.Prerelease = &yes
			case "missing-bool":
				row.Draft = nil
			case "wrong-tag":
				row.Tag = "v0.48.0"
			case "missing-asset":
				row.Assets = row.Assets[:1]
			case "duplicate":
				row.Assets = append(row.Assets, row.Assets[0])
			case "wrong-url":
				row.Assets[0].URL = "https://example.invalid/bad"
			case "checksum":
				sums = strings.Repeat("0", 64) + sums[64:]
			case "duplicate-checksum":
				sums += sums
				row.Assets[1].Size = int64(len(sums))
			case "wrong-size":
				row.Assets[0].Size++
			}
			fixtureRelease(t, b, row, data, sums)
			if err := action(b, "install", "--version", "0.47.0", "--trust-dependencies"); err == nil {
				t.Fatal("invalid artifact accepted")
			}
			current(t, b, "0.46.0")
			absent(t, filepath.Join(b.root, "0.47.0"))
			clean(t, b)
		})
	}
}

func TestTLSRedirectsBoundsAndCancellation(t *testing.T) {
	for _, target := range []string{"https://release-assets.githubusercontent.com/payload?token=test", "http://release-assets.githubusercontent.com/payload", "https://evil.invalid/payload", "https://user@release-assets.githubusercontent.com/payload"} {
		t.Run(target, func(t *testing.T) {
			b := newTestBackend(t)
			tlsFixture(t, b, func(w http.ResponseWriter, r *http.Request) {
				if r.Host == "github.com" {
					http.Redirect(w, r, target, 302)
					return
				}
				fmt.Fprint(w, "ok")
			})
			var data bytes.Buffer
			err := b.fetch(context.Background(), releaseBase+"v0.47.0/test", &data, 10)
			if strings.HasPrefix(target, "https://release-assets.") {
				if err != nil || data.String() != "ok" {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("bad redirect accepted")
			}
		})
	}
	b := newTestBackend(t)
	tlsFixture(t, b, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "slow") {
			<-r.Context().Done()
			return
		}
		fmt.Fprint(w, strings.Repeat("x", 100))
	})
	if err := b.fetch(context.Background(), apiBase+"latest", io.Discard, 10); err == nil {
		t.Fatal("size bound ignored")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := b.fetch(ctx, apiBase+"slow", io.Discard, 100); err == nil {
		t.Fatal("cancellation ignored")
	}
}

func TestLocalPublicationRaceAndProfiles(t *testing.T) {
	b := newTestBackend(t)
	mockDeps(b)
	data := archiveBytes(t, b.platform)
	row, sums := releaseData("0.47.0", b.platform, data)
	tlsFixture(t, b, func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "api.github.com" {
			_ = json.NewEncoder(w).Encode(row)
			return
		}
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			fmt.Fprint(w, sums)
			return
		}
		write(t, b.localEntry, []byte("late foreign entry"), 0700)
		_, _ = w.Write(data)
	})
	if err := action(b, "install", "--location", "local", "--trust-dependencies"); err == nil {
		t.Fatal("late foreign entry overwritten")
	}
	got, _ := os.ReadFile(b.localEntry)
	if string(got) != "late foreign entry" {
		t.Fatal("lost foreign entry")
	}
	absent(t, filepath.Join(b.root, "current"))
	absent(t, filepath.Join(b.root, "0.47.0"))
	clean(t, b)
	releasesFixture(t, b, "0.47.0")
	if err := action(b, "install", "--trust-dependencies"); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(b.home, ".profile"), []byte("user\n"), 0640)
	if err := action(b, "integrate"); err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(b.home, ".profile")
	content, _ := os.ReadFile(profile)
	changed := strings.ReplaceAll(string(content), "export PATH=", "export EDITED=")
	write(t, profile, []byte(changed), 0640)
	if err := action(b, "integrate"); err == nil {
		t.Fatal("edited integration overwritten")
	}
	if err := action(b, "uninstall"); err == nil {
		t.Fatal("edited integration removed")
	}
	current(t, b, "0.47.0")
}
