//go:build linux

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func installedPair(t *testing.T) *fixture {
	t.Helper()
	f := newFixture(t, "amd64")
	for _, v := range []string{"1.2.3", "1.2.4"} {
		f.release(t, "picoclaw", v, nil)
		mustRun(t, f, "install", "--provider", "picoclaw", "--trust-download")
	}
	return f
}

func TestActivationRechecksAfterVerification(t *testing.T) {
	for _, change := range []string{"payload", "receipt"} {
		t.Run(change, func(t *testing.T) {
			f := installedPair(t)
			dir := filepath.Join(f.b.root("picoclaw"), "versions", "1.2.3")
			f.b.checkpoint = func(name string) error {
				if name != "before-activation" {
					return nil
				}
				if change == "payload" {
					data := fakeELF("amd64", "modified", false)
					return os.WriteFile(filepath.Join(dir, "tool"), data, 0755)
				}
				var rec receipt
				if err := readJSON(filepath.Join(dir, "receipt.json"), &rec); err != nil {
					return err
				}
				rec.Artifact.SHA256 = strings.Repeat("a", 64)
				if err := os.Remove(filepath.Join(dir, "receipt.json")); err != nil {
					return err
				}
				return writeJSON(filepath.Join(dir, "receipt.json"), rec)
			}
			if err := f.run(context.Background(), "use", "--provider", "picoclaw", "--version", "1.2.3"); err == nil {
				t.Fatal("activated a version changed after verification")
			}
			if got, err := f.b.current("picoclaw"); err != nil || got != "1.2.4" {
				t.Fatalf("previous selection changed: %s %v", got, err)
			}
		})
	}
}

func TestDetachedRemovalRechecksAndRetainsFailures(t *testing.T) {
	for _, change := range []string{"replace-directory", "unknown-file", "payload", "partial-cleanup"} {
		t.Run(change, func(t *testing.T) {
			f := installedPair(t)
			parent := filepath.Dir(f.b.root("picoclaw"))
			source := filepath.Join(f.b.root("picoclaw"), "versions", "1.2.3")
			var detached string
			f.b.checkpoint = func(name string) error {
				if change == "replace-directory" && name == "before-removal-detach" {
					if err := os.Rename(source, filepath.Join(parent, "saved-version")); err != nil {
						return err
					}
					if err := os.Mkdir(source, 0700); err != nil {
						return err
					}
					return os.WriteFile(filepath.Join(source, "personal-note"), []byte("preserve"), 0600)
				}
				if name == "after-removal-detach" {
					paths, err := filepath.Glob(filepath.Join(parent, ".atm-ai-remove-*", "version"))
					if err != nil || len(paths) != 1 {
						t.Fatalf("missing detached version: %v %v", paths, err)
					}
					detached = paths[0]
					switch change {
					case "unknown-file":
						return os.WriteFile(filepath.Join(detached, "personal-note"), []byte("preserve"), 0600)
					case "payload":
						return os.WriteFile(filepath.Join(detached, "tool"), []byte("changed data"), 0755)
					}
				}
				if change == "partial-cleanup" && name == "removed-tool" {
					return errors.New("synthetic cleanup interruption")
				}
				return nil
			}
			err := f.run(context.Background(), "remove", "--provider", "picoclaw", "--version", "1.2.3")
			if err == nil || detached == "" || !strings.Contains(err.Error(), filepath.Dir(detached)) {
				t.Fatalf("missing retained-recovery report: %v %s", err, detached)
			}
			name, want := "personal-note", "preserve"
			switch change {
			case "payload":
				name, want = "tool", "changed data"
			case "partial-cleanup":
				name = "receipt.json"
				if _, err := os.Lstat(filepath.Join(detached, "tool")); !os.IsNotExist(err) {
					t.Fatalf("partial removal did not reach the expected interruption: %v", err)
				}
			}
			data, err := os.ReadFile(filepath.Join(detached, name))
			if err != nil || (change != "partial-cleanup" && string(data) != want) {
				t.Fatalf("detached data was deleted after failed removal: %s %v", data, err)
			}
			if got, err := f.b.current("picoclaw"); err != nil || got != "1.2.4" {
				t.Fatalf("active selection changed: %s %v", got, err)
			}
		})
	}
}

func TestTarDownloadUsesDeclaredSizeLimit(t *testing.T) {
	f := newFixture(t, "amd64")
	a := f.release(t, "picoclaw", "1.2.3", nil)
	a.Size--
	stage := t.TempDir()
	if err := os.Chmod(stage, 0700); err != nil {
		t.Fatal(err)
	}
	if err := f.b.extractBinary(context.Background(), a, stage, filepath.Join(stage, "tool")); err == nil {
		t.Fatal("archive exceeding declared size was accepted")
	}
}
