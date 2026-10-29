//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/AtomyCloud/atm/internal/protocol"
)

func TestOllamaPathStableBundleAndMenu(t *testing.T) {
	f := integrationFixture(t, "ollama")
	ctx := context.Background()
	root, home := f.b.root("ollama"), os.Getenv("HOME")
	profile := filepath.Join(home, ".profile")
	original := integrationRead(t, profile)
	external := filepath.Join(home, ".local", "bin", "ollama")
	models := filepath.Join(home, ".ollama", "models", "sentinel")
	service := filepath.Join(home, ".config", "systemd", "user", "ollama.service")
	for _, p := range []string{external, models, service} {
		writeFixture(t, p, []byte("external sentinel"), 0600)
	}
	ollamaFixtureRelease(t, f, "1.2.4")
	mustRun(t, f, "update", "--provider", "ollama", "--trust-download")
	mustRun(t, f, "use", "--provider", "ollama", "--version", "1.2.3")
	if !bytes.Equal(original, integrationRead(t, profile)) {
		t.Fatal("implicit profile edit")
	}
	integrationMissing(t, filepath.Join(root, "path.json"))
	integrationMissing(t, filepath.Join(root, "bin"))
	writeFixture(t, filepath.Join(home, ".zshrc"), []byte("# existing zsh\n"), 0644)
	mustRun(t, f, "integrate") // CORE path apply --plugins sends no provider.
	link := filepath.Join(root, "bin", "ollama")
	before, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	profiles := map[string][]byte{}
	for _, name := range profileNames {
		profiles[name] = integrationRead(t, filepath.Join(home, name))
	}
	receiptBytes := integrationRead(t, filepath.Join(root, "path.json"))
	for _, v := range []string{"1.2.4", "1.2.3"} {
		mustRun(t, f, "use", "--provider", "ollama", "--version", v)
		if target, err := os.Readlink(filepath.Join(root, "current")); err != nil || target != "versions/"+v+"/tree" {
			t.Fatal("bundle selection changed shape", target, err)
		}
		if target, err := os.Readlink(link); err != nil || target != "../current/bin/ollama" {
			t.Fatal("wrong stable command target", target, err)
		}
		after, err := os.Lstat(link)
		if err != nil || !os.SameFile(before, after) {
			t.Fatal("selection rewrote stable command", err)
		}
		if !bytes.Equal(integrationRead(t, link), fakeELF("amd64", v, false)) {
			t.Fatal("stable link did not follow selection")
		}
		if got := string(integrationRead(t, filepath.Join(root, "current", "lib", "ollama", "lib.so"))); got != "library-"+v {
			t.Fatal("bundle library missing or wrong version", got)
		}
		for name, data := range profiles {
			if !bytes.Equal(data, integrationRead(t, filepath.Join(home, name))) {
				t.Fatal("selection edited profile", name)
			}
		}
		if !bytes.Equal(receiptBytes, integrationRead(t, filepath.Join(root, "path.json"))) {
			t.Fatal("selection rewrote PATH receipt")
		}
	}
	calls := f.calls
	var logs []string
	f.b.log = func(s string) { logs = append(logs, s) }
	choices, err := f.b.menu(ctx, "ollama")
	if err != nil {
		t.Fatal(err)
	}
	if err = protocol.ValidateChoices(choices); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"integrate", "remove-path"} {
		found := false
		for _, c := range choices {
			if c.ID == id {
				found = c.Operation == "integrate" && len(c.Arguments) >= 2 && c.Arguments[0] == "--provider" && c.Arguments[1] == "ollama"
			}
		}
		if !found {
			t.Fatal("missing owned menu action", id)
		}
	}
	mustRun(t, f, "status")
	if !strings.Contains(strings.Join(logs, "\n"), providers["ollama"].name+": "+label("path_recorded")) || calls != f.calls {
		t.Fatal("missing PATH status or unexpected network")
	}
	mustRun(t, f, "integrate", "--provider", "ollama", "--remove-path")
	if !bytes.Equal(original, integrationRead(t, profile)) {
		t.Fatal("detach did not restore exact original bytes")
	}
	if vs, current, err := f.b.ollamaInspect(ctx); err != nil || len(vs) != 2 || current != "1.2.3" {
		t.Fatal("detach changed bundle", vs, current, err)
	}
	mustRun(t, f, "integrate", "--provider", "ollama")
	mustRun(t, f, "uninstall", "--provider", "ollama")
	for _, p := range []string{external, models, service} {
		if string(integrationRead(t, p)) != "external sentinel" {
			t.Fatal("external data changed", p)
		}
	}
}

