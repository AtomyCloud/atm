//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func hermesIntegrationFixture(t *testing.T, arch string) *hermesAgentFixture {
	t.Helper()
	f := newHermesSelectionFixture(t, arch)
	sourceDir := t.TempDir()
	if err := os.Chmod(sourceDir, 0700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(sourceDir, "atm-ai")
	// Exercise the large-payload boundary without executing a provider binary.
	data := append(fakeELF(arch, "native fixture", false), make([]byte, 2<<20)...)
	writeFixture(t, source, data, 0755)
	f.b.hermes.selfPath = func() (string, error) { return source, nil }
	writeFixture(t, filepath.Join(os.Getenv("HOME"), ".profile"), []byte("# no newline"), 0640)
	writeFixture(t, filepath.Join(os.Getenv("HOME"), ".bashrc"), []byte("# bash\n"), 0600)
	return f
}

func TestHermesIntegrationLifecycle(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		t.Run(arch, func(t *testing.T) {
			f := hermesIntegrationFixture(t, arch)
			root, home := f.b.root(hermesAgent), os.Getenv("HOME")
			writeFixture(t, filepath.Join(home, ".zshrc"), []byte("# zsh\n"), 0644)
			original := map[string][]byte{}
			for _, name := range profileNames {
				original[name] = integrationRead(t, filepath.Join(home, name))
			}
			pointer, err := pathSnapshot(filepath.Join(root, "current.json"), false)
			if err != nil {
				t.Fatal(err)
			}
			f.b.checkpoint = func(point string) error {
				if point == "path-before-publish" {
					if i, err := os.Stat(filepath.Join(home, ".atm-ai-path.lock", "journal.json")); err == nil && i.Size() > maxProfile {
						t.Fatal("binary entered profile journal")
					}
				}
				return nil
			}
			// Provider-less integration is the generic CORE PATH apply route.
			mustRun(t, f.fixture, "integrate")
			bridge, err := f.b.inspectHermesBridge(context.Background())
			if err != nil || bridge == nil || bridge.payload.hash.Size <= maxProfile {
				t.Fatal(bridge, err)
			}
			// Enrollment and detach never silently refresh a previously owned copy.
			f.b.hermes.selfPath = func() (string, error) {
				t.Fatal("reenrollment attempted to refresh the native bridge")
				return "", errors.New("unexpected self copy")
			}
			for _, args := range [][]string{{"--provider", hermesAgent}, {"--provider", hermesAgent, "--remove-path"}, {"--provider", hermesAgent, "--remove-path"}, {"--provider", hermesAgent}} {
				mustRun(t, f.fixture, "integrate", args...)
				now, err := f.b.inspectHermesBridge(context.Background())
				if err != nil || !sameHermesBridge(bridge, now) {
					t.Fatal("bridge replaced during reenrollment/detach", err)
				}
				if len(args) == 3 {
					for _, name := range profileNames {
						if !bytes.Equal(original[name], integrationRead(t, filepath.Join(home, name))) {
							t.Fatal("detach changed original profile", name)
						}
					}
					integrationMissing(t, filepath.Join(root, "path.json"))
				}
			}
			mustRun(t, f.fixture, "use", hermesUseArgs()...)
			if err := f.b.hermesStatus(context.Background()); err != nil {
				t.Fatal(err)
			}
			choices, err := f.b.hermesMenu(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			found := map[string]bool{}
			for _, c := range choices {
				found[c.ID] = true
			}
			if !found["integrate"] || !found["remove-path"] || !found["use"] {
				t.Fatal(choices)
			}
			now, err := pathSnapshot(filepath.Join(root, "current.json"), false)
			if err != nil || !pathUnchanged(now, pointer) {
				t.Fatal("PATH changed selection", err)
			}
			integrationMissing(t, filepath.Join(home, ".atm-ai-path.lock"))
			integrationMissing(t, root+".lock")
		})
	}
}

