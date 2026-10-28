//go:build linux

package main

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"hash/crc32"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AtomyCloud/atm/internal/protocol"
	"github.com/ulikunitz/xz"
)

func TestXZDictionaryPreflightEveryActualBlock(t *testing.T) {
	var buf bytes.Buffer
	zw, err := (xz.WriterConfig{DictCap: 4096, BlockSize: 64}).NewWriter(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = zw.Write(bytes.Repeat([]byte("fixture block payload"), 20)); err != nil {
		t.Fatal(err)
	}
	if err = zw.Close(); err != nil {
		t.Fatal(err)
	}
	original := buf.Bytes()
	headerSize := (int(original[12]) + 1) * 4
	header := original[12 : 12+headerSize]
	offsets := []int{}
	for start := 12; start < len(original); {
		index := bytes.Index(original[start:], header)
		if index < 0 {
			break
		}
		offset := start + index
		offsets = append(offsets, offset)
		start = offset + headerSize
	}
	if len(offsets) < 2 {
		t.Fatal("fixture lacks multiple actual block headers")
	}
	for _, which := range []int{-1, 0, 1} {
		t.Run(fmt.Sprint(which), func(t *testing.T) {
			data := append([]byte(nil), original...)
			if which >= 0 {
				start := offsets[which]
				h := data[start : start+headerSize]
				if h[1] != 0 || h[2] != 0x21 || h[3] != 1 {
					t.Fatal("unexpected fixture header")
				}
				h[4] = 40
				binary.LittleEndian.PutUint32(h[headerSize-4:], crc32.ChecksumIEEE(h[:headerSize-4]))
			}
			name := filepath.Join(t.TempDir(), "archive.xz")
			write(t, name, data, 0600)
			f, err := os.Open(name)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			err = preflightXZ(context.Background(), f)
			if which < 0 {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "dictionary") {
				t.Fatalf("malicious block %d accepted or wrong failure: %v", which, err)
			}
		})
	}
}
func TestXZConcatCorruptionAndCancellation(t *testing.T) {
	good := archiveItems(t, []tarItem{{"flutter/file", "content", "", tar.TypeReg, 0644}})
	for _, kind := range []string{"concat", "footer", "header", "cancel", "short"} {
		t.Run(kind, func(t *testing.T) {
			data := append([]byte(nil), good...)
			ctx := context.Background()
			switch kind {
			case "concat":
				data = append(data, good...)
			case "footer":
				data[len(data)-1] ^= 1
			case "header":
				data[8] ^= 1
			case "short":
				data = data[:16]
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			name := filepath.Join(t.TempDir(), "archive.xz")
			write(t, name, data, 0600)
			root, err := os.OpenRoot(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			if err = extractArchive(ctx, name, root, "flutter"); err == nil {
				t.Fatal("unsafe stream accepted")
			}
		})
	}
}
func TestSafeInternalHardlinks(t *testing.T) {
	items := []tarItem{{"flutter/file", "data", "", tar.TypeReg, 0644}, {"flutter/hard", "", "flutter/file", tar.TypeLink, 0644}, {"flutter/link", "", "hard", tar.TypeSymlink, 0777}}
	name := filepath.Join(t.TempDir(), "archive.xz")
	write(t, name, archiveItems(t, items), 0600)
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err = extractArchive(context.Background(), name, root, "flutter"); err != nil {
		t.Fatal(err)
	}
	f, err := root.Open("link")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil || string(data) != "data" {
		t.Fatal(string(data), err)
	}
}

func TestCancelledInstallCleansStage(t *testing.T) {
	b := newTestBackend(t)
	legacy(t, b, "3.40.0")
	if err := action(b, "use", "3.40.0"); err != nil {
		t.Fatal(err)
	}
	row := rowFor("3.47.8", b.platform, nil)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/releases_linux.json" {
			_ = json.NewEncoder(w).Encode(map[string]any{"base_url": officialBase, "releases": []release{row}})
			return
		}
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	b.base = srv.URL
	b.client = srv.Client()
	b.req.Operation = "install"
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := b.execute(ctx); err == nil {
		t.Fatal("cancellation ignored")
	}
	assertCurrent(t, b, "3.40.0")
	assertAbsent(t, filepath.Join(b.root, "3.47.8"))
	assertCleanStage(t, b)
}
func TestOwnershipRefusesEditsAndMalformedReceipts(t *testing.T) {
	for _, kind := range []string{"file", "mode", "missing", "receipt", "receipt-link", "launcher", "extra"} {
		t.Run(kind, func(t *testing.T) {
			b := newTestBackend(t)
			releasesFixture(t, b, "3.47.8")
			if err := action(b, "install"); err != nil {
				t.Fatal(err)
			}
			legacy(t, b, "3.40.0")
			if err := action(b, "use", "3.40.0"); err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(b.root, "3.47.8")
			name := filepath.Join(dir, "packages/flutter/README.md")
			switch kind {
			case "file":
				write(t, name, []byte("my changes"), 0600)
			case "mode":
				if err := os.Chmod(name, 0700); err != nil {
					t.Fatal(err)
				}
			case "missing":
				if err := os.Remove(name); err != nil {
					t.Fatal(err)
				}
			case "receipt":
				write(t, filepath.Join(dir, receiptName), []byte("{}"), 0600)
			case "receipt-link":
				if err := os.Remove(filepath.Join(dir, receiptName)); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("packages/flutter/README.md", filepath.Join(dir, receiptName)); err != nil {
					t.Fatal(err)
				}
			case "launcher":
				write(t, filepath.Join(dir, "bin/flutter"), []byte("#!/bin/sh\nexit 77\n"), 0700)
			case "extra":
				write(t, filepath.Join(dir, "user-project/data"), []byte("mine"), 0600)
			}
			for _, op := range []string{"remove", "uninstall"} {
				args := []string{"3.47.8"}
				if op == "uninstall" {
					args = nil
				}
				err := action(b, op, args...)
				if kind == "receipt-link" && op == "uninstall" {
					if err != nil {
						t.Fatal(err)
					}
				} else if err == nil {
					t.Fatal("changed SDK removed", op)
				}
			}
			if _, err := os.Stat(dir); err != nil {
				t.Fatal("changed SDK lost")
			}
			assertCurrent(t, b, "3.40.0")
		})
	}
}
func TestUnsafePathsLocksAndProfiles(t *testing.T) {
	for _, kind := range []string{"root", "ancestor", "current", "version", "bin", "profile", "lock"} {
		t.Run(kind, func(t *testing.T) {
			b := newTestBackend(t)
			legacy(t, b, "3.47.8")
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
				target = filepath.Join(b.root, "3.47.8")
			case "bin":
				target = filepath.Join(b.root, "3.47.8/bin")
			case "profile":
				if err := action(b, "use", "3.47.8"); err != nil {
					t.Fatal(err)
				}
				target = filepath.Join(b.home, ".bashrc")
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
			op, args := "use", []string{"3.47.8"}
			if kind == "profile" {
				op = "integrate"
				args = nil
			}
			if err := action(b, op, args...); err == nil {
				t.Fatal("unsafe path accepted")
			}
			entries, err := os.ReadDir(outside)
			if err != nil || len(entries) != 0 {
				t.Fatal("outside path changed")
			}
		})
	}
}
func TestProfileRollbackAndMalformedBlocks(t *testing.T) {
	for _, bad := range []string{profileStart, profileEnd, profileEnd + "\n" + profileStart, profileStart + "\n" + profileStart + "\n" + profileEnd, "x" + profileStart + "\n" + profileEnd} {
		if _, err := profileContent([]byte(bad), ""); err == nil {
			t.Fatal("bad block accepted")
		}
	}
	b := newTestBackend(t)
	write(t, filepath.Join(b.home, ".profile"), []byte("user\n"), 0640)
	plans, err := b.profilePlans(false)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(b.home, ".bashrc"), []byte("concurrent"), 0600)
	if _, err := applyProfiles(context.Background(), plans); err == nil {
		t.Fatal("concurrent edits overwritten")
	}
	data, _ := os.ReadFile(filepath.Join(b.home, ".profile"))
	if string(data) != "user\n" {
		t.Fatal("rollback failed")
	}
}
func TestProtocolAndNoSubprocessImplementation(t *testing.T) {
	b := newTestBackend(t)
	for _, op := range []string{"menu", "status", "use"} {
		b.req.Operation = op
		b.req.Arguments = nil
		data, _ := json.Marshal(b.req)
		var out bytes.Buffer
		if err := serve(context.Background(), bytes.NewReader(data), &out); err != nil {
			t.Fatal(err)
		}
		stream := protocol.Stream{RequestID: b.req.RequestID}
		for _, line := range bytes.Split(bytes.TrimSpace(out.Bytes()), []byte("\n")) {
			if _, err := stream.Accept(line); err != nil {
				t.Fatal(err)
			}
		}
		if stream.Result == nil {
			t.Fatal("missing result")
		}
	}
	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range file.Imports {
			if imp.Path.Value == `"os/exec"` {
				t.Fatal("production backend must not launch subprocesses")
			}
		}
	}
}

