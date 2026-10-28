//go:build linux

package main

import (
	"context"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStaleDestinationRecheckedUnderLock(t *testing.T) {
	b := testBackend(t)
	other := filepath.Join(b.home, "other REA")
	data, _ := json.Marshal(destination{1, "rea", other})
	write(t, b.state, data, 0600)
	called := false
	if e := b.locked(func() error { called = true; return nil }); e == nil || called {
		t.Fatal("stale destination acquired", e, called)
	}
	now, _ := os.ReadFile(b.state)
	if string(now) != string(data) {
		t.Fatal("state overwritten")
	}
	absent(t, b.root)
}
func TestNoEmptyPATHComponents(t *testing.T) {
	t.Setenv("PATH", ":relative:.:/absolute::")
	for _, e := range processEnv("/tmp/fixture", "/tools/node", "/tools/ps") {
		if strings.HasPrefix(e, "PATH=") {
			p := strings.TrimPrefix(e, "PATH=")
			if p != "/tools:/tools:/absolute" {
				t.Fatal(p)
			}
		}
	}
	t.Setenv("PATH", "")
	for _, e := range processEnv("/tmp/fixture", "/tools/node", "/tools/ps") {
		if e == "PATH=/tools:/tools:" {
			t.Fatal("trailing current-directory lookup")
		}
	}
}
func TestInvalidCandidateFallbackWithoutExecutingIt(t *testing.T) {
	for _, kind := range []string{"non-elf", "npm-script", "missing-npm", "bad-npm-version"} {
		t.Run(kind, func(t *testing.T) {
			b := testBackend(t)
			dir := toolchain(t, b)
			fallback := filepath.Join(b.req.AppsDir, "node/current/bin")
			copyFixture(t, compiledTool, filepath.Join(fallback, "node"))
			write(t, filepath.Join(fallback, "npm-cli.js"), []byte("fixture"), 0600)
			os.Symlink("npm-cli.js", filepath.Join(fallback, "npm"))
			switch kind {
			case "non-elf":
				write(t, filepath.Join(dir, "node"), []byte("#!/bin/sh\nexit 99\n"), 0700)
			case "npm-script":
				os.Remove(filepath.Join(dir, "npm"))
				write(t, filepath.Join(dir, "npm"), []byte("#!/bin/sh\nexit 99\n"), 0700)
			case "missing-npm":
				os.Remove(filepath.Join(dir, "npm"))
			case "bad-npm-version":
				base := b.run
				b.run = func(ctx context.Context, path string, args, env []string, work string, timeout time.Duration) (string, error) {
					if path == filepath.Join(dir, "node") && len(args) == 2 && args[1] == "--version" {
						return "invalid", nil
					}
					return base(ctx, path, args, env, work, timeout)
				}
			}
			d, e := b.dependencies(context.Background(), t.TempDir())
			if e != nil || d.Node != filepath.Join(fallback, "node") {
				t.Fatal(d, e)
			}
		})
	}
}
func TestMissingNativeLauncherAndLegacyCustomDetection(t *testing.T) {
	b := testBackend(t)
	toolchain(t, b)
	fixtureRegistry(t, b, "6.8.0")
	if e := action(b, "install", "--trust-dependencies"); e != nil {
		t.Fatal(e)
	}
	os.Remove(filepath.Join(b.root, "bin/rea"))
	if e := action(b, "status"); e == nil {
		t.Fatal("missing launcher reported installed")
	}
	for _, override := range []bool{false, true} {
		t.Run(map[bool]string{false: "default", true: "override"}[override], func(t *testing.T) {
			b := testBackend(t)
			custom := filepath.Join(b.home, "old custom REA")
			entry := filepath.Join(custom, "bin/rea")
			write(t, entry, []byte("manual"), 0700)
			state := filepath.Join(filepath.Dir(b.state), "install.json")
			if override {
				state = filepath.Join(b.home, "legacy override.json")
				t.Setenv("ATM_REA_STATE_FILE", state)
			}
			data, _ := json.Marshal(map[string]any{"schema": 1, "root": custom})
			write(t, state, data, 0600)
			var logs []string
			b.log = func(s string) { logs = append(logs, s) }
			if e := action(b, "status"); e != nil {
				t.Fatal(e)
			}
			if !strings.Contains(strings.Join(logs, "\n"), entry) {
				t.Fatal(logs)
			}
			if e := action(b, "uninstall"); e == nil {
				t.Fatal("legacy adopted")
			}
			got, _ := os.ReadFile(entry)
			if string(got) != "manual" {
				t.Fatal("legacy changed")
			}
		})
	}
}
func TestShrinkwrapUnsupportedAndIncompleteGraphs(t *testing.T) {
	b := testBackend(t)
	fixture := fixtureRegistry(t, b, "6.8.0")
	a := fixture.metadata["6.8.0"]
	for _, lock := range []string{`{"lockfileVersion":1,"dependencies":{"x":{"version":"git+https://evil/x"}}}`, `{"lockfileVersion":3,"packages":{}}`, `{"lockfileVersion":3,"packages":{"":{"dependencies":{"missing":"1.0.0"}}}}`, `{"lockfileVersion":3,"packages":{"":{},"node_modules/x":{"version":"file:../x"}}}`, `{"lockfileVersion":3,"packages":{"":{},"node_modules/x":{"version":"https://evil/x.tgz"}}}`} {
		p := filepath.Join(t.TempDir(), "archive")
		write(t, p, tarball(t, a.Version, map[string][]byte{"package/npm-shrinkwrap.json": []byte(lock)}), 0600)
		if e := inspectArchive(context.Background(), p, a); e == nil {
			t.Fatal("unsafe lock accepted", lock)
		}
	}
	p := filepath.Join(t.TempDir(), "archive")
	write(t, p, tarball(t, a.Version, map[string][]byte{"package/npm-shrinkwrap.json": nil}), 0600)
	if e := inspectArchive(context.Background(), p, a); e != nil {
		t.Fatal("ordinary package without shrinkwrap refused", e)
	}
}
func TestSameVersionEngineIsRecheckedByStagedNPM(t *testing.T) {
	b := testBackend(t)
	tools := toolchain(t, b)
	control(t, tools, "", "27.0.0")
	f := fixtureRegistry(t, b, "6.8.0")
	pkg, _ := json.Marshal(map[string]any{"name": "rea-agents", "version": "6.8.0", "bin": map[string]string{"rea": "scripts/rea.mjs", "rea-agents": "scripts/rea.mjs"}, "engines": map[string]string{"node": ">=27.0.0"}})
	blob := tarball(t, "6.8.0", map[string][]byte{"package/package.json": pkg})
	sum := sha512.Sum512(blob)
	a := f.metadata["6.8.0"]
	a.Engine = ">=27.0.0"
	a.Integrity = "sha512-" + base64.StdEncoding.EncodeToString(sum[:])
	f.archives["6.8.0"] = blob
	f.metadata["6.8.0"] = a
	if e := action(b, "install", "--trust-dependencies"); e != nil {
		t.Fatal(e)
	}
	before, _ := os.ReadFile(filepath.Join(b.root, "versions/6.8.0", dependencyFile))
	control(t, tools, "npm-error", "26.12.1")
	if e := action(b, "install", "--trust-dependencies"); e == nil {
		t.Fatal("same-version npm failure bypassed")
	}
	after, _ := os.ReadFile(filepath.Join(b.root, "versions/6.8.0", dependencyFile))
	if string(before) != string(after) {
		t.Fatal("runtime refreshed despite engine failure")
	}
}
func TestStatusRefusesForeignMarkerAndLauncher(t *testing.T) {
	for _, kind := range []string{"marker", "regular", "symlink", "version-bin-symlink"} {
		t.Run(kind, func(t *testing.T) {
			b := testBackend(t)
			toolchain(t, b)
			fixtureRegistry(t, b, "6.8.0")
			if e := action(b, "install", "--trust-dependencies"); e != nil {
				t.Fatal(e)
			}
			if kind == "marker" {
				write(t, filepath.Join(b.root, marker), []byte("foreign"), 0600)
			} else if kind == "version-bin-symlink" {
				bin := filepath.Join(b.root, "versions/6.8.0/bin")
				if e := os.Rename(bin, bin+"-original"); e != nil {
					t.Fatal(e)
				}
				external := filepath.Join(t.TempDir(), "external-bin")
				copyFixture(t, compiledTool, filepath.Join(external, "rea"))
				if e := os.Symlink(external, bin); e != nil {
					t.Fatal(e)
				}
			} else {
				os.Remove(filepath.Join(b.root, "bin/rea"))
				if kind == "regular" {
					write(t, filepath.Join(b.root, "bin/rea"), []byte("foreign executable"), 0700)
				} else {
					os.Symlink(compiledTool, filepath.Join(b.root, "bin/rea"))
				}
			}
			if e := action(b, "status"); e == nil {
				t.Fatal("foreign native status accepted")
			}
		})
	}
}
func TestTLSInstallWithoutShrinkwrap(t *testing.T) {
	b := testBackend(t)
	toolchain(t, b)
	f := fixtureRegistry(t, b, "6.8.0")
	blob := tarball(t, "6.8.0", map[string][]byte{"package/npm-shrinkwrap.json": nil})
	sum := sha512.Sum512(blob)
	a := f.metadata["6.8.0"]
	a.Integrity = "sha512-" + base64.StdEncoding.EncodeToString(sum[:])
	f.metadata["6.8.0"] = a
	f.archives["6.8.0"] = blob
	if e := action(b, "install", "--trust-dependencies"); e != nil {
		t.Fatal(e)
	}
}
