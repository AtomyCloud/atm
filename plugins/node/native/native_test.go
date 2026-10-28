//go:build linux

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
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
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/AtomyCloud/atm/internal/config"
	"github.com/AtomyCloud/atm/internal/plugin"
	"github.com/AtomyCloud/atm/internal/protocol"
	"github.com/AtomyCloud/atm/internal/runner"
)

func TestMain(m *testing.M) {
	if mode := os.Getenv("ATM_NODE_TEST_COMMAND"); mode != "" {
		switch mode {
		case "version":
			fmt.Println("v24.3.2")
		case "noise":
			fmt.Print(strings.Repeat("x", 20000))
		case "wait":
			if file := os.Getenv("ATM_NODE_TEST_PID"); file != "" {
				_ = os.WriteFile(file, []byte(strconv.Itoa(os.Getpid())), 0600)
			}
			time.Sleep(time.Minute)
		case "bad":
			fmt.Println("not a version")
		case "fail":
			os.Exit(9)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}
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
	home := filepath.Join(t.TempDir(), "user")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("ATM_DRY_RUN", "")
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	req := protocol.Request{Protocol: 1, RequestID: strings.Repeat("a", 32), PluginID: "node", Operation: "menu", Home: filepath.Join(t.TempDir(), "runtime"), AppsDir: filepath.Join(t.TempDir(), "Apps")}
	b, err := newBackend(req, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	b.run = func(context.Context, string, []string, time.Duration) (string, error) {
		t.Fatal("unexpected command execution")
		return "", nil
	}
	return b
}

type tarItem struct {
	name, body, link string
	kind             byte
	mode             int64
}

func archiveBytes(t *testing.T, prefix string, extra ...tarItem) []byte {
	t.Helper()
	items := []tarItem{{"bin/node", "native fixture", "", tar.TypeReg, 0755}, {"lib/node_modules/npm/bin/npm-cli.js", "npm fixture", "", tar.TypeReg, 0755}, {"lib/node_modules/npm/bin/npx-cli.js", "npx fixture", "", tar.TypeReg, 0755}, {"bin/npm", "", "../lib/node_modules/npm/bin/npm-cli.js", tar.TypeSymlink, 0777}, {"bin/npx", "", "../lib/node_modules/npm/bin/npx-cli.js", tar.TypeSymlink, 0777}}
	items = append(items, extra...)
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, item := range items {
		size := int64(len(item.body))
		if item.kind != tar.TypeReg {
			size = 0
		}
		h := &tar.Header{Name: prefix + "/" + item.name, Linkname: item.link, Typeflag: item.kind, Mode: item.mode, Size: size}
		if err := tw.WriteHeader(h); err != nil {
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
func releaseServer(t *testing.T, b *backend, versions ...string) {
	t.Helper()
	payloads := map[string][]byte{}
	for _, v := range versions {
		prefix := "node-v" + v + "-" + b.platform
		payloads["/v"+v+"/"+prefix+".tar.gz"] = archiveBytes(t, prefix)
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/index.json" {
			rows := []map[string]any{}
			for _, v := range versions {
				rows = append(rows, map[string]any{"version": "v" + v, "files": []string{b.platform}})
			}
			_ = json.NewEncoder(w).Encode(rows)
			return
		}
		if strings.HasSuffix(r.URL.Path, "SHASUMS256.txt") {
			dir := strings.TrimSuffix(r.URL.Path, "SHASUMS256.txt")
			for name, data := range payloads {
				if strings.HasPrefix(name, dir) {
					fmt.Fprintf(w, "%x  %s\n", sha256.Sum256(data), filepath.Base(name))
					return
				}
			}
		}
		if data, ok := payloads[r.URL.Path]; ok {
			_, _ = w.Write(data)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	b.client = srv.Client()
	b.base = srv.URL
}
func action(b *backend, op string, args ...string) error {
	b.req.Operation = op
	b.req.Arguments = args
	_, err := b.execute(context.Background())
	return err
}
func TestLifecycleAndProfiles(t *testing.T) {
	b := newTestBackend(t)
	releaseServer(t, b, "24.2.0", "24.11.0", "26.1.0")
	original := []byte("# user text\n# >>> ATM PATH >>>\nexport KEEP=yes\n# <<< ATM PATH <<<\n")
	for _, name := range []string{".profile", ".bashrc", ".zshrc"} {
		write(t, filepath.Join(b.home, name), original, 0640)
	}
	if err := action(b, "install"); err != nil {
		t.Fatal(err)
	}
	if v, _ := b.current(); v != "24.11.0" {
		t.Fatal(v)
	}
	if _, err := os.Stat(b.req.Home); !os.IsNotExist(err) {
		t.Fatal("request Home incorrectly used as user home")
	}
	for _, name := range []string{".profile", ".bashrc", ".zshrc"} {
		data, _ := os.ReadFile(filepath.Join(b.home, name))
		if !bytes.HasPrefix(data, original) || strings.Count(string(data), profileStart) != 1 {
			t.Fatal(string(data))
		}
	}
	if err := action(b, "install", "--version", "26.1.0"); err != nil {
		t.Fatal(err)
	}
	if err := action(b, "use", "v24.11.0"); err != nil {
		t.Fatal(err)
	}
	if err := action(b, "remove", "24.11.0"); err == nil {
		t.Fatal("removed active")
	}
	user := filepath.Join(b.root, "26.1.0", "lib/node_modules/custom/user.txt")
	write(t, user, []byte("global npm/user file"), 0600)
	if err := action(b, "remove", "26.1.0"); err == nil {
		t.Fatal("removed user addition")
	}
	if err := action(b, "uninstall"); err == nil {
		t.Fatal("uninstall ignored modified owned version")
	}
	if v, _ := b.current(); v != "24.11.0" {
		t.Fatal("uninstall changed active before preflight")
	}
	if err := os.RemoveAll(filepath.Dir(user)); err != nil {
		t.Fatal(err)
	}
	if err := action(b, "remove", "26.1.0"); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(b.root, "notes"), []byte("unrelated"), 0600)
	if err := action(b, "integrate"); err != nil {
		t.Fatal(err)
	}
	if err := action(b, "uninstall"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".profile", ".bashrc", ".zshrc"} {
		data, _ := os.ReadFile(filepath.Join(b.home, name))
		if !bytes.Equal(data, original) {
			t.Fatalf("profile not preserved: %q", data)
		}
	}
	if _, err := os.Stat(filepath.Join(b.root, "notes")); err != nil {
		t.Fatal(err)
	}
}
func TestBareUpdatePreservesActiveMajor(t *testing.T) {
	b := newTestBackend(t)
	releaseServer(t, b, "24.9.0", "25.2.0", "26.1.0", "26.2.0")
	if err := action(b, "update"); err == nil || !strings.Contains(err.Error(), "--major or --version") {
		t.Fatalf("bare update without current must require a selector: %v", err)
	}
	for _, version := range []string{"24.9.0", "25.2.0", "26.1.0"} {
		if err := action(b, "install", "--version", version); err != nil {
			t.Fatal(err)
		}
		if err := action(b, "update"); err != nil {
			t.Fatal(err)
		}
		current, err := b.current()
		want := version
		if version == "26.1.0" {
			want = "26.2.0"
		}
		if err != nil || current != want {
			t.Fatalf("update from %s selected %s, want %s: %v", version, current, want, err)
		}
	}
	write(t, filepath.Join(b.root, "22.1.0/bin/node"), []byte("legacy"), 0700)
	if err := action(b, "use", "22.1.0"); err != nil {
		t.Fatal(err)
	}
	if err := action(b, "update"); err == nil || !strings.Contains(err.Error(), "--major or --version") {
		t.Fatalf("bare update from unsupported major must require a selector: %v", err)
	}
	if current, _ := b.current(); current != "22.1.0" {
		t.Fatal("refused update changed current", current)
	}
	if err := action(b, "update", "--major", "26"); err != nil {
		t.Fatal(err)
	}
	if current, _ := b.current(); current != "26.2.0" {
		t.Fatal("explicit update selector ignored", current)
	}
	if err := action(b, "install"); err != nil {
		t.Fatal(err)
	}
	if current, _ := b.current(); current != "24.9.0" {
		t.Fatal("bare install no longer defaults to major 24", current)
	}
}

func TestLegacyAndUnsafeRoots(t *testing.T) {
	b := newTestBackend(t)
	write(t, filepath.Join(b.root, "22.1.0/bin/node"), []byte("legacy"), 0700)
	if err := action(b, "use", "22.1.0"); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(b.root, "24.1.0/bin/node"), []byte("legacy"), 0700)
	if err := action(b, "use", "24.1.0"); err != nil {
		t.Fatal(err)
	}
	if err := action(b, "remove", "22.1.0"); err == nil {
		t.Fatal("removed legacy")
	}
	if err := action(b, "uninstall"); err != nil {
		t.Fatal(err)
	}
	if v, _ := b.current(); v != "24.1.0" {
		t.Fatal("lost legacy current")
	}
	if err := os.Remove(filepath.Join(b.root, "current")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(b.root, "current")); err != nil {
		t.Fatal(err)
	}
	if err := action(b, "use", "22.1.0"); err == nil {
		t.Fatal("replaced external current")
	}
	b2 := newTestBackend(t)
	if err := os.MkdirAll(filepath.Dir(b2.root), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), b2.root); err != nil {
		t.Fatal(err)
	}
	if err := action(b2, "install", "--version", "24.1.0"); err == nil {
		t.Fatal("adopted symlink root")
	}
}
func TestLatestOptionsMenus(t *testing.T) {
	b := newTestBackend(t)
	releaseServer(t, b, "24.9.1", "24.10.0", "24.2.0", "24.11.0", "25.1.0", "26.1.0")
	got, err := b.latest(context.Background(), "24")
	if err != nil || !reflect.DeepEqual(got, []string{"24.11.0", "24.10.0", "24.9.1"}) {
		t.Fatal(got, err)
	}
	for _, args := range [][]string{{"--major", "24"}, {"--locations"}, {"--installed", "use"}, nil} {
		cs, err := b.menu(context.Background(), args)
		if err != nil {
			t.Fatal(err)
		}
		if len(cs) > 100 {
			t.Fatal(len(cs))
		}
	}
	cs, err := b.menu(context.Background(), []string{"--major", "24"})
	if err != nil || len(cs) != 4 || cs[3].Prompt != "Node.js version" || !reflect.DeepEqual(cs[3].Arguments, []string{"--version"}) {
		t.Fatal(cs, err)
	}
	for _, args := range [][]string{{"--version", "../../x"}, {"--major", "23"}, {"--version", "24.1.0", "--major", "24"}, {"--location", "system", "--version", "24.1.0"}, {"--version"}, {"--major", "24", "--major", "25"}, {"--accept-system-packages"}, {"--junk"}} {
		if _, err := parseOptions(args); err == nil {
			t.Fatal("accepted", args)
		}
	}
	if o, err := parseOptions([]string{"--version=node-v22.5.1"}); err != nil || o.version != "22.5.1" {
		t.Fatal(o, err)
	}
}
func TestChecksumFailures(t *testing.T) {
	good := strings.Repeat("a", 64) + "  node.tar.gz\n"
	for _, data := range []string{"", "bad node.tar.gz", good + good, strings.Repeat("a", 64) + " other", good + "broken"} {
		if _, err := checksum(data, "node.tar.gz"); err == nil {
			t.Fatal("accepted checksum", data)
		}
	}
	if got, err := checksum(good, "node.tar.gz"); err != nil || got != strings.Repeat("a", 64) {
		t.Fatal(got, err)
	}
}
func TestArchiveAttacks(t *testing.T) {
	cases := []tarItem{{"../../escape", "x", "", tar.TypeReg, 0600}, {"bad", "", "../../escape", tar.TypeSymlink, 0777}, {"bad", "", "/etc/passwd", tar.TypeSymlink, 0777}, {"bad", "", "root/missing", tar.TypeLink, 0600}, {"bin/node", "duplicate", "", tar.TypeReg, 0700}, {receiptName, "fake", "", tar.TypeReg, 0600}, {"device", "", "", tar.TypeFifo, 0600}, {"suid", "x", "", tar.TypeReg, 04755}, {"bin/npm/attack", "x", "", tar.TypeReg, 0600}}
	for _, item := range cases {
		t.Run(item.name+item.link, func(t *testing.T) {
			dir := t.TempDir()
			archive := filepath.Join(dir, "archive")
			write(t, archive, archiveBytes(t, "root", item), 0600)
			dest := filepath.Join(dir, "payload")
			if err := os.Mkdir(dest, 0700); err != nil {
				t.Fatal(err)
			}
			r, err := os.OpenRoot(dest)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			if err = extractArchive(context.Background(), archive, r, "root"); err == nil {
				t.Fatal("accepted malicious archive")
			}
		})
	}
	for _, corrupt := range []bool{false, true} {
		dir := t.TempDir()
		data := archiveBytes(t, "root")
		if corrupt {
			data = data[:len(data)-6]
		}
		archive := filepath.Join(dir, "archive")
		write(t, archive, data, 0600)
		dest := filepath.Join(dir, "payload")
		_ = os.Mkdir(dest, 0700)
		r, _ := os.OpenRoot(dest)
		err := extractArchive(context.Background(), archive, r, "root")
		r.Close()
		if corrupt && err == nil {
			t.Fatal("accepted truncated gzip")
		}
		if !corrupt && err != nil {
			t.Fatal(err)
		}
	}
}
func TestFailureAndCancellationPreserveCurrent(t *testing.T) {
	b := newTestBackend(t)
	releaseServer(t, b, "24.1.0", "24.2.0")
	if err := action(b, "install", "--version", "24.1.0"); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(b.home, ".profile"))
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "SHASUMS256.txt") {
			fmt.Fprintf(w, "%s  node-v24.2.0-%s.tar.gz\n", strings.Repeat("0", 64), b.platform)
		} else {
			fmt.Fprint(w, "bad archive")
		}
	}))
	defer srv.Close()
	b.base = srv.URL
	b.client = srv.Client()
	if err := action(b, "install", "--version", "24.2.0"); err == nil {
		t.Fatal("checksum failure accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	b.req.Operation = "install"
	b.req.Arguments = []string{"--version", "24.2.0"}
	if _, err := b.execute(ctx); err != context.Canceled {
		t.Fatal(err)
	}
	if current, _ := b.current(); current != "24.1.0" {
		t.Fatal(current)
	}
	after, _ := os.ReadFile(filepath.Join(b.home, ".profile"))
	if !bytes.Equal(before, after) {
		t.Fatal("profile changed")
	}
	entries, _ := os.ReadDir(b.root)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".atm-stage") || e.Name() == ".atm-native-lock" {
			t.Fatal("leaked stage/lock")
		}
	}
}
func TestProfilesPreflightRollbackAndLock(t *testing.T) {
	b := newTestBackend(t)
	write(t, filepath.Join(b.root, "24.1.0/bin/node"), []byte("legacy"), 0700)
	original := filepath.Join(t.TempDir(), "original")
	write(t, original, []byte("untouched"), 0600)
	if err := os.Symlink(original, filepath.Join(b.home, ".bashrc")); err != nil {
		t.Fatal(err)
	}
	if err := action(b, "use", "24.1.0"); err == nil {
		t.Fatal("accepted symlink profile")
	}
	if _, err := os.Lstat(filepath.Join(b.home, ".profile")); !os.IsNotExist(err) {
		t.Fatal("partial profile write")
	}
	_ = os.Remove(filepath.Join(b.home, ".bashrc"))
	write(t, filepath.Join(b.home, ".bashrc"), []byte(profileStart+"\n"), 0600)
	if err := action(b, "use", "24.1.0"); err == nil {
		t.Fatal("accepted malformed profile")
	}
	_ = os.Remove(filepath.Join(b.home, ".bashrc"))
	plans, err := b.profilePlans(false)
	if err != nil {
		t.Fatal(err)
	}
	rollback, err := applyProfiles(context.Background(), plans)
	if err != nil {
		t.Fatal(err)
	}
	if err = rollback(); err != nil {
		t.Fatal(err)
	}
	_ = os.Mkdir(filepath.Join(b.root, ".atm-native-lock"), 0700)
	if err := action(b, "use", "24.1.0"); err == nil {
		t.Fatal("ignored lock")
	}
}
func TestNativeCommandsAndSystemPolicy(t *testing.T) {
	b := newTestBackend(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	b.run = runCommand
	t.Setenv("ATM_NODE_TEST_COMMAND", "version")
	if v := b.probe(context.Background(), exe); v != "24.3.2" {
		t.Fatal(v)
	}
	for _, mode := range []string{"noise", "bad", "fail"} {
		t.Setenv("ATM_NODE_TEST_COMMAND", mode)
		if v := b.probe(context.Background(), exe); v != "unavailable" {
			t.Fatal(mode, v)
		}
	}
	t.Setenv("ATM_NODE_TEST_COMMAND", "noise")
	if _, err := runCommand(context.Background(), exe, []string{"update"}, time.Second); err != nil {
		t.Fatal("truncated APT-style success rejected", err)
	}
	t.Setenv("ATM_NODE_TEST_COMMAND", "wait")
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := runCommand(ctx, exe, []string{"--version"}, 3*time.Second); err == nil || time.Since(start) > 2*time.Second {
		t.Fatal("unbounded cancellation", err)
	}
	dir := t.TempDir()
	for _, name := range []string{"sudo", "apt-get", "node", "npm"} {
		if err := os.Symlink(exe, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	t.Setenv("ATM_NODE_SYSTEM_BINARY", filepath.Join(dir, "node"))
	t.Setenv("ATM_NODE_SYSTEM_NPM", filepath.Join(dir, "npm"))
	calls := [][]string{}
	b.run = func(ctx context.Context, name string, args []string, d time.Duration) (string, error) {
		calls = append(calls, append([]string{name}, args...))
		return "v24.3.2\n", nil
	}
	if err := action(b, "install", "--location", "system"); err == nil || len(calls) != 0 {
		t.Fatal("missing consent executed")
	}
	if err := action(b, "install", "--location", "system", "--accept-system-packages"); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 3 {
		t.Fatal(calls)
	}
	if os.Geteuid() != 0 {
		for _, call := range calls[:2] {
			if call[1] != "-n" || call[2] != "--" {
				t.Fatal(call)
			}
		}
	}
	if !reflect.DeepEqual(calls[1][len(calls[1])-5:], []string{"--no-remove", "--yes", "install", "nodejs", "npm"}) {
		t.Fatal(calls)
	}
	if _, err := os.Stat(b.root); !os.IsNotExist(err) {
		t.Fatal("system install touched portable root")
	}
	calls = nil
	b.run = func(context.Context, string, []string, time.Duration) (string, error) {
		calls = append(calls, nil)
		return "", fmt.Errorf("authorization failed")
	}
	if err := action(b, "install", "--location", "system", "--accept-system-packages"); err == nil || !strings.Contains(err.Error(), "sudo -v") || len(calls) != 1 {
		t.Fatal(err, calls)
	}
}
func TestDryRunAndStatusOnlyProbes(t *testing.T) {
	b := newTestBackend(t)
	t.Setenv("ATM_DRY_RUN", "1")
	if err := action(b, "install"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(b.root); !os.IsNotExist(err) {
		t.Fatal("dry run wrote")
	}
	t.Setenv("ATM_DRY_RUN", "")
	if err := action(b, "menu"); err != nil {
		t.Fatal(err)
	}
	exe, _ := os.Executable()
	t.Setenv("ATM_NODE_LOCAL_BINARY", exe)
	t.Setenv("ATM_NODE_SYSTEM_BINARY", exe)
	t.Setenv("PATH", t.TempDir())
	calls := 0
	b.run = func(context.Context, string, []string, time.Duration) (string, error) { calls++; return "v24.1.0", nil }
	if err := action(b, "status"); err != nil || calls != 2 {
		t.Fatal(err, calls)
	}
}
func TestCompiledBackendThroughCore(t *testing.T) {
	b := newTestBackend(t)
	root := t.TempDir()
	dir := filepath.Join(root, "plugins/node")
	bin := filepath.Join(dir, "bin/node-native")
	if err := os.MkdirAll(filepath.Dir(bin), 0700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin/go"), "build", "-o", bin, ".")
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, data)
	}
	manifest := map[string]any{"schema": 2, "id": "node", "name": "Node.js", "version": "0.0.1", "description": "Node test backend", "order": 1, "min_core_api": 2, "platforms": []string{"linux"}, "executor": "native", "protocol": "jsonl-v1", "operations": []string{"menu", "status", "install", "update", "use", "remove", "uninstall", "integrate"}, "executables": map[string]string{"linux/" + runtime.GOARCH: "bin/node-native"}}
	data, _ := json.Marshal(manifest)
	write(t, filepath.Join(dir, "plugin.json"), data, 0600)
	c := config.Config{Home: root, PluginsDir: filepath.Join(root, "plugins"), AppsDir: b.req.AppsDir, Language: "en-us"}
	ps, err := plugin.Discover(c, "linux")
	if err != nil || len(ps) != 1 {
		t.Fatal(ps, err)
	}
	t.Setenv("PATH", t.TempDir())
	for _, op := range []string{"menu", "integrate"} {
		plan, err := runner.Plugin(c, ps[0], op, nil)
		if err != nil {
			t.Fatal(err)
		}
		out := runner.RunNative(context.Background(), c, plan, runner.NativeOptions{Timeout: 5 * time.Second})
		if op == "menu" && (out.Status != "success" || len(out.Choices) != 10) {
			t.Fatalf("menu: %+v", out)
		}
		if op == "integrate" && out.Status != "error" {
			t.Fatalf("integrate empty install: %+v", out)
		}
	}
	// A real native command fixture blocks in status. CORE must terminate backend
	// and descendants in the shared process group under cancellation.
	exe, _ := os.Executable()
	t.Setenv("ATM_NODE_LOCAL_BINARY", exe)
	t.Setenv("ATM_NODE_SYSTEM_BINARY", filepath.Join(t.TempDir(), "absent"))
	t.Setenv("ATM_NODE_TEST_COMMAND", "wait")
	pidFile := filepath.Join(t.TempDir(), "probe.pid")
	t.Setenv("ATM_NODE_TEST_PID", pidFile)
	plan, err := runner.Plugin(c, ps[0], "status", nil)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	out := runner.RunNative(context.Background(), c, plan, runner.NativeOptions{Timeout: 150 * time.Millisecond})
	if out.Code != "timeout" || time.Since(start) > 3*time.Second {
		t.Fatalf("cancel: %+v", out)
	}
	pidData, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal("native child did not start", err)
	}
	pid, err := strconv.Atoi(string(pidData))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if os.IsNotExist(err) {
			break
		}
		end := bytes.LastIndexByte(stat, ')')
		if end >= 0 && len(stat) > end+2 && stat[end+2] == 'Z' {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("CORE cancellation left probe alive", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestFetchBoundsRedirectAndCancellation(t *testing.T) {
	b := newTestBackend(t)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, "https://example.invalid/x", 302)
		case "/slow":
			<-r.Context().Done()
		default:
			fmt.Fprint(w, strings.Repeat("x", 100))
		}
	}))
	defer srv.Close()
	b.base = srv.URL
	b.client = srv.Client()
	if err := b.fetch(context.Background(), "/large", io.Discard, 10); err == nil {
		t.Fatal("unbounded fetch")
	}
	if err := b.fetch(context.Background(), "/redirect", io.Discard, 100); err == nil {
		t.Fatal("cross-origin redirect")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := b.fetch(ctx, "/slow", io.Discard, 100); err == nil {
		t.Fatal("ignored cancellation")
	}
}
