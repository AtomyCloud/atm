//go:build linux

package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func hermesRefreshArgs() []string {
	return []string{"--provider", hermesAgent, "--refresh-launcher"}
}

func TestHermesRefreshMutations(t *testing.T) {
	for _, kind := range []string{"old-preflight", "old-exchange", "source", "source-inode", "stage", "stage-inode", "profile", "path", "selection", "completion", "cancel-before", "cancel-after", "error-after", "old-before-unlink", "receipt-before-unlink", "foreign-before-unlink", "partial-cleanup", "directory-cleanup", "provider-lock", "home-lock"} {
		t.Run(kind, func(t *testing.T) {
			f := hermesRefreshFixture(t, "amd64", false, true)
			root, home := f.b.root(hermesAgent), os.Getenv("HOME")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			seen := false
			var retainedStage string
			f.b.checkpoint = func(point string) error {
				want := "hermes-refresh-before-exchange"
				switch kind {
				case "old-preflight":
					want = "hermes-refresh-preflight"
				case "cancel-after", "error-after":
					want = "hermes-refresh-after-exchange"
				case "old-before-unlink", "foreign-before-unlink":
					want = "hermes-refresh-before-cleanup-hermes"
				case "receipt-before-unlink", "partial-cleanup":
					want = "hermes-refresh-before-cleanup-launcher.json"
				case "directory-cleanup":
					want = "hermes-refresh-before-cleanup-directory"
				}
				if point != want {
					return nil
				}
				seen = true
				stages, err := filepath.Glob(filepath.Join(root+".lock", ".hermes-bridge-*"))
				if err != nil {
					t.Fatal(err)
				}
				if len(stages) == 1 {
					retainedStage = stages[0]
				}
				if kind != "old-preflight" && len(stages) != 1 {
					t.Fatal("missing stage", stages)
				}
				replace := func(path string) {
					data := integrationRead(t, path)
					i, err := os.Lstat(path)
					if err != nil {
						t.Fatal(err)
					}
					if err := os.Rename(path, path+"-saved"); err != nil {
						t.Fatal(err)
					}
					writeFixture(t, path, data, i.Mode().Perm())
				}
				switch kind {
				case "old-preflight", "old-exchange":
					hermesRefreshReplaceBridge(t, f, filepath.Join(root, "bin"))
				case "source":
					source, _ := f.b.hermes.selfPath()
					writeFixture(t, source, fakeELF("amd64", "late source", false), 0755)
				case "source-inode":
					source, _ := f.b.hermes.selfPath()
					replace(source)
				case "stage":
					hermesRefreshReplaceBridge(t, f, retainedStage)
				case "stage-inode":
					replace(filepath.Join(retainedStage, "hermes"))
					if err := os.Remove(filepath.Join(retainedStage, "hermes-saved")); err != nil {
						t.Fatal(err)
					}
				case "profile":
					replace(filepath.Join(home, ".profile"))
				case "path":
					replace(filepath.Join(root, "path.json"))
				case "selection":
					replace(filepath.Join(root, "current.json"))
				case "completion":
					replace(filepath.Join(root, "versions", hermesPMVersion, "complete.json"))
				case "cancel-before", "cancel-after":
					cancel()
				case "error-after", "partial-cleanup":
					return errors.New("injected interruption")
				case "old-before-unlink":
					replace(filepath.Join(retainedStage, "hermes"))
					if err := os.Remove(filepath.Join(retainedStage, "hermes-saved")); err != nil {
						t.Fatal(err)
					}
				case "receipt-before-unlink":
					replace(filepath.Join(retainedStage, "launcher.json"))
					if err := os.Remove(filepath.Join(retainedStage, "launcher.json-saved")); err != nil {
						t.Fatal(err)
					}
				case "foreign-before-unlink", "directory-cleanup":
					writeFixture(t, filepath.Join(retainedStage, "foreign"), []byte("preserve"), 0600)
				case "provider-lock", "home-lock":
					lock := root + ".lock"
					if kind == "home-lock" {
						lock = filepath.Join(home, ".atm-ai-path.lock")
					}
					if err := os.Rename(lock, lock+"-saved"); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(lock, 0700); err != nil {
						t.Fatal(err)
					}
				}
				return nil
			}
			err := f.run(ctx, "integrate", hermesRefreshArgs()...)
			if !seen || err == nil || !strings.Contains(err.Error(), "retained") {
				t.Fatal("mutation accepted without retention", seen, err)
			}
			if (kind == "old-preflight" || kind == "old-exchange") && !strings.Contains(err.Error(), "original bridge authority changed") {
				t.Fatal("coherent replacement did not reach original authority guard", err)
			}
			if kind == "stage" && !strings.Contains(err.Error(), "native bridge stage changed") {
				t.Fatal("coherent stage replacement did not reach pinned stage guard", err)
			}
			if strings.HasPrefix(kind, "cancel-") && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			for _, lock := range []string{root + ".lock", filepath.Join(home, ".atm-ai-path.lock")} {
				if _, err := os.Lstat(lock); err != nil {
					t.Fatal("lock disappeared", lock, err)
				}
			}
			if kind == "partial-cleanup" || kind == "receipt-before-unlink" {
				integrationMissing(t, filepath.Join(retainedStage, "hermes"))
				integrationRead(t, filepath.Join(retainedStage, "launcher.json"))
			}
			if kind == "foreign-before-unlink" || kind == "directory-cleanup" {
				if string(integrationRead(t, filepath.Join(retainedStage, "foreign"))) != "preserve" {
					t.Fatal("foreign cleanup")
				}
			}
			if kind == "cancel-after" || kind == "error-after" {
				integrationRead(t, filepath.Join(retainedStage, "hermes"))
				integrationRead(t, filepath.Join(retainedStage, "launcher.json"))
			}
		})
	}
}

