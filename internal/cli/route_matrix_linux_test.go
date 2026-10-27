package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/AtomyCloud/atm/internal/core"
)

// Native executable traps detect attempted delegation even if a caller ignores
// subprocess errors. They do not require Bash or Python to run the test itself.
func TestMain(m *testing.M) {
	if marker := os.Getenv("ATM_TEST_ADMIN_PROCESS_MARKER"); marker != "" {
		switch filepath.Base(os.Args[0]) {
		case "bash", "sh", "python", "python3", "git", "curl", "wget", "sudo", "atm", "atm-legacy.sh":
			if err := os.WriteFile(marker, []byte(strings.Join(os.Args, "\n")), 0600); err != nil {
				os.Exit(87)
			}
			os.Exit(86)
		}
	}
	os.Exit(m.Run())
}

func administrativeProcessTraps(t *testing.T, root, home string) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "process-traps")
	for _, path := range []string{dir, filepath.Join(home, "bin"), filepath.Join(home, "libexec")} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	paths := []string{filepath.Join(home, "bin", "atm"), filepath.Join(home, "libexec", "atm-legacy.sh")}
	for _, name := range []string{"bash", "sh", "python", "python3", "git", "curl", "wget", "sudo"} {
		paths = append(paths, filepath.Join(dir, name))
	}
	for _, path := range paths {
		if err := os.Symlink(exe, path); err != nil {
			t.Fatal(err)
		}
	}
	marker := filepath.Join(root, "unexpected-process")
	t.Setenv("ATM_TEST_ADMIN_PROCESS_MARKER", marker)
	// Prove the trap is live before relying on the absence of its marker.
	cmd := exec.Command(filepath.Join(dir, "bash"), "--version")
	if err := cmd.Run(); err == nil || cmd.ProcessState.ExitCode() != 86 {
		t.Fatalf("process trap did not fire: %v", err)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLinuxAdministrativeRouteMatrix(t *testing.T) {
	root, home := recoveryCLIFixture(t)
	home = filepath.Join(home, "ATM space \u00e7")
	if err := os.MkdirAll(filepath.Join(home, "lang"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"core-api.json":   `{"schema":1,"plugin_api":1,"update_protocol":1}`,
		"lang/en-us.lang": "ATM_TEST=fixture\n",
	} {
		if err := os.WriteFile(filepath.Join(home, filepath.FromSlash(name)), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("ATM_HOME", home)
	t.Setenv("ATM_APPS_DIR", filepath.Join(root, "user", "Apps"))
	t.Setenv("GITHUB_TOKEN", "")
	traps := administrativeProcessTraps(t, root, home)
	before := recoveryCLISnapshot(t, root)
	var network atomic.Int32
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	http.DefaultTransport = catalogTransport(func(*http.Request) (*http.Response, error) {
		network.Add(1)
		return nil, fmt.Errorf("unexpected network request")
	})
	unchanged := func(t *testing.T) {
		t.Helper()
		if network.Load() != 0 {
			t.Fatal("administrative plan/refusal/offline read attempted network")
		}
		if !reflect.DeepEqual(before, recoveryCLISnapshot(t, root)) {
			t.Fatal("changed runtime, HOME, profiles, apps, plugins, inputs, state or temporary files; or launched a process trap")
		}
	}
	invoke := func(t *testing.T, args []string, wantError string) string {
		t.Helper()
		var out, stderr bytes.Buffer
		code := Run(append([]string{"--home", home}, args...), nil, &out, &stderr)
		if wantError == "" {
			if code != 0 || out.Len() == 0 || stderr.Len() != 0 {
				t.Fatalf("%v: exit=%d out=%s err=%s", args, code, &out, &stderr)
			}
		} else if code == 0 || !strings.Contains(stderr.String(), wantError) {
			t.Fatalf("%v: expected %q; exit=%d out=%s err=%s", args, wantError, code, &out, &stderr)
		}
		unchanged(t)
		return out.String()
	}
	// Shared CLI/TUI dispatch inventory, including aliases and CLI-only signed
	// package inspection/enrollment. Missing inputs must never be read by a plan.
	routes := []struct {
		command, canonical string
		mutating           bool
	}{
		{"updates check", "updates check", false},
		{"plugins check-updates", "updates check", false},
		{"self-update --check", "updates check", false},
		{"updates check-core", "updates check-core", false},
		{"updates changes", "updates changes", false},
		{"updates backups", "updates backups", false},
		{"updates plugin fixture", "updates plugin fixture", true},
		{"plugins update fixture", "updates plugin fixture", true},
		{"updates all", "updates all", true},
		{"plugins update --all", "updates all", true},
		{"updates restore backup-1", "updates restore backup-1", true},
		{"updates recover plugins", "updates recover plugins", true},
		{"updates verify fixture " + strings.Repeat("a", 40), "updates verify fixture " + strings.Repeat("a", 40), true},
		{"updates verify plugin:fixture " + strings.Repeat("a", 40), "updates verify plugin:fixture " + strings.Repeat("a", 40), true},
		{"updates core", "updates core", true},
		{"self-update", "updates core", true},
		{"updates core missing.json missing.sig missing.archive", "updates core missing.json missing.sig missing.archive", true},
		{"updates enroll-core missing.json missing.sig missing.archive", "updates enroll-core missing.json missing.sig missing.archive", true},
		{"updates trust", "updates trust", false},
		{"updates verify-package missing.json missing.sig missing.archive", "updates verify-package missing.json missing.sig missing.archive", false},
		{"path show", "path show", false},
		{"path apply", "path apply", true},
		{"path apply --plugins", "path apply --plugins", true},
		{"setup portable", "setup portable", true},
		{"setup portable --manifest-sha256 " + strings.Repeat("a", 64), "setup portable --manifest-sha256 " + strings.Repeat("a", 64), true},
		{"install", "install", true},
	}
	for _, mode := range []string{"no-shell", "process-traps"} {
		t.Run(mode, func(t *testing.T) {
			path := ""
			if mode == "process-traps" {
				path = traps
			}
			t.Setenv("PATH", path)
			for _, route := range routes {
				t.Run(route.command, func(t *testing.T) {
					args := strings.Fields(route.command)
					if !core.IsCommand(args) {
						t.Fatal("route no longer uses shared native dispatch")
					}
					out := invoke(t, append([]string{"--dry-run", "--yes", "--json"}, args...), "")
					var plan core.Plan
					if err := json.Unmarshal([]byte(out), &plan); err != nil || plan.Executor != "native-core" || !plan.DryRun || plan.Mutating != route.mutating || strings.Join(plan.Arguments, " ") != route.canonical {
						t.Fatalf("incorrect plan: %s (%v)", out, err)
					}
					if route.mutating {
						for _, prefix := range [][]string{nil, {"--json"}} {
							invoke(t, append(prefix, args...), "require --yes")
						}
					}
				})
			}
			for _, command := range []string{
				"doctor", "plugins list", "plugins sort", "lang list", "lang current", "language current",
				"updates trust", "updates check-core", "updates changes", "updates backups", "path show",
			} {
				t.Run("offline/"+command, func(t *testing.T) {
					invoke(t, append([]string{"--json"}, strings.Fields(command)...), "")
				})
			}
			for _, command := range []string{"plugins search fixture", "plugins download fixture", "plugins sort za", "lang set en-us"} {
				invoke(t, append([]string{"--dry-run", "--yes", "--json"}, strings.Fields(command)...), "")
			}
			invoke(t, []string{"--json", "plugins", "download", "fixture"}, "requires --yes")
			missing := filepath.Join(root, "missing-installation")
			invoke(t, []string{"--dry-run", "--yes", "recover-core", "--root", missing}, "")
			invoke(t, []string{"recover-core", "--root", missing}, "requires --yes")
			// Legacy Git/system setup are intentionally not native operations.
			for _, command := range []string{
				"self-update --git", "self-update --tarball", "setup system",
				"updates recover fixture", "updates recover core", "updates recover ../bad",
				"updates restore ../bad", "updates plugin ../bad", "updates check extra",
				"plugins check-updates extra", "plugins update", "plugins update fixture extra",
				"path apply --system", "setup portable --manifest-sha256 invalid",
			} {
				t.Run("rejected/"+command, func(t *testing.T) {
					invoke(t, append([]string{"--dry-run", "--yes"}, strings.Fields(command)...), "unsupported native CORE operation")
				})
			}
		})
	}
}
