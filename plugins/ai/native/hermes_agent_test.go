//go:build linux

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

type hermesAgentFixture struct {
	*fixture
	source   hermesSource
	commands [][]string
	fault    string
}

func newHermesAgentFixture(t *testing.T, arch string) *hermesAgentFixture {
	t.Helper()
	f := &hermesAgentFixture{fixture: newFixture(t, arch)}
	machine, target := "x86_64", "linux-x64"
	if arch == "arm64" {
		machine, target = "aarch64", "linux-arm64"
	}
	uvEntry := "uv-" + machine + "-unknown-linux-gnu/uv"
	uvBytes := archive(t, uvEntry, fakeELF(arch, "0.13.5", false), tar.TypeReg)
	uvURL := "https://github.com/astral-sh/uv/releases/download/0.13.5/uv-" + machine + "-unknown-linux-gnu.tar.gz"
	lock := map[string]any{"packages": map[string]any{
		"unrelated": map[string]any{"version": "1", "artifacts": map[string]any{"any": []any{map[string]string{"url": "unused"}}}},
		"uv":        map[string]any{"version": "0.13.5", "artifacts": map[string]any{target: hermesPin{uvURL, fmt.Sprintf("%x", sha256.Sum256(uvBytes))}}},
		"python":    map[string]any{"version": "3.14.9+20261021", "artifacts": map[string]any{target: hermesPin{"https://github.com/astral-sh/python-build-standalone/releases/download/20261021/synthetic.tar.gz", strings.Repeat("a", 64)}}},
	}}
	lockBytes, _ := json.Marshal(lock)
	files := map[string][]byte{
		"pm/lock.json": lockBytes, "pm/cli.py": []byte("# synthetic PM, never executed\n"),
		"pm/__init__.py": []byte("# fixture\n"), "hermes_cli/__main__.py": []byte("# fixture\n"),
		"hermes_cli/venv_sync.py": []byte("# fixture\n"), "hermes_cli/_launchers.py": []byte("# fixture\n"),
		"pyproject.toml": []byte("# fixture\n"), "uv.lock": []byte("# fixture\n"),
	}
	f.source = hermesSource{Version: hermesPMVersion, Commit: hermesPMCommit, Tree: strings.Repeat("0", 40), Files: map[string]hermesSourceFile{}}
	for name, body := range files {
		f.source.Files[name] = hermesSourceFile{100644, int64(len(body)), hermesObjectHash("blob", body)}
	}
	trees, err := hermesSourceTrees(context.Background(), f.source)
	if err != nil {
		t.Fatal(err)
	}
	f.source.Tree = trees["."]
	put := func(endpoint string, value any) {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		f.responses[hermesSourceAPI+endpoint] = data
	}
	release := map[string]any{"tag_name": "v" + hermesPMVersion, "draft": false, "prerelease": false}
	put("releases/latest", release)
	put("releases/tags/v"+hermesPMVersion, release)
	put("git/ref/tags/v"+hermesPMVersion, map[string]any{"ref": "refs/tags/v" + hermesPMVersion, "object": map[string]string{"type": "commit", "sha": hermesPMCommit}})
	put("git/commits/"+hermesPMCommit, map[string]any{"sha": hermesPMCommit, "tree": map[string]string{"sha": f.source.Tree}})
	var tree []map[string]any
	for name, sha := range trees {
		if name != "." {
			tree = append(tree, map[string]any{"path": name, "mode": "040000", "type": "tree", "sha": sha})
		}
	}
	for name, entry := range f.source.Files {
		tree = append(tree, map[string]any{"path": name, "mode": "100644", "type": "blob", "sha": entry.SHA, "size": entry.Size})
	}
	put("git/trees/"+f.source.Tree+"?recursive=1", map[string]any{"sha": f.source.Tree, "truncated": false, "tree": tree})
	var payload bytes.Buffer
	gz := gzip.NewWriter(&payload)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "hermes-agent-" + hermesPMCommit + "/", Typeflag: tar.TypeDir, Mode: 0755}); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		data := files[name]
		if err := tw.WriteHeader(&tar.Header{Name: "hermes-agent-" + hermesPMCommit + "/" + name, Typeflag: tar.TypeReg, Mode: 0644, Size: int64(len(data))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	f.responses[hermesSourceArchive+hermesPMCommit] = payload.Bytes()
	f.responses[uvURL] = uvBytes
	f.b.hermes = &hermesTools{uid: func() int { return 1000 }, run: func(ctx context.Context, executable string, args []string, dir string, env []string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		f.commands = append(f.commands, append([]string{executable}, args...))
		versionDir := filepath.Dir(dir)
		if dir != filepath.Join(f.b.root(hermesAgent), "versions", hermesPMVersion, "source") || !reflect.DeepEqual(env, hermesEnvironment(versionDir)) {
			t.Fatal("wrong final source or inherited environment", dir, env)
		}
		joined := strings.Join(args, " ")
		if f.fault != "" && strings.Contains(joined, f.fault) {
			return "", errors.New("synthetic command failure")
		}
		python := filepath.Join(versionDir, "python", "cpython", "bin", "python3.14")
		switch joined {
		case "python install --no-config --no-bin --no-registry 3.14.9":
			writeFixture(t, python, fakeELF(arch, "python", false), 0755)
		case "python find --no-config --managed-python --system --no-project 3.14.9":
			return python + "\n", nil
		case "-s -B -m pm.cli install --without agent-browser --without cua-driver":
			seedHermesPMCompletion(t, versionDir, arch, time.Now().UTC())
		case "-s -B -m pm.cli doctor":
		default:
			if reflect.DeepEqual(args, []string{"-s", "-B", "-m", "hermes_cli.venv_sync", "--project-root", dir, "--check", "--json"}) {
				return `{"state":"current","ok":true}`, nil
			}
			bin := filepath.Join(dir, ".hermes", "bin")
			if reflect.DeepEqual(args, []string{"-I", "-B", filepath.Join(dir, "hermes_cli", "_launchers.py"), bin}) {
				for _, name := range []string{"hermes", "hermes-acp"} {
					writeFixture(t, filepath.Join(bin, name), []byte("# synthetic launcher never executed\n"), 0755)
				}
				return filepath.Join(bin, "hermes") + "\n" + filepath.Join(bin, "hermes-acp") + "\n", nil
			}
			t.Fatal("unexpected upstream command", joined)
		}
		return "", nil
	}}
	return f
}

func hermesAgentArgs() []string {
	return []string{"--provider", hermesAgent, "--trust-download", "--accept-dependency-execution"}
}

func TestHermesAgentPrepareAndReadonly(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		t.Run(arch, func(t *testing.T) {
			f := newHermesAgentFixture(t, arch)
			private := filepath.Join(os.Getenv("HOME"), ".hermes", "config.yaml")
			writeFixture(t, private, []byte("do not read or modify"), 0600)
			t.Setenv("OPENAI_API_KEY", "must-not-inherit")
			mustRun(t, f.fixture, "install", hermesAgentArgs()...)
			if len(f.commands) != 6 {
				t.Fatal("wrong command count", f.commands)
			}
			vs, current, err := f.b.hermesInspect(context.Background())
			if err != nil || len(vs) != 1 || current != hermesPMVersion {
				t.Fatal(vs, current, err)
			}
			calls := f.calls
			mustRun(t, f.fixture, "status")
			mustRun(t, f.fixture, "menu", hermesAgent)
			if f.calls != calls || len(f.commands) != 6 {
				t.Fatal("readonly operation ran network or PM")
			}
			mustRun(t, f.fixture, "install", hermesAgentArgs()...)
			mustRun(t, f.fixture, "update", hermesAgentArgs()...)
			if len(f.commands) != 6 {
				t.Fatal("same-version install/update repaired dependencies implicitly")
			}
			if got := string(integrationRead(t, private)); got != "do not read or modify" {
				t.Fatal("external data changed")
			}
			integrationMissing(t, filepath.Join(f.b.root(hermesAgent), "path.json"))
			integrationMissing(t, f.b.root(hermesAgent)+".lock")
		})
	}
}

