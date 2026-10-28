package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/AtomyCloud/atm/internal/plugin"
)

func nativeFixture(t *testing.T, id string) plugin.Manifest {
	t.Helper()
	m := plugin.Manifest{Schema: 2, MinAPI: 2, ID: id, Name: id, Version: "0.0.1",
		Executor: "native", Protocol: "jsonl-v1", Platforms: []string{"linux"},
		Operations: []string{"status"}, Executables: map[string]string{
			"linux/amd64": "bin/linux-amd64/atm-" + id,
			"linux/arm64": "bin/linux-arm64/atm-" + id,
		}}
	writeFixture(t, "plugins/"+id+"/native/main.go", "package main\nfunc main() { panic(\"backend must never execute during build\") }\n")
	writeManifest(t, m)
	return m
}

func writeFixture(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
}

func writeManifest(t *testing.T, m plugin.Manifest) {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, "plugins/"+m.ID+"/plugin.json", string(b))
}

func TestNativeDiscoveryAndRuntimeExclusions(t *testing.T) {
	t.Chdir(t.TempDir())
	nativeFixture(t, "future_one")
	nativeFixture(t, "future_two")
	legacy := plugin.Manifest{Schema: 1, MinAPI: 1, ID: "legacy", Name: "Legacy", Version: "0.0.1",
		Executor: "legacy-bash", Protocol: "tty-v1", Platforms: []string{"linux"}, Operations: []string{"status"}}
	writeManifest(t, legacy)
	writeFixture(t, "plugins/metadata-only/plugin.metadata", "legacy metadata")
	manifests, err := discoverNative()
	if err != nil || len(manifests) != 2 || manifests[0].ID != "future_one" || manifests[1].ID != "future_two" {
		t.Fatal(manifests, err)
	}
	ids := map[string]bool{}
	for _, m := range manifests {
		ids[m.ID] = true
	}
	for _, path := range []string{"plugins/future_two/native", "plugins/future_two/native/fixture.json", "plugins/future_one/bin/stale", "plugins/future_two/tests/data.json"} {
		if !excludedRuntimePath(path, ids) {
			t.Fatal("native development payload leaked", path)
		}
	}
	for _, path := range []string{"plugins/legacy/bin/helper", "plugins/legacy/native/data.json", "plugins/future_one/plugin.sh", "plugins/future_one/plugin.json", "plugins/future_two/lang/en-us.lang"} {
		if excludedRuntimePath(path, ids) {
			t.Fatal("runtime payload lost", path)
		}
	}
}

func TestUnactivatedPreviewRetainsLegacyManifest(t *testing.T) {
	t.Chdir(t.TempDir())
	preview := nativeFixture(t, "preview")
	data, err := json.Marshal(preview)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, "plugins/preview/tests/native-preview.json", string(data))
	legacy := plugin.Manifest{Schema: 1, MinAPI: 1, ID: "preview", Name: "Preview", Version: "0.0.1",
		Executor: "legacy-bash", Protocol: "tty-v1", Platforms: []string{"linux"}, Operations: []string{"menu", "status"}}
	writeManifest(t, legacy)
	manifests, err := discoverNative()
	if err != nil || len(manifests) != 0 {
		t.Fatalf("unactivated preview entered the native build: %v %v", manifests, err)
	}
	if excludedRuntimePath("plugins/preview/plugin.json", nil) {
		t.Fatal("production legacy manifest was excluded")
	}
	for _, path := range []string{"plugins/preview/native/main.go", "plugins/preview/tests/native-preview.json"} {
		if !excludedRuntimePath(path, nil) {
			t.Fatalf("development-only file entered runtime package: %s", path)
		}
	}
}

