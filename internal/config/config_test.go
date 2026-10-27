package config

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func isolated(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, key := range []string{"ATM_CONFIG_FILE", "ATM_PLUGINS_CONFIG_FILE", "ATM_LANG", "ATM_APPS_DIR", "ATM_PLUGIN_SORT_ORDER", "ATM_DRY_RUN", "ATM_STATE_DIR"} {
		t.Setenv(key, "")
	}
	t.Setenv("ATM_CONFIG_DIR", dir)
	return dir
}

func TestLoadIsReadOnlyAndSave(t *testing.T) {
	dir := isolated(t)
	c, err := Load(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatal("load wrote files")
	}
	p := c.Preferences
	p.Sort = "za"
	p.Language = "pt-br"
	if err := c.Save(p); err != nil {
		t.Fatal(err)
	}
	again, err := Load(c.Home, false)
	if err != nil {
		t.Fatal(err)
	}
	if again.Sort != "za" || again.Language != "pt-br" {
		t.Fatal(again)
	}
	t.Setenv("ATM_LANG", "de")
	again, err = Load(c.Home, false)
	if err != nil || again.Language != "de" {
		t.Fatal(again, err)
	}
}

func TestDryRunNoWrite(t *testing.T) {
	dir := isolated(t)
	c, err := Load(t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Save(c.Preferences); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatal("dry-run wrote preferences")
	}
}

func TestStaleHomeIgnored(t *testing.T) {
	dir := isolated(t)
	os.WriteFile(filepath.Join(dir, "atm.conf"), []byte("ATM_HOME=\"/old/Downloads/atm\"\nATM_LANG=\"de\"\n"), 0600)
	home := t.TempDir()
	c, err := Load(home, false)
	if err != nil || c.Home != home || c.Language != "de" {
		t.Fatal(c, err)
	}
}

func TestInvalidPreferences(t *testing.T) {
	for _, p := range []Preferences{{Schema: 2, Language: "en-us", Sort: "az"}, {Schema: 1, Language: "../bad", Sort: "az"}, {Schema: 1, Language: "en-us", Sort: "shell"}} {
		if p.Validate() == nil {
			t.Fatal("accepted", p)
		}
	}
}

func TestStateDirectoryPreservesLegacyAndEnvironment(t *testing.T) {
	dir := isolated(t)
	legacy := filepath.Join(t.TempDir(), "state")
	if err := os.WriteFile(filepath.Join(dir, "atm.conf"), []byte("ATM_STATE_DIR="+strconv.Quote(legacy)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(t.TempDir(), false)
	if err != nil || c.StateDir != legacy {
		t.Fatal(c.StateDir, err)
	}
	env := filepath.Join(t.TempDir(), "override")
	t.Setenv("ATM_STATE_DIR", env)
	c, err = Load(c.Home, false)
	if err != nil || c.StateDir != env {
		t.Fatal(c.StateDir, err)
	}
	if _, err := os.Stat(env); !os.IsNotExist(err) {
		t.Fatal("read-only configuration created state")
	}
	t.Setenv("ATM_STATE_DIR", "relative")
	if _, err := Load(c.Home, false); err == nil {
		t.Fatal("relative state accepted")
	}
}
