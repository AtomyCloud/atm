//go:build linux

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPathDetachPreservesFollowingProviderBlock(t *testing.T) {
	f := installedPair(t)
	f.release(t, "codex-cli", "1.2.3", nil)
	mustRun(t, f, "install", "--provider", "codex-cli", "--trust-download")
	home := f.b.discovery.home
	if err := os.MkdirAll(home, 0700); err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(home, ".profile")
	if err := os.WriteFile(profile, []byte("export PERSONAL=kept"), 0600); err != nil {
		t.Fatal(err)
	}
	mustRun(t, f, "integrate", "--provider", "picoclaw")
	mustRun(t, f, "integrate", "--provider", "codex-cli")
	mustRun(t, f, "integrate", "--provider", "picoclaw", "--remove-path")
	data, err := os.ReadFile(profile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "export PERSONAL=kept\n# >>> ATM native AI codex-cli PATH >>>") {
		t.Fatalf("detach joined user text to the other provider's block: %q", data)
	}
	if _, _, err = f.b.inspect(context.Background(), "codex-cli", true); err != nil {
		t.Fatalf("detach invalidated the other provider: %v", err)
	}
	mustRun(t, f, "integrate", "--provider", "codex-cli", "--remove-path")
	data, err = os.ReadFile(profile)
	if err != nil || (string(data) != "export PERSONAL=kept" && string(data) != "export PERSONAL=kept\n") {
		t.Fatalf("unrelated text changed: %q %v", data, err)
	}
}

func TestPathFinalPublicationRechecksWholeState(t *testing.T) {
	for _, kind := range []string{"selected-payload", "earlier-profile", "earlier-mode"} {
		t.Run(kind, func(t *testing.T) {
			f := integrationFixture(t, "picoclaw")
			home := os.Getenv("HOME")
			profile := filepath.Join(home, ".profile")
			changed := false
			f.b.checkpoint = func(point string) error {
				if point != "path-published-.bashrc" || changed {
					return nil
				}
				changed = true
				switch kind {
				case "selected-payload":
					tool := filepath.Join(f.b.root("picoclaw"), "versions/1.2.3/tool")
					data, err := os.ReadFile(tool)
					if err != nil {
						return err
					}
					data[len(data)-1] ^= 1
					return os.WriteFile(tool, data, 0755)
				case "earlier-profile":
					return os.WriteFile(profile, []byte("# intervening user edit\n"), 0640)
				default:
					return os.Chmod(profile, 0600)
				}
			}
			err := f.run(context.Background(), "integrate", "--provider", "picoclaw")
			if !changed || err == nil {
				t.Fatalf("reported success after late state change: changed=%v err=%v", changed, err)
			}
			if kind != "selected-payload" {
				journal := filepath.Join(home, ".atm-ai-path.lock/journal.json")
				if _, e := os.Stat(journal); e != nil {
					t.Fatalf("lost uncertain recovery evidence: %v", e)
				}
				data, e := os.ReadFile(profile)
				if e != nil || (kind == "earlier-profile" && string(data) != "# intervening user edit\n") {
					t.Fatalf("intervening user text lost: %q %v", data, e)
				}
				if kind == "earlier-mode" {
					i, e := os.Stat(profile)
					if e != nil || i.Mode().Perm() != 0600 {
						t.Fatalf("intervening mode lost: %v", e)
					}
				}
			}
		})
	}
}