func TestHermesIntegrationPreservesForeignState(t *testing.T) {
	for _, kind := range []string{"profile", "bin", "symlink", "hardlink", "binary", "receipt", "missing-receipt", "home-lock", "provider-lock"} {
		t.Run(kind, func(t *testing.T) {
			f := hermesIntegrationFixture(t, "amd64")
			root, home := f.b.root(hermesAgent), os.Getenv("HOME")
			if kind != "bin" {
				mustRun(t, f.fixture, "integrate", "--provider", hermesAgent)
			}
			binary := filepath.Join(root, "bin", "hermes")
			switch kind {
			case "profile":
				writeFixture(t, filepath.Join(home, ".profile"), []byte("# manual edit\n"), 0640)
			case "bin":
				writeFixture(t, binary, []byte("foreign binary"), 0755)
			case "symlink":
				if err := os.Remove(binary); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("../versions/"+hermesPMVersion+"/source/.hermes/bin/hermes", binary); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(binary, filepath.Join(home, "saved")); err != nil {
					t.Fatal(err)
				}
			case "binary":
				writeFixture(t, binary, fakeELF("amd64", "edited", false), 0755)
			case "receipt":
				writeFixture(t, filepath.Join(root, "bin", "launcher.json"), []byte("{}"), 0600)
			case "missing-receipt":
				if err := os.Remove(filepath.Join(root, "bin", "launcher.json")); err != nil {
					t.Fatal(err)
				}
			case "home-lock":
				if err := os.Mkdir(filepath.Join(home, ".atm-ai-path.lock"), 0700); err != nil {
					t.Fatal(err)
				}
			case "provider-lock":
				if err := os.Mkdir(root+".lock", 0700); err != nil {
					t.Fatal(err)
				}
			}
			before := integrationRead(t, filepath.Join(home, ".profile"))
			for _, args := range [][]string{{"--provider", hermesAgent}, {"--provider", hermesAgent, "--remove-path"}} {
				if err := f.run(context.Background(), "integrate", args...); err == nil {
					t.Fatal("foreign state accepted")
				}
			}
			if !bytes.Equal(before, integrationRead(t, filepath.Join(home, ".profile"))) {
				t.Fatal("foreign state overwritten")
			}
		})
	}
}

func TestHermesIntegrationLateChanges(t *testing.T) {
	for _, remove := range []bool{false, true} {
		for _, kind := range []string{"source", "pointer", "profile", "binary", "coherent-bridge", "cancel", "published-profile"} {
			t.Run(kind+map[bool]string{true: "-detach", false: "-enroll"}[remove], func(t *testing.T) {
				f := hermesIntegrationFixture(t, "amd64")
				root, home := f.b.root(hermesAgent), os.Getenv("HOME")
				if remove {
					mustRun(t, f.fixture, "integrate", "--provider", hermesAgent)
				}
				profile := filepath.Join(home, ".profile")
				before := integrationRead(t, profile)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				seen := false
				f.b.checkpoint = func(point string) error {
					wanted := "path-before-publish"
					if kind == "published-profile" {
						wanted = "path-published-.profile"
					}
					if point != wanted {
						return nil
					}
					seen = true
					switch kind {
					case "source":
						writeFixture(t, filepath.Join(root, "versions", hermesPMVersion, "source", "pm", "cli.py"), []byte("edited"), 0644)
					case "pointer":
						writeFixture(t, filepath.Join(root, "current.json"), []byte("{}"), 0600)
					case "profile", "published-profile":
						writeFixture(t, profile, []byte("# late manual edit"), 0640)
					case "binary", "coherent-bridge":
						bin := filepath.Join(root, "bin")
						writeFixture(t, filepath.Join(bin, "hermes"), fakeELF("amd64", "coherent edit", false), 0755)
						if kind == "coherent-bridge" {
							var rec hermesBridgeReceipt
							if err := readJSON(filepath.Join(bin, "launcher.json"), &rec); err != nil {
								t.Fatal(err)
							}
							_, hash, err := hermesCompletionRead(context.Background(), bin, filepath.Join(bin, "hermes"), hermesBridgeLimit, true, false)
							if err != nil {
								t.Fatal(err)
							}
							rec.Executable = hash
							data, _ := json.Marshal(rec)
							writeFixture(t, filepath.Join(bin, "launcher.json"), data, 0600)
						}
					case "cancel":
						cancel()
					}
					if kind == "published-profile" {
						return errors.New("late failure")
					}
					return nil
				}
				args := []string{"--provider", hermesAgent}
				if remove {
					args = append(args, "--remove-path")
				}
				err := f.run(ctx, "integrate", args...)
				if err == nil || !seen || !strings.Contains(err.Error(), "retained") {
					t.Fatal(seen, err)
				}
				if kind == "cancel" && !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				if kind != "profile" && kind != "published-profile" && !bytes.Equal(before, integrationRead(t, profile)) {
					t.Fatal("profile changed on refusal")
				}
				if kind == "profile" || kind == "published-profile" {
					if string(integrationRead(t, profile)) != "# late manual edit" {
						t.Fatal("late edit overwritten")
					}
				}
				integrationRead(t, filepath.Join(home, ".atm-ai-path.lock", "hermes-bridge.json"))
				if kind == "published-profile" {
					data := integrationRead(t, filepath.Join(home, ".atm-ai-path.lock", "journal.json"))
					if len(data) > maxProfile {
						t.Fatal("binary entered retained journal")
					}
				}
				if _, err := os.Lstat(filepath.Join(root, "bin", "hermes")); err != nil {
					t.Fatal("native copy removed after failure", err)
				}
			})
		}
	}
}

