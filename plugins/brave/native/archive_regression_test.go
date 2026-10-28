//go:build linux

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

type archiveTestEntry struct {
	h    tar.Header
	data string
}

func archiveTestBytes(t *testing.T, entries ...archiveTestEntry) []byte {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		if err := tw.WriteHeader(&e.h); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(tw, e.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func archiveTestFile(t *testing.T, data []byte) *os.File {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "archive-*.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	if _, err = f.Write(data); err != nil {
		t.Fatal(err)
	}
	return f
}

func independentArchiveInventory(t *testing.T, dir string) map[string][32]byte {
	t.Helper()
	result := map[string][32]byte{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		result[filepath.ToSlash(rel)] = sha256.Sum256(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestArchiveIndependentRoundTripPortableNames(t *testing.T) {
	source, dest := t.TempDir(), t.TempDir()
	for name, value := range map[string]string{
		"Default/Bookmarks":                   "default bookmarks",
		"Profile 12/Bookmarks":                "multi-digit profile",
		"Default/caf\u00e9.json":              "Unicode filename",
		"Default/" + strings.Repeat("x", 101): "long component",
		"Local State":                         "private state",
	} {
		p := filepath.Join(source, name)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	in, err := os.OpenRoot(source)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	entries, err := gather(context.Background(), in, false)
	if err != nil {
		t.Fatal(err)
	}
	f := archiveTestFile(t, nil)
	if err := writeArchive(context.Background(), f, in, entries, false); err != nil {
		t.Fatal(err)
	}
	if err := scanArchive(context.Background(), f, nil, "Brave-Browser"); err != nil {
		t.Fatalf("own backup cannot be read: %v", err)
	}
	out, err := os.OpenRoot(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	if err := scanArchive(context.Background(), f, out, "Brave-Browser"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(independentArchiveInventory(t, source), independentArchiveInventory(t, dest)) {
		t.Fatal("round-trip changed profile contents")
	}
}

func TestArchiveIndependentHostileInputs(t *testing.T) {
	root := archiveTestEntry{h: tar.Header{Name: "Brave-Browser/", Typeflag: tar.TypeDir, Mode: 0700}}
	file := archiveTestEntry{h: tar.Header{Name: "Brave-Browser/Default/Bookmarks", Typeflag: tar.TypeReg, Mode: 0600, Size: 4}, data: "data"}
	for _, tc := range []struct {
		name string
		h    tar.Header
	}{
		{"traversal", tar.Header{Name: "Brave-Browser/../../escape", Typeflag: tar.TypeReg}},
		{"absolute", tar.Header{Name: "/outside", Typeflag: tar.TypeReg}},
		{"backslash", tar.Header{Name: "Brave-Browser/..\\escape", Typeflag: tar.TypeReg}},
		{"symlink", tar.Header{Name: "Brave-Browser/link", Typeflag: tar.TypeSymlink, Linkname: "../../outside"}},
		{"hardlink", tar.Header{Name: "Brave-Browser/link", Typeflag: tar.TypeLink, Linkname: "Brave-Browser/Default/Bookmarks"}},
		{"fifo", tar.Header{Name: "Brave-Browser/fifo", Typeflag: tar.TypeFifo}},
		{"device", tar.Header{Name: "Brave-Browser/device", Typeflag: tar.TypeChar}},
		{"special-mode", tar.Header{Name: "Brave-Browser/suid", Typeflag: tar.TypeReg, Mode: 04755}},
		{"foreign-root", tar.Header{Name: "Other/file", Typeflag: tar.TypeReg}},
		{"singleton", tar.Header{Name: "Brave-Browser/SingletonLock", Typeflag: tar.TypeReg}},
		{"pax-xattr", tar.Header{Name: "Brave-Browser/x", Typeflag: tar.TypeReg, Xattrs: map[string]string{"user.unexpected": "value"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := archiveTestFile(t, archiveTestBytes(t, root, file, archiveTestEntry{h: tc.h}))
			if err := scanArchive(context.Background(), f, nil, "Brave-Browser"); err == nil {
				t.Fatal("hostile input accepted")
			}
		})
	}
	for name, entries := range map[string][]archiveTestEntry{
		"duplicate":         {root, file, file},
		"parent-file-late":  {root, file, {h: tar.Header{Name: "Brave-Browser/Default", Typeflag: tar.TypeReg}}},
		"parent-file-first": {root, {h: tar.Header{Name: "Brave-Browser/Default", Typeflag: tar.TypeReg}}, file},
		"empty-profile":     {root},
	} {
		t.Run(name, func(t *testing.T) {
			if err := scanArchive(context.Background(), archiveTestFile(t, archiveTestBytes(t, entries...)), nil, "Brave-Browser"); err == nil {
				t.Fatal("invalid archive accepted")
			}
		})
	}
	valid := archiveTestBytes(t, root, file)
	corrupt := append([]byte(nil), valid...)
	corrupt[len(corrupt)-8] ^= 1
	for name, data := range map[string][]byte{
		"checksum":     corrupt,
		"truncated":    valid[:len(valid)-4],
		"concatenated": append(append([]byte(nil), valid...), valid...),
		"trailing":     append(append([]byte(nil), valid...), 'x'),
	} {
		t.Run(name, func(t *testing.T) {
			if err := scanArchive(context.Background(), archiveTestFile(t, data), nil, "Brave-Browser"); err == nil {
				t.Fatal("invalid gzip accepted")
			}
		})
	}
}

func TestArchiveIndependentManyEntriesAndCancellation(t *testing.T) {
	entries := []archiveTestEntry{{h: tar.Header{Name: "Brave-Browser/", Typeflag: tar.TypeDir, Mode: 0700}}}
	for i := 0; i < 20000; i++ {
		entries = append(entries, archiveTestEntry{h: tar.Header{Name: "Brave-Browser/files/" + strconv.Itoa(i), Typeflag: tar.TypeReg, Mode: 0600}})
	}
	f := archiveTestFile(t, archiveTestBytes(t, entries...))
	if err := scanArchive(context.Background(), f, nil, "Brave-Browser"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := scanArchive(ctx, f, nil, "Brave-Browser"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled scan: %v", err)
	}
}
