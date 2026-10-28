//go:build linux

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRollbackPreservesConcurrentModeChanges(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "new", true: "existing"}[existing], func(t *testing.T) {
			name := filepath.Join(t.TempDir(), "profile")
			before := []byte(nil)
			if existing {
				before = []byte("before\n")
				write(t, name, before, 0600)
			}
			p := profilePlan{path: name, before: before, after: []byte("after\n"), mode: 0600, exists: existing}
			rollback, err := applyProfiles(context.Background(), []profilePlan{p})
			if err != nil {
				t.Fatal(err)
			}
			if err = os.Chmod(name, 0640); err != nil {
				t.Fatal(err)
			}
			if err = rollback(); err == nil {
				t.Fatal("rollback overwrote concurrent chmod")
			}
			info, err := os.Stat(name)
			if err != nil || info.Mode().Perm() != 0640 {
				t.Fatal(info, err)
			}
			contents, err := os.ReadFile(name)
			if err != nil || string(contents) != "after\n" {
				t.Fatal(string(contents), err)
			}
		})
	}
}
