//go:build linux

package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

type statFixture struct {
	os.FileInfo
	stat *syscall.Stat_t
}

func (s statFixture) Sys() any { return s.stat }

func TestJournalOwnershipPolicy(t *testing.T) {
	uid := uint32(os.Geteuid())
	if !ownedByCurrentUser(statFixture{stat: &syscall.Stat_t{Uid: uid}}) {
		t.Fatal("current user refused")
	}
	if ownedByCurrentUser(statFixture{stat: &syscall.Stat_t{Uid: uid + 1}}) {
		t.Fatal("foreign owner accepted")
	}
}

func TestJournalDirectoryGuards(t *testing.T) {
	for _, mode := range []os.FileMode{0755, 0770, 0777} {
		t.Run(mode.String(), func(t *testing.T) {
			root := t.TempDir()
			if err := os.Chmod(root, 0700); err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(root, "state")
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(dir, mode); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "operation.json")
			if _, _, err := openJournal(path); err == nil {
				t.Fatal("nonprivate directory accepted")
			}
			if _, _, err := readRecord(path); err == nil {
				t.Fatal("read ignored directory permissions")
			}
			absent(t, filepath.Join(dir, ".native-lock"))
		})
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0777); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(root, 0700)
	dir := filepath.Join(root, "private")
	if err := journalDirectory(dir, true); err == nil {
		t.Fatal("writable parent accepted")
	}
	absent(t, dir)
	if err := trustedSystemFile("relative"); err == nil {
		t.Fatal("relative system command accepted")
	}
}

func TestAPTRegexFallbackNamesRefused(t *testing.T) {
	for _, name := range []string{"python3.pip", "python3.11", "lib.thing:amd64"} {
		if validPackage(name, "apt-get") {
			t.Fatalf("ambiguous APT name accepted: %s", name)
		}
		for _, action := range []string{"install", "remove"} {
			if _, err := planCommands(systemInfo{Manager: "apt-get"}, []string{name}, action, ""); err == nil {
				t.Fatalf("ambiguous %s plan accepted", action)
			}
		}
	}
}
