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
	b, err := newBackend(protocol.Request{Protocol: 1, RequestID: strings.Repeat("a", 32), PluginID: "java", Operation: "menu", Home: filepath.Join(t.TempDir(), "runtime"), AppsDir: filepath.Join(t.TempDir(), "Apps with 'quotes'")}, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	b.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Error("unexpected network without TLS fixture")
		return nil, fmt.Errorf("fixture required")
	})}
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
	for _, pattern := range []string{".atm-stage-*", ".atm-switch-*", ".atm-native-lock"} {
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
	if platform == "aarch64" {
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
func archiveBytes(t *testing.T, platform, version string, extra ...tarItem) []byte {
	return archiveItems(t, append([]tarItem{{"jdk/bin/java", string(elfBytes(platform)), "", tar.TypeReg, 0755}, {"jdk/bin/javac", string(elfBytes(platform)), "", tar.TypeReg, 0755}, {"jdk/release", "JAVA_VERSION=\"" + version + "\"\nIMPLEMENTOR=\"fixture\"\n", "", tar.TypeReg, 0644}, {"jdk/lib/data", "fixture", "", tar.TypeReg, 0644}}, extra...))
}
func legacy(t *testing.T, b *backend, target, actual string) {
	t.Helper()
	for _, name := range []string{"java", "javac"} {
		write(t, filepath.Join(b.root, target, "bin", name), elfBytes(b.platform), 0700)
	}
	write(t, filepath.Join(b.root, target, "release"), []byte("JAVA_VERSION=\""+actual+"\"\n"), 0600)
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
		response, err := transport.RoundTrip(clone)
		if response != nil {
			response.Request = r
		}
		return response, err
	})}
}
func temurinMetadata(arch, actual string, data []byte) map[string]any {
	release := "jdk-" + actual + "+1"
	name := "OpenJDK26U-jdk_" + arch + "_linux_hotspot_" + strings.ReplaceAll(actual, "+", "_") + "_1.tar.gz"
	return map[string]any{"release_name": release, "vendor": "eclipse", "version": map[string]any{"major": 26, "semver": "26.0.2+101"}, "binary": map[string]any{"architecture": arch, "os": "linux", "image_type": "jdk", "jvm_impl": "hotspot", "heap_size": "normal", "package": map[string]any{"name": name, "link": "https://github.com/adoptium/temurin26-binaries/releases/download/" + strings.ReplaceAll(release, "+", "%2B") + "/" + name, "checksum": fmt.Sprintf("%x", sha256.Sum256(data)), "size": len(data)}}}
}
func vendorFixture(t *testing.T, b *backend, vendor, actual string, extra ...tarItem) []byte {
	t.Helper()
	data := archiveBytes(t, b.platform, actual, extra...)
	sum := fmt.Sprintf("%x", sha256.Sum256(data))
	tlsFixture(t, b, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "ATM/0.0.2" || r.Header.Get("Accept") != "application/json" {
			t.Error("missing API headers")
		}
		if vendor == "temurin" && r.Host == "api.adoptium.net" {
			if r.URL.Path != "/v3/assets/latest/26/hotspot" || r.URL.Query().Get("architecture") != b.platform || r.URL.Query().Get("image_type") != "jdk" {
				t.Errorf("unexpected Adoptium request %s", r.URL.String())
			}
			_ = json.NewEncoder(w).Encode([]any{temurinMetadata(b.platform, actual, data)})
			return
		}
		if vendor == "microsoft" && r.Host == "aka.ms" {
			suffix := ""
			if strings.HasSuffix(r.URL.Path, ".sha256sum.txt") {
				suffix = ".sha256sum.txt"
			}
			http.Redirect(w, r, "https://download.visualstudio.microsoft.com/download/fixture/microsoft-jdk-"+actual+"-linux-"+b.platform+".tar.gz"+suffix, 302)
			return
		}
		if vendor == "corretto" && strings.HasPrefix(r.URL.Path, "/downloads/latest") {
			suffix := ""
			if strings.Contains(r.URL.Path, "latest_sha256") {
				suffix = ".sha256"
			}
			http.Redirect(w, r, "https://corretto.aws/downloads/resources/"+actual+"/amazon-corretto-"+actual+"-linux-"+b.platform+".tar.gz"+suffix, 302)
			return
		}
		if strings.HasSuffix(r.URL.Path, ".sha256") || strings.HasSuffix(r.URL.Path, ".sha256sum.txt") {
			if vendor == "microsoft" {
				fmt.Fprintf(w, "%s  microsoft-jdk-%s-linux-%s.tar.gz\n", sum, actual, b.platform)
			} else {
				fmt.Fprintln(w, sum)
			}
			return
		}
		_, _ = w.Write(data)
	})
	return data
}