func TestRegenerableVersionMetadataMissing(t *testing.T) {
	b := newTestBackend(t)
	releasesFixture(t, b, "3.47.8")
	if err := action(b, "install"); err != nil {
		t.Fatal(err)
	}
	legacy(t, b, "3.40.0")
	dir := filepath.Join(b.root, "3.47.8")
	if err := os.Remove(filepath.Join(dir, "bin/cache/flutter.version.json")); err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"3.40.0", "3.47.8", "3.40.0"} {
		if err := action(b, "use", v); err != nil {
			t.Fatal("cache invalidation blocked selection", err)
		}
	}
	if err := action(b, "status"); err != nil {
		t.Fatal(err)
	}
	if err := action(b, "remove", "3.47.8"); err == nil {
		t.Fatal("changed SDK should not be deleted")
	}
	write(t, filepath.Join(dir, "bin/cache/flutter.version.json"), []byte(`{"frameworkVersion":"3.99.0"}`), 0600)
	if err := action(b, "use", "3.47.8"); err == nil {
		t.Fatal("changed version cache accepted")
	}
	if err := os.Remove(filepath.Join(dir, "bin/cache/flutter.version.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, receiptName)); err != nil {
		t.Fatal(err)
	}
	if err := action(b, "use", "3.47.8"); err == nil {
		t.Fatal("unowned version without version metadata accepted")
	}
}

