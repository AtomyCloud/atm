//go:build linux

package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestOllamaPathReviewReappearingIntegrationStopsUninstall(t *testing.T) {
	f := integrationFixture(t, "ollama")
	ctx := context.Background()
	mustRun(t, f, "integrate", "--provider", "ollama")
	before, err := f.b.ollamaVerify(ctx, "1.2.3", f.b.ollamaVersion("1.2.3"))
	if err != nil {
		t.Fatal(err)
	}
	seen := false
	f.b.checkpoint = func(point string) error {
		if point != "ollama-before-uninstall-deselect" {
			return nil
		}
		seen = true
		integrationMissing(t, filepath.Join(f.b.root("ollama"), "path.json"))
		return f.b.changeIntegration(ctx, "ollama", false)
	}
	err = f.run(ctx, "uninstall", "--provider", "ollama")
	if !seen || err == nil || !strings.Contains(err.Error(), "PATH integration changed") {
		t.Fatal("reappearing integration did not stop uninstall", seen, err)
	}
	if current, err := f.b.ollamaCurrent(); err != nil || current != "1.2.3" {
		t.Fatal("active version changed", current, err)
	}
	after, err := f.b.ollamaVerify(ctx, "1.2.3", f.b.ollamaVersion("1.2.3"))
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("bundle changed", err)
	}
	if rec, err := f.b.inspectIntegration("ollama", "1.2.3"); err != nil || rec == nil {
		t.Fatal("reappearing integration was damaged", err)
	}
}

func TestOllamaPathReviewPinsCompleteSelectedReceipt(t *testing.T) {
	for _, point := range []string{"path-before-publish", "path-published-.bashrc"} {
		for _, coherent := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/coherent-receipt=%t", point, coherent), func(t *testing.T) {
				f := newFixture(t, "amd64")
				home := os.Getenv("HOME")
				original := []byte("# unrelated original profile\n")
				for _, name := range []string{".profile", ".bashrc"} {
					writeFixture(t, filepath.Join(home, name), original, 0600)
				}
				ollamaFixtureRelease(t, f, "1.2.3")
				mustRun(t, f, "install", "--provider", "ollama", "--trust-download")
				root := f.b.root("ollama")
				version := filepath.Join(root, "versions", "1.2.3")
				libraryName := "lib/ollama/lib.so.1"
				library := filepath.Join(version, "tree", libraryName)
				changed := []byte("new library bytes after the initial PATH snapshot")
				seen := false
				f.b.checkpoint = func(at string) error {
					if at != point || seen {
						return nil
					}
					seen = true
					if err := os.WriteFile(library, changed, 0600); err != nil {
						return err
					}
					if coherent {
						rec, err := ollamaReadReceipt(version)
						if err != nil {
							return err
						}
						entry := rec.Files[libraryName]
						entry.Size = int64(len(changed))
						entry.SHA256 = fmt.Sprintf("%x", sha256.Sum256(changed))
						rec.Files[libraryName] = entry
						data, err := json.Marshal(rec)
						if err != nil {
							return err
						}
						return os.WriteFile(filepath.Join(version, "receipt.json"), data, 0600)
					}
					return nil
				}
				if err := f.run(context.Background(), "integrate", "--provider", "ollama"); err == nil {
					t.Fatal("changed selected library/receipt accepted during PATH integration")
				}
				if !seen {
					t.Fatal("integration did not reach the selected fault boundary")
				}
				for _, name := range []string{".profile", ".bashrc"} {
					if got := integrationRead(t, filepath.Join(home, name)); string(got) != string(original) {
						t.Fatalf("profile not restored: %s %q", name, got)
					}
				}
				integrationMissing(t, filepath.Join(root, "path.json"))
				integrationMissing(t, filepath.Join(root, "bin", "ollama"))
				if got := integrationRead(t, library); string(got) != string(changed) {
					t.Fatal("concurrent library edit was overwritten")
				}
			})
		}
	}
}
