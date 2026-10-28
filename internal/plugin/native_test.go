package plugin

import (
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/AtomyCloud/atm/internal/config"
	"github.com/AtomyCloud/atm/internal/data"
)

func TestOptionalNativeIntegrationContract(t *testing.T) {
	m := nativeSample()
	if err := m.Validate(m.ID); err != nil {
		t.Fatal("integrate must remain optional", err)
	}
	m.Operations = []string{"integrate"}
	if err := m.Validate(m.ID); err != nil {
		t.Fatal("native integrate rejected", err)
	}
	m.Operations = []string{"integrate", "integrate"}
	if m.Validate(m.ID) == nil {
		t.Fatal("duplicate integrate accepted")
	}
	m = Manifest{Schema: 1, MinAPI: 1, ID: "fixture", Name: "Fixture", Version: "0.0.1", Platforms: []string{"linux"}, Executor: "legacy-bash", Protocol: "tty-v1", Operations: []string{"integrate"}}
	if m.Validate(m.ID) == nil {
		t.Fatal("legacy integrate accepted")
	}
	b, err := os.ReadFile("../../docs/schema/plugin-v2.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Properties struct {
			Operations struct {
				Items struct {
					Enum []string `json:"enum"`
				} `json:"items"`
			} `json:"operations"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(b, &schema); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(schema.Properties.Operations.Items.Enum, NativeActions) {
		t.Fatal("schema and runtime native actions differ")
	}
}

func nativeSample() Manifest {
	return Manifest{Schema: 2, ID: "fixture", Name: "Fixture", Version: "0.0.1", MinAPI: 2, Platforms: []string{"linux", "windows", "darwin"}, Executor: "native", Protocol: "jsonl-v1", Operations: []string{"status", "install"}, Executables: map[string]string{"linux/amd64": "bin/fixture", "linux/arm64": "bin/fixture", "windows/amd64": "bin/fixture.exe", "darwin/amd64": "bin/fixture", "darwin/arm64": "bin/fixture"}}
}

func TestMachOArchitectureValidation(t *testing.T) {
	for _, target := range []struct {
		name string
		cpu  uint32
	}{{"darwin/amd64", 0x01000007}, {"darwin/arm64", 0x0100000c}} {
		h := make([]byte, 64)
		binary.LittleEndian.PutUint32(h, 0xfeedfacf)
		binary.LittleEndian.PutUint32(h[4:], target.cpu)
		binary.LittleEndian.PutUint32(h[12:], 2)
		path := filepath.Join(t.TempDir(), "native")
		if err := os.WriteFile(path, h, 0700); err != nil {
			t.Fatal(err)
		}
		if err := CheckBinary(path, target.name); err != nil {
			t.Fatal(err)
		}
		other := "darwin/arm64"
		if target.name == other {
			other = "darwin/amd64"
		}
		if CheckBinary(path, other) == nil {
			t.Fatal("wrong Mach-O architecture accepted")
		}
		binary.LittleEndian.PutUint32(h[12:], 6)
		if err := os.WriteFile(path, h, 0700); err != nil {
			t.Fatal(err)
		}
		if CheckBinary(path, target.name) == nil {
			t.Fatal("Mach-O library accepted as executable")
		}
	}
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
		func(m *Manifest) { m.Operations = []string{"shell"} }, func(m *Manifest) { m.Operations = []string{"install", "install"} },
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
	if _, err := p[0].NativeExecutable("freebsd/arm64"); err == nil {
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
