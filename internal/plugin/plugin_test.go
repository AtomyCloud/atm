package plugin

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/AtomyCloud/atm/internal/config"
	"github.com/AtomyCloud/atm/internal/data"
)

func sample(id string) Manifest {
	return Manifest{Schema: 1, ID: id, Name: id, Version: "0.0.1", Order: 1, MinAPI: 1, Platforms: []string{"linux"}, Executor: "legacy-bash", Protocol: "tty-v1", Operations: []string{"menu", "install"}}
}

func TestRepositoryManifests(t *testing.T) {
	home := filepath.Join("..", "..")
	c := config.Config{Home: home, PluginsDir: filepath.Join(home, "plugins"), Sort: "default"}
	plugins, err := Discover(c, "linux")
	if err != nil {
		t.Fatal(err)
	}
	if len(plugins) != 16 {
		t.Fatalf("want 16, got %d", len(plugins))
	}
	for _, p := range plugins {
		legacy, err := readLegacy(p.Directory)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(p.Manifest, legacy) {
			t.Errorf("metadata drift: %s", p.ID)
		}
		if p.LegacyMetadata {
			t.Errorf("missing JSON manifest: %s", p.ID)
		}
	}
	windows, err := Discover(c, "windows")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range windows {
		if p.Available || p.Reason == "" {
			t.Fatal("advertised Windows compatibility", p.ID)
		}
	}
}

func TestDiscoveryDoesNotExecute(t *testing.T) {
	dir := t.TempDir()
	pdir := filepath.Join(dir, "example")
	if err := data.WriteJSON(filepath.Join(pdir, "plugin.json"), sample("example")); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(pdir, "plugin.sh"), []byte("exit 99\n"), 0600)
	os.WriteFile(filepath.Join(pdir, "plugin.metadata"), []byte("exit 99\n"), 0600)
	plugins, err := Discover(config.Config{PluginsDir: dir}, "linux")
	if err != nil || len(plugins) != 1 {
		t.Fatal(plugins, err)
	}
	plugins, err = Discover(config.Config{PluginsDir: dir, Disabled: map[string]bool{"ATM_PLUGIN_EXAMPLE_ENABLED": true}}, "linux")
	if err != nil || plugins[0].Available {
		t.Fatal("disabled plugin available", err)
	}
}

func TestInvalidManifestNeverFallsBack(t *testing.T) {
	dir := t.TempDir()
	pdir := filepath.Join(dir, "example")
	os.MkdirAll(pdir, 0700)
	os.WriteFile(filepath.Join(pdir, "plugin.json"), []byte("{}"), 0600)
	os.WriteFile(filepath.Join(pdir, "plugin.metadata"), []byte("ATM_PLUGIN_ID=example\n"), 0600)
	if _, err := Discover(config.Config{PluginsDir: dir}, "linux"); err == nil {
		t.Fatal("invalid JSON accepted")
	}
}

func TestSortAndContract(t *testing.T) {
	p := []Plugin{{Manifest: sample("z")}, {Manifest: sample("a")}}
	Sort(p, "az")
	if p[0].ID != "a" {
		t.Fatal(p)
	}
	Sort(p, "za")
	if p[0].ID != "z" {
		t.Fatal(p)
	}
	for _, change := range []func(*Manifest){
		func(m *Manifest) { m.ID = "../escape" }, func(m *Manifest) { m.Name = "\x1b[31m" },
		func(m *Manifest) { m.MinAPI = 99 }, func(m *Manifest) { m.Platforms = []string{"windows"} },
		func(m *Manifest) { m.Operations = []string{"menu", "menu"} }, func(m *Manifest) { m.Executor = "shell-command" },
	} {
		m := sample("example")
		change(&m)
		if m.Validate("example") == nil {
			t.Fatal("accepted", m)
		}
	}
}
