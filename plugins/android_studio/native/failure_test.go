//go:build linux && amd64

package main

import (
	"bytes"
	"context"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type callbackContext struct {
	context.Context
	check func() error
}

func (c callbackContext) Err() error { return c.check() }

func TestDesktopRollbackBeforeCurrentCommit(t *testing.T) {
	b := newTestBackend(t)
	a := installFixture(t, b, 0, false)
	other := installFixture(t, b, 1, true)
	if err := action(b, "use", a.target()); err != nil {
		t.Fatal(err)
	}
	if err := action(b, "integrate"); err != nil {
		t.Fatal(err)
	}
	desktop := filepath.Join(b.home, ".local/share/applications", desktopFileName)
	state := filepath.Join(b.root, desktopReceiptName)
	before, _ := os.ReadFile(desktop)
	stateBefore, _ := os.ReadFile(state)
	ctx := callbackContext{Context: context.Background(), check: func() error {
		now, err := os.ReadFile(state)
		if err == nil && !bytes.Equal(now, stateBefore) {
			return context.Canceled
		}
		return nil
	}}
	if err := b.withLock(func() error { return b.activate(ctx, other.dir()) }); err == nil {
		t.Fatal("ignored cancellation before current commit")
	}
	assertCurrent(t, b, a.dir())
	after, _ := os.ReadFile(desktop)
	stateAfter, _ := os.ReadFile(state)
	if !bytes.Equal(before, after) || !bytes.Equal(stateBefore, stateAfter) {
		t.Fatal("desktop rollback incomplete")
	}
	assertCleanStage(t, b)
}
func TestDesktopUnownedChangedAndMissingProtection(t *testing.T) {
	for _, kind := range []string{"unowned", "changed", "mode", "missing", "state", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			b := newTestBackend(t)
			a := installFixture(t, b, 0, false)
			other := installFixture(t, b, 1, true)
			if err := action(b, "use", a.target()); err != nil {
				t.Fatal(err)
			}
			desktop := filepath.Join(b.home, ".local/share/applications", desktopFileName)
			if kind == "unowned" {
				write(t, desktop, []byte("user entry"), 0644)
				if err := action(b, "integrate"); err == nil {
					t.Fatal("unowned desktop replaced")
				}
				data, _ := os.ReadFile(desktop)
				if string(data) != "user entry" {
					t.Fatal("unowned entry lost")
				}
				assertAbsent(t, filepath.Join(b.home, ".profile"))
				return
			}
			if err := action(b, "integrate"); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "changed":
				write(t, desktop, []byte("user entry"), 0644)
			case "mode":
				if err := os.Chmod(desktop, 0600); err != nil {
					t.Fatal(err)
				}
			case "missing":
				if err := os.Remove(desktop); err != nil {
					t.Fatal(err)
				}
			case "state":
				write(t, filepath.Join(b.root, desktopReceiptName), []byte("{}"), 0600)
			case "symlink":
				if err := os.Remove(desktop); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(t.TempDir(), "entry"), desktop); err != nil {
					t.Fatal(err)
				}
			}
			if err := action(b, "use", other.target()); err == nil {
				t.Fatal("unsafe configured desktop accepted during switch")
			}
			assertCurrent(t, b, a.dir())
			if kind != "missing" {
				if err := action(b, "uninstall"); err == nil {
					t.Fatal("unsafe desktop deleted")
				}
				assertCurrent(t, b, a.dir())
			}
		})
	}
}
func TestEditedSDKOwnershipAndIdentity(t *testing.T) {
	for _, kind := range []string{"file", "mode", "missing", "receipt", "launcher", "product", "build"} {
		t.Run(kind, func(t *testing.T) {
			b := newTestBackend(t)
			a := installFixture(t, b, 0, false)
			other := installFixture(t, b, 1, false)
			dir := filepath.Join(b.root, a.dir())
			file := filepath.Join(dir, "lib/source")
			switch kind {
			case "file":
				write(t, file, []byte("edited"), 0600)
			case "mode":
				if err := os.Chmod(file, 0700); err != nil {
					t.Fatal(err)
				}
			case "missing":
				if err := os.Remove(file); err != nil {
					t.Fatal(err)
				}
			case "receipt":
				write(t, filepath.Join(dir, receiptName), []byte("{}"), 0600)
			case "launcher":
				write(t, filepath.Join(dir, "bin/studio.sh"), []byte("#!/bin/sh\nexit 88\n"), 0700)
			case "product":
				write(t, filepath.Join(dir, "product-info.json"), []byte("{}"), 0600)
			case "build":
				write(t, filepath.Join(dir, "build.txt"), []byte("AI-999.999.999"), 0600)
			}
			for _, op := range []string{"remove", "uninstall"} {
				args := []string{a.target()}
				if op == "uninstall" {
					args = nil
				}
				if err := action(b, op, args...); err == nil {
					t.Fatal("modified payload removed", op)
				}
			}
			assertCurrent(t, b, other.dir())
			if _, err := os.Stat(dir); err != nil {
				t.Fatal(err)
			}
			if kind == "launcher" || kind == "product" || kind == "build" || kind == "receipt" {
				if err := action(b, "use", a.target()); err == nil {
					t.Fatal("changed identity selected")
				}
			}
		})
	}
}
func TestUnsafePathsAndLocks(t *testing.T) {
	for _, kind := range []string{"root", "ancestor", "current", "version", "bin", "profile", "desktop-parent", "lock"} {
		t.Run(kind, func(t *testing.T) {
			b := newTestBackend(t)
			a := catalog[0]
			legacy(t, b, a)
			outside := t.TempDir()
			target := ""
			switch kind {
			case "root":
				target = b.root
			case "ancestor":
				target = b.req.AppsDir
			case "current":
				target = filepath.Join(b.root, "current")
			case "version":
				target = filepath.Join(b.root, a.dir())
			case "bin":
				target = filepath.Join(b.root, a.dir(), "bin")
			case "profile":
				if err := action(b, "use", a.target()); err != nil {
					t.Fatal(err)
				}
				target = filepath.Join(b.home, ".bashrc")
			case "desktop-parent":
				if err := action(b, "use", a.target()); err != nil {
					t.Fatal(err)
				}
				target = filepath.Join(b.home, ".local")
			case "lock":
				if err := os.Mkdir(filepath.Join(b.root, ".atm-native-lock"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if target != "" {
				if _, err := os.Lstat(target); err == nil {
					if err = os.Rename(target, target+"-saved"); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Symlink(outside, target); err != nil {
					t.Fatal(err)
				}
			}
			op, args := "use", []string{a.target()}
			if kind == "profile" || kind == "desktop-parent" {
				op = "integrate"
				args = nil
			}
			if err := action(b, op, args...); err == nil {
				t.Fatal("unsafe path accepted")
			}
			entries, err := os.ReadDir(outside)
			if err != nil || len(entries) != 0 {
				t.Fatal("outside root modified")
			}
		})
	}
}
func TestConflictingArtifactPreserved(t *testing.T) {
	b := newTestBackend(t)
	a := installFixture(t, b, 0, false)
	old, _ := os.ReadFile(filepath.Join(b.root, a.dir(), receiptName))
	a.SHA256 = strings.Repeat("f", 64)
	if err := action(b, "install", installArgs(a)...); err == nil {
		t.Fatal("replaced same-version bytes")
	}
	now, _ := os.ReadFile(filepath.Join(b.root, a.dir(), receiptName))
	if !bytes.Equal(old, now) {
		t.Fatal("receipt changed")
	}
}
func TestIdentityHashCancellation(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "file"), []byte("data"), 0600)
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := hashFile(ctx, root, "file"); err == nil {
		t.Fatal("identity hash ignored cancellation")
	}
}
func TestNoSubprocessImplementation(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			if imp.Path.Value == `"os/exec"` {
				t.Fatal("backend must not launch subprocesses")
			}
		}
	}
}
func TestSavedOfficialCatalogHashes(t *testing.T) {
	file := os.Getenv("ATM_TEST_STUDIO_ARCHIVE_HTML")
	if file == "" {
		t.Skip("optional official archive snapshot")
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range catalog {
		if !bytes.Contains(data, []byte(a.SHA256+" "+a.Package)) || !bytes.Contains(data, []byte(officialBase+a.Version+"/"+a.Package)) {
			t.Fatal("catalog differs from official snapshot", a.ID)
		}
	}
}

func TestStampedProductVersionLifecycle(t *testing.T) {
	b := newTestBackend(t)
	a := catalog[0]
	items := sdkItems(a, false)
	items[2].body = strings.Replace(items[2].body, `"version":"2025.3.4"`, `"version":"AI-253.12345.99"`, 1)
	a = fixture(t, b, a, archiveItems(t, items))
	if err := action(b, "install", installArgs(a)...); err != nil {
		t.Fatal(err)
	}
	rec, err := b.readReceipt(a.dir())
	if err != nil || rec.Identity.ProductVersion != "AI-253.12345.99" || rec.Identity.Build != "253.12345.99" {
		t.Fatal(rec.Identity, err)
	}
	for _, op := range []string{"status", "use", "integrate", "uninstall"} {
		args := []string(nil)
		if op == "use" {
			args = []string{a.target()}
		}
		if err := action(b, op, args...); err != nil {
			t.Fatal(op, err)
		}
	}
	assertAbsent(t, filepath.Join(b.root, a.dir()))
	legacy(t, b, catalog[0])
	write(t, filepath.Join(b.root, a.dir(), "product-info.json"), []byte(items[2].body), 0600)
	if err := action(b, "use", a.target()); err != nil {
		t.Fatal("legacy stamped SDK", err)
	}
	assertAbsent(t, filepath.Join(b.root, a.dir(), receiptName))
}

func TestStampedProductVersionMustMatchBuild(t *testing.T) {
	for _, value := range []string{"AI-999.999.999", "AI-invalid", "AI-AI-253.12345.99", "2024.1.1", "253.12345.99"} {
		t.Run(value, func(t *testing.T) {
			b := newTestBackend(t)
			a := catalog[0]
			items := sdkItems(a, false)
			items[2].body = strings.Replace(items[2].body, `"version":"2025.3.4"`, `"version":"`+value+`"`, 1)
			a = fixture(t, b, a, archiveItems(t, items))
			if err := action(b, "install", installArgs(a)...); err == nil {
				t.Fatal("inconsistent stamped product identity accepted")
			}
			assertAbsent(t, filepath.Join(b.root, a.dir()))
			assertCleanStage(t, b)
		})
	}
}

func TestUpstreamTwoComponentStampedBuild(t *testing.T) {
	// Upstream tools_test.py (f1fe893899010c13c03c03e4e959cd158e35b5c9)
	// verifies version AI-1234.3333 paired with buildNumber 1234.3333.
	b := newTestBackend(t)
	a := catalog[0]
	items := sdkItems(a, false)
	items[2].body = strings.ReplaceAll(items[2].body, "253.12345.99", "1234.3333")
	items[2].body = strings.Replace(items[2].body, `"version":"2025.3.4"`, `"version":"AI-1234.3333"`, 1)
	items[3].body = "AI-1234.3333\n"
	a = fixture(t, b, a, archiveItems(t, items))
	if err := action(b, "install", installArgs(a)...); err != nil {
		t.Fatal(err)
	}
	rec, err := b.readReceipt(a.dir())
	if err != nil || rec.Identity.ProductVersion != "AI-1234.3333" || rec.Identity.Build != "1234.3333" {
		t.Fatal(rec.Identity, err)
	}
}