func TestHermesIntegrationPinsBridgeBeforePreflight(t *testing.T) {
	for _, state := range []string{"absent", "dormant", "enrolled"} {
		for _, remove := range []bool{false, true} {
			kinds := []string{"coherent-rewrite", "directory-replacement"}
			if state == "absent" {
				kinds = []string{"creation"}
			}
			for _, kind := range kinds {
				name := state + "/" + map[bool]string{false: "enroll", true: "detach"}[remove] + "/" + kind
				t.Run(name, func(t *testing.T) {
					f := hermesIntegrationFixture(t, "amd64")
					root, home := f.b.root(hermesAgent), os.Getenv("HOME")
					if state != "absent" {
						mustRun(t, f.fixture, "integrate", "--provider", hermesAgent)
						if state == "dormant" {
							mustRun(t, f.fixture, "integrate", "--provider", hermesAgent, "--remove-path")
						}
					}
					before := map[string]pathState{}
					for _, path := range []string{filepath.Join(home, ".profile"), filepath.Join(home, ".bashrc"), filepath.Join(root, "path.json"), filepath.Join(root, "current.json")} {
						snapshot, err := pathSnapshot(path, false)
						if err != nil {
							t.Fatal(err)
						}
						before[path] = snapshot
					}
					originalBridge, err := f.b.inspectHermesBridge(context.Background())
					if err != nil {
						t.Fatal(err)
					}
					var changed *hermesBridgeState
					f.b.checkpoint = func(point string) error {
						if point != "path-preflight" {
							return nil
						}
						bin := filepath.Join(root, "bin")
						body := fakeELF("amd64", "preflight coherent replacement", false)
						if kind == "directory-replacement" {
							// Identical bytes with new identities must also be rejected.
							body = integrationRead(t, filepath.Join(bin, "hermes"))
							if err := os.Rename(bin, filepath.Join(home, "saved-bridge")); err != nil {
								t.Fatal(err)
							}
						}
						writeFixture(t, filepath.Join(bin, "hermes"), body, 0755)
						_, hash, err := hermesCompletionRead(context.Background(), bin, filepath.Join(bin, "hermes"), hermesBridgeLimit, true, false)
						if err != nil {
							t.Fatal(err)
						}
						data, err := json.Marshal(hermesBridgeReceipt{1, "ai", hermesAgent, root, "amd64", hash})
						if err != nil {
							t.Fatal(err)
						}
						writeFixture(t, filepath.Join(bin, "launcher.json"), data, 0600)
						changed, err = f.b.inspectHermesBridge(context.Background())
						if err != nil || changed == nil {
							t.Fatal("replacement must remain internally coherent", err)
						}
						return nil
					}
					args := []string{"--provider", hermesAgent}
					if remove {
						args = append(args, "--remove-path")
					}
					err = f.run(context.Background(), "integrate", args...)
					if err == nil || changed == nil || !strings.Contains(err.Error(), "retained") {
						t.Fatal("preflight bridge mutation accepted without retained evidence", err)
					}
					var evidence struct {
						Schema        int
						Root          string
						Remove        bool
						Selection     pathState
						Bridge        *hermesCompletionFile
						BridgeReceipt pathState
					}
					if err := readJSON(filepath.Join(home, ".atm-ai-path.lock", "hermes-bridge.json"), &evidence); err != nil {
						t.Fatal(err)
					}
					if (evidence.Bridge == nil) != (state == "absent") || evidence.BridgeReceipt.Exists != (state != "absent") {
						t.Fatal("original bridge presence not retained", evidence)
					}
					if originalBridge != nil && (*evidence.Bridge != originalBridge.payload.hash || !samePathState(evidence.BridgeReceipt, originalBridge.receipt)) {
						t.Fatal("recovery evidence adopted intervening bridge")
					}
					for path, original := range before {
						now, err := pathSnapshot(path, false)
						if err != nil || !pathUnchanged(now, original) {
							t.Fatal("preflight mutation changed profile/enrollment/selection", path, err)
						}
					}
					now, err := f.b.inspectHermesBridge(context.Background())
					if err != nil || !sameHermesBridge(changed, now) {
						t.Fatal("intervening bridge evidence not preserved", err)
					}
				})
			}
		}
	}
}

