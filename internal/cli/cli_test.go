package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

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
	return home, dir
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
	t.Setenv("PATH", t.TempDir())
	var out, errors bytes.Buffer
	code := Run([]string{"--home", home, "--dry-run", "--json", "install", "node", "--version", "24.16.0"}, os.Stdin, &out, &errors)
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
