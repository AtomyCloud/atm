package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/AtomyCloud/atm/internal/protocol"
)

func TestNativeNodeCLIWithoutShell(t *testing.T) {
	repository, _ := fixture(t)
	root := t.TempDir()
	home := filepath.Join(root, "runtime")
	user := filepath.Join(root, "user")
	apps := filepath.Join(user, "Apps")
	pluginDir := filepath.Join(home, "plugins", "node")
	write := func(name string, value []byte, mode os.FileMode) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, value, mode); err != nil {
			t.Fatal(err)
		}
	}
	manifest, err := os.ReadFile(filepath.Join(repository, "plugins/node/plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(pluginDir, "plugin.json"), manifest, 0600)
	write(filepath.Join(home, "core-api.json"), []byte(`{"schema":1,"plugin_api":1}`), 0600)
	write(filepath.Join(home, "lang/en-us.lang"), []byte("ATM_TEST=fixture\n"), 0600)
	binary := filepath.Join(pluginDir, "bin", "linux-"+runtime.GOARCH, "atm-node")
	build := exec.Command(filepath.Join(runtime.GOROOT(), "bin/go"), "build", "-o", binary, "./plugins/node/native")
	build.Dir = repository
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build Node: %v\n%s", err, out)
	}
	for _, key := range []string{"ATM_NODE_LOCAL_BINARY", "ATM_NODE_SYSTEM_BINARY", "ATM_NODE_SYSTEM_NPM"} {
		t.Setenv(key, filepath.Join(user, "absent"))
	}
	t.Setenv("HOME", user)
	t.Setenv("ATM_CONFIG_DIR", filepath.Join(root, "config"))
	t.Setenv("ATM_STATE_DIR", filepath.Join(root, "state"))
	t.Setenv("ATM_APPS_DIR", apps)
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:9")
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:9")
	t.Setenv("NO_PROXY", "")
	write(filepath.Join(user, ".zshrc"), []byte("# existing user setting\n"), 0600)
	localNode := filepath.Join(user, ".local/bin/node")
	write(localNode, []byte("external runtime preserved"), 0700)
	traps := administrativeProcessTraps(t, root, home)
	t.Setenv("PATH", traps)
	run := func(want int, args ...string) []protocol.Event {
		t.Helper()
		var out, diagnostics bytes.Buffer
		code := Run(append([]string{"--home", home, "--json"}, args...), nil, &out, &diagnostics)
		if code != want {
			t.Fatalf("%v: exit=%d want=%d\n%s\n%s", args, code, want, &out, &diagnostics)
		}
		events := []protocol.Event{}
		for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
			if line == "" {
				continue
			}
			var e protocol.Event
			if err := json.Unmarshal([]byte(line), &e); err != nil {
				t.Fatal(err)
			}
			events = append(events, e)
		}
		return events
	}
	run(1, "install", "node", "--version", "26.2.0") // nil input cannot authorize mutation
	events := run(0, "--yes", "plugin", "node", "menu")
	if len(events) < 2 || len(events[len(events)-1].Choices) == 0 {
		t.Fatal("missing native menu result")
	}
	run(0, "status", "node")
	if _, err := os.Stat(apps); !os.IsNotExist(err) {
		t.Fatal("menu/status wrote Apps")
	}
	for _, version := range []string{"24.16.0", "26.2.0"} {
		// Legacy-looking executable data must be selectable without execution/adoption.
		write(filepath.Join(apps, "node", version, "bin/node"), []byte("not executed"), 0700)
	}
	run(0, "--yes", "use", "node", "24.16.0")
	run(0, "--yes", "use", "node", "26.2.0")
	current, err := os.Readlink(filepath.Join(apps, "node/current"))
	if err != nil || filepath.Base(current) != "26.2.0" {
		t.Fatal("version not switched", current, err)
	}
	profile, err := os.ReadFile(filepath.Join(user, ".zshrc"))
	if err != nil || !strings.Contains(string(profile), "# existing user setting") || !strings.Contains(string(profile), "node/current/bin") {
		t.Fatal("profile integration failed", err)
	}
	// Shells are used only to validate saved profile syntax/resolution, not by the backend.
	for _, name := range []string{".profile", ".bashrc", ".zshrc"} {
		probe := exec.Command("/bin/bash", "--noprofile", "--norc", "-c", `. "$TEST_PROFILE"; command -v node`)
		probe.Env = []string{"HOME=" + user, "PATH=" + filepath.Dir(localNode) + ":/usr/bin:/bin", "TEST_PROFILE=" + filepath.Join(user, name)}
		out, err := probe.CombinedOutput()
		if err != nil || strings.TrimSpace(string(out)) != filepath.Join(apps, "node/current/bin/node") {
			t.Fatalf("profile %s resolves wrong Node: %s (%v)", name, out, err)
		}
	}
	if external, err := os.ReadFile(localNode); err != nil || string(external) != "external runtime preserved" {
		t.Fatal("external runtime changed", err)
	}
	run(1, "--yes", "remove", "node", "24.16.0") // no native ownership receipt
	run(1, "--yes", "install", "node", "--version", "../../escape")
	run(1, "--yes", "install", "node", "--location", "system") // extra system consent is required
	if _, err := os.Stat(filepath.Join(apps, "node/24.16.0/bin/node")); err != nil {
		t.Fatal("unowned version removed", err)
	}
	if _, err := os.Stat(filepath.Join(root, "unexpected-process")); !os.IsNotExist(err) {
		t.Fatal("native backend delegated to shell/helper")
	}
}