func TestHermesRefreshRefusesMissingLockedForeign(t *testing.T) {
	for _, kind := range []string{"missing", "foreign", "changed", "receipt", "provider-lock", "home-lock", "unselected", "wrong-arch", "hardlink", "symlink", "source-writable", "source-shell"} {
		t.Run(kind, func(t *testing.T) {
			f := hermesRefreshFixture(t, "amd64", false, true)
			root, home := f.b.root(hermesAgent), os.Getenv("HOME")
			bin := filepath.Join(root, "bin")
			switch kind {
			case "missing":
				if err := os.Rename(bin, filepath.Join(home, "saved")); err != nil {
					t.Fatal(err)
				}
			case "foreign":
				writeFixture(t, filepath.Join(bin, "extra"), []byte("foreign"), 0600)
			case "changed":
				writeFixture(t, filepath.Join(bin, "hermes"), fakeELF("amd64", "changed", false), 0755)
			case "receipt":
				writeFixture(t, filepath.Join(bin, "launcher.json"), []byte("{}"), 0600)
			case "provider-lock":
				if err := os.Mkdir(root+".lock", 0700); err != nil {
					t.Fatal(err)
				}
			case "home-lock":
				if err := os.Mkdir(filepath.Join(home, ".atm-ai-path.lock"), 0700); err != nil {
					t.Fatal(err)
				}
			case "unselected":
				if err := os.Remove(filepath.Join(root, "current.json")); err != nil {
					t.Fatal(err)
				}
			case "wrong-arch":
				source, _ := f.b.hermes.selfPath()
				writeFixture(t, source, fakeELF("arm64", "wrong", false), 0755)
			case "hardlink":
				if err := os.Link(filepath.Join(bin, "hermes"), filepath.Join(home, "hardlink")); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Rename(bin, bin+"-saved"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(bin+"-saved", bin); err != nil {
					t.Fatal(err)
				}
			case "source-writable":
				source, _ := f.b.hermes.selfPath()
				if err := os.Chmod(source, 0775); err != nil {
					t.Fatal(err)
				}
			case "source-shell":
				source, _ := f.b.hermes.selfPath()
				writeFixture(t, source, []byte("#!/bin/sh\nexit 1\n"), 0755)
			}
			preserved := hermesRefreshPreserved(t, f)
			if err := f.run(context.Background(), "integrate", hermesRefreshArgs()...); err == nil {
				t.Fatal("invalid refresh accepted")
			}
			preserved()
		})
	}
}

func TestHermesRefreshNoopMutations(t *testing.T) {
	for _, kind := range []string{"old", "source", "profile", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			f := hermesRefreshFixture(t, "amd64", true, false)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			seen := false
			f.b.checkpoint = func(point string) error {
				if point != "hermes-refresh-before-noop" {
					return nil
				}
				seen = true
				switch kind {
				case "old":
					hermesRefreshReplaceBridge(t, f, filepath.Join(f.b.root(hermesAgent), "bin"))
				case "source":
					source, _ := f.b.hermes.selfPath()
					writeFixture(t, source, fakeELF("amd64", "changed", false), 0755)
				case "profile":
					writeFixture(t, filepath.Join(os.Getenv("HOME"), ".profile"), []byte("changed"), 0640)
				case "cancel":
					cancel()
				}
				return nil
			}
			err := f.run(ctx, "integrate", hermesRefreshArgs()...)
			if !seen || err == nil || !strings.Contains(err.Error(), "retained") {
				t.Fatal(seen, err)
			}
			if kind == "old" && !strings.Contains(err.Error(), "original bridge authority changed") {
				t.Fatal("coherent no-op replacement did not reach original authority guard", err)
			}
			integrationMissing(t, filepath.Join(f.b.root(hermesAgent), "path.json"))
		})
	}
}

func hermesRefreshFixture(t *testing.T, arch string, dormant, changed bool) *hermesAgentFixture {
	t.Helper()
	f := hermesIntegrationFixture(t, arch)
	if home := os.Getenv("HOME"); home != filepath.Join(filepath.Dir(f.b.req.AppsDir), "user") {
		t.Fatal("fixture HOME is not isolated", home)
	}
	mustRun(t, f.fixture, "integrate", "--provider", hermesAgent)
	if dormant {
		mustRun(t, f.fixture, "integrate", "--provider", hermesAgent, "--remove-path")
	}
	if changed {
		source, err := f.b.hermes.selfPath()
		if err != nil {
			t.Fatal(err)
		}
		writeFixture(t, source, fakeELF(arch, "replacement native backend", false), 0755)
	}
	return f
}

