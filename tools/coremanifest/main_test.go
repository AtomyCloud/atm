package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/AtomyCloud/atm/internal/coreupdate"
)

type entry struct {
	name string
	data []byte
	mode os.FileMode
}

func hash(b []byte) string { return fmt.Sprintf("%x", sha256.Sum256(b)) }

func fixtureOptions(t *testing.T, goos, arch string) options {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return options{archive: filepath.Join(dir, "input"), version: "0.0.3", commit: strings.Repeat("a", 40), goos: goos, arch: arch, out: filepath.Join(dir, "output")}
}

func fixtureBinary(goos, arch string) []byte {
	b := make([]byte, 128)
	switch goos {
	case "linux":
		copy(b, "\x7fELF")
		b[4], b[5], b[6] = 2, 1, 1
		machine := uint16(62)
		if arch == "arm64" {
			machine = 183
		}
		binary.LittleEndian.PutUint16(b[16:], 2)
		binary.LittleEndian.PutUint16(b[18:], machine)
	case "windows":
		copy(b, "MZ")
		binary.LittleEndian.PutUint32(b[60:], 64)
		copy(b[64:], "PE\x00\x00")
		binary.LittleEndian.PutUint16(b[68:], 0x8664)
	case "darwin":
		binary.LittleEndian.PutUint32(b, 0xfeedfacf)
		cpu := uint32(0x01000007)
		if arch == "arm64" {
			cpu = 0x0100000c
		}
		binary.LittleEndian.PutUint32(b[4:], cpu)
		binary.LittleEndian.PutUint32(b[12:], 2)
	}
	return b
}

func fixtureEntries(t *testing.T, o options) []entry {
	t.Helper()
	name := "atm"
	if o.goos == "windows" {
		name += ".exe"
	}
	files := []entry{
		{name, fixtureBinary(o.goos, o.arch), 0755},
		{"LICENSE", []byte("license"), 0644},
		{"README.md", []byte("runtime guide"), 0644},
		{"core-api.json", []byte(`{"schema":1,"plugin_api":1,"update_protocol":1}`), 0644},
		{"bin/atm", []byte("#!/bin/sh\nexit 0\n"), 0755},
		{"lang/en-us.lang", []byte("key=value\n"), 0644},
	}
	hashes := map[string]string{}
	for _, f := range files {
		hashes[f.name] = hash(f.data)
	}
	release, err := json.Marshal(map[string]any{"schema": 1, "version": o.version, "commit": o.commit, "os": o.goos, "arch": o.arch, "migration": true, "files": hashes})
	if err != nil {
		t.Fatal(err)
	}
	return append(files, entry{"release.json", append(release, '\n'), 0644})
}