func TestOfficialVendorsBothArchitectures(t *testing.T) {
	for _, arch := range []string{"x64", "aarch64"} {
		for _, vendor := range vendors[:5] {
			t.Run(arch+"/"+vendor, func(t *testing.T) {
				b := newTestBackend(t)
				b.platform = arch
				actual := "26.0.1"
				if vendor == "microsoft" || vendor == "graalvm" {
					actual = "25.0.2"
				}
				if vendor == "temurin" {
					actual = "26.0.2.1"
				}
				vendorFixture(t, b, vendor, actual)
				if err := action(b, "install", "--vendor", vendor); err != nil {
					t.Fatal(err)
				}
				target := vendor + "/" + actual
				assertCurrent(t, b, target)
				rec, err := b.readReceipt(target)
				if err != nil {
					t.Fatal(err)
				}
				if rec.Artifact.ActualVersion != actual || rec.Artifact.Size <= 0 || rec.Artifact.SHA256 == "" || rec.Artifact.ResolvedURL == "" {
					t.Fatal(rec.Artifact)
				}
				if err = b.verifyOwned(context.Background(), target); err != nil {
					t.Fatal(err)
				}
				if vendor == "corretto" && !strings.Contains(rec.Artifact.ChecksumURL, "/downloads/resources/") {
					t.Fatal(rec.Artifact)
				}
				if vendor == "microsoft" && !strings.HasPrefix(rec.Artifact.ResolvedURL, "https://download.visualstudio.microsoft.com/") {
					t.Fatal(rec.Artifact)
				}
				if err = action(b, "uninstall"); err != nil {
					t.Fatal(err)
				}
				assertCurrent(t, b, "")
				assertClean(t, b)
			})
		}
	}
}

