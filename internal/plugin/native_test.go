package plugin

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/AtomyCloud/atm/internal/config"
	"github.com/AtomyCloud/atm/internal/data"
)

func nativeSample() Manifest {
	return Manifest{Schema: 2, ID: "fixture", Name: "Fixture", Version: "0.0.1", MinAPI: 2, Platforms: []string{"linux", "windows"}, Executor: "native", Protocol: "jsonl-v1", Operations: []string{"status", "install"}, Executables: map[string]string{"linux/amd64": "bin/fixture", "linux/arm64": "bin/fixture", "windows/amd64": "bin/fixture.exe"}}
}

func TestNativeManifestPathsAndTargets(t *testing.T) {
	if err := nativeSample().Validate("fixture"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"../escape", "/absolute", "bin/../exe", "bin\\exe", "C:/exe", "bin/x:stream", "bin/NUL.exe", "bin/COM1", "bin/a.", "bin/a ", "bin/*", "bin/\x1bfile"} {
		m := nativeSample()
		m.Executables["linux/amd64"] = path
		if m.Validate("fixture") == nil {
			t.Fatal("unsafe path accepted", path)
		}
	}
	for _, change := range []func(*Manifest){
		func(m *Manifest) { m.MinAPI = 1 }, func(m *Manifest) { m.Schema = 1 }, func(m *Manifest) { m.Protocol = "tty-v1" },
		func(m *Manifest) { m.Executables["windows/amd64"] = "bin/a.cmd" }, func(m *Manifest) { m.Executables["linux/386"] = "bin/fixture" },
		func(m *Manifest) { m.Operations = []string{"menu"} }, func(m *Manifest) { m.Operations = []string{"install", "install"} },
		func(m *Manifest) { m.Operations = nil }, func(m *Manifest) { delete(m.Executables, "windows/amd64") },
	} {
		m := nativeSample()
		change(&m)
		if m.Validate("fixture") == nil {
			t.Fatal("invalid native manifest accepted", m)
		}
	}
}

func TestNativeDiscoveryAndExecutableChecks(t *testing.T) {
	dir := t.TempDir()
	pdir := filepath.Join(dir, "fixture")
	m := nativeSample()
	if err := data.WriteJSON(filepath.Join(pdir, "plugin.json"), m); err != nil {
		t.Fatal(err)
	}
	c := config.Config{PluginsDir: dir}
	p, err := Discover(c, runtime.GOOS)
	if err != nil || len(p) != 1 || p[0].Available {
		t.Fatal("missing binary advertised", p, err)
	}
	if err := os.Mkdir(filepath.Join(pdir, "bin"), 0700); err != nil {
		t.Fatal(err)
	}
	name := "fixture"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(pdir, "bin", name)
	if err := os.WriteFile(path, []byte("not executable data"), 0700); err != nil {
		t.Fatal(err)
	}
	p, err = Discover(c, runtime.GOOS)
	if err != nil || !p[0].Available {
		t.Fatal("discovery must not launch executable", p, err)
	}
	if err := CheckBinary(path, runtime.GOOS+"/"+runtime.GOARCH); err == nil {
		t.Fatal("nonbinary accepted")
	}
	if _, err := p[0].NativeExecutable("darwin/arm64"); err == nil {
		t.Fatal("unsupported target accepted")
	}
	if runtime.GOOS == "linux" {
		if err := os.Chmod(path, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := p[0].NativeExecutable("linux/amd64"); err == nil {
			t.Fatal("nonexecutable accepted")
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("/bin/sh", path); err != nil {
			t.Fatal(err)
		}
		if _, err := p[0].NativeExecutable("linux/amd64"); err == nil {
			t.Fatal("symlink accepted")
		}
	}
}