func TestNativeDiscoveryRefusals(t *testing.T) {
	for _, scenario := range []string{"missing-source", "only-test-source", "traversal", "source-output", "duplicate-output", "hidden-output", "checksum-output", "id-mismatch", "bad-target", "source-link", "manifest-link", "plugin-link"} {
		t.Run(scenario, func(t *testing.T) {
			t.Chdir(t.TempDir())
			m := nativeFixture(t, "future")
			switch scenario {
			case "missing-source", "only-test-source":
				if err := os.Remove("plugins/future/native/main.go"); err != nil {
					t.Fatal(err)
				}
				if scenario == "only-test-source" {
					writeFixture(t, "plugins/future/native/main_test.go", "package main")
				}
			case "traversal":
				m.Executables["linux/amd64"] = "bin/../../escape"
			case "source-output":
				m.Executables["linux/amd64"] = "native/main.go"
			case "duplicate-output":
				m.Executables["linux/amd64"] = m.Executables["linux/arm64"]
			case "hidden-output":
				m.Executables["linux/amd64"] = "bin/.gitignore"
			case "checksum-output":
				m.Executables["linux/amd64"] += ".sha256"
			case "id-mismatch":
				writeFixture(t, "plugins/future/plugin.json", `{"schema":2,"id":"other"}`)
			case "bad-target":
				m.Executables["linux/386"] = "bin/386/atm"
			case "source-link", "manifest-link", "plugin-link":
				if runtime.GOOS == "windows" {
					t.Skip("symlink privileges differ on Windows")
				}
				path := "plugins/future/native/main.go"
				if scenario == "manifest-link" {
					path = "plugins/future/plugin.json"
				} else if scenario == "plugin-link" {
					path = "plugins/future"
				}
				if err := os.Rename(path, path+"-saved"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Base(path)+"-saved", path); err != nil {
					t.Fatal(err)
				}
			}
			if !strings.HasSuffix(scenario, "-link") && scenario != "id-mismatch" {
				writeManifest(t, m)
			}
			if _, err := discoverNative(); err == nil {
				t.Fatal("unsafe native declaration accepted")
			}
			// Missing sources cannot be hidden by building an unsupported host.
			if scenario == "missing-source" {
				if err := buildPluginsOnly("darwin", "arm64", "out"); err == nil {
					t.Fatal("missing source ignored on undeclared target")
				}
				if _, err := os.Stat("out"); !os.IsNotExist(err) {
					t.Fatal("failed preflight wrote outputs")
				}
			}
		})
	}
}

func TestGenericSourceBuild(t *testing.T) {
	t.Chdir(t.TempDir())
	writeFixture(t, "go.mod", "module fixture\ngo 1.26.0\n")
	m := nativeFixture(t, "future")
	writeFixture(t, "plugins/future/"+m.Executables["linux/amd64"], "stale checkout executable")
	if err := buildPluginsOnly("darwin", "arm64", "unavailable"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat("unavailable"); !os.IsNotExist(err) {
		t.Fatal("unavailable backend wrote output")
	}
	for _, arch := range []string{"amd64", "arm64"} {
		if err := buildPluginsOnly("linux", arch, "out"); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join("out", "plugins", m.ID, filepath.FromSlash(m.Executables["linux/"+arch]))
		if err := plugin.CheckBinary(path, "linux/"+arch); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		digest, err := os.ReadFile(path + ".sha256")
		if err != nil || string(digest) != fmt.Sprintf("%x  %s\n", sha256.Sum256(data), filepath.Base(path)) {
			t.Fatal("incorrect native checksum", string(digest), err)
		}
	}
}

func TestNativePublishGuards(t *testing.T) {
	for _, name := range []string{"../escape", "plugins/future/../../escape", "plugins/future/plugin.json", "/absolute", "plugins/future/bin/../source"} {
		if err := publishNative(t.TempDir(), payload{name, nil, 0755}); err == nil {
			t.Fatal("unsafe output accepted", name)
		}
	}
	for _, scenario := range []string{"existing-rules", "ignore-link", "root-link"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			out := filepath.Join(root, "out")
			name := "plugins/future/bin/target/backend"
			ignore := filepath.Join(out, "plugins/future/bin/target/.gitignore")
			if scenario == "existing-rules" {
				writeFixture(t, ignore, "developer-rule\n")
			} else {
				if runtime.GOOS == "windows" {
					t.Skip("symlink privileges differ on Windows")
				}
				link := out
				if scenario == "ignore-link" {
					link = ignore
				}
				if err := os.MkdirAll(filepath.Dir(link), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(t.TempDir(), link); err != nil {
					t.Fatal(err)
				}
			}
			if err := publishNative(out, payload{name, []byte("binary"), 0755}); err == nil {
				t.Fatal("unsafe output accepted")
			}
			if scenario == "existing-rules" {
				data, _ := os.ReadFile(ignore)
				if string(data) != "developer-rule\n" {
					t.Fatal("developer ignore rules changed")
				}
			}
		})
	}
}