func TestJavaLifecycleLinksProfilesAndLegacy(t *testing.T) {
	b := newTestBackend(t)
	original := "# user\n# >>> ATM native go PATH >>>\nexport GOPATH='/preserved'\n# <<< ATM native go PATH <<<\n"
	for _, p := range []string{".profile", ".bashrc", ".zshrc"} {
		write(t, filepath.Join(b.home, p), []byte(original), 0640)
	}
	dataPath := filepath.Join(b.root, "personal/reports/keep")
	write(t, dataPath, []byte("mine"), 0600)
	legacy(t, b, "temurin/21", "21.0.4")
	extra := []tarItem{{"jdk/lib/link", "", "data", tar.TypeSymlink, 0777}, {"jdk/lib/chain", "", "link", tar.TypeSymlink, 0777}, {"jdk/lib/hard", "", "jdk/lib/data", tar.TypeLink, 0644}, {"jdk/lib/alias", "", "../bin", tar.TypeSymlink, 0777}}
	vendorFixture(t, b, "openjdk", "26.0.1", extra...)
	if err := action(b, "install"); err != nil {
		t.Fatal(err)
	}
	assertCurrent(t, b, "openjdk/26.0.1")
	for _, link := range []string{"lib/link", "lib/chain", "lib/hard"} {
		data, err := os.ReadFile(filepath.Join(b.root, "openjdk/26.0.1", link))
		if err != nil || string(data) != "fixture" {
			t.Fatal(link, err)
		}
	}
	if _, err := os.Stat(filepath.Join(b.root, "openjdk/26.0.1/lib/alias/java")); err != nil {
		t.Fatal(err)
	}
	if err := action(b, "use", "jdk/26.0.1"); err != nil {
		t.Fatal(err)
	}
	if err := action(b, "integrate"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{".profile", ".bashrc", ".zshrc"} {
		data, _ := os.ReadFile(filepath.Join(b.home, p))
		if !strings.HasPrefix(string(data), original) || !strings.Contains(string(data), "export JAVA_HOME="+shellQuote(filepath.Join(b.root, "current"))) {
			t.Fatal(string(data))
		}
	}
	assertAbsent(t, b.req.Home)
	if err := action(b, "remove", "openjdk/26.0.1"); err == nil {
		t.Fatal("removed current")
	}
	if err := action(b, "use", "eclipse/21"); err != nil {
		t.Fatal(err)
	}
	assertCurrent(t, b, "temurin/21")
	assertAbsent(t, filepath.Join(b.root, "temurin/21", receiptName))
	if err := action(b, "remove", "openjdk/26.0.1"); err != nil {
		t.Fatal(err)
	}
	if err := action(b, "uninstall"); err != nil {
		t.Fatal(err)
	}
	assertCurrent(t, b, "temurin/21")
	if err := action(b, "remove", "temurin/21"); err == nil {
		t.Fatal("legacy deleted")
	}
	data, _ := os.ReadFile(dataPath)
	if string(data) != "mine" {
		t.Fatal("user data lost")
	}
	assertClean(t, b)
}

func TestMutableVendorUpdateAndConflictingArtifact(t *testing.T) {
	b := newTestBackend(t)
	vendorFixture(t, b, "corretto", "26.0.1")
	if err := action(b, "install", "--vendor", "corretto"); err != nil {
		t.Fatal(err)
	}
	vendorFixture(t, b, "corretto", "26.0.2")
	if err := action(b, "update"); err != nil {
		t.Fatal(err)
	}
	assertCurrent(t, b, "corretto/26.0.2")
	if err := b.validInstalled("corretto/26.0.1"); err != nil {
		t.Fatal("old version lost", err)
	}
	if err := action(b, "update"); err != nil {
		t.Fatal("identical update failed", err)
	}
	vendorFixture(t, b, "corretto", "26.0.2", tarItem{"jdk/new", "different artifact", "", tar.TypeReg, 0644})
	if err := action(b, "update"); err == nil || !strings.Contains(err.Error(), "different artifact") {
		t.Fatal("same version replaced", err)
	}
	assertAbsent(t, filepath.Join(b.root, "corretto/26.0.2/new"))
	vendorFixture(t, b, "corretto", "26.0.1")
	if err := action(b, "update"); err == nil {
		t.Fatal("downgraded active")
	}
	assertCurrent(t, b, "corretto/26.0.2")
}

func TestBareUpdateDoesNotUpgradeMajor(t *testing.T) {
	b := newTestBackend(t)
	legacy(t, b, "temurin/21", "21.0.4")
	if err := action(b, "use", "temurin/21"); err != nil {
		t.Fatal(err)
	}
	if err := action(b, "update"); err == nil || !strings.Contains(err.Error(), "no official URL configured") {
		t.Fatal(err)
	}
	assertCurrent(t, b, "temurin/21")
	vendorFixture(t, b, "temurin", "26.0.2.1")
	if err := action(b, "update", "--vendor", "temurin"); err != nil {
		t.Fatal("explicit major selection failed", err)
	}
	assertCurrent(t, b, "temurin/26.0.2.1")
}

func TestCustomJDKEarlyAccessAndConflict(t *testing.T) {
	b := newTestBackend(t)
	data := archiveBytes(t, b.platform, "27-ea+4")
	sum := fmt.Sprintf("%x", sha256.Sum256(data))
	tlsFixture(t, b, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/jdk.tar.gz" {
			http.Redirect(w, r, "/resolved.tar.gz", 302)
			return
		}
		_, _ = w.Write(data)
	})
	args := []string{"--vendor", "custom", "--version", "team-ea", "--url", "https://custom.invalid/jdk.tar.gz", "--sha256", sum}
	if err := action(b, "install", args...); err != nil {
		t.Fatal(err)
	}
	assertCurrent(t, b, "custom/team-ea")
	rec, err := b.readReceipt("custom/team-ea")
	if err != nil || rec.Artifact.ActualVersion != "27-ea+4" || rec.Artifact.ResolvedURL != "https://custom.invalid/resolved.tar.gz" {
		t.Fatal(rec, err)
	}
	if err := action(b, "update"); err == nil {
		t.Fatal("invented automatic custom update")
	}
	if err := action(b, "uninstall"); err != nil {
		t.Fatal(err)
	}
	assertCurrent(t, b, "")
}

