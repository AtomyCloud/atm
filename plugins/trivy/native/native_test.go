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
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ATM_DRY_RUN", "")
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	b, err := newBackend(protocol.Request{Protocol: 1, RequestID: strings.Repeat("a", 32), PluginID: "trivy", Operation: "menu", Home: filepath.Join(t.TempDir(), "runtime"), AppsDir: filepath.Join(t.TempDir(), "Apps")}, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	b.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Error("unexpected network without TLS fixture")
		return nil, fmt.Errorf("test network disabled")
	})}
	b.run = func(context.Context, string, []string, time.Duration) (string, error) {
		t.Fatal("unexpected subprocess")
		return "", nil
	}
	return b
}
func action(b *backend, op string, args ...string) error {
	b.req.Operation = op
	b.req.Arguments = args
	_, err := b.execute(context.Background())
	return err
}
func assertAbsent(t *testing.T, p string) {
	t.Helper()
	if _, err := os.Lstat(p); !os.IsNotExist(err) {
		t.Fatalf("expected absent %s: %v", p, err)
	}
}
func assertCurrent(t *testing.T, b *backend, v string) {
	t.Helper()
	got, err := b.current()
	if err != nil || got != v {
		t.Fatalf("current=%s want=%s: %v", got, v, err)
	}
}
func assertClean(t *testing.T, b *backend) {
	t.Helper()
	for _, pattern := range []string{".atm-stage-*", ".atm-switch-*", ".atm-native-lock", ".atm-trivy-docker-*"} {
		matches, _ := filepath.Glob(filepath.Join(b.root, pattern))
		if len(matches) > 0 {
			t.Fatal("temporary artifacts remain", matches)
		}
	}
}
func elfBytes(platform string) []byte {
	data := make([]byte, 64)
	copy(data, []byte{127, 'E', 'L', 'F', 2, 1, 1})
	binary.LittleEndian.PutUint16(data[16:], 2)
	machine := uint16(62)
	if platform == "Linux-ARM64" {
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
	var data bytes.Buffer
	gz := gzip.NewWriter(&data)
	tw := tar.NewWriter(gz)
	for _, item := range items {
		size := int64(len(item.body))
		if item.kind != tar.TypeReg {
			size = 0
		}
		if err := tw.WriteHeader(&tar.Header{Name: item.name, Typeflag: item.kind, Size: size, Mode: item.mode, Linkname: item.link}); err != nil {
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
	return data.Bytes()
}
func archiveBytes(t *testing.T, platform string, extra ...tarItem) []byte {
	return archiveItems(t, append([]tarItem{{"trivy", string(elfBytes(platform)), "", tar.TypeReg, 0755}, {"README.md", "ignored", "", tar.TypeReg, 0644}}, extra...))
}
func legacy(t *testing.T, b *backend, v string) {
	t.Helper()
	write(t, filepath.Join(b.root, v, "bin/trivy"), elfBytes(b.platform), 0700)
	write(t, filepath.Join(b.root, v, ".atm-trivy-managed"), []byte("old marker"), 0600)
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
	name := "trivy_" + v + "_" + platform + ".tar.gz"
	checkName := "trivy_" + v + "_checksums.txt"
	sums := fmt.Sprintf("%x  %s\n", sha256.Sum256(data), name)
	return release{Tag: "v" + v, Draft: &stable, Prerelease: &stable, Assets: []releaseAsset{{name, releaseBase + "v" + v + "/" + name, int64(len(data))}, {checkName, releaseBase + "v" + v + "/" + checkName, int64(len(sums))}}}, sums
}
func fixtureRelease(t *testing.T, b *backend, row release, data []byte, sums string) {
	tlsFixture(t, b, func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "api.github.com" {
			_ = json.NewEncoder(w).Encode(row)
			return
		}
		if r.Host != "github.com" {
			t.Errorf("unexpected host %s", r.Host)
			http.Error(w, "bad host", 400)
			return
		}
		if strings.HasSuffix(r.URL.Path, "_checksums.txt") {
			fmt.Fprint(w, sums)
			return
		}
		_, _ = w.Write(data)
	})
}
func releasesFixture(t *testing.T, b *backend, versions ...string) {
	data := archiveBytes(t, b.platform)
	tlsFixture(t, b, func(w http.ResponseWriter, r *http.Request) {
		v := versions[0]
		if r.Host == "api.github.com" {
			if strings.Contains(r.URL.Path, "/tags/") {
				v = strings.TrimPrefix(filepath.Base(r.URL.Path), "v")
			}
			found := false
			for _, known := range versions {
				found = found || v == known
			}
			if !found {
				http.NotFound(w, r)
				return
			}
			row, _ := releaseData(v, b.platform, data)
			_ = json.NewEncoder(w).Encode(row)
			return
		}
		parts := strings.Split(r.URL.Path, "/")
		v = strings.TrimPrefix(parts[len(parts)-2], "v")
		if strings.HasSuffix(r.URL.Path, "_checksums.txt") {
			_, sums := releaseData(v, b.platform, data)
			fmt.Fprint(w, sums)
			return
		}
		_, _ = w.Write(data)
	})
}

func TestBinaryLifecycleLegacyProfilesAndData(t *testing.T) {
	b := newTestBackend(t)
	releasesFixture(t, b, "0.76.1", "0.75.0")
	original := "# user\n# >>> ATM native go PATH >>>\nexport GOPATH='/keep'\n# <<< ATM native go PATH <<<\n"
	for _, p := range []string{".profile", ".bashrc", ".zshrc"} {
		write(t, filepath.Join(b.home, p), []byte(original), 0640)
	}
	preserved := []string{filepath.Join(b.root, "cache/db"), filepath.Join(b.root, "reports/personal"), filepath.Join(b.home, ".cache/trivy/db"), filepath.Join(b.req.Home, "state/trivy/docker-version")}
	for _, p := range preserved {
		write(t, p, []byte("keep"), 0600)
	}
	legacy(t, b, "0.74.0")
	if err := action(b, "install", "--method", "binary"); err != nil {
		t.Fatal(err)
	}
	assertCurrent(t, b, "0.76.1")
	if err := b.verifyOwned(context.Background(), "0.76.1"); err != nil {
		t.Fatal(err)
	}
	assertAbsent(t, filepath.Join(b.root, "0.76.1/README.md"))
	if err := action(b, "install", "--method=binary", "--version=v0.75.0"); err != nil {
		t.Fatal(err)
	}
	if err := action(b, "remove", "0.75.0"); err == nil {
		t.Fatal("removed current")
	}
	if err := action(b, "update", "--method", "binary"); err != nil {
		t.Fatal(err)
	}
	if err := action(b, "remove", "0.75.0"); err != nil {
		t.Fatal(err)
	}
	if err := action(b, "use", "0.74.0"); err != nil {
		t.Fatal(err)
	}
	assertAbsent(t, filepath.Join(b.root, "0.74.0", receiptName))
	if err := action(b, "remove", "0.74.0"); err == nil {
		t.Fatal("legacy removal accepted")
	}
	if err := action(b, "use", "0.76.1"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{".profile", ".bashrc", ".zshrc"} {
		data, _ := os.ReadFile(filepath.Join(b.home, p))
		if !strings.HasPrefix(string(data), original) || !strings.Contains(string(data), shellQuote(filepath.Join(b.root, "current/bin"))) {
			t.Fatal(string(data))
		}
	}
	if err := action(b, "integrate"); err != nil {
		t.Fatal(err)
	}
	if err := action(b, "uninstall"); err != nil {
		t.Fatal(err)
	}
	assertCurrent(t, b, "")
	if err := b.validInstalled("0.74.0"); err != nil {
		t.Fatal("legacy lost", err)
	}
	for _, p := range preserved {
		data, _ := os.ReadFile(p)
		if string(data) != "keep" {
			t.Fatal("data lost", p)
		}
	}
	for _, p := range []string{".profile", ".bashrc", ".zshrc"} {
		data, _ := os.ReadFile(filepath.Join(b.home, p))
		if string(data) != original {
			t.Fatal("profile content lost")
		}
	}
	assertClean(t, b)
}
func TestChangedOwnedPayloadRefused(t *testing.T) {
	for _, kind := range []string{"extra", "edit", "mode", "missing", "receipt", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			b := newTestBackend(t)
			releasesFixture(t, b, "0.76.1")
			if err := action(b, "install", "--method", "binary"); err != nil {
				t.Fatal(err)
			}
			legacy(t, b, "0.75.0")
			if err := action(b, "use", "0.75.0"); err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(b.root, "0.76.1")
			switch kind {
			case "extra":
				write(t, filepath.Join(dir, "mine"), []byte("user"), 0600)
			case "edit":
				write(t, filepath.Join(dir, "bin/trivy"), []byte("changed"), 0700)
			case "mode":
				if err := os.Chmod(filepath.Join(dir, "bin/trivy"), 0755); err != nil {
					t.Fatal(err)
				}
			case "missing":
				if err := os.Remove(filepath.Join(dir, "bin/trivy")); err != nil {
					t.Fatal(err)
				}
			case "receipt":
				write(t, filepath.Join(dir, receiptName), []byte("{}"), 0600)
			case "symlink":
				if err := os.Symlink("bin/trivy", filepath.Join(dir, "link")); err != nil {
					t.Fatal(err)
				}
			}
			for _, op := range []string{"use", "remove", "uninstall"} {
				args := []string{"0.76.1"}
				if op == "uninstall" {
					args = nil
				}
				if err := action(b, op, args...); err == nil {
					t.Fatal("changed payload accepted", op)
				}
			}
			assertCurrent(t, b, "0.75.0")
			if _, err := os.Stat(dir); err != nil {
				t.Fatal("changed payload deleted")
			}
		})
	}
}
func TestUnsafePathsAndProfileRollback(t *testing.T) {
	for _, kind := range []string{"root", "ancestor", "current", "version", "bin", "profile"} {
		t.Run(kind, func(t *testing.T) {
			b := newTestBackend(t)
			target := t.TempDir()
			legacy(t, b, "0.76.1")
			p := ""
			switch kind {
			case "root":
				p = b.root
			case "ancestor":
				p = b.req.AppsDir
			case "current":
				p = filepath.Join(b.root, "current")
			case "version":
				p = filepath.Join(b.root, "0.76.1")
			case "bin":
				p = filepath.Join(b.root, "0.76.1/bin")
			case "profile":
				p = filepath.Join(b.home, ".bashrc")
			}
			if _, err := os.Lstat(p); err == nil {
				if err = os.Rename(p, p+"-old"); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(target, p); err != nil {
				t.Fatal(err)
			}
			if err := action(b, "use", "0.76.1"); err == nil {
				t.Fatal("symlink accepted")
			}
			entries, _ := os.ReadDir(target)
			if len(entries) != 0 {
				t.Fatal("wrote outside root")
			}
			assertAbsent(t, filepath.Join(b.home, ".profile"))
		})
	}
	b := newTestBackend(t)
	write(t, filepath.Join(b.home, ".profile"), []byte("user\n"), 0640)
	plans, err := b.profilePlans(false)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(b.home, ".bashrc"), []byte("concurrent"), 0600)
	if _, err = applyProfiles(context.Background(), plans); err == nil {
		t.Fatal("overwrote concurrent profile")
	}
	data, _ := os.ReadFile(filepath.Join(b.home, ".profile"))
	if string(data) != "user\n" {
		t.Fatal("profile rollback failed")
	}
	for _, bad := range []string{profileStart, profileEnd, profileStart + "\n" + profileStart + "\n" + profileEnd, "x" + profileStart + "\n" + profileEnd} {
		if _, err := profileContent([]byte(bad), ""); err == nil {
			t.Fatal("bad marker accepted")
		}
	}
}
func TestReleaseMetadataAndChecksumRefusals(t *testing.T) {
	for _, kind := range []string{"draft", "prerelease", "missing-bool", "tag", "wrong-version", "missing-asset", "duplicate", "wrong-url", "wrong-size", "checksum", "duplicate-checksum", "missing-checksum"} {
		t.Run(kind, func(t *testing.T) {
			b := newTestBackend(t)
			data := archiveBytes(t, b.platform)
			row, sums := releaseData("0.76.1", b.platform, data)
			yes := true
			switch kind {
			case "draft":
				row.Draft = &yes
			case "prerelease":
				row.Prerelease = &yes
			case "missing-bool":
				row.Draft = nil
			case "tag":
				row.Tag = "v0.76.1-rc1"
			case "wrong-version":
				row.Tag = "v0.76.2"
			case "missing-asset":
				row.Assets = row.Assets[:1]
			case "duplicate":
				row.Assets = append(row.Assets, row.Assets[0])
			case "wrong-url":
				row.Assets[0].URL = "https://evil.invalid/archive"
			case "wrong-size":
				row.Assets[0].Size = limits.DownloadBytes + 1
			case "checksum":
				sums = "not a checksum"
				row.Assets[1].Size = int64(len(sums))
			case "duplicate-checksum":
				sums += sums
				row.Assets[1].Size = int64(len(sums))
			case "missing-checksum":
				sums = strings.ReplaceAll(sums, "trivy_", "other_")
				row.Assets[1].Size = int64(len(sums))
			}
			fixtureRelease(t, b, row, data, sums)
			if _, err := b.resolve(context.Background(), "0.76.1", true); err == nil {
				t.Fatal("invalid release accepted")
			}
		})
	}
}
func TestArchiveFailuresPreserveSelection(t *testing.T) {
	for _, kind := range []string{"checksum", "short", "oversize", "truncated", "traversal", "absolute", "link", "hardlink", "fifo", "duplicate", "dot-binary", "missing", "not-elf", "wrong-arch", "mode"} {
		t.Run(kind, func(t *testing.T) {
			b := newTestBackend(t)
			legacy(t, b, "0.75.0")
			if err := action(b, "use", "0.75.0"); err != nil {
				t.Fatal(err)
			}
			data := archiveBytes(t, b.platform)
			switch kind {
			case "traversal":
				data = archiveBytes(t, b.platform, tarItem{"../outside", "x", "", tar.TypeReg, 0644})
			case "absolute":
				data = archiveBytes(t, b.platform, tarItem{"/outside", "x", "", tar.TypeReg, 0644})
			case "link":
				data = archiveBytes(t, b.platform, tarItem{"link", "", "trivy", tar.TypeSymlink, 0777})
			case "hardlink":
				data = archiveBytes(t, b.platform, tarItem{"link", "", "trivy", tar.TypeLink, 0644})
			case "fifo":
				data = archiveBytes(t, b.platform, tarItem{"pipe", "", "", tar.TypeFifo, 0600})
			case "duplicate":
				data = archiveBytes(t, b.platform, tarItem{"trivy", "x", "", tar.TypeReg, 0755})
			case "dot-binary":
				data = archiveBytes(t, b.platform, tarItem{"./trivy", "x", "", tar.TypeReg, 0755})
			case "missing":
				data = archiveItems(t, []tarItem{{"readme", "only", "", tar.TypeReg, 0644}})
			case "not-elf":
				data = archiveItems(t, []tarItem{{"trivy", "bad", "", tar.TypeReg, 0755}})
			case "wrong-arch":
				other := "Linux-ARM64"
				if b.platform == other {
					other = "Linux-64bit"
				}
				data = archiveBytes(t, other)
			case "mode":
				data = archiveBytes(t, b.platform, tarItem{"suid", "bad", "", tar.TypeReg, 04755})
			case "truncated":
				data = data[:len(data)-6]
			}
			row, sums := releaseData("0.76.1", b.platform, data)
			switch kind {
			case "checksum":
				sums = strings.Repeat("0", 64) + sums[64:]
			case "short":
				row.Assets[0].Size++
			case "oversize":
				row.Assets[0].Size--
			}
			fixtureRelease(t, b, row, data, sums)
			if err := action(b, "install", "--method", "binary"); err == nil {
				t.Fatal("bad archive accepted")
			}
			assertCurrent(t, b, "0.75.0")
			assertAbsent(t, filepath.Join(b.root, "0.76.1"))
			assertClean(t, b)
		})
	}
}
func TestOfflineChoicesDryRunAndProtocol(t *testing.T) {
	b := newTestBackend(t)
	b.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Error("unexpected network")
		return nil, fmt.Errorf("offline")
	})}
	for _, op := range []string{"menu", "status", "install", "update"} {
		if err := action(b, op); err != nil {
			t.Fatal(err)
		}
	}
	assertAbsent(t, b.root)
	b.req.Operation = "install"
	b.req.Arguments = []string{"--version", "v0.76.1"}
	choices, err := b.execute(context.Background())
	if err != nil || len(choices) != 3 {
		t.Fatal(choices, err)
	}
	if !reflect.DeepEqual(choices[1].Arguments, []string{"--method", "docker", "--version", "0.76.1"}) {
		t.Fatal(choices)
	}
	t.Setenv("ATM_DRY_RUN", "1")
	for _, op := range []string{"menu", "status", "install", "update", "use", "remove", "uninstall", "integrate"} {
		if err := action(b, op); err != nil {
			t.Fatal(err)
		}
	}
	assertAbsent(t, b.root)
	t.Setenv("ATM_DRY_RUN", "")
	for _, op := range []string{"menu", "install", "status"} {
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
		if stream.Result == nil || stream.Result.Status != "success" {
			t.Fatal(stream.Result)
		}
		if op == "install" && stream.Result.Message != label("choose_method") {
			t.Fatal(stream.Result)
		}
		if op == "menu" && !strings.Contains(stream.Result.Message, "https://trivy.dev/") {
			t.Fatal(stream.Result)
		}
	}
}
func TestTLSRedirectBoundsAndCancellation(t *testing.T) {
	for _, target := range []string{"https://release-assets.githubusercontent.com/asset?token=fixture", "https://objects.githubusercontent.com/asset", "http://release-assets.githubusercontent.com/asset", "https://evil.invalid/asset", "https://release-assets.githubusercontent.com.evil.invalid/asset", "https://user@release-assets.githubusercontent.com/asset"} {
		t.Run(target, func(t *testing.T) {
			b := newTestBackend(t)
			tlsFixture(t, b, func(w http.ResponseWriter, r *http.Request) {
				if r.Host == "github.com" {
					http.Redirect(w, r, target, 302)
					return
				}
				fmt.Fprint(w, "archive")
			})
			var out bytes.Buffer
			err := b.fetch(context.Background(), releaseBase+"v0.76.1/trivy.tar.gz", &out, 10)
			good := target == "https://release-assets.githubusercontent.com/asset?token=fixture" || target == "https://objects.githubusercontent.com/asset"
			if good && (err != nil || out.String() != "archive") {
				t.Fatal(err, out.String())
			}
			if !good && err == nil {
				t.Fatal("unsafe redirect accepted")
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
		t.Fatal("bound ignored")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := b.fetch(ctx, apiBase+"slow", io.Discard, 100); err == nil {
		t.Fatal("cancellation ignored")
	}
}

func TestCompiledBackendThroughCoreEmptyPATH(t *testing.T) {
	b := newTestBackend(t)
	root := t.TempDir()
	dir := filepath.Join(root, "plugins/trivy")
	manifest, err := os.ReadFile("../plugin.json")
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "plugin.json"), manifest, 0600)
	bin := filepath.Join(dir, "bin/linux-"+runtime.GOARCH+"/atm-trivy")
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
	for _, op := range []string{"menu", "status", "install", "update"} {
		plan, err := runner.Plugin(c, ps[0], op, nil)
		if err != nil {
			t.Fatal(err)
		}
		out := runner.RunNative(context.Background(), c, plan, runner.NativeOptions{Timeout: 5 * time.Second})
		if out.Status != "success" {
			t.Fatalf("%s: %+v", op, out)
		}
	}
	assertAbsent(t, b.root)
	plan, err := runner.Plugin(c, ps[0], "install", []string{"--method", "binary"})
	if err != nil {
		t.Fatal(err)
	}
	dry := c
	dry.DryRun = true
	if out := runner.RunNative(context.Background(), dry, plan, runner.NativeOptions{}); out.Code != "dry_run" {
		t.Fatal(out)
	}
	t.Setenv("ATM_DRY_RUN", "1")
	if out := runner.RunNative(context.Background(), c, plan, runner.NativeOptions{}); out.Status != "success" {
		t.Fatal(out)
	}
	t.Setenv("ATM_DRY_RUN", "")
	assertAbsent(t, b.root)
	releasesFixture(t, b, "0.76.1", "0.75.0")
	for _, v := range []string{"0.76.1", "0.75.0"} {
		if err := action(b, "install", "--method", "binary", "--version", v); err != nil {
			t.Fatal(err)
		}
	}
	legacy(t, b, "0.74.0")
	for _, step := range []struct {
		op   string
		args []string
	}{{"use", []string{"0.76.1"}}, {"remove", []string{"0.75.0"}}, {"integrate", nil}, {"status", nil}, {"uninstall", nil}} {
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
	assertAbsent(t, filepath.Join(root, ".profile"))
	if err := b.validInstalled("0.74.0"); err != nil {
		t.Fatal(err)
	}
}
