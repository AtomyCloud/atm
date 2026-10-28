package main

import (
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"testing"

	"github.com/AtomyCloud/atm/internal/plugin"
)

func sourceNodeManifest(t *testing.T) plugin.Manifest {
	t.Helper()
	b, err := os.ReadFile("../../plugins/node/plugin.json")
	if err != nil {
		t.Fatal(err)
	}
	var m plugin.Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestNodeManifest(t *testing.T) {
	m := sourceNodeManifest(t)
	if err := m.Validate("node"); err != nil {
		t.Fatal(err)
	}
	if m.Schema != 2 || m.MinAPI != 2 || m.Executor != "native" || m.Protocol != "jsonl-v1" || m.Version != "0.0.1" || !m.FullSetup {
		t.Fatal("incorrect native Node contract", m)
	}
	if !slices.Equal(m.Platforms, []string{"linux"}) || !reflect.DeepEqual(m.Executables, map[string]string{
		"linux/amd64": "bin/linux-amd64/atm-node", "linux/arm64": "bin/linux-arm64/atm-node",
	}) {
		t.Fatal("Node backend must advertise only Linux amd64/arm64", m)
	}
	ops := []string{"menu", "status", "install", "update", "use", "remove", "uninstall", "integrate"}
	if !slices.Equal(m.Operations, ops) {
		t.Fatal("Node operations differ", m.Operations)
	}
	if len(plugin.NativeTargets) != 5 {
		t.Fatal("CORE targets must remain unchanged")
	}
}

func TestRuntimeExclusions(t *testing.T) {
	for _, path := range []string{
		"plugins/node/native", "plugins/node/native/main.go", "plugins/node/native/main_test.go",
		"plugins/node/bin", "plugins/node/bin/linux-amd64/atm-node", "plugins/node/tests/test_path.py",
		"plugins/node/internal-docs/notes.md", "plugins/node/.codex/agent.toml", "plugins/node/cache.pyc",
		"plugins/node/trace.log", "plugins/node/helper.go",
	} {
		if !excludedRuntimePath(filepath.FromSlash(path)) {
			t.Errorf("runtime leaks %s", path)
		}
	}
	for _, path := range []string{"plugins/node/plugin.json", "plugins/node/plugin.sh", "plugins/node/plugin.metadata", "plugins/node/lang/en-us.lang", "bin/atm"} {
		if excludedRuntimePath(filepath.FromSlash(path)) {
			t.Errorf("required runtime excluded: %s", path)
		}
	}
}

func TestNodePayloadGuards(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Linux executable permission checks require a Unix host")
	}
	m := sourceNodeManifest(t)
	for _, target := range []string{"linux/amd64", "linux/arm64"} {
		t.Run(target, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "plugins", "node", filepath.FromSlash(m.Executables[target]))
			if _, err := nodePayload(m, target, root); err == nil {
				t.Fatal("missing executable accepted")
			}
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				t.Fatal(err)
			}
			write := func(data []byte) {
				t.Helper()
				if err := os.WriteFile(path, data, 0755); err != nil {
					t.Fatal(err)
				}
			}
			write([]byte("#!/bin/sh\nexit 0\n"))
			if _, err := nodePayload(m, target, root); err == nil {
				t.Fatal("shell fallback accepted")
			}
			header := make([]byte, 64)
			copy(header, "\x7fELF\x02\x01")
			machine := uint16(62)
			if target == "linux/arm64" {
				machine = 183
			}
			binary.LittleEndian.PutUint16(header[18:], machine)
			write(header)
			file, err := nodePayload(m, target, root)
			if err != nil || file.name != "plugins/node/"+m.Executables[target] || file.mode != 0755 {
				t.Fatal(file, err)
			}
			binary.LittleEndian.PutUint16(header[18:], 0)
			write(header)
			if _, err := nodePayload(m, target, root); err == nil {
				t.Fatal("wrong architecture accepted")
			}
			if runtime.GOOS != "windows" {
				binary.LittleEndian.PutUint16(header[18:], machine)
				write(header)
				if err := os.Chmod(path, 0644); err != nil {
					t.Fatal(err)
				}
				if _, err := nodePayload(m, target, root); err == nil {
					t.Fatal("non-executable accepted")
				}
			}
		})
	}
}

func TestNodeOnlyRejectsUnsupportedTargets(t *testing.T) {
	for _, target := range [][2]string{{"windows", "amd64"}, {"darwin", "arm64"}, {"linux", "386"}} {
		out := filepath.Join(t.TempDir(), "absent")
		if err := buildNodeOnly(target[0], target[1], out); err == nil {
			t.Fatal("unsupported Node target accepted", target)
		}
		if _, err := os.Stat(out); !os.IsNotExist(err) {
			t.Fatal("unsupported target wrote output")
		}
	}
}

func TestNodePackageTargetCoverage(t *testing.T) {
	m := sourceNodeManifest(t)
	delete(m.Executables, "linux/amd64")
	root := t.TempDir()
	t.Chdir(root)
	if err := os.MkdirAll("plugins/node", 0755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("plugins/node/plugin.json", data, 0644); err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(root, "stage")
	if _, err := buildNode("linux", "amd64", stage); err == nil {
		t.Fatal("missing supported target silently omitted")
	}
	for _, target := range [][2]string{{"windows", "amd64"}, {"darwin", "amd64"}, {"darwin", "arm64"}} {
		files, err := buildNode(target[0], target[1], stage)
		if err != nil || len(files) != 0 {
			t.Fatal("pending Node backend packaged", target, files, err)
		}
	}
	if _, err := os.Stat(stage); !os.IsNotExist(err) {
		t.Fatal("unsupported target created staging files")
	}
}

func TestPublishNode(t *testing.T) {
	out := t.TempDir()
	file := payload{"plugins/node/bin/linux-amd64/atm-node", []byte("first"), 0755}
	if err := publishNode(out, file); err != nil {
		t.Fatal(err)
	}
	file.data = []byte("second")
	if err := publishNode(out, file); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(out, filepath.FromSlash(file.name))
	ignored, err := os.ReadFile(filepath.Join(filepath.Dir(destination), ".gitignore"))
	if err != nil || string(ignored) != "*\n" {
		t.Fatal("generated binary lacks ignore protection", string(ignored), err)
	}
	b, err := os.ReadFile(destination)
	if err != nil || string(b) != "second" {
		t.Fatal(string(b), err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	if err := os.Remove(destination); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(out, "untouched"), destination); err != nil {
		t.Fatal(err)
	}
	if err := publishNode(out, file); err == nil {
		t.Fatal("symlink output accepted")
	}
	if err := os.RemoveAll(filepath.Join(out, "plugins")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(out, "plugins")); err != nil {
		t.Fatal(err)
	}
	if err := publishNode(out, file); err == nil {
		t.Fatal("symlink output parent accepted")
	}
}
