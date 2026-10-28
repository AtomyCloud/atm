//go:build linux

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AtomyCloud/atm/internal/protocol"
)

// This opt-in test fetches public metadata, never provider artifacts or code.
func TestOfficialReleaseMetadata(t *testing.T) {
	if os.Getenv("ATM_TEST_AI_PUBLIC_RELEASES") != "1" {
		t.Skip("set ATM_TEST_AI_PUBLIC_RELEASES=1 for read-only upstream verification")
	}
	root := t.TempDir()
	b, err := newBackend(protocol.Request{PluginID: "ai", AppsDir: filepath.Join(root, "apps")}, func(s string) { t.Log(s) })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	for _, id := range []string{"picoclaw", "hermes-desktop", "codex-cli", hermesSystem, "ollama"} {
		for _, arch := range []string{"amd64", "arm64"} {
			if id == hermesSystem && arch == "arm64" {
				continue // System DEB preview is explicitly amd64-only.
			}
			t.Run(id+"/"+arch, func(t *testing.T) {
				b.arch = arch
				a, err := b.resolve(ctx, id, "latest")
				if err != nil {
					t.Fatal(err)
				}
				if a.Provider != id || a.Arch != arch {
					t.Fatalf("wrong provider or architecture: %+v", a)
				}
				if err := a.validate(); err != nil {
					t.Fatal(err)
				}
				t.Logf("%s %s/%s: %s (%d bytes, SHA-256 present)", id, a.Version, arch, a.URL, a.Size)
			})
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("metadata discovery changed its temporary root: entries=%v error=%v", entries, err)
	}
}

// Parse one real upstream archive in disposable staging; never install or run it.
func TestOfficialPicoClawArchiveLayout(t *testing.T) {
	if os.Getenv("ATM_TEST_AI_PUBLIC_ARCHIVE") != "1" {
		t.Skip("set ATM_TEST_AI_PUBLIC_ARCHIVE=1 for disposable public archive parsing")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	b, err := newBackend(protocol.Request{PluginID: "ai", AppsDir: filepath.Join(root, "unused-apps")}, func(s string) { t.Log(s) })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	a, err := b.resolve(ctx, "picoclaw", "latest")
	if err != nil {
		t.Fatal(err)
	}
	tool := filepath.Join(root, "tool")
	if err = b.extractBinary(ctx, a, root, tool); err != nil {
		t.Fatal(err)
	}
	if err = validateELF(tool, b.arch, false); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Lstat(filepath.Join(root, "unused-apps")); !os.IsNotExist(err) {
		t.Fatalf("archive parsing created an installation root: %v", err)
	}
	t.Logf("Parsed and SHA-256/ELF verified PicoClaw %s %s in temporary staging; no execution", a.Version, a.Arch)
}
