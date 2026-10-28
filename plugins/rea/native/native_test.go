//go:build linux

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
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

	"github.com/AtomyCloud/atm/internal/protocol"
)

var compiledNative, compiledTool string

func TestMain(m *testing.M) {
	if os.Getenv("ATM_REA_TEST_HELPER") == "1" {
		os.Exit(serveTestHelper())
	}
	dir, e := os.MkdirTemp("", "atm-rea-compiled-")
	if e != nil {
		panic(e)
	}
	build := func(out string, args ...string) {
		cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin/go"), append([]string{"build", "-o", out}, args...)...)
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if data, e := cmd.CombinedOutput(); e != nil {
			fmt.Fprintln(os.Stderr, string(data))
			os.RemoveAll(dir)
			os.Exit(1)
		}
	}
	compiledNative = filepath.Join(dir, "atm-rea")
	compiledTool = filepath.Join(dir, "fixture")
	build(compiledNative, ".")
	data, e := os.ReadFile("process_fixture.txt")
	if e != nil {
		panic(e)
	}
	source := filepath.Join(dir, "main.go")
	if e = os.WriteFile(source, data, 0600); e != nil {
		panic(e)
	}
	build(compiledTool, source)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
func write(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(path, data, mode); e != nil {
		t.Fatal(e)
	}
	if e := os.Chmod(path, mode); e != nil {
		t.Fatal(e)
	}
}
func copyFixture(t *testing.T, source, path string) {
	t.Helper()
	data, e := os.ReadFile(source)
	if e != nil {
		t.Fatal(e)
	}
	write(t, path, data, 0700)
}
func testBackend(t *testing.T) *backend {
	t.Helper()
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	if e := os.Mkdir(home, 0700); e != nil {
		t.Fatal(e)
	}
	for k, v := range map[string]string{"HOME": home, "XDG_CONFIG_HOME": filepath.Join(home, ".config"), "ATM_CONFIG_DIR": filepath.Join(home, ".config/atm"), "ATM_CONFIG_FILE": filepath.Join(home, ".config/atm/atm.conf"), "ATM_PLUGINS_CONFIG_FILE": filepath.Join(home, ".config/atm/plugins.conf"), "ATM_STATE_DIR": filepath.Join(home, "state"), "ATM_REA_INSTALL_ROOT": "", "ATM_DRY_RUN": "0", "NODE_OPTIONS": "--require /never-load", "NODE_PATH": "/never-read", "npm_config_force": "true", "NPM_CONFIG_REGISTRY": "https://evil.invalid", "REA_PRIVATE_SECRET": "private"} {
		t.Setenv(k, v)
	}
	req := protocol.Request{Protocol: 1, RequestID: strings.Repeat("a", 32), PluginID: "rea", Home: filepath.Join(home, "Apps/atm"), AppsDir: filepath.Join(home, "Apps"), Language: "en-us"}
	b, e := newBackend(req, func(string) {})
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func toolchain(t *testing.T, b *backend) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "tools with 'quotes'/bin")
	for _, name := range []string{"node", "ps"} {
		copyFixture(t, compiledTool, filepath.Join(dir, name))
	}
	npm := filepath.Join(filepath.Dir(dir), "lib/npm/bin/npm-cli.js")
	write(t, npm, []byte("synthetic npm marker"), 0600)
	if e := os.Symlink(npm, filepath.Join(dir, "npm")); e != nil {
		t.Fatal(e)
	}
	t.Setenv("PATH", dir)
	return dir
}
func control(t *testing.T, dir, mode, version string) {
	data, _ := json.Marshal(map[string]string{"Mode": mode, "Version": version})
	write(t, filepath.Join(dir, "control.json"), data, 0600)
}
func action(b *backend, op string, args ...string) error {
	b.req.Operation = op
	b.req.Arguments = args
	_, e := b.execute(context.Background())
	return e
}

type rewriteTransport struct {
	base   http.RoundTripper
	target *url.URL
}

func (r rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	q := req.Clone(req.Context())
	u := *q.URL
	u.Scheme = r.target.Scheme
	u.Host = r.target.Host
	q.URL = &u
	return r.base.RoundTrip(q)
}

type registryFixture struct {
	latest      string
	archives    map[string][]byte
	metadata    map[string]artifact
	hits        int
	redirect    bool
	URL         string
	Certificate []byte
}