func TestHermesIntegrationBridgeStaging(t *testing.T) {
	for _, kind := range []string{"source", "stage", "stage-coherent", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			f := hermesIntegrationFixture(t, "amd64")
			root := f.b.root(hermesAgent)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f.b.checkpoint = func(point string) error {
				if point != "hermes-bridge-before-publish" {
					return nil
				}
				matches, err := filepath.Glob(filepath.Join(root+".lock", ".hermes-bridge-*"))
				if err != nil || len(matches) != 1 {
					t.Fatal(matches, err)
				}
				if kind == "cancel" {
					cancel()
					return nil
				}
				path := filepath.Join(matches[0], "hermes")
				if kind == "source" {
					path, _ = f.b.hermes.selfPath()
				}
				writeFixture(t, path, fakeELF("amd64", "replaced", false), 0755)
				if kind == "stage-coherent" {
					var rec hermesBridgeReceipt
					if err := readJSON(filepath.Join(matches[0], "launcher.json"), &rec); err != nil {
						t.Fatal(err)
					}
					_, rec.Executable, err = hermesCompletionRead(context.Background(), matches[0], path, hermesBridgeLimit, true, false)
					if err != nil {
						t.Fatal(err)
					}
					data, _ := json.Marshal(rec)
					writeFixture(t, filepath.Join(matches[0], "launcher.json"), data, 0600)
				}
				return nil
			}
			if err := f.run(ctx, "integrate", "--provider", hermesAgent); err == nil || !strings.Contains(err.Error(), "retained") {
				t.Fatal(err)
			}
			integrationMissing(t, filepath.Join(root, "bin"))
			integrationMissing(t, filepath.Join(root, "path.json"))
			if string(integrationRead(t, filepath.Join(os.Getenv("HOME"), ".profile"))) != "# no newline" {
				t.Fatal("profile changed before publication")
			}
		})
	}
}