func TestOllamaPathStrictOwnership(t *testing.T) {
	for _, kind := range []string{"wrong-link", "missing-link", "extra-bin", "extra-root", "missing-receipt", "receipt-provider", "profile-edit", "library-edit", "bin-symlink", "link-hardlink"} {
		t.Run(kind, func(t *testing.T) {
			f := integrationFixture(t, "ollama")
			mustRun(t, f, "integrate", "--provider", "ollama")
			root, home := f.b.root("ollama"), os.Getenv("HOME")
			link := filepath.Join(root, "bin", "ollama")
			switch kind {
			case "wrong-link", "missing-link":
				if err := os.Remove(link); err != nil {
					t.Fatal(err)
				}
				if kind == "wrong-link" {
					if err := os.Symlink("../current", link); err != nil {
						t.Fatal(err)
					}
				}
			case "extra-bin":
				writeFixture(t, filepath.Join(root, "bin", "manual"), []byte("keep"), 0600)
			case "extra-root":
				writeFixture(t, filepath.Join(root, "manual"), []byte("keep"), 0600)
			case "missing-receipt":
				if err := os.Remove(filepath.Join(root, "path.json")); err != nil {
					t.Fatal(err)
				}
			case "receipt-provider":
				var rec pathReceipt
				if err := readJSON(filepath.Join(root, "path.json"), &rec); err != nil {
					t.Fatal(err)
				}
				rec.Provider = "picoclaw"
				data, err := json.Marshal(rec)
				if err != nil {
					t.Fatal(err)
				}
				writeFixture(t, filepath.Join(root, "path.json"), data, 0600)
			case "profile-edit":
				writeFixture(t, filepath.Join(home, ".bashrc"), []byte("manual replacement"), 0600)
			case "library-edit":
				writeFixture(t, filepath.Join(root, "versions", "1.2.3", "tree", "lib", "ollama", "lib.so.1"), []byte("manual library"), 0600)
			case "bin-symlink":
				bin := filepath.Join(root, "bin")
				moved := filepath.Join(home, "moved-bin")
				if err := os.Rename(bin, moved); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(moved, bin); err != nil {
					t.Fatal(err)
				}
			case "link-hardlink":
				if err := os.Link(link, filepath.Join(home, "hardlinked-command")); err != nil {
					t.Fatal(err)
				}
			}
			profile := integrationRead(t, filepath.Join(home, ".profile"))
			for _, args := range [][]string{{"--provider", "ollama"}, {"--provider", "ollama", "--remove-path"}} {
				if err := f.run(context.Background(), "integrate", args...); err == nil {
					t.Fatal("unsafe integration accepted", args)
				}
			}
			if err := f.run(context.Background(), "uninstall", "--provider", "ollama"); err == nil {
				t.Fatal("unsafe uninstall accepted")
			}
			if v, err := f.b.ollamaCurrent(); err != nil || v != "1.2.3" {
				t.Fatal("destructive work before refusal", v, err)
			}
			if !bytes.Equal(profile, integrationRead(t, filepath.Join(home, ".profile"))) {
				t.Fatal("profile changed on refusal")
			}
			if !bytes.Equal(integrationRead(t, filepath.Join(root, "current", "bin", "ollama")), fakeELF("amd64", "1.2.3", false)) {
				t.Fatal("bundle executable changed on refusal")
			}
		})
	}
}