func fixtureRegistry(t *testing.T, b *backend, versions ...string) *registryFixture {
	t.Helper()
	f := &registryFixture{archives: map[string][]byte{}, metadata: map[string]artifact{}}
	for _, v := range versions {
		blob := tarball(t, v, nil)
		sum := sha512.Sum512(blob)
		f.latest = v
		f.archives[v] = blob
		f.metadata[v] = artifact{v, registry + "/rea-agents/-/rea-agents-" + v + ".tgz", "sha512-" + base64.StdEncoding.EncodeToString(sum[:]), "^22.23.4 || ^24.22.1 || >=26.12.1"}
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits++
		if f.redirect {
			http.Redirect(w, r, "https://evil.invalid/payload", 302)
			return
		}
		for v, a := range f.metadata {
			if r.URL.Path == "/rea-agents/"+v || r.URL.Path == "/rea-agents/latest" && v == f.latest {
				json.NewEncoder(w).Encode(map[string]any{"name": "rea-agents", "version": v, "bin": map[string]string{"rea": "scripts/rea.mjs", "rea-agents": "scripts/rea.mjs"}, "engines": map[string]string{"node": a.Engine}, "dist": map[string]string{"tarball": a.URL, "integrity": a.Integrity}})
				return
			}
			if r.URL.Path == strings.TrimPrefix(a.URL, registry) {
				w.Write(f.archives[v])
				return
			}
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	b.client = &http.Client{Transport: rewriteTransport{srv.Client().Transport, u}, Timeout: time.Second * 10}
	f.URL = srv.URL
	f.Certificate = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	return f
}
func tarball(t *testing.T, v string, extra map[string][]byte) []byte {
	t.Helper()
	p, _ := json.Marshal(map[string]any{"name": "rea-agents", "version": v, "bin": map[string]string{"rea": "scripts/rea.mjs", "rea-agents": "scripts/rea.mjs"}, "engines": map[string]string{"node": "^22.23.4 || ^24.22.1 || >=26.12.1"}, "dependencies": map[string]string{"example": "1.0.0"}})
	integrity := "sha512-" + base64.StdEncoding.EncodeToString(make([]byte, 64))
	lock, _ := json.Marshal(map[string]any{"lockfileVersion": 3, "packages": map[string]any{"": map[string]any{"name": "rea-agents", "version": v, "dependencies": map[string]string{"example": "1.0.0"}}, "node_modules/example": map[string]string{"version": "1.0.0", "resolved": registry + "/example/-/example-1.0.0.tgz", "integrity": integrity}}})
	files := map[string][]byte{"package/package.json": p, "package/npm-shrinkwrap.json": lock, "package/scripts/rea.mjs": []byte("synthetic REA, never evaluated\n"), "package/dist/runtime.js": []byte("synthetic runtime")}
	for k, v := range extra {
		if v == nil {
			delete(files, k)
		} else {
			files[k] = v
		}
	}
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tw := tar.NewWriter(gz)
	for name, data := range files {
		if e := tw.WriteHeader(&tar.Header{Name: name, Mode: 0644, Size: int64(len(data)), Typeflag: tar.TypeReg}); e != nil {
			t.Fatal(e)
		}
		tw.Write(data)
	}
	tw.Close()
	gz.Close()
	return out.Bytes()
}
func absent(t *testing.T, p string) {
	t.Helper()
	if _, e := os.Lstat(p); !os.IsNotExist(e) {
		t.Fatalf("expected absent %s: %v", p, e)
	}
}

func TestTLSLifecycleCustomRootRefreshAndPreservation(t *testing.T) {
	b := testBackend(t)
	tools := toolchain(t, b)
	f := fixtureRegistry(t, b, "6.8.0", "6.9.0")
	f.latest = "6.8.0"
	private := map[string]string{filepath.Join(b.home, ".codex/config.toml"): "private codex", filepath.Join(b.home, ".claude/settings.json"): "private claude", filepath.Join(b.home, ".agents/mcp.json"): "private agents", filepath.Join(b.home, ".npmrc"): "force=true", filepath.Join(b.home, ".profile"): "keep profile\n", filepath.Join(b.home, "Apps/rea/bin/rea"): "manual command"}
	for p, s := range private {
		write(t, p, []byte(s), 0600)
	}
	custom := filepath.Join(b.home, "REA tools 'literal' $dollar")
	if e := action(b, "install", "--path", custom, "--trust-dependencies"); e != nil {
		t.Fatal(e)
	}
	if b.root != filepath.Join(custom, "native-v1") {
		t.Fatal(b.root)
	}
	v, e := b.current()
	if e != nil || v != "6.8.0" {
		t.Fatal(v, e)
	}
	b.setBase(b.defaultBase)
	if e = b.remembered(); e != nil || b.base != custom {
		t.Fatal(b.base, e)
	}
	unknown := filepath.Join(b.root, "captures/private.txt")
	write(t, unknown, []byte("keep unknown"), 0600)
	for p, s := range private {
		data, _ := os.ReadFile(p)
		if string(data) != s {
			t.Fatal("private data changed", p)
		}
	}
	if e = action(b, "install", "--path", filepath.Join(b.home, "other"), "--trust-dependencies"); e == nil {
		t.Fatal("destination switched")
	}
	f.latest = "6.9.0"
	if e = action(b, "update", "--trust-dependencies"); e != nil {
		t.Fatal(e)
	}
	if e = b.verifyOwned(context.Background(), "6.8.0"); e != nil {
		t.Fatal(e)
	}
	control(t, tools, "", "26.12.2")
	if e = action(b, "install", "--version", "6.9.0", "--trust-dependencies"); e != nil {
		t.Fatal(e)
	}
	var d dependencies
	if e = privateJSON(filepath.Join(b.root, "versions/6.9.0", dependencyFile), &d); e != nil || d.Version != "26.12.2" {
		t.Fatal(d, e)
	}
	if e = action(b, "integrate"); e != nil {
		t.Fatal(e)
	}
	profile, _ := os.ReadFile(filepath.Join(b.home, ".profile"))
	if !strings.Contains(string(profile), shellQuote(filepath.Join(b.root, "bin"))) {
		t.Fatal(string(profile))
	}
	if e = action(b, "uninstall"); e != nil {
		t.Fatal(e)
	}
	absent(t, b.state)
	absent(t, filepath.Join(b.root, "current"))
	absent(t, filepath.Join(b.root, "versions/6.8.0"))
	absent(t, filepath.Join(b.root, "versions/6.9.0"))
	for p, s := range private {
		data, _ := os.ReadFile(p)
		if string(data) != s {
			t.Fatal("private data changed", p)
		}
	}
	data, _ := os.ReadFile(unknown)
	if string(data) != "keep unknown" {
		t.Fatal("unknown removed")
	}
}
func TestInstallFailuresPreserveSelection(t *testing.T) {
	for _, mode := range []string{"npm-error", "bad-package", "escape", "digest", "redirect", "activation-conflict", "state-conflict"} {
		t.Run(mode, func(t *testing.T) {
			b := testBackend(t)
			tools := toolchain(t, b)
			f := fixtureRegistry(t, b, "6.8.0", "6.9.0")
			f.latest = "6.8.0"
			if e := action(b, "install", "--trust-dependencies"); e != nil {
				t.Fatal(e)
			}
			old, _ := os.ReadFile(b.state)
			f.latest = "6.9.0"
			control(t, tools, mode, "")
			if mode == "digest" {
				f.archives["6.9.0"] = []byte("wrong")
			}
			if mode == "redirect" {
				f.redirect = true
			}
			if mode == "activation-conflict" {
				if e := os.Remove(filepath.Join(b.root, "bin/rea")); e != nil {
					t.Fatal(e)
				}
			}
			if mode == "state-conflict" {
				data, _ := json.Marshal(map[string]string{"path": b.state})
				write(t, filepath.Join(tools, "state-target.json"), data, 0600)
			}
			if e := action(b, "update", "--trust-dependencies"); e == nil {
				t.Fatal("failure accepted")
			}
			v, e := b.current()
			if e != nil || v != "6.8.0" {
				t.Fatal(v, e)
			}
			now, _ := os.ReadFile(b.state)
			if mode == "state-conflict" {
				if string(now) != "foreign" {
					t.Fatal("foreign receipt replaced")
				}
			} else if !bytes.Equal(old, now) {
				t.Fatal("state changed")
			}
			matches, _ := filepath.Glob(filepath.Join(b.root, ".atm-stage-*"))
			if len(matches) > 0 {
				t.Fatal("stage leak", matches)
			}
			absent(t, filepath.Join(b.root, ".atm-native-lock"))
			absent(t, b.state+".lock")
		})
	}
}
func TestUnknownAndEditedFilesRefuseRemoval(t *testing.T) {
	for _, kind := range []string{"unknown-file", "edited-file", "version-dir", "current", "launcher", "path-edit", "receipt-duplicate"} {
		t.Run(kind, func(t *testing.T) {
			b := testBackend(t)
			toolchain(t, b)
			fixtureRegistry(t, b, "6.8.0")
			if e := action(b, "install", "--trust-dependencies"); e != nil {
				t.Fatal(e)
			}
			p := filepath.Join(b.root, "versions/6.8.0")
			switch kind {
			case "unknown-file":
				write(t, filepath.Join(p, "private"), []byte("keep"), 0600)
			case "edited-file":
				write(t, filepath.Join(p, "lib/node_modules/rea-agents/scripts/rea.mjs"), []byte("edited"), 0644)
			case "version-dir":
				write(t, filepath.Join(b.root, "versions/not-owned/secret"), []byte("private"), 0600)
			case "current":
				os.Remove(filepath.Join(b.root, "current"))
				os.Symlink("/tmp", filepath.Join(b.root, "current"))
			case "launcher":
				os.Remove(filepath.Join(b.root, "bin/rea"))
				write(t, filepath.Join(b.root, "bin/rea"), []byte("foreign"), 0600)
			case "path-edit":
				if e := action(b, "integrate"); e != nil {
					t.Fatal(e)
				}
				write(t, filepath.Join(b.home, ".profile"), []byte(pathStart+"\nforeign\n"+pathEnd+"\n"), 0640)
			case "receipt-duplicate":
				name := filepath.Join(p, receiptName)
				data, _ := os.ReadFile(name)
				data = append([]byte(`{"schema":1,`), data[1:]...)
				write(t, name, data, 0600)
			}
			if e := action(b, "uninstall"); e == nil {
				t.Fatal("unsafe uninstall accepted")
			}
			if _, e := os.Stat(filepath.Join(p, "lib/node_modules/rea-agents/package.json")); e != nil {
				t.Fatal("payload removed", e)
			}
		})
	}
}
func TestConfiguredStateAndStrictReceipts(t *testing.T) {
	b := testBackend(t)
	t.Setenv("ATM_STATE_DIR", "")
	state := filepath.Join(b.home, "persisted state")
	write(t, os.Getenv("ATM_CONFIG_FILE"), []byte("ATM_STATE_DIR='"+state+"'\n"), 0600)
	c, e := newBackend(b.req, func(string) {})
	if e != nil || c.state != filepath.Join(state, "rea/native-install.json") {
		t.Fatal(c, e)
	}
	for _, data := range []string{"null", "{}", `{"schema":1,"schema":1}`, `{"schema":1} {}`, `{"schema":1,"plugin":"rea","base":"/tmp","unknown":true}`} {
		write(t, c.state, []byte(data), 0600)
		if e = c.remembered(); e == nil {
			t.Fatal("malformed receipt accepted", data)
		}
	}
}
func TestDependencyTrustFloorsAndIsolation(t *testing.T) {
	for _, v := range []string{"22.23.4", "24.22.1", "26.12.1", "27.0.0"} {
		if !nodeSupported(v) {
			t.Fatal(v)
		}
	}
	for _, v := range []string{"22.23.3", "24.22.0", "26.12.0", "23.99.99", "25.0.0", "26.12.1-rc.1"} {
		if nodeSupported(v) {
			t.Fatal(v)
		}
	}
	b := testBackend(t)
	dir := toolchain(t, b)
	if e := action(b, "install"); e == nil {
		t.Fatal("untrusted install")
	}
	absent(t, filepath.Join(dir, "calls.jsonl"))
	absent(t, b.root)
	if e := action(b, "install", "--dependencies"); e != nil {
		t.Fatal(e)
	}
	absent(t, filepath.Join(dir, "calls.jsonl"))
	if e := action(b, "install", "--dependencies", "--trust-dependencies"); e != nil {
		t.Fatal(e)
	}
	control(t, dir, "overflow", "")
	work := t.TempDir()
	if _, e := b.dependencies(context.Background(), work); e == nil {
		t.Fatal("overflow accepted")
	}
	control(t, dir, "", "20.0.0")
	fallback := filepath.Join(b.req.AppsDir, "node/current/bin")
	copyFixture(t, compiledTool, filepath.Join(fallback, "node"))
	write(t, filepath.Join(fallback, "npm-cli.js"), []byte("fixture"), 0600)
	os.Symlink("npm-cli.js", filepath.Join(fallback, "npm"))
	d, e := b.dependencies(context.Background(), work)
	if e != nil || d.Node != filepath.Join(fallback, "node") {
		t.Fatal(d, e)
	}
}
func TestGuardedChangesRollbackPreservesModes(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	write(t, a, []byte("before"), 0640)
	ca, e := planned(a, fileState{Exists: true, Mode: 0640, Data: []byte("after")})
	if e != nil {
		t.Fatal(e)
	}
	cb, e := planned(b, regularState([]byte("new")))
	if e != nil {
		t.Fatal(e)
	}
	write(t, b, []byte("late conflict"), 0600)
	if _, e = applyChanges(context.Background(), []change{ca, cb}); e == nil {
		t.Fatal("late conflict accepted")
	}
	s, _ := snapshot(a)
	if s.Mode != 0640 || string(s.Data) != "before" {
		t.Fatal(s)
	}
	c, e := planned(filepath.Join(dir, "new"), regularState([]byte("new")))
	if e != nil {
		t.Fatal(e)
	}
	undo, e := applyChanges(context.Background(), []change{c})
	if e != nil {
		t.Fatal(e)
	}
	if e = undo(); e != nil {
		t.Fatal(e)
	}
	absent(t, c.path)
}
func TestArchiveAndMetadataGuards(t *testing.T) {
	b := testBackend(t)
	f := fixtureRegistry(t, b, "6.8.0")
	a := f.metadata["6.8.0"]
	for _, extra := range []map[string][]byte{{"package/../../escape": []byte("x")}, {"package/npm-shrinkwrap.json": []byte(`{"packages":{"x":{"resolved":"https://evil.invalid/a.tgz"}}}`)}, {"package/package.json": []byte(`{"name":"wrong"}`)}} {
		data := tarball(t, a.Version, extra)
		p := filepath.Join(t.TempDir(), "bad.tgz")
		write(t, p, data, 0600)
		if e := inspectArchive(context.Background(), p, a); e == nil {
			t.Fatal("bad archive accepted")
		}
	}
	a.URL = "https://evil.invalid/archive"
	if e := a.validate(); e == nil {
		t.Fatal("foreign artifact accepted")
	}
	if _, e := b.resolve(context.Background(), "1.2.3-beta"); e == nil {
		t.Fatal("prerelease accepted")
	}
}
func TestCompiledLauncherLiteralStreamsAndPinnedRuntime(t *testing.T) {
	b := testBackend(t)
	tools := toolchain(t, b)
	fixtureRegistry(t, b, "6.8.0")
	if e := action(b, "install", "--trust-dependencies"); e != nil {
		t.Fatal(e)
	}
	dir := filepath.Join(b.root, "versions/6.8.0")
	launcher := filepath.Join(dir, "bin/rea")
	copyFixture(t, compiledNative, launcher)
	alias := filepath.Join(t.TempDir(), "custom-alias")
	if e := os.Symlink(launcher, alias); e != nil {
		t.Fatal(e)
	}
	args := []string{"a b", "$HOME", "; touch nope", "'quoted'", "", "--version"}
	cmd := exec.Command(alias, args...)
	cmd.Stdin = strings.NewReader("literal input\n")
	cmd.Dir = b.home
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err := cmd.Run()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 37 {
		t.Fatal(err, stderr.String(), out.String())
	}
	var got struct {
		Args                     []string
		Stdin, Path, Cwd, Marker string
	}
	if e := json.Unmarshal(out.Bytes(), &got); e != nil {
		t.Fatal(e)
	}
	want := append([]string{filepath.Join(dir, "lib/node_modules/rea-agents/scripts/rea.mjs")}, args...)
	if !reflect.DeepEqual(got.Args, want) || got.Stdin != "literal input\n" || got.Cwd != b.home || got.Marker != "private" || !strings.HasPrefix(got.Path, tools+":") || stderr.String() != "synthetic stderr\n" {
		t.Fatalf("bad forwarding %+v stderr=%q", got, stderr.String())
	}
	for _, path := range []string{"", ":relative:.:"} {
		cmd := exec.Command(alias, args...)
		for _, v := range os.Environ() {
			if !strings.HasPrefix(v, "PATH=") {
				cmd.Env = append(cmd.Env, v)
			}
		}
		cmd.Env = append(cmd.Env, "PATH="+path)
		data, e := cmd.Output()
		exit, ok := e.(*exec.ExitError)
		if !ok || exit.ExitCode() != 37 {
			t.Fatal(e, string(data))
		}
		var result struct{ Path string }
		if e = json.Unmarshal(data, &result); e != nil {
			t.Fatal(e)
		}
		for _, p := range strings.Split(result.Path, ":") {
			if !filepath.IsAbs(p) {
				t.Fatal("launcher introduced cwd lookup", result.Path)
			}
		}
	}
	node := filepath.Join(tools, "node")
	data, e := os.ReadFile(node)
	if e != nil {
		t.Fatal(e)
	}
	// Replace the fixture atomically; a recently exited executable can still be mapped.
	write(t, node+".replacement", append(data, []byte("changed")...), 0700)
	if e = os.Rename(node+".replacement", node); e != nil {
		t.Fatal(e)
	}
	output, e := exec.Command(alias).CombinedOutput()
	if e == nil || !strings.Contains(string(output), "pinned Node changed") {
		t.Fatal(e, string(output))
	}
}
