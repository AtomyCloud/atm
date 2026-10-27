package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/AtomyCloud/atm/internal/coreupdate"
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
	if code := Run([]string{"--home", home, "--yes", "updates", "core"}, os.Stdin, &out, &stderr); code == 0 || !strings.Contains(stderr.String(), "full-package") {
		t.Fatal(code, stderr.String())
	}
	files, _ := os.ReadDir(dir)
	if len(files) != 0 {
		t.Fatal("unconfirmed operation wrote state")
	}
}

func TestReleaseTrustAndPackageDryRunAreOffline(t *testing.T) {
	_, _ = fixture(t)
	root := t.TempDir()
	home := filepath.Join(root, "runtime")
	for _, name := range []string{"runtime", "state", "config", "apps", "inputs"} {
		if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name, "sentinel"), []byte("unchanged"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(home, "core-api.json"), []byte(`{"schema":1,"plugin_api":1,"update_protocol":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ATM_CONFIG_DIR", filepath.Join(root, "config"))
	t.Setenv("ATM_STATE_DIR", filepath.Join(root, "state"))
	t.Setenv("ATM_APPS_DIR", filepath.Join(root, "apps"))
	snapshot := func() map[string]string {
		files := map[string]string{}
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			var data []byte
			if !entry.IsDir() {
				data, err = os.ReadFile(path)
				if err != nil {
					return err
				}
			}
			files[path] = fmt.Sprintf("%s:%s:%x", info.Mode(), info.ModTime().UTC(), sha256.Sum256(data))
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return files
	}
	before := snapshot()
	t.Setenv("PATH", "")
	t.Setenv("GITHUB_TOKEN", "invalid\ntoken")
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	http.DefaultTransport = catalogTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("offline trust inspection attempted network")
		return nil, nil
	})
	var out, stderr bytes.Buffer
	if code := Run([]string{"--home", home, "--json", "updates", "trust"}, os.Stdin, &out, &stderr); code != 0 {
		t.Fatal(code, stderr.String())
	}
	var trust struct {
		Value coreupdate.TrustIdentity `json:"result"`
	}
	if err := json.Unmarshal(out.Bytes(), &trust); err != nil || trust.Value.Fingerprint != coreupdate.OfficialKeyFingerprint || trust.Value.ReplacementEnabled {
		t.Fatal(out.String(), err)
	}
	out.Reset()
	stderr.Reset()
	if code := Run([]string{"--home", home, "--dry-run", "updates", "verify-package", "absent.json", "absent.sig", "absent.tar.gz"}, os.Stdin, &out, &stderr); code != 0 {
		t.Fatal(code, stderr.String())
	}
	if !strings.Contains(out.String(), `"mutating": false`) {
		t.Fatal(out.String())
	}
	out.Reset()
	stderr.Reset()
	if code := Run([]string{"--home", home, "updates", "verify-package", "absent.json", "absent.sig", "absent.tar.gz"}, os.Stdin, &out, &stderr); code == 0 || strings.Contains(stderr.String(), "--yes") {
		t.Fatal("verification should read inputs but not require mutation consent", code, stderr.String())
	}
	if !reflect.DeepEqual(before, snapshot()) {
		t.Fatal("trust commands modified runtime, state, settings, apps or input files")
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