func TestHermesAliasOptions(t *testing.T) {
	executable := "/tmp/apps/ai/hermes-agent/native-v1/bin/hermes"
	valid := []string{"--accept-runtime-execution", "--dry-run", "--", "--help"}
	o, err := parseHermesAlias(executable, valid, false)
	if err != nil || o.apps != "/tmp/apps" || !o.dry || len(o.args) != 1 || o.args[0] != "--help" {
		t.Fatal(o, err)
	}
	for _, args := range [][]string{nil, {"--dry-run", "--"}, {"--accept-runtime-execution"}, {"--apps-dir", "/tmp/other", "--accept-runtime-execution", "--"}, {"--hermes-cli", "--accept-runtime-execution", "--"}, {"--accept-runtime-execution", "--accept-runtime-execution", "--"}} {
		if _, err := parseHermesAlias(executable, args, false); err == nil {
			t.Fatal("accepted alias override or absent consent", args)
		}
	}
	for _, path := range []string{"/tmp/hermes", "/tmp/apps/ai/hermes-agent/native-v1/bin/../bin/hermes", "/tmp/apps/other/native-v1/bin/hermes"} {
		if _, err := parseHermesAlias(path, valid, false); err == nil {
			t.Fatal("accepted foreign alias layout", path)
		}
	}
}

func TestHermesIntegrationRequiresSelection(t *testing.T) {
	f := hermesIntegrationFixture(t, "amd64")
	root := f.b.root(hermesAgent)
	if err := os.Remove(filepath.Join(root, "current.json")); err != nil {
		t.Fatal(err)
	}
	mustRun(t, f.fixture, "integrate")
	if err := f.run(context.Background(), "integrate", "--provider", hermesAgent); err == nil {
		t.Fatal("unselected preparation enrolled")
	}
	integrationMissing(t, filepath.Join(root, "bin"))
	choices, err := f.b.hermesMenu(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range choices {
		if c.Operation == "integrate" {
			t.Fatal("unselected menu offered PATH")
		}
	}
}

func TestHermesIntegrationDiscovery(t *testing.T) {
	f := hermesIntegrationFixture(t, "amd64")
	mustRun(t, f.fixture, "integrate", "--provider", hermesAgent)
	bridge := filepath.Join(f.b.root(hermesAgent), "bin", "hermes")
	alias := filepath.Join(os.Getenv("HOME"), ".local", "bin", "hermes")
	if err := os.MkdirAll(filepath.Dir(alias), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(bridge, alias); err != nil {
		t.Fatal(err)
	}
	r, err := f.b.discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]string{}
	for _, e := range r.Evidence {
		found[e.Path] = e.State
	}
	if found[bridge] != "preview-candidate" || found[alias] != "native-preview-alias" {
		t.Fatal(found)
	}
}

func TestHermesIntegrationSelfCopyGuards(t *testing.T) {
	for _, kind := range []string{"shell", "wrong-arch", "symlink", "hardlink", "oversize", "writable"} {
		t.Run(kind, func(t *testing.T) {
			f := hermesIntegrationFixture(t, "amd64")
			source, err := f.b.hermes.selfPath()
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "shell":
				writeFixture(t, source, []byte("#!/bin/sh\nexit 1\n"), 0755)
			case "wrong-arch":
				writeFixture(t, source, fakeELF("arm64", "wrong", false), 0755)
			case "symlink":
				if err := os.Rename(source, source+"-target"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(source+"-target", source); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(source, source+"-link"); err != nil {
					t.Fatal(err)
				}
			case "oversize":
				if err := os.Truncate(source, hermesBridgeLimit+1); err != nil {
					t.Fatal(err)
				}
			case "writable":
				if err := os.Chmod(source, 0775); err != nil {
					t.Fatal(err)
				}
			}
			if err := f.run(context.Background(), "integrate", "--provider", hermesAgent); err == nil {
				t.Fatal("unsafe native backend accepted")
			}
			integrationMissing(t, filepath.Join(f.b.root(hermesAgent), "bin"))
			integrationMissing(t, filepath.Join(f.b.root(hermesAgent), "path.json"))
			if string(integrationRead(t, filepath.Join(os.Getenv("HOME"), ".profile"))) != "# no newline" {
				t.Fatal("unsafe source changed profiles")
			}
		})
	}
}