func TestUpdatePreservesChannelAndExplicitSwitch(t *testing.T) {
	b := newTestBackend(t)
	rows := []release{}
	payloads := map[string][]byte{}
	for _, v := range []string{"3.50.0-0.1.pre", "3.50.0-0.2.pre", "4.0.0"} {
		data := archiveItems(t, sdkItems(v, b.platform))
		r := rowFor(v, b.platform, data)
		if strings.Contains(v, "pre") {
			r.Channel = "beta"
			r.Archive = strings.ReplaceAll(r.Archive, "stable", "beta")
		}
		rows = append(rows, r)
		payloads[r.Archive] = data
	}
	fixture(t, b, rows, payloads)
	if err := action(b, "install", "--channel", "beta", "--version", "3.50.0-0.1.pre"); err != nil {
		t.Fatal(err)
	}
	if err := action(b, "update"); err != nil {
		t.Fatal(err)
	}
	assertCurrent(t, b, "3.50.0-0.2.pre")
	if err := action(b, "update", "--channel=stable"); err != nil {
		t.Fatal(err)
	}
	assertCurrent(t, b, "4.0.0")
	legacy(t, b, "3.40.0")
	if err := action(b, "use", "3.40.0"); err != nil {
		t.Fatal(err)
	}
	if err := action(b, "update"); err == nil {
		t.Fatal("guessed unowned channel")
	}
	assertCurrent(t, b, "3.40.0")
	if err := action(b, "update", "--channel", "stable"); err != nil {
		t.Fatal(err)
	}
	assertCurrent(t, b, "4.0.0")
}