func TestJavaChangedOwnershipRefused(t *testing.T) {
	for _, kind := range []string{"extra", "binary", "release", "mode", "receipt", "link"} {
		t.Run(kind, func(t *testing.T) {
			b := newTestBackend(t)
			vendorFixture(t, b, "openjdk", "26.0.1")
			if err := action(b, "install"); err != nil {
				t.Fatal(err)
			}
			legacy(t, b, "temurin/21", "21.0.4")
			if err := action(b, "use", "temurin/21"); err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(b.root, "openjdk/26.0.1")
			switch kind {
			case "extra":
				write(t, filepath.Join(dir, "mine"), []byte("personal"), 0600)
			case "binary":
				write(t, filepath.Join(dir, "bin/java"), []byte("bad"), 0700)
			case "release":
				write(t, filepath.Join(dir, "release"), []byte("JAVA_VERSION=\"26.0.2\"\n"), 0600)
			case "mode":
				if err := os.Chmod(filepath.Join(dir, "bin/javac"), 0755); err != nil {
					t.Fatal(err)
				}
			case "receipt":
				write(t, filepath.Join(dir, receiptName), []byte("{}"), 0600)
			case "link":
				if err := os.Symlink("/outside", filepath.Join(dir, "added")); err != nil {
					t.Fatal(err)
				}
			}
			for _, op := range []string{"use", "remove", "uninstall"} {
				args := []string{"openjdk/26.0.1"}
				if op == "uninstall" {
					args = nil
				}
				if err := action(b, op, args...); err == nil {
					t.Fatal("changed payload accepted", op)
				}
			}
			assertCurrent(t, b, "temurin/21")
			if _, err := os.Stat(dir); err != nil {
				t.Fatal("changed payload deleted")
			}
		})
	}
}

func TestJavaUnsafePathsAndProfileRollback(t *testing.T) {
	for _, kind := range []string{"root", "ancestor", "vendor", "version", "bin", "current", "profile"} {
		t.Run(kind, func(t *testing.T) {
			b := newTestBackend(t)
			legacy(t, b, "openjdk/26.0.1", "26.0.1")
			target := t.TempDir()
			p := ""
			switch kind {
			case "root":
				p = b.root
			case "ancestor":
				p = b.req.AppsDir
			case "vendor":
				p = filepath.Join(b.root, "openjdk")
			case "version":
				p = filepath.Join(b.root, "openjdk/26.0.1")
			case "bin":
				p = filepath.Join(b.root, "openjdk/26.0.1/bin")
			case "current":
				p = filepath.Join(b.root, "current")
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
			if err := action(b, "use", "openjdk/26.0.1"); err == nil {
				t.Fatal("unsafe path accepted")
			}
			entries, _ := os.ReadDir(target)
			if len(entries) != 0 {
				t.Fatal("wrote outside root")
			}
			assertAbsent(t, filepath.Join(b.home, ".profile"))
		})
	}
	b := newTestBackend(t)
	write(t, filepath.Join(b.home, ".profile"), []byte("before\n"), 0640)
	plans, err := b.profilePlans(false)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(b.home, ".bashrc"), []byte("concurrent"), 0600)
	if _, err = applyProfiles(context.Background(), plans); err == nil {
		t.Fatal("concurrent profile overwritten")
	}
	data, _ := os.ReadFile(filepath.Join(b.home, ".profile"))
	if string(data) != "before\n" {
		t.Fatal("profile rollback failed")
	}
}

