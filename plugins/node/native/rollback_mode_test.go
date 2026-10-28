package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestProfileRollbackPreservesExternalPermissions(t *testing.T) {
	for _, existing := range []bool{false, true} {
		name := map[bool]string{false: "new", true: "existing"}[existing]
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "profile")
			var before []byte
			if existing {
				before = []byte("before\n")
				if err := os.WriteFile(path, before, 0600); err != nil {
					t.Fatal(err)
				}
			}
			p := profilePlan{path: path, before: before, after: []byte("after\n"), mode: 0600, exists: existing}
			rollback, err := applyProfiles(context.Background(), []profilePlan{p})
			if err != nil {
				t.Fatal(err)
			}
			if err = os.Chmod(path, 0640); err != nil {
				t.Fatal(err)
			}
			if err = rollback(); err == nil {
				t.Fatal("rollback replaced externally changed permissions")
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0640 {
				t.Fatalf("permissions changed: %v", info.Mode())
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != "after\n" {
				t.Fatalf("profile changed: %q, %v", data, err)
			}
		})
	}
}
