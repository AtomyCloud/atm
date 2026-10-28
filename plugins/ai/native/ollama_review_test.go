//go:build linux

package main

import (
	"archive/tar"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// Independent bundle fixture: the native test ELF and a shared-library alias
// are inspected as data; neither is ever executed.
func reviewOllamaBundle(t *testing.T, version string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw, err := zstd.NewWriter(&buf, zstd.WithEncoderConcurrency(1))
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(zw)
	for _, item := range []struct {
		name, link string
		mode       int64
		kind       byte
		data       []byte
	}{
		{"bin", "", 0755, tar.TypeDir, nil},
		{"bin/ollama", "", 0755, tar.TypeReg, fakeELF("amd64", version, false)},
		{"lib", "", 0755, tar.TypeDir, nil},
		{"lib/ollama", "", 0755, tar.TypeDir, nil},
		{"lib/ollama/libggml-base.so.1", "", 0644, tar.TypeReg, []byte("synthetic library " + version)},
		{"lib/ollama/libggml-base.so", "libggml-base.so.1", 0777, tar.TypeSymlink, nil},
	} {
		if err = tw.WriteHeader(&tar.Header{Name: item.name, Linkname: item.link, Mode: item.mode, Typeflag: item.kind, Size: int64(len(item.data))}); err != nil {
			t.Fatal(err)
		}
		if _, err = tw.Write(item.data); err != nil {
			t.Fatal(err)
		}
	}
	if err = tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err = zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestOllamaReviewModifiedBundleRefusesDestruction(t *testing.T) {
	for _, change := range []string{"library-content", "unknown-file", "library-link"} {
		t.Run(change, func(t *testing.T) {
			f := newFixture(t, "amd64")
			f.release(t, "ollama", "1.2.3", reviewOllamaBundle(t, "1.2.3"))
			mustRun(t, f, "install", "--provider", "ollama", "--trust-download")
			root := f.b.root("ollama")
			var library string
			if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
				if err == nil && entry.Name() == "libggml-base.so.1" {
					library = path
				}
				return err
			}); err != nil || library == "" {
				t.Fatalf("full library payload missing: %v", err)
			}
			alias := filepath.Join(filepath.Dir(library), "libggml-base.so")
			if data, err := os.ReadFile(alias); err != nil || string(data) != "synthetic library 1.2.3" {
				t.Fatalf("library alias lost: %q %v", data, err)
			}
			changed := library
			switch change {
			case "library-content":
				if err := os.WriteFile(library, []byte("modified"), 0600); err != nil {
					t.Fatal(err)
				}
			case "unknown-file":
				changed = filepath.Join(filepath.Dir(library), "personal-notes")
				writeFixture(t, changed, []byte("keep"), 0600)
			case "library-link":
				changed = alias
				if err := os.Remove(alias); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("missing.so", alias); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.Readlink(filepath.Join(root, "current"))
			if err != nil {
				t.Fatal(err)
			}
			calls := f.calls
			for _, op := range []string{"use", "uninstall"} {
				args := []string{"--provider", "ollama"}
				if op == "use" {
					args = append(args, "--version", "1.2.3")
				}
				if err := f.run(context.Background(), op, args...); err == nil {
					t.Fatalf("%s accepted %s", op, change)
				}
				if _, err := os.Lstat(changed); err != nil {
					t.Fatalf("changed entry lost: %v", err)
				}
				if after, err := os.Readlink(filepath.Join(root, "current")); err != nil || after != before {
					t.Fatalf("active selection changed on refusal: %q %v", after, err)
				}
			}
			if f.calls != calls {
				t.Fatal("local preservation checks requested network")
			}
		})
	}
}

func TestOllamaReviewStagedSelectionIsRevalidated(t *testing.T) {
	for _, change := range []string{"foreign-target", "regular-file", "hardlink"} {
		t.Run(change, func(t *testing.T) {
			f := newFixture(t, "amd64")
			for _, v := range []string{"1.2.3", "1.2.4"} {
				f.release(t, "ollama", v, reviewOllamaBundle(t, v))
				mustRun(t, f, "install", "--provider", "ollama", "--trust-download")
			}
			current := filepath.Join(f.b.root("ollama"), "current")
			before, err := os.Readlink(current)
			if err != nil {
				t.Fatal(err)
			}
			f.b.checkpoint = func(point string) error {
				if point != "ollama-before-activation" {
					return nil
				}
				stages, err := filepath.Glob(filepath.Join(filepath.Dir(f.b.root("ollama")), ".atm-ollama-select-*", "current"))
				if err != nil || len(stages) != 1 {
					t.Fatalf("selection fixture: %v %v", stages, err)
				}
				link := stages[0]
				if change == "hardlink" {
					return os.Link(link, filepath.Join(filepath.Dir(link), "unexpected-alias"))
				}
				if err := os.Remove(link); err != nil {
					return err
				}
				if change == "regular-file" {
					return os.WriteFile(link, []byte("not a selection link"), 0600)
				}
				return os.Symlink("/outside/foreign-ollama", link)
			}
			if err := f.run(context.Background(), "use", "--provider", "ollama", "--version", "1.2.3"); err == nil {
				t.Fatal("modified staged selection reported success")
			}
			if after, err := os.Readlink(current); err != nil || after != before {
				t.Fatalf("previous selection was replaced: %q %v", after, err)
			}
		})
	}
}

func TestOllamaReviewValidTarRejectsTrailingData(t *testing.T) {
	f := newFixture(t, "amd64")
	valid := reviewOllamaBundle(t, "1.2.3")
	decoder, err := zstd.NewReader(nil, zstd.WithDecoderConcurrency(1))
	if err != nil {
		t.Fatal(err)
	}
	defer decoder.Close()
	raw, err := decoder.DecodeAll(valid, nil)
	if err != nil {
		t.Fatal(err)
	}
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1))
	if err != nil {
		t.Fatal(err)
	}
	defer encoder.Close()
	invalid := encoder.EncodeAll(append(raw, []byte("nonzero data after a complete valid tar")...), nil)
	f.release(t, "ollama", "1.2.3", invalid)
	if err = f.run(context.Background(), "install", "--provider", "ollama", "--trust-download"); err == nil {
		t.Fatal("valid bundle with nonzero trailing data was installed")
	}
	if _, err = os.Lstat(f.b.root("ollama")); !os.IsNotExist(err) {
		t.Fatalf("invalid archive published state: %v", err)
	}
	f.release(t, "ollama", "1.2.3", valid)
	mustRun(t, f, "install", "--provider", "ollama", "--trust-download")
}
