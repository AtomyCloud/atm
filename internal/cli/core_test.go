package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCoreAdministrativeDryRunsWithoutShellOrNetwork(t *testing.T) {
	home, dir := fixture(t)
	t.Setenv("PATH", "")
	t.Setenv("GITHUB_TOKEN", "invalid\ntoken")
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	http.DefaultTransport = catalogTransport(func(*http.Request) (*http.Response, error) { t.Fatal("dry-run used network"); return nil, nil })
	for _, args := range [][]string{{"updates", "check"}, {"updates", "all"}, {"updates", "plugin", "node"}, {"updates", "backups"}, {"updates", "changes"}, {"updates", "restore", "backup-id"}, {"updates", "recover", "plugins"}, {"updates", "core"}, {"path", "apply"}, {"path", "show"}, {"setup", "portable"}, {"install"}, {"plugins", "check-updates"}, {"plugins", "update", "--all"}, {"self-update", "--check"}} {
		var out, stderr bytes.Buffer
		code := Run(append([]string{"--home", home, "--dry-run", "--json"}, args...), os.Stdin, &out, &stderr)
		var p map[string]any
		if code != 0 || json.Unmarshal(out.Bytes(), &p) != nil || p["executor"] != "native-core" || p["dry_run"] != true {
			t.Fatal(args, code, out.String(), stderr.String())
		}
		if strings.Contains(out.String(), `"bin/atm"`) {
			t.Fatal("legacy CORE route", out.String())
		}
	}
	files, _ := os.ReadDir(dir)
	if len(files) != 0 {
		t.Fatal("dry-run wrote preferences")
	}
}

func TestCoreChecksAndBackupListingWithoutShell(t *testing.T) {
	home, dir := fixture(t)
	t.Setenv("PATH", "")
	t.Setenv("GITHUB_TOKEN", "")
	plugins := t.TempDir()
	t.Setenv("ATM_PLUGIN_DIR", plugins)
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	calls := 0
	http.DefaultTransport = catalogTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		body := `{"sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`
		if strings.Contains(r.URL.Path, "/git/trees/") {
			body = `{"truncated":false,"tree":[]}`
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	for _, args := range [][]string{{"updates", "check"}, {"plugins", "check-updates"}, {"self-update", "--check"}, {"updates", "backups"}, {"updates", "changes"}} {
		var out, stderr bytes.Buffer
		if code := Run(append([]string{"--home", home, "--json"}, args...), os.Stdin, &out, &stderr); code != 0 {
			t.Fatal(args, code, stderr.String())
		}
		if !json.Valid(out.Bytes()) {
			t.Fatal("invalid output", out.String())
		}
		if args[len(args)-1] == "check" && !strings.Contains(out.String(), `"component": "core"`) {
			t.Fatal(out.String())
		}
	}
	if calls != 6 {
		t.Fatal("unexpected network count", calls)
	}
	files, _ := os.ReadDir(dir)
	if len(files) != 0 {
		t.Fatal("inspection wrote state")
	}
}

func TestCoreConsentAndTrustBoundaries(t *testing.T) {
	home, dir := fixture(t)
	t.Setenv("PATH", "")
	for _, args := range [][]string{{"updates", "all"}, {"updates", "restore", "backup-id"}, {"updates", "verify", "node", strings.Repeat("a", 40)}, {"path", "apply"}, {"setup", "portable"}} {
		var out, stderr bytes.Buffer
		if code := Run(append([]string{"--home", home, "--json"}, args...), os.Stdin, &out, &stderr); code == 0 || !strings.Contains(stderr.String(), "--yes") {
			t.Fatal(args, code, stderr.String())
		}
	}
	var out, stderr bytes.Buffer
	if code := Run([]string{"--home", home, "--yes", "updates", "core"}, os.Stdin, &out, &stderr); code == 0 || !strings.Contains(stderr.String(), "Ed25519") {
		t.Fatal(code, stderr.String())
	}
	files, _ := os.ReadDir(dir)
	if len(files) != 0 {
		t.Fatal("unconfirmed operation wrote state")
	}
}

func TestAdministrativeRecoveryBypassesBrokenPluginDiscovery(t *testing.T) {
	home, _ := fixture(t)
	dir := t.TempDir()
	t.Setenv("ATM_PLUGIN_DIR", dir)
	if err := os.Mkdir(filepath.Join(dir, "broken"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken", "plugin.json"), []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if code := Run([]string{"--home", home, "--json", "updates", "backups"}, os.Stdin, &out, &stderr); code != 0 {
		t.Fatal(code, stderr.String())
	}
}