func TestHermesAgentConsentAndPendingRoutes(t *testing.T) {
	f := newHermesAgentFixture(t, "amd64")
	for _, args := range [][]string{{"--provider", hermesAgent}, {"--provider", hermesAgent, "--trust-download"}, {"--provider", hermesAgent, "--accept-dependency-execution"}} {
		if err := f.run(context.Background(), "install", args...); err == nil {
			t.Fatal("missing consent accepted")
		}
	}
	if err := f.run(context.Background(), "use", "--provider", hermesAgent, "--version", hermesPMVersion); err == nil {
		t.Fatal("unowned selection accepted")
	}
	for _, op := range []string{"remove", "uninstall", "integrate"} {
		if err := f.run(context.Background(), op, "--provider", hermesAgent, "--version", hermesPMVersion); err == nil {
			t.Fatal("pending route accepted", op)
		}
	}
	if err := f.run(context.Background(), "update", hermesAgentArgs()...); err == nil {
		t.Fatal("unowned update accepted")
	}
	if f.calls != 0 || len(f.commands) != 0 {
		t.Fatal("consent failure performed work")
	}
	integrationMissing(t, f.b.root(hermesAgent))
}

func TestHermesAgentFailurePreservesAttempt(t *testing.T) {
	for _, fault := range []string{"python install", "python find", "pm.cli install", "pm.cli doctor"} {
		t.Run(fault, func(t *testing.T) {
			f := newHermesAgentFixture(t, "amd64")
			f.fault = fault
			if err := f.run(context.Background(), "install", hermesAgentArgs()...); err == nil || !strings.Contains(err.Error(), "retained") {
				t.Fatal("failure not retained", err)
			}
			root := f.b.root(hermesAgent)
			integrationMissing(t, filepath.Join(root, "current.json"))
			integrationMissing(t, filepath.Join(root, "versions", hermesPMVersion, "complete.json"))
			if err := dirs(root+".lock", false, true); err != nil {
				t.Fatal("lock missing", err)
			}
			calls := f.calls
			mustRun(t, f.fixture, "status")
			if err := f.run(context.Background(), "install", hermesAgentArgs()...); err == nil {
				t.Fatal("partial attempt retried")
			}
			if calls != f.calls {
				t.Fatal("locked attempt performed network")
			}
		})
	}
}

