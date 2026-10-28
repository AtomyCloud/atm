//go:build linux

package main

import (
	"context"
	"os"
	"testing"
)

// Optional provider compatibility check: parse public bytes, never execute them.
func TestOfficialPublicArchive(t *testing.T) {
	path := os.Getenv("ATM_TEST_ZSH_OFFICIAL_ARCHIVE")
	if path == "" {
		t.Skip("optional public provider artifact not supplied")
	}
	commit := os.Getenv("ATM_TEST_ZSH_OFFICIAL_COMMIT")
	if metadata := os.Getenv("ATM_TEST_ZSH_OFFICIAL_METADATA"); metadata != "" {
		data, err := readFile(metadata, 1<<20, false)
		if err != nil {
			t.Fatal(err)
		}
		got, err := parseCommit(data)
		if err != nil || got != commit {
			t.Fatalf("metadata/archive commit mismatch: %q, %v", got, err)
		}
	}
	data, err := readFile(path, 32<<20, false)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := officialEntries(context.Background(), data, commit)
	if err != nil {
		t.Fatal(err)
	}
	files := 0
	for _, en := range entries {
		if !en.Dir {
			files++
			if en.Hash != digest(en.Data) {
				t.Fatal("materialized file hash mismatch", en.Path)
			}
		}
	}
	if files == 0 {
		t.Fatal("empty official archive")
	}
	t.Logf("parsed commit %s: %d entries, %d regular files; no code executed", commit, len(entries), files)
}