func TestOfflineStartupDryRunAndProtocol(t *testing.T) {
	b := newTestBackend(t)
	for _, op := range []string{"menu", "status"} {
		if err := action(b, op); err != nil {
			t.Fatal(err)
		}
	}
	assertAbsent(t, b.root)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	b.req.Operation = "install"
	b.req.Arguments = nil
	if _, err := b.execute(ctx); err == nil {
		t.Fatal("cancelled install accepted")
	}
	assertAbsent(t, b.root)
	t.Setenv("ATM_DRY_RUN", "1")
	for _, op := range []string{"menu", "status", "install", "update", "use", "remove", "uninstall", "integrate"} {
		if err := action(b, op); err != nil {
			t.Fatal(err)
		}
	}
	assertAbsent(t, b.root)
	t.Setenv("ATM_DRY_RUN", "")
	for _, op := range []string{"menu", "status"} {
		b.req.Operation = op
		b.req.Arguments = nil
		request, _ := json.Marshal(b.req)
		var out bytes.Buffer
		if err := serve(context.Background(), bytes.NewReader(request), &out); err != nil {
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
	}
}

func TestCompiledJavaBackendThroughCoreEmptyPATH(t *testing.T) {
	b := newTestBackend(t)
	root := t.TempDir()
	dir := filepath.Join(root, "plugins/java")
	manifest, err := os.ReadFile("../plugin.json")
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "plugin.json"), manifest, 0600)
	bin := filepath.Join(dir, "bin/linux-"+runtime.GOARCH+"/atm-java")
	if err = os.MkdirAll(filepath.Dir(bin), 0700); err != nil {
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
	plan, err := runner.Plugin(c, ps[0], "install", nil)
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
	vendorFixture(t, b, "openjdk", "26.0.1")
	if err := action(b, "install"); err != nil {
		t.Fatal(err)
	}
	vendorFixture(t, b, "corretto", "26.0.2")
	if err := action(b, "install", "--vendor", "corretto"); err != nil {
		t.Fatal(err)
	}
	legacy(t, b, "temurin/21", "21.0.4")
	for _, step := range []struct {
		op   string
		args []string
	}{{"use", []string{"jdk/26.0.1"}}, {"remove", []string{"corretto/26.0.2"}}, {"integrate", nil}, {"status", nil}, {"uninstall", nil}} {
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
	if err := b.validInstalled("temurin/21"); err != nil {
		t.Fatal(err)
	}
}

func TestChecksumDownloadIdentity(t *testing.T) {
	b := newTestBackend(t)
	vendorFixture(t, b, "microsoft", "25.0.2")
	o, _ := parseOptions([]string{"--vendor", "microsoft"})
	spec, err := b.resolve(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(spec.Origins, []string{"https://aka.ms"}) || !strings.HasSuffix(spec.ChecksumFilename, "25.0.2-linux-"+b.platform+".tar.gz") {
		t.Fatal(spec)
	}
	var out bytes.Buffer
	final, n, err := b.fetch(context.Background(), spec.URL, spec.policy(), &out, limits.DownloadBytes)
	if err != nil || n != int64(out.Len()) || !strings.HasPrefix(final, "https://download.visualstudio.microsoft.com/") {
		t.Fatal(final, n, err)
	}
}