func TestHermesAgentSourceMutationBeforeExecution(t *testing.T) {
	f := newHermesAgentFixture(t, "amd64")
	f.b.checkpoint = func(point string) error {
		if point == "hermes-before-bootstrap" {
			writeFixture(t, filepath.Join(f.b.root(hermesAgent), "versions", hermesPMVersion, "source", "pm", "cli.py"), []byte("changed"), 0600)
		}
		return nil
	}
	if err := f.run(context.Background(), "install", hermesAgentArgs()...); err == nil {
		t.Fatal("changed source accepted")
	}
	if len(f.commands) != 0 {
		t.Fatal("changed source executed")
	}
}

func TestHermesAgentCancellationAndForeignRoot(t *testing.T) {
	f := newHermesAgentFixture(t, "amd64")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := f.run(ctx, "install", hermesAgentArgs()...); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(f.b.root(hermesAgent), "manual"), []byte("keep"), 0600)
	if err := f.run(context.Background(), "install", hermesAgentArgs()...); err == nil {
		t.Fatal("foreign root adopted")
	}
	if f.calls != 0 || len(f.commands) != 0 {
		t.Fatal("foreign root performed work")
	}
}

func TestHermesAgentRefusesRootBeforeWrites(t *testing.T) {
	f := newHermesAgentFixture(t, "amd64")
	f.b.hermes.uid = func() int { return 0 }
	if err := f.run(context.Background(), "install", hermesAgentArgs()...); err == nil || !strings.Contains(err.Error(), "refuses root") {
		t.Fatal("root accepted", err)
	}
	if f.calls != 0 || len(f.commands) != 0 {
		t.Fatal("root attempt performed work")
	}
	integrationMissing(t, f.b.root(hermesAgent))
}

func TestHermesAgentHostPolicy(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := hermesHostTools(dir); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"sudo", "su", "apt", "apt-get", "dnf", "yum", "pacman", "apk"} {
		integrationMissing(t, filepath.Join(dir, "host-bin", name))
	}
	cat, err := hermesHostCommand("cat")
	if err != nil {
		t.Fatal(err)
	}
	b := &backend{}
	out, err := b.hermesRun(context.Background(), cat, []string{"/proc/self/status"}, dir, hermesEnvironment(dir))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "NoNewPrivs:\t1") {
		t.Fatal("privilege elevation was not disabled")
	}
}

func TestHermesAgentFinalPublicationGuards(t *testing.T) {
	for _, kind := range []string{"source", "receipt", "selection", "cancel", "cancel-publication"} {
		t.Run(kind, func(t *testing.T) {
			f := newHermesAgentFixture(t, "amd64")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			seen := false
			root := f.b.root(hermesAgent)
			f.b.checkpoint = func(point string) error {
				want := "hermes-before-complete"
				if kind == "cancel-publication" {
					want = "hermes-before-select"
				}
				if point != want {
					return nil
				}
				seen = true
				switch kind {
				case "source":
					writeFixture(t, filepath.Join(root, "versions", hermesPMVersion, "source", "pm", "cli.py"), []byte("changed"), 0644)
				case "receipt":
					writeFixture(t, filepath.Join(root, "versions", hermesPMVersion, "source.json"), []byte("{}"), 0600)
				case "selection":
					writeFixture(t, filepath.Join(root, "current.json"), []byte("manual selection"), 0600)
				case "cancel", "cancel-publication":
					cancel()
				}
				return nil
			}
			if err := f.run(ctx, "install", hermesAgentArgs()...); err == nil {
				t.Fatal("late mutation accepted")
			}
			if !seen {
				t.Fatal("completion boundary not reached")
			}
			if kind == "selection" {
				if got := string(integrationRead(t, filepath.Join(root, "current.json"))); got != "manual selection" {
					t.Fatal("manual selection replaced")
				}
			} else {
				integrationMissing(t, filepath.Join(root, "current.json"))
			}
			if err := dirs(root+".lock", false, true); err != nil {
				t.Fatal("missing retained lock", err)
			}
		})
	}
}
