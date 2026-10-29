//go:build linux

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Inspection journals must never authorize deletion or produce success after
// another writer changes them, including a verified same-image refresh.
func TestHermesRefreshPreservesEditedEvidence(t *testing.T) {
	for _, point := range []string{"hermes-refresh-preflight", "hermes-refresh-before-noop", "hermes-refresh-before-cleanup-directory"} {
		t.Run(point, func(t *testing.T) {
			f := hermesIntegrationFixture(t, "amd64")
			mustRun(t, f.fixture, "integrate", "--provider", hermesAgent)
			root, home := f.b.root(hermesAgent), os.Getenv("HOME")
			if point != "hermes-refresh-before-noop" {
				source, err := f.b.hermes.selfPath()
				if err != nil {
					t.Fatal(err)
				}
				writeFixture(t, source, fakeELF("amd64", "different backend", false), 0755)
			}
			journal := filepath.Join(root+".lock", "hermes-refresh.json")
			edited := []byte("manual evidence; must be retained\n")
			profile, err := pathSnapshot(filepath.Join(home, ".profile"), false)
			if err != nil {
				t.Fatal(err)
			}
			seen := false
			f.b.checkpoint = func(name string) error {
				if name == point {
					seen = true
					writeFixture(t, journal, edited, 0600)
				}
				return nil
			}
			err = f.run(context.Background(), "integrate", "--provider", hermesAgent, "--refresh-launcher")
			if !seen || err == nil || !strings.Contains(err.Error(), "journal changed") || !strings.Contains(err.Error(), "retained") {
				t.Fatal("changed inspection evidence reported success or was not reached", seen, err)
			}
			if !bytes.Equal(integrationRead(t, journal), edited) {
				t.Fatal("manual evidence was overwritten or deleted")
			}
			after, err := pathSnapshot(filepath.Join(home, ".profile"), false)
			if err != nil || !pathUnchanged(after, profile) {
				t.Fatal("refresh changed profile", err)
			}
			for _, path := range []string{root + ".lock", filepath.Join(home, ".atm-ai-path.lock")} {
				if info, err := os.Lstat(path); err != nil || !info.IsDir() {
					t.Fatal("uncertain refresh did not retain lock", path, err)
				}
			}
		})
	}
}