func writeArchive(t *testing.T, o options, files []entry, root string) []byte {
	t.Helper()
	var b bytes.Buffer
	if o.goos == "windows" {
		w := zip.NewWriter(&b)
		for _, f := range files {
			h := &zip.FileHeader{Name: root + "/" + f.name, Method: zip.Deflate}
			h.SetMode(f.mode)
			r, err := w.CreateHeader(h)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = r.Write(f.data); err != nil {
				t.Fatal(err)
			}
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
	} else {
		gz := gzip.NewWriter(&b)
		w := tar.NewWriter(gz)
		for _, f := range files {
			h := &tar.Header{Name: root + "/" + f.name, Mode: int64(f.mode.Perm()), Size: int64(len(f.data)), Typeflag: tar.TypeReg}
			if f.mode&os.ModeSymlink != 0 {
				h.Typeflag, h.Linkname, h.Size = tar.TypeSymlink, "README.md", 0
			}
			if err := w.WriteHeader(h); err != nil {
				t.Fatal(err)
			}
			if h.Typeflag == tar.TypeReg {
				if _, err := w.Write(f.data); err != nil {
					t.Fatal(err)
				}
			}
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(o.archive, b.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func makeFixture(t *testing.T, o options) ([]byte, []entry) {
	t.Helper()
	files := fixtureEntries(t, o)
	return writeArchive(t, o, files, "atm-"+o.version+"-"+o.goos+"-"+o.arch), files
}

func read(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func absent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected output %s: %v", path, err)
	}
}

func TestPrepareTargetsAndExactSigningInput(t *testing.T) {
	t.Setenv("PATH", "/nonexistent")
	for _, target := range []string{"linux/amd64", "linux/arm64", "windows/amd64", "darwin/amd64", "darwin/arm64"} {
		t.Run(target, func(t *testing.T) {
			goos, arch, _ := strings.Cut(target, "/")
			o := fixtureOptions(t, goos, arch)
			archive, files := makeFixture(t, o)
			if err := prepare(context.Background(), o); err != nil {
				t.Fatal(err)
			}
			raw := read(t, filepath.Join(o.out, "manifest.json"))
			input := read(t, filepath.Join(o.out, "manifest.signing-input"))
			if !bytes.Equal(input, append([]byte(coreupdate.PackageSignatureDomain), raw...)) {
				t.Fatal("signing bytes differ")
			}
			pub, private, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			m, err := coreupdate.VerifyPackageManifest(raw, ed25519.Sign(private, input), pub, goos, arch)
			if err != nil {
				t.Fatal(err)
			}
			if m.Archive.SHA256 != hash(archive) || m.Archive.Size != int64(len(archive)) || m.Archive.Mode != 0644 || m.ReleaseSHA256 != hash(files[len(files)-1].data) {
				t.Fatal("incorrect fingerprints", m)
			}
			want, _, err := o.manifest()
			if err != nil || m.Source != want.Source || m.Scope != coreupdate.PackageScope {
				t.Fatal("incorrect identity", m, err)
			}
			if _, err := coreupdate.VerifyPackageManifest(raw, ed25519.Sign(private, raw), pub, goos, arch); err == nil {
				t.Fatal("missing domain accepted")
			}
			if _, err := coreupdate.VerifyPackageManifest(append(raw, ' '), ed25519.Sign(private, input), pub, goos, arch); err == nil {
				t.Fatal("changed exact bytes accepted")
			}
			entries, err := os.ReadDir(o.out)
			if err != nil || len(entries) != 2 {
				t.Fatal(entries, err)
			}
			if !bytes.Equal(read(t, o.archive), archive) {
				t.Fatal("source changed")
			}
			if err := prepare(context.Background(), o); err == nil {
				t.Fatal("existing output overwritten")
			}
			if !bytes.Equal(read(t, filepath.Join(o.out, "manifest.json")), raw) {
				t.Fatal("output changed on refusal")
			}
			o.out += "-second"
			if err := prepare(context.Background(), o); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(read(t, filepath.Join(o.out, "manifest.json")), raw) {
				t.Fatal("output is nondeterministic")
			}
		})
	}
}

func TestInvalidIdentityNoOutput(t *testing.T) {
	for _, mutate := range []func(*options){
		func(o *options) { o.version = "v0.0.3" }, func(o *options) { o.version = "0.00.3" },
		func(o *options) { o.version = "0.0.3-rc.1" }, func(o *options) { o.version = "0.0.3+build" },
		func(o *options) { o.version = "1000000000.0.0" }, func(o *options) { o.version = "0.0.3\n" },
		func(o *options) { o.commit += "-dirty" }, func(o *options) { o.commit = strings.Repeat("A", 40) },
		func(o *options) { o.commit = "abc1234" }, func(o *options) { o.goos = "freebsd" },
		func(o *options) { o.goos, o.arch = "windows", "arm64" }, func(o *options) { o.archive = "" },
	} {
		o := fixtureOptions(t, "linux", "amd64")
		mutate(&o)
		if err := prepare(context.Background(), o); err == nil {
			t.Fatal("invalid identity accepted", o)
		}
		absent(t, o.out)
	}
}

func TestUnsafePackagesNoOutput(t *testing.T) {
	for _, goos := range []string{"linux", "windows"} {
		for _, kind := range []string{"missing-release", "duplicate-release", "oversized-release", "wrong-root", "wrong-version", "wrong-commit", "hash", "private", "link", "traversal", "binary", "duplicate-payload", "corrupt"} {
			t.Run(goos+"/"+kind, func(t *testing.T) {
				o := fixtureOptions(t, goos, "amd64")
				files := fixtureEntries(t, o)
				root := "atm-" + o.version + "-" + o.goos + "-" + o.arch
				switch kind {
				case "missing-release":
					files = files[:len(files)-1]
				case "duplicate-release":
					files = append(files, files[len(files)-1])
				case "oversized-release":
					files[len(files)-1].data = bytes.Repeat([]byte(" "), maxReleaseBytes+1)
				case "wrong-root":
					root += "-other"
				case "wrong-version":
					files[len(files)-1].data = bytes.ReplaceAll(files[len(files)-1].data, []byte(o.version), []byte("0.0.4"))
				case "wrong-commit":
					files[len(files)-1].data = bytes.ReplaceAll(files[len(files)-1].data, []byte(o.commit), []byte(strings.Repeat("b", 40)))
				case "hash":
					files[1].data = []byte("changed")
				case "private":
					files = append(files, entry{"internal-docs/MEMORY.md", []byte("private"), 0644})
				case "link":
					files = append(files, entry{"lib/linked", nil, os.ModeSymlink | 0777})
				case "traversal":
					files = append(files, entry{"../outside", []byte("no"), 0644})
				case "binary":
					old := hash(files[0].data)
					files[0].data = bytes.Repeat([]byte("!"), 128)
					files[len(files)-1].data = bytes.ReplaceAll(files[len(files)-1].data, []byte(old), []byte(hash(files[0].data)))
				case "duplicate-payload":
					files = append(files, files[0])
				}
				archive := writeArchive(t, o, files, root)
				if kind == "corrupt" {
					if err := os.WriteFile(o.archive, archive[:len(archive)-8], 0600); err != nil {
						t.Fatal(err)
					}
				}
				if err := prepare(context.Background(), o); err == nil {
					t.Fatal("unsafe package accepted")
				}
				absent(t, o.out)
			})
		}
	}
}

func TestInputAndOutputBoundaries(t *testing.T) {
	o := fixtureOptions(t, "linux", "amd64")
	makeFixture(t, o)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := prepare(ctx, o); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	absent(t, o.out)
	for _, path := range []string{filepath.Dir(o.archive), o.archive + "-missing"} {
		bad := o
		bad.archive = path
		if err := prepare(context.Background(), bad); err == nil {
			t.Fatal("invalid input accepted")
		}
		absent(t, o.out)
	}
	f, err := os.Create(o.archive + "-oversized")
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(coreupdate.MaxPackageBytes + 1); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()
	bad := o
	bad.archive = o.archive + "-oversized"
	if err := prepare(context.Background(), bad); err == nil {
		t.Fatal("oversized input accepted")
	}
	absent(t, o.out)
	if runtime.GOOS == "windows" {
		return
	}
	if err := os.Symlink(o.archive, o.archive+"-link"); err != nil {
		t.Fatal(err)
	}
	bad.archive = o.archive + "-link"
	if err := prepare(context.Background(), bad); err == nil {
		t.Fatal("linked input accepted")
	}
	absent(t, o.out)
	if err := os.Symlink(filepath.Dir(o.archive), o.archive+"-dirlink"); err != nil {
		t.Fatal(err)
	}
	bad.archive = filepath.Join(o.archive+"-dirlink", filepath.Base(o.archive))
	if err := prepare(context.Background(), bad); err == nil {
		t.Fatal("linked input parent accepted")
	}
	bad = o
	bad.out = filepath.Join(o.archive+"-dirlink", "new")
	if err := prepare(context.Background(), bad); err == nil {
		t.Fatal("linked output parent accepted")
	}
	absent(t, filepath.Join(filepath.Dir(o.archive), "new"))
	if err := os.Symlink(o.archive, o.out); err != nil {
		t.Fatal(err)
	}
	if err := prepare(context.Background(), o); err == nil {
		t.Fatal("linked output replaced")
	}
	if info, err := os.Lstat(o.out); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("output symlink lost", err)
	}
}

type interruptContext struct {
	context.Context
	check func() error
}

func (c interruptContext) Err() error { return c.check() }

func TestOutputFailureCleanup(t *testing.T) {
	o := fixtureOptions(t, "linux", "amd64")
	artifacts := []artifact{{"manifest.json", []byte("one")}, {"manifest.json", []byte("two")}}
	if err := writeOutputs(context.Background(), o.out, artifacts); err == nil {
		t.Fatal("exclusive file creation failed")
	}
	absent(t, o.out)
	ctx := interruptContext{Context: context.Background(), check: func() error {
		if _, err := os.Stat(filepath.Join(o.out, "manifest.json")); err == nil {
			if err := os.WriteFile(filepath.Join(o.out, "unrelated"), []byte("preserve"), 0600); err != nil {
				t.Fatal(err)
			}
			return context.Canceled
		}
		return nil
	}}
	if err := writeOutputs(ctx, o.out, artifacts[:1]); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	absent(t, filepath.Join(o.out, "manifest.json"))
	if string(read(t, filepath.Join(o.out, "unrelated"))) != "preserve" {
		t.Fatal("cleanup deleted unrelated content")
	}
}

func TestCLI(t *testing.T) {
	for _, args := range [][]string{nil, {"--private-key", "never-read"}, {"--binary", "input"}, {"extra"}, {"--out"}} {
		if err := run(context.Background(), args, io.Discard); err == nil {
			t.Fatal("invalid args accepted", args)
		}
	}
	var help bytes.Buffer
	if err := run(context.Background(), []string{"--help"}, &help); !errors.Is(err, flag.ErrHelp) || !strings.Contains(help.String(), "external raw Ed25519") {
		t.Fatal(help.String(), err)
	}
	o := fixtureOptions(t, "linux", "amd64")
	makeFixture(t, o)
	args := []string{"--archive", o.archive, "--version", o.version, "--commit", o.commit, "--os", o.goos, "--arch", o.arch, "--out", o.out}
	if err := run(context.Background(), args, io.Discard); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotCleanup(t *testing.T) {
	temp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", temp)
	t.Setenv("TMP", temp)
	t.Setenv("TEMP", temp)
	for _, corrupt := range []bool{false, true} {
		o := fixtureOptions(t, "linux", "amd64")
		makeFixture(t, o)
		if corrupt {
			if err := os.WriteFile(o.archive, []byte("invalid archive"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		err := prepare(context.Background(), o)
		if (err != nil) != corrupt {
			t.Fatal(err)
		}
		entries, err := os.ReadDir(temp)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), "atm-coremanifest-") {
				t.Fatal("snapshot left behind", e.Name())
			}
		}
	}
}