func hermesRefreshPreserved(t *testing.T, f *hermesAgentFixture) func() {
	t.Helper()
	root, home := f.b.root(hermesAgent), os.Getenv("HOME")
	before := map[string]pathState{}
	for _, path := range []string{filepath.Join(home, ".profile"), filepath.Join(home, ".bashrc"), filepath.Join(home, ".zshrc"), filepath.Join(root, "path.json"), filepath.Join(root, "current.json"), filepath.Join(root, "owner.json"), filepath.Join(root, "versions", hermesPMVersion, "source.json"), filepath.Join(root, "versions", hermesPMVersion, "complete.json")} {
		s, err := pathSnapshot(path, false)
		if err != nil {
			t.Fatal(err)
		}
		before[path] = s
	}
	return func() {
		t.Helper()
		for path, old := range before {
			now, err := pathSnapshot(path, false)
			if err != nil || !pathUnchanged(now, old) {
				t.Fatal("refresh changed unrelated authority", path, err)
			}
		}
	}
}

func hermesRefreshReplaceBridge(t *testing.T, f *hermesAgentFixture, bin string) {
	t.Helper()
	// Keep the displaced tree outside the managed inventory so refusal must
	// come from pinned authority, not an unrelated unknown-entry check.
	if err := os.Rename(bin, filepath.Join(t.TempDir(), "saved-bridge")); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(bin, "hermes"), fakeELF(f.b.arch, "coherent foreign bridge", false), 0755)
	_, hash, err := hermesCompletionRead(context.Background(), bin, filepath.Join(bin, "hermes"), hermesBridgeLimit, true, false)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(hermesBridgeReceipt{1, "ai", hermesAgent, f.b.root(hermesAgent), f.b.arch, hash})
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(bin, "launcher.json"), data, 0600)
}

func TestHermesRefreshOptions(t *testing.T) {
	if o, err := parseOptions("integrate", hermesRefreshArgs()); err != nil || !o.refreshLauncher || o.removePath || o.provider != hermesAgent {
		t.Fatal(o, err)
	}
	for _, op := range []string{"install", "update", "use", "remove", "uninstall", "menu", "status"} {
		if _, err := parseOptions(op, hermesRefreshArgs()); err == nil {
			t.Fatal("refresh accepted for", op)
		}
	}
	for _, args := range [][]string{{"--refresh-launcher"}, {"--provider", "ollama", "--refresh-launcher"}, {"--provider", "", "--refresh-launcher"}, {"--provider", hermesAgent, "--refresh-launcher=true"}} {
		if _, err := parseOptions("integrate", args); err == nil {
			t.Fatal("unsafe refresh syntax accepted", args)
		}
	}
	for _, extra := range [][]string{{"--remove-path"}, {"--refresh-launcher"}, {"--version", hermesPMVersion}, {"--version", "latest"}, {"--trust-download"}, {"--accept-dependency-execution"}, {"--accept-system-packages"}, {"--accept-runtime-execution"}} {
		if _, err := parseOptions("integrate", append(hermesRefreshArgs(), extra...)); err == nil {
			t.Fatal("extra consent/version/action accepted", extra)
		}
	}
}

func TestHermesRefreshLifecycle(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		for _, dormant := range []bool{false, true} {
			t.Run(arch+map[bool]string{false: "/enrolled", true: "/dormant"}[dormant], func(t *testing.T) {
				f := hermesRefreshFixture(t, arch, dormant, true)
				preserved := hermesRefreshPreserved(t, f)
				old, err := f.b.inspectHermesBridge(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				choices, err := f.b.hermesMenu(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, c := range choices {
					if c.ID == "refresh-launcher" {
						found = true
						if c.Operation != "integrate" || strings.Join(c.Arguments, " ") != strings.Join(hermesRefreshArgs(), " ") {
							t.Fatal(c)
						}
					}
				}
				if !found {
					t.Fatal("owned bridge missing menu action")
				}
				mustRun(t, f.fixture, "integrate", hermesRefreshArgs()...)
				now, err := f.b.inspectHermesBridge(context.Background())
				if err != nil || now == nil || sameHermesBridge(old, now) || now.payload.hash.SHA256 == old.payload.hash.SHA256 {
					t.Fatal("refresh did not replace owned image", err)
				}
				preserved()
				mustRun(t, f.fixture, "integrate", hermesRefreshArgs()...)
				after, err := f.b.inspectHermesBridge(context.Background())
				if err != nil || !sameHermesBridge(now, after) || !now.payload.info.ModTime().Equal(after.payload.info.ModTime()) {
					t.Fatal("same image changed inode", err)
				}
				preserved()
				integrationMissing(t, f.b.root(hermesAgent)+".lock")
				integrationMissing(t, filepath.Join(os.Getenv("HOME"), ".atm-ai-path.lock"))
			})
		}
	}
}
