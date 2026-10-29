//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHermesDeactivateReappearingState(t *testing.T) {
	for _, kind := range []string{"selection", "bridge"} {
		t.Run(kind, func(t *testing.T) {
			f := hermesIntegrationFixture(t, "amd64")
			mustRun(t, f.fixture, "integrate", "--provider", hermesAgent)
			root, home := f.b.root(hermesAgent), os.Getenv("HOME")
			pointer := integrationRead(t, filepath.Join(root, "current.json"))
			payload := integrationRead(t, filepath.Join(root, "bin", "hermes"))
			receipt := integrationRead(t, filepath.Join(root, "bin", "launcher.json"))
			prepared := hermesFixtureTreeSnapshot(t, filepath.Join(root, "versions"))
			seen := false
			f.b.checkpoint = func(point string) error {
				if point != "hermes-deactivate-after-deselect" {
					return nil
				}
				seen = true
				if kind == "selection" {
					writeFixture(t, filepath.Join(root, "current.json"), pointer, 0600)
				} else {
					writeFixture(t, filepath.Join(root, "bin", "hermes"), payload, 0755)
					writeFixture(t, filepath.Join(root, "bin", "launcher.json"), receipt, 0600)
					if _, err := f.b.inspectHermesBridge(context.Background()); err != nil {
						t.Fatal("reappearing bridge must be internally coherent", err)
					}
				}
				return nil
			}
			err := f.run(context.Background(), "uninstall", "--provider", hermesAgent, "--keep-preparation")
			if !seen || err == nil || !strings.Contains(err.Error(), "original "+kind+" changed") || !strings.Contains(err.Error(), "selection removed") || !strings.Contains(err.Error(), "retained") {
				t.Fatal("late authority was adopted or its phase lost", seen, err)
			}
			if kind == "selection" {
				if !bytes.Equal(integrationRead(t, filepath.Join(root, "current.json")), pointer) {
					t.Fatal("intervening selection overwritten")
				}
			} else if !bytes.Equal(integrationRead(t, filepath.Join(root, "bin", "hermes")), payload) || !bytes.Equal(integrationRead(t, filepath.Join(root, "bin", "launcher.json")), receipt) {
				t.Fatal("intervening bridge removed")
			}
			hermesFixtureTreeUnchanged(t, filepath.Join(root, "versions"), prepared)
			for _, lock := range []string{root + ".lock", filepath.Join(home, ".atm-ai-path.lock")} {
				if i, err := os.Lstat(lock); err != nil || !i.IsDir() {
					t.Fatal("uncertain operation must retain locks", lock, err)
				}
			}
		})
	}
}

func TestHermesIntegrationRetainsEmptyUncertainLock(t *testing.T) {
	f := hermesIntegrationFixture(t, "amd64")
	home := os.Getenv("HOME")
	lock := filepath.Join(home, ".atm-ai-path.lock")
	before := integrationRead(t, filepath.Join(home, ".profile"))
	seen := false
	f.b.checkpoint = func(point string) error {
		if point != "path-before-publish" {
			return nil
		}
		seen = true
		for _, name := range []string{"hermes-bridge.json", "journal.json"} {
			if err := os.Remove(filepath.Join(lock, name)); err != nil {
				t.Fatal(err)
			}
		}
		return errors.New("evidence disappeared before profile publication")
	}
	err := f.run(context.Background(), "integrate", "--provider", hermesAgent)
	if !seen || err == nil || !strings.Contains(err.Error(), "retained") {
		t.Fatal(seen, err)
	}
	entries, err := os.ReadDir(lock)
	if err != nil || len(entries) != 0 {
		t.Fatal("normal integration removed an empty but uncertain HOME lock", entries, err)
	}
	if !bytes.Equal(before, integrationRead(t, filepath.Join(home, ".profile"))) {
		t.Fatal("profile changed before publication")
	}
}