func TestOllamaPathRecovery(t *testing.T) {
	for _, remove := range []bool{false, true} {
		for _, kind := range []string{"error", "cancel", "intervening-edit"} {
			t.Run(kind+map[bool]string{false: "-enable", true: "-detach"}[remove], func(t *testing.T) {
				f := integrationFixture(t, "ollama")
				args := []string{"--provider", "ollama"}
				if remove {
					mustRun(t, f, "integrate", args...)
					args = append(args, "--remove-path")
				}
				home := os.Getenv("HOME")
				profile := filepath.Join(home, ".profile")
				before := integrationRead(t, profile)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				f.b.checkpoint = func(point string) error {
					if point != "path-published-.profile" {
						return nil
					}
					if kind == "cancel" {
						cancel()
						return nil
					}
					if kind == "intervening-edit" {
						writeFixture(t, profile, []byte("user edit"), 0640)
					}
					return errors.New("synthetic PATH failure")
				}
				err := f.run(ctx, "integrate", args...)
				if err == nil || (kind == "cancel" && !errors.Is(err, context.Canceled)) {
					t.Fatal("missing failure", err)
				}
				lock := filepath.Join(home, ".atm-ai-path.lock")
				if kind == "intervening-edit" {
					if !strings.Contains(err.Error(), "restoration incomplete") || string(integrationRead(t, profile)) != "user edit" {
						t.Fatal("uncertain restoration lost edit", err)
					}
					var journal struct {
						Schema  int
						Changes []pathChange
					}
					if err := json.Unmarshal(integrationRead(t, filepath.Join(lock, "journal.json")), &journal); err != nil {
						t.Fatal(err)
					}
					found := false
					for _, c := range journal.Changes {
						found = found || (c.Path == profile && bytes.Equal(c.Before.Data, before) && c.Before.Mode == 0640)
					}
					if journal.Schema != 1 || !found {
						t.Fatal("missing recovery evidence")
					}
					f.b.checkpoint = nil
					if err := f.run(context.Background(), "uninstall", "--provider", "ollama"); err == nil {
						t.Fatal("uninstall ignored uncertain PATH state")
					}
				} else {
					if !bytes.Equal(before, integrationRead(t, profile)) {
						t.Fatal("profile not restored")
					}
					integrationMissing(t, lock)
					if _, _, err := f.b.ollamaInspect(context.Background()); err != nil {
						t.Fatal("restored store invalid", err)
					}
				}
				if v, err := f.b.ollamaCurrent(); err != nil || v != "1.2.3" {
					t.Fatal("PATH failure destroyed bundle", v, err)
				}
			})
		}
	}
}

func TestOllamaPathLocksAndPartialGenericApply(t *testing.T) {
	f := integrationFixture(t, "picoclaw", "codex-cli", "ollama")
	home := os.Getenv("HOME")
	lock := filepath.Join(home, ".atm-ai-path.lock")
	if err := os.Mkdir(lock, 0700); err != nil {
		t.Fatal(err)
	}
	for _, op := range []string{"integrate", "uninstall"} {
		if err := f.run(context.Background(), op, "--provider", "ollama"); err == nil || !strings.Contains(err.Error(), "profiles locked") {
			t.Fatal("Ollama bypassed HOME lock", err)
		}
	}
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	providerLock := f.b.root("ollama") + ".lock"
	if err := os.Mkdir(providerLock, 0700); err != nil {
		t.Fatal(err)
	}
	err := f.run(context.Background(), "integrate")
	if err == nil || !strings.Contains(err.Error(), "previously completed providers=[picoclaw codex-cli]") {
		t.Fatal("generic apply did not report partial completion", err)
	}
	for _, id := range []string{"picoclaw", "codex-cli"} {
		rec, err := f.b.inspectIntegration(id, "1.2.3")
		if err != nil || rec == nil {
			t.Fatal("earlier provider was rolled back", id, err)
		}
	}
	integrationMissing(t, filepath.Join(f.b.root("ollama"), "path.json"))
	if err := os.Remove(providerLock); err != nil {
		t.Fatal(err)
	}
	f.b.checkpoint = func(point string) error {
		if point == "path-preflight" {
			if err := f.b.changeIntegration(context.Background(), "picoclaw", false); err == nil || !strings.Contains(err.Error(), "profiles locked") {
				t.Fatal("other AI provider bypassed shared lock", err)
			}
		}
		return nil
	}
	mustRun(t, f, "integrate", "--provider", "ollama")
}

func TestOllamaPathUninstallDetachBeforeDestruction(t *testing.T) {
	f := integrationFixture(t, "ollama")
	before, err := f.b.ollamaVerify(context.Background(), "1.2.3", f.b.ollamaVersion("1.2.3"))
	if err != nil {
		t.Fatal(err)
	}
	mustRun(t, f, "integrate", "--provider", "ollama")
	f.b.checkpoint = func(point string) error {
		if point == "ollama-before-uninstall-deselect" {
			integrationMissing(t, filepath.Join(f.b.root("ollama"), "path.json"))
			integrationMissing(t, filepath.Join(f.b.root("ollama"), "bin", "ollama"))
			return errors.New("synthetic failure after PATH detach")
		}
		return nil
	}
	if err := f.run(context.Background(), "uninstall", "--provider", "ollama"); err == nil {
		t.Fatal("expected partial uninstall failure")
	}
	if v, err := f.b.ollamaCurrent(); err != nil || v != "1.2.3" {
		t.Fatal("deselected before failure", v, err)
	}
	after, err := f.b.ollamaVerify(context.Background(), "1.2.3", f.b.ollamaVersion("1.2.3"))
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("bundle changed after detach-only failure", err)
	}
	f.b.checkpoint = nil
	mustRun(t, f, "uninstall", "--provider", "ollama")
}
