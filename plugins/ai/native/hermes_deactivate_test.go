//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type hermesPreservedEntry struct {
	info os.FileInfo
	data []byte
	link string
}

func hermesPreservedTree(t *testing.T, root string) map[string]hermesPreservedEntry {
	t.Helper()
	result := map[string]hermesPreservedEntry{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		i, err := os.Lstat(path)
		if err != nil {
			return err
		}
		e := hermesPreservedEntry{info: i}
		if i.Mode().IsRegular() {
			e.data, err = os.ReadFile(path)
		}
		if i.Mode()&os.ModeSymlink != 0 {
			e.link, err = os.Readlink(path)
		}
		result[path] = e
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func hermesAssertPreserved(t *testing.T, before, after map[string]hermesPreservedEntry) {
	t.Helper()
	if len(before) != len(after) {
		t.Fatal("preserved inventory changed")
	}
	for path, old := range before {
		now, ok := after[path]
		if !ok || !os.SameFile(old.info, now.info) || old.info.Mode() != now.info.Mode() || old.link != now.link || !bytes.Equal(old.data, now.data) {
			t.Fatal("preserved preparation changed", path)
		}
	}
}

func TestHermesDeactivateLifecycle(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		for _, state := range []string{"enrolled", "dormant", "never-enrolled"} {
			t.Run(arch+"/"+state, func(t *testing.T) {
				f := hermesIntegrationFixture(t, arch)
				root, home := f.b.root(hermesAgent), os.Getenv("HOME")
				version := filepath.Join(root, "versions", hermesPMVersion)
				for _, path := range []string{filepath.Join(version, "home", ".hermes", "config.yaml"), filepath.Join(version, "home", ".hermes", "sessions", "private"), filepath.Join(home, ".hermes", "config.yaml"), filepath.Join(home, "foreign-bin", "hermes")} {
					writeFixture(t, path, []byte("private user data\n"), 0600)
				}
				if err := os.Symlink("config.yaml", filepath.Join(version, "home", ".hermes", "private-link")); err != nil {
					t.Fatal(err)
				}
				profiles := map[string]pathState{}
				for _, name := range profileNames {
					s, err := pathSnapshot(filepath.Join(home, name), false)
					if err != nil {
						t.Fatal(err)
					}
					profiles[name] = s
				}
				if state != "never-enrolled" {
					mustRun(t, f.fixture, "integrate", "--provider", hermesAgent)
					if state == "dormant" {
						mustRun(t, f.fixture, "integrate", "--provider", hermesAgent, "--remove-path")
					}
				}
				before := hermesPreservedTree(t, filepath.Join(root, "versions"))
				outside := hermesPreservedTree(t, filepath.Join(home, ".hermes"))
				for i := 0; i < 2; i++ {
					if err := f.b.deactivateHermesAgent(context.Background()); err != nil {
						t.Fatal(err)
					}
					for _, path := range []string{filepath.Join(root, "current.json"), filepath.Join(root, "path.json"), filepath.Join(root, "bin"), root + ".lock", filepath.Join(home, ".atm-ai-path.lock")} {
						integrationMissing(t, path)
					}
					hermesAssertPreserved(t, before, hermesPreservedTree(t, filepath.Join(root, "versions")))
					hermesAssertPreserved(t, outside, hermesPreservedTree(t, filepath.Join(home, ".hermes")))
					for name, old := range profiles {
						now, err := pathSnapshot(filepath.Join(home, name), false)
						if err != nil || !samePathState(now, old) {
							t.Fatal("profile contents changed", name, err)
						}
						if state == "never-enrolled" && !pathUnchanged(now, old) {
							t.Fatal("unenrolled profile rewritten")
						}
					}
				}
				mustRun(t, f.fixture, "use", hermesUseArgs()...)
				integrationMissing(t, filepath.Join(root, "bin"))
				integrationMissing(t, filepath.Join(root, "path.json"))
				mustRun(t, f.fixture, "integrate", "--provider", hermesAgent)
				if err := f.b.hermesStatus(context.Background()); err != nil {
					t.Fatal(err)
				}
				hermesAssertPreserved(t, before, hermesPreservedTree(t, filepath.Join(root, "versions")))
				if f.calls != 0 || len(f.commands) != 0 {
					t.Fatal("deactivation/reactivation executed provider")
				}
			})
		}
	}
}

func TestHermesDeactivateRefusesUncertainPreparation(t *testing.T) {
	for _, kind := range []string{"incomplete", "unselected-incomplete", "foreign-owner", "current", "source", "completion", "bridge", "bridge-unknown", "bridge-symlink", "profile", "provider-lock", "home-lock", "unknown-root", "unselected-bridge"} {
		t.Run(kind, func(t *testing.T) {
			f := hermesIntegrationFixture(t, "amd64")
			root, home := f.b.root(hermesAgent), os.Getenv("HOME")
			mustRun(t, f.fixture, "integrate", "--provider", hermesAgent)
			version := filepath.Join(root, "versions", hermesPMVersion)
			switch kind {
			case "incomplete", "unselected-incomplete":
				if err := os.Remove(filepath.Join(version, "complete.json")); err != nil {
					t.Fatal(err)
				}
				if kind == "unselected-incomplete" {
					if err := os.Remove(filepath.Join(root, "current.json")); err != nil {
						t.Fatal(err)
					}
				}
			case "foreign-owner":
				writeFixture(t, filepath.Join(root, "owner.json"), []byte("{}"), 0600)
			case "current":
				writeFixture(t, filepath.Join(root, "current.json"), []byte("{}"), 0600)
			case "source":
				writeFixture(t, filepath.Join(version, "source", "pm", "cli.py"), []byte("changed"), 0644)
			case "completion":
				writeFixture(t, filepath.Join(version, "complete.json"), []byte("{}"), 0600)
			case "bridge":
				writeFixture(t, filepath.Join(root, "bin", "hermes"), fakeELF("amd64", "changed", false), 0755)
			case "bridge-unknown":
				writeFixture(t, filepath.Join(root, "bin", "extra"), []byte("foreign"), 0600)
			case "bridge-symlink":
				if err := os.Rename(filepath.Join(root, "bin"), filepath.Join(home, "saved-bin")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(home, "saved-bin"), filepath.Join(root, "bin")); err != nil {
					t.Fatal(err)
				}
			case "profile":
				writeFixture(t, filepath.Join(home, ".profile"), []byte("# edited block"), 0640)
			case "provider-lock":
				if err := os.Mkdir(root+".lock", 0700); err != nil {
					t.Fatal(err)
				}
			case "home-lock":
				if err := os.Mkdir(filepath.Join(home, ".atm-ai-path.lock"), 0700); err != nil {
					t.Fatal(err)
				}
			case "unknown-root":
				writeFixture(t, filepath.Join(root, "unknown"), []byte("foreign"), 0600)
			case "unselected-bridge":
				mustRun(t, f.fixture, "integrate", "--provider", hermesAgent, "--remove-path")
				if err := os.Remove(filepath.Join(root, "current.json")); err != nil {
					t.Fatal(err)
				}
			}
			before := hermesPreservedTree(t, root)
			profile := integrationRead(t, filepath.Join(home, ".profile"))
			if err := f.b.deactivateHermesAgent(context.Background()); err == nil {
				t.Fatal("uncertain preparation accepted")
			}
			hermesAssertPreserved(t, before, hermesPreservedTree(t, root))
			if !bytes.Equal(profile, integrationRead(t, filepath.Join(home, ".profile"))) {
				t.Fatal("refusal changed profile")
			}
		})
	}
}

func TestHermesDeactivateInterruptedPhases(t *testing.T) {
	for _, point := range []string{"hermes-deactivate-preflight", "path-preflight", "path-before-publish", "path-published-.profile", "hermes-deactivate-after-detach", "hermes-deactivate-before-retire", "hermes-deactivate-after-retire", "hermes-refresh-before-cleanup-hermes", "hermes-refresh-before-cleanup-launcher.json", "hermes-refresh-before-cleanup-directory", "hermes-deactivate-before-deselect", "hermes-deactivate-after-deselect"} {
		for _, canceled := range []bool{false, true} {
			t.Run(point+map[bool]string{false: "/failure", true: "/cancel"}[canceled], func(t *testing.T) {
				f := hermesIntegrationFixture(t, "amd64")
				mustRun(t, f.fixture, "integrate", "--provider", hermesAgent)
				root, home := f.b.root(hermesAgent), os.Getenv("HOME")
				before := hermesPreservedTree(t, filepath.Join(root, "versions"))
				ctx, cancel := context.WithCancel(context.Background())
				originalProfile := integrationRead(t, filepath.Join(home, ".profile"))
				defer cancel()
				seen := false
				f.b.checkpoint = func(p string) error {
					if p != point {
						return nil
					}
					seen = true
					if canceled {
						cancel()
						return nil
					}
					return errors.New("injected deactivation failure")
				}
				err := f.b.deactivateHermesAgent(ctx)
				if !seen || err == nil || !strings.Contains(err.Error(), "deactivation phase") || !strings.Contains(err.Error(), "retained") {
					t.Fatal(seen, err)
				}
				if canceled && !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				if point == "path-published-.profile" && !bytes.Equal(originalProfile, integrationRead(t, filepath.Join(home, ".profile"))) {
					t.Fatal("guarded detach failure did not restore the original owned profile")
				}
				for _, path := range []string{root + ".lock", filepath.Join(home, ".atm-ai-path.lock"), filepath.Join(root+".lock", "hermes-deactivate.json")} {
					if _, err := os.Lstat(path); err != nil {
						t.Fatal("missing retained evidence", path, err)
					}
				}
				hermesAssertPreserved(t, before, hermesPreservedTree(t, filepath.Join(root, "versions")))
				if point == "hermes-deactivate-after-deselect" {
					integrationMissing(t, filepath.Join(root, "current.json"))
				} else {
					integrationRead(t, filepath.Join(root, "current.json"))
				}
				if point == "hermes-refresh-before-cleanup-launcher.json" {
					integrationMissing(t, filepath.Join(root+".lock", "hermes-retired-bin", "hermes"))
					integrationRead(t, filepath.Join(root+".lock", "hermes-retired-bin", "launcher.json"))
				}
				f.b.checkpoint = nil
				if err := f.b.deactivateHermesAgent(context.Background()); err == nil {
					t.Fatal("interrupted attempt silently repaired")
				}
			})
		}
	}
}

func TestHermesDeactivatePinsOriginalAuthority(t *testing.T) {
	for _, point := range []string{"hermes-deactivate-preflight", "path-preflight", "hermes-deactivate-after-detach", "hermes-deactivate-before-deselect"} {
		for _, kind := range []string{"pointer", "owner", "source", "completion", "coherent-bridge", "coherent-directory", "profile", "private-home", "lock"} {
			if point == "hermes-deactivate-before-deselect" && strings.HasPrefix(kind, "coherent-") {
				continue
			}
			t.Run(point+"/"+kind, func(t *testing.T) {
				f := hermesIntegrationFixture(t, "amd64")
				mustRun(t, f.fixture, "integrate", "--provider", hermesAgent)
				root, home := f.b.root(hermesAgent), os.Getenv("HOME")
				seen := false
				f.b.checkpoint = func(p string) error {
					if p != point {
						return nil
					}
					seen = true
					path := filepath.Join(root, "current.json")
					switch kind {
					case "owner":
						path = filepath.Join(root, "owner.json")
					case "source":
						path = filepath.Join(root, "versions", hermesPMVersion, "source.json")
					case "completion":
						path = filepath.Join(root, "versions", hermesPMVersion, "complete.json")
					case "private-home":
						path = filepath.Join(root, "versions", hermesPMVersion, "home")
					case "lock":
						path = root + ".lock"
					case "profile":
						writeFixture(t, filepath.Join(home, ".profile"), []byte("# late edit"), 0640)
						return nil
					case "coherent-bridge", "coherent-directory":
						bin := filepath.Join(root, "bin")
						if kind == "coherent-directory" {
							if err := os.Rename(bin, filepath.Join(home, "saved-bin")); err != nil {
								t.Fatal(err)
							}
						}
						writeFixture(t, filepath.Join(bin, "hermes"), fakeELF("amd64", "coherent replacement", false), 0755)
						_, hash, err := hermesCompletionRead(context.Background(), bin, filepath.Join(bin, "hermes"), hermesBridgeLimit, true, false)
						if err != nil {
							t.Fatal(err)
						}
						data, _ := json.Marshal(hermesBridgeReceipt{1, "ai", hermesAgent, root, "amd64", hash})
						writeFixture(t, filepath.Join(bin, "launcher.json"), data, 0600)
						if _, err := f.b.inspectHermesBridge(context.Background()); err != nil {
							t.Fatal("fixture must be coherent", err)
						}
						return nil
					}
					if kind == "private-home" || kind == "lock" {
						if err := os.Rename(path, filepath.Join(home, "saved-directory")); err != nil {
							t.Fatal(err)
						}
						if err := os.Mkdir(path, 0700); err != nil {
							t.Fatal(err)
						}
						return nil
					}
					data := integrationRead(t, path)
					if err := os.Rename(path, filepath.Join(home, "saved-authority")); err != nil {
						t.Fatal(err)
					}
					writeFixture(t, path, data, 0600)
					return nil
				}
				err := f.b.deactivateHermesAgent(context.Background())
				if !seen || err == nil || !strings.Contains(err.Error(), "retained") {
					t.Fatal(seen, err)
				}
				if strings.HasPrefix(kind, "coherent-") && !strings.Contains(err.Error(), "original bridge changed") {
					t.Fatal("not original bridge refusal", err)
				}
				integrationRead(t, filepath.Join(root, "current.json"))
			})
		}
	}
}

func TestHermesDeactivateAbsentNoWrites(t *testing.T) {
	f := newHermesAgentFixture(t, "amd64")
	home := os.Getenv("HOME")
	if err := os.MkdirAll(home, 0700); err != nil {
		t.Fatal(err)
	}
	before := hermesPreservedTree(t, home)
	if err := f.b.deactivateHermesAgent(context.Background()); err == nil {
		t.Fatal("absent preparation should clearly refuse")
	}
	hermesAssertPreserved(t, before, hermesPreservedTree(t, home))
	integrationMissing(t, f.b.root(hermesAgent))
	integrationMissing(t, f.b.root(hermesAgent)+".lock")
}

func TestHermesDeactivateOptionsAndMenu(t *testing.T) {
	valid := []string{"--provider", hermesAgent, "--keep-preparation"}
	if _, err := parseOptions("uninstall", valid); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"--provider", hermesAgent},
		{"--keep-preparation"},
		{"--provider", "ollama", "--keep-preparation"},
		{"--provider", hermesAgent, "--keep-preparation=true"},
		append(append([]string{}, valid...), "--keep-preparation"),
		append(append([]string{}, valid...), "--version", hermesPMVersion),
		append(append([]string{}, valid...), "--trust-download"),
		append(append([]string{}, valid...), "--accept-dependency-execution"),
		append(append([]string{}, valid...), "--accept-system-packages"),
		append(append([]string{}, valid...), "--remove-path"),
		append(append([]string{}, valid...), "--refresh-launcher"),
	} {
		if _, err := parseOptions("uninstall", args); err == nil {
			t.Fatal("accepted unsafe deactivation options", args)
		}
	}
	for _, op := range []string{"install", "update", "use", "remove", "integrate", "status", "menu"} {
		if _, err := parseOptions(op, valid); err == nil {
			t.Fatal("accepted deactivation flag for", op)
		}
	}
	f := hermesIntegrationFixture(t, "amd64")
	choices, err := f.b.hermesMenu(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range choices {
		if c.Operation == "uninstall" {
			found = true
			if c.Label != "Deactivate Hermes (keep preparation and data)" || len(c.Arguments) != 3 || c.Arguments[2] != "--keep-preparation" {
				t.Fatal(c)
			}
		}
	}
	if !found {
		t.Fatal("missing explicit deactivation menu")
	}
	mustRun(t, f.fixture, "uninstall", valid...)
	choices, err = f.b.hermesMenu(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range choices {
		if c.Operation == "uninstall" {
			t.Fatal("inactive menu offers deactivation", c)
		}
	}
}

func TestHermesDeactivateNoopAuthority(t *testing.T) {
	for _, kind := range []string{"cancel", "selection", "source", "profile", "coherent-bridge"} {
		t.Run(kind, func(t *testing.T) {
			f := hermesIntegrationFixture(t, "amd64")
			if err := f.b.deactivateHermesAgent(context.Background()); err != nil {
				t.Fatal(err)
			}
			root, home := f.b.root(hermesAgent), os.Getenv("HOME")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			seen := false
			f.b.checkpoint = func(point string) error {
				if point != "hermes-deactivate-before-noop" {
					return nil
				}
				seen = true
				switch kind {
				case "cancel":
					cancel()
				case "selection":
					writeFixture(t, filepath.Join(root, "current.json"), []byte(`{"Version":"0.22.5"}`), 0600)
				case "source":
					writeFixture(t, filepath.Join(root, "versions", hermesPMVersion, "source", "pm", "cli.py"), []byte("changed"), 0644)
				case "profile":
					writeFixture(t, filepath.Join(home, ".zshrc"), []byte("# appeared"), 0600)
				case "coherent-bridge":
					bin := filepath.Join(root, "bin")
					writeFixture(t, filepath.Join(bin, "hermes"), fakeELF("amd64", "appeared", false), 0755)
					_, hash, err := hermesCompletionRead(context.Background(), bin, filepath.Join(bin, "hermes"), hermesBridgeLimit, true, false)
					if err != nil {
						t.Fatal(err)
					}
					data, _ := json.Marshal(hermesBridgeReceipt{1, "ai", hermesAgent, root, "amd64", hash})
					writeFixture(t, filepath.Join(bin, "launcher.json"), data, 0600)
				}
				return nil
			}
			err := f.b.deactivateHermesAgent(ctx)
			if !seen || err == nil || !strings.Contains(err.Error(), "retained") {
				t.Fatal(seen, err)
			}
			if kind == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		})
	}
}

func TestHermesDeactivateRetainsChangedEvidence(t *testing.T) {
	for _, point := range []string{"hermes-deactivate-preflight", "hermes-deactivate-before-deselect", "hermes-deactivate-after-deselect"} {
		for _, kind := range []string{"journal", "provider-unknown", "home-unknown"} {
			t.Run(point+"/"+kind, func(t *testing.T) {
				f := hermesIntegrationFixture(t, "amd64")
				root, home := f.b.root(hermesAgent), os.Getenv("HOME")
				path := filepath.Join(root+".lock", "hermes-deactivate.json")
				if kind == "provider-unknown" {
					path = filepath.Join(root+".lock", "unknown.json")
				}
				if kind == "home-unknown" {
					path = filepath.Join(home, ".atm-ai-path.lock", "unknown.json")
				}
				seen := false
				f.b.checkpoint = func(p string) error {
					if p == point {
						seen = true
						writeFixture(t, path, []byte("foreign evidence"), 0600)
					}
					return nil
				}
				err := f.b.deactivateHermesAgent(context.Background())
				if !seen || err == nil || !strings.Contains(err.Error(), "retained") {
					t.Fatal(seen, err)
				}
				if string(integrationRead(t, path)) != "foreign evidence" {
					t.Fatal("foreign journal removed/changed")
				}
				integrationRead(t, filepath.Join(root+".lock", "hermes-deactivate.json"))
			})
		}
	}
}
