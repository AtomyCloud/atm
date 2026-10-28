package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/AtomyCloud/atm/internal/catalog"
	"github.com/AtomyCloud/atm/internal/plugin"
)

type catalogTransport func(*http.Request) (*http.Response, error)

func (f catalogTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestNativeCatalogCLIWithoutShell(t *testing.T) {
	home, dir := fixture(t)
	t.Setenv("PATH", "")
	t.Setenv("GITHUB_TOKEN", "")
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	calls := 0
	http.DefaultTransport = catalogTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "api.github.com" {
			t.Fatal("unexpected origin")
		}
		body := `{"sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`
		if calls == 2 {
			body = `{"truncated":false,"tree":[]}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})
	var out, errors bytes.Buffer
	if code := Run([]string{"--home", home, "--json", "plugins", "search", "brave"}, os.Stdin, &out, &errors); code != 0 {
		t.Fatal(code, &errors)
	}
	var snapshot catalog.Snapshot
	if err := json.Unmarshal(out.Bytes(), &snapshot); err != nil || snapshot.Source != catalog.Source || len(snapshot.Entries) != 0 || calls != 2 {
		t.Fatal(snapshot, calls, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatal("read-only search wrote config")
	}
}

func fixture(t *testing.T) (string, string) {
	t.Helper()
	home, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, key := range []string{"ATM_HOME", "ATM_LANG", "ATM_PLUGIN_SORT_ORDER", "ATM_CONFIG_FILE", "ATM_PLUGINS_CONFIG_FILE", "ATM_DRY_RUN", "ATM_PLUGIN_DIR", "ATM_APPS_DIR"} {
		t.Setenv(key, "")
	}
	t.Setenv("ATM_CONFIG_DIR", dir)
	t.Setenv("ATM_STATE_DIR", dir)
	return home, dir
}

func TestNativeCatalogDryRunAndOutput(t *testing.T) {
	home, dir := fixture(t)
	t.Setenv("PATH", "")
	// This invalid token would fail before any request if dry-run entered the client.
	t.Setenv("GITHUB_TOKEN", "invalid\ntoken")
	var out, errors bytes.Buffer
	if code := Run([]string{"--home", home, "--dry-run", "--json", "plugins", "search", "brave"}, os.Stdin, &out, &errors); code != 0 {
		t.Fatal(code, &errors)
	}
	var plan map[string]any
	if err := json.Unmarshal(out.Bytes(), &plan); err != nil || plan["executor"] != "native-core" || plan["query"] != "brave" || plan["dry_run"] != true {
		t.Fatal(plan, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatal("search dry-run wrote config")
	}
	snapshot := catalog.Snapshot{Source: catalog.Source, Commit: strings.Repeat("a", 40), Entries: []catalog.Entry{{Manifest: plugin.Manifest{ID: "brave", Name: "Brave", Version: "0.0.1", Executor: "legacy-bash"}, Local: true}}}
	out.Reset()
	if err := printCatalog(&out, snapshot, false); err != nil || !strings.Contains(out.String(), "Brave") || !strings.Contains(out.String(), "true") {
		t.Fatal(out.String(), err)
	}
	out.Reset()
	if err := printCatalog(&out, snapshot, true); err != nil {
		t.Fatal(err)
	}
	var got catalog.Snapshot
	if err := json.Unmarshal(out.Bytes(), &got); err != nil || got.Commit != snapshot.Commit || len(got.Entries) != 1 {
		t.Fatal(got, err)
	}
	out.Reset()
	if err := printCatalog(&out, catalog.Snapshot{Source: catalog.Source}, false); err != nil || !strings.Contains(out.String(), "No plugins found") {
		t.Fatal(err)
	}
}

func TestNativeDownloadConfirmationAndDryRun(t *testing.T) {
	home, dir := fixture(t)
	plugins := filepath.Join(t.TempDir(), "plugins")
	t.Setenv("ATM_PLUGIN_DIR", plugins)
	t.Setenv("PATH", "")
	t.Setenv("GITHUB_TOKEN", "invalid\ntoken")
	for _, tc := range []struct {
		args     []string
		ok       bool
		contains string
	}{
		{[]string{"--dry-run", "--json", "plugins", "download", "alpha"}, true, "native-core"},
		{[]string{"--json", "plugins", "download", "alpha"}, false, "--yes"},
		{[]string{"--dry-run", "plugins", "download", "../bad"}, false, "invalid plugin ID"},
		{[]string{"--dry-run", "plugins", "download"}, false, "usage:"},
		{[]string{"--dry-run", "plugins", "download", "alpha", "extra"}, false, "usage:"},
	} {
		var out, stderr bytes.Buffer
		code := Run(append([]string{"--home", home}, tc.args...), os.Stdin, &out, &stderr)
		if (code == 0) != tc.ok || !strings.Contains(out.String()+stderr.String(), tc.contains) {
			t.Fatal(tc, code, out.String(), stderr.String())
		}
	}
	if _, err := os.Stat(plugins); !os.IsNotExist(err) {
		t.Fatal("unconfirmed/dry-run created plugin root")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatal("unconfirmed/dry-run wrote preferences")
	}
}

func TestVersionAndDiscoveryWithoutShell(t *testing.T) {
	home, dir := fixture(t)
	t.Setenv("PATH", t.TempDir())
	for _, args := range [][]string{{"--version"}, {"--home", home, "--json", "plugins", "list"}, {"--home", home, "doctor"}, {"--home", home, "lang", "list"}} {
		var out, errors bytes.Buffer
		if code := Run(args, os.Stdin, &out, &errors); code != 0 {
			t.Fatalf("%v: %d %s", args, code, &errors)
		}
		if out.Len() == 0 {
			t.Fatal("empty output")
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatal("read-only command wrote config")
	}
}

func TestDryRunPlanAndPreferences(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux plan")
	}
	home, dir := fixture(t)
	plugins := t.TempDir()
	path := filepath.Join(plugins, "fixture")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	manifest, err := json.Marshal(plugin.Manifest{Schema: 1, ID: "fixture", Name: "Fixture", Version: "0.0.1", MinAPI: 1,
		Platforms: []string{"linux"}, Executor: "legacy-bash", Protocol: "tty-v1", Operations: []string{"install"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "plugin.json"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ATM_PLUGIN_DIR", plugins)
	t.Setenv("PATH", t.TempDir())
	var out, errors bytes.Buffer
	code := Run([]string{"--home", home, "--dry-run", "--json", "install", "fixture", "--version", "1.26.0"}, os.Stdin, &out, &errors)
	if code != 0 {
		t.Fatal(code, &errors)
	}
	var plan map[string]any
	if err := json.Unmarshal(out.Bytes(), &plan); err != nil || plan["dry_run"] != true {
		t.Fatal(plan, err)
	}
	for _, args := range [][]string{{"plugins", "sort", "za"}, {"lang", "set", "pt-br"}} {
		out.Reset()
		errors.Reset()
		if code := Run(append([]string{"--home", home, "--dry-run"}, args...), os.Stdin, &out, &errors); code != 0 {
			t.Fatal(&errors)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatal("dry-run wrote config")
	}
}

func TestNonTTYAndInvalidCommands(t *testing.T) {
	home, _ := fixture(t)
	for _, args := range [][]string{{"tui"}, {"updates", "core"}, {"self-update", "--git"}, {"plugins", "list", "bad"}, {"lang", "set", "../bad"}} {
		var out, errors bytes.Buffer
		if code := Run(append([]string{"--home", home}, args...), os.Stdin, &out, &errors); code == 0 {
			t.Fatal("accepted", args)
		}
		if strings.TrimSpace(errors.String()) == "" {
			t.Fatal("missing error")
		}
	}
}

func TestNativeDryRunAndConfirmationWithoutExecution(t *testing.T) {
	home, configDir := fixture(t)
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "native_fixture")
	if err := os.MkdirAll(filepath.Join(pluginDir, "bin"), 0700); err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile("../../tests/fixtures/native_fixture/plugin.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(pluginDir, "plugin.json"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	name := "fixture"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	// Discovery may inspect this file, but dry-run and confirmation must not launch it.
	if err = os.WriteFile(filepath.Join(pluginDir, "bin", name), []byte("not a binary"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ATM_PLUGIN_DIR", dir)
	t.Setenv("PATH", "")
	var out, errors bytes.Buffer
	if code := Run([]string{"--home", home, "--dry-run", "install", "native_fixture", "literal;argument"}, os.Stdin, &out, &errors); code != 0 {
		t.Fatal(code, &errors)
	}
	if !strings.Contains(out.String(), "install") || !strings.Contains(out.String(), "literal;argument") {
		t.Fatal("incomplete human plan", &out)
	}
	out.Reset()
	errors.Reset()
	if code := Run([]string{"--home", home, "--json", "install", "native_fixture"}, os.Stdin, &out, &errors); code == 0 || !strings.Contains(errors.String(), "--yes") {
		t.Fatal("missing confirmation guard", code, &errors)
	}
	entries, err := os.ReadDir(configDir)
	if err != nil || len(entries) != 0 {
		t.Fatal("dry-run wrote preferences", err)
	}
}
