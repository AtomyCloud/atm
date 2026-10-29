//go:build linux

package main

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func rocmFixtureItems() []ollamaTarItem {
	return []ollamaTarItem{
		{"./", "", tar.TypeDir, 0755, nil},
		{"lib/", "", tar.TypeDir, 0755, nil},
		{"lib/ollama/", "", tar.TypeDir, 0755, nil},
		{"lib/ollama/rocm_v7/", "", tar.TypeDir, 0755, nil},
		{"lib/ollama/rocm_v7/libhip.so.1", "", tar.TypeReg, 0644, []byte("synthetic ROCm library")},
		{"lib/ollama/rocm_v7/libhip.so", "libhip.so.1", tar.TypeSymlink, 0777, nil},
	}
}

func rocmFixtureRelease(t *testing.T, f *fixture, v string, base, companion []ollamaTarItem) (artifact, artifact) {
	t.Helper()
	if base == nil {
		base = ollamaFixtureItems(f.b.arch, v)
	}
	if companion == nil {
		companion = rocmFixtureItems()
	}
	a := f.release(t, "ollama", v, ollamaFixtureArchive(t, base))
	body := ollamaFixtureArchive(t, companion)
	c := artifact{Provider: "ollama", Version: v, Arch: "amd64", URL: "https://github.com/ollama/ollama/releases/download/v" + v + "/" + ollamaROCmName, SHA256: fmt.Sprintf("%x", sha256.Sum256(body)), Format: "tar.zst", Entry: "lib/ollama", Size: int64(len(body))}
	var r release
	if err := json.Unmarshal(f.responses[f.latest], &r); err != nil {
		t.Fatal(err)
	}
	r.Assets = append(r.Assets, releaseAsset{Name: ollamaROCmName, URL: c.URL, Digest: "sha256:" + c.SHA256, Size: c.Size})
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	f.responses[f.latest] = data
	f.responses[strings.TrimSuffix(f.latest, "latest")+"tags/v"+v] = data
	f.responses[c.URL] = body
	return a, c
}

func TestOllamaROCmOptionsAndMenu(t *testing.T) {
	for _, op := range []string{"install", "update"} {
		o, err := parseOptions(op, []string{"--provider", "ollama", "--rocm", "--trust-download"})
		if err != nil || !o.rocm {
			t.Fatalf("%s: %+v %v", op, o, err)
		}
	}
	for _, op := range []string{"use", "remove", "uninstall", "integrate", "status", "menu"} {
		if _, err := parseOptions(op, []string{"--provider", "ollama", "--rocm"}); err == nil {
			t.Fatalf("accepted --rocm for %s", op)
		}
	}
	for _, args := range [][]string{
		{"--provider", "ollama", "--rocm=true"}, {"--provider", "ollama", "--rocm=false"},
		{"--provider", "ollama", "--rocm", "true"}, {"--provider", "ollama", "--rocm", "--rocm"},
		{"--provider", "picoclaw", "--rocm"}, {"--rocm"},
	} {
		if _, err := parseOptions("install", args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	for _, arch := range []string{"amd64", "arm64"} {
		f := newFixture(t, arch)
		cs, err := f.b.menu(context.Background(), "ollama")
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, c := range cs {
			if strings.Contains(strings.Join(c.Arguments, " "), "--rocm") {
				count++
				if !strings.Contains(c.Label, "hardware compatibility unknown") {
					t.Fatalf("misleading menu label: %s", c.Label)
				}
			}
		}
		if (arch == "amd64" && count != 2) || (arch == "arm64" && count != 0) || f.calls != 0 {
			t.Fatalf("%s actions=%d network=%d", arch, count, f.calls)
		}
		if arch == "arm64" {
			if err := f.run(context.Background(), "install", "--provider", "ollama", "--trust-download", "--rocm"); err == nil || f.calls != 0 {
				t.Fatalf("arm64 opt-in: %v network=%d", err, f.calls)
			}
			integrationMissing(t, f.b.req.AppsDir)
		}
	}
}

func TestOllamaROCmLifecycleAndVariantPreservation(t *testing.T) {
	f := integrationFixture(t, "ollama")
	ctx := context.Background()
	baseReceipt := integrationRead(t, filepath.Join(f.b.ollamaVersion("1.2.3"), "receipt.json"))
	if bytes.Contains(baseReceipt, []byte("ROCm")) {
		t.Fatal("base receipt format changed")
	}
	mustRun(t, f, "integrate", "--provider", "ollama")
	profile := integrationRead(t, filepath.Join(os.Getenv("HOME"), ".profile"))
	rocmFixtureRelease(t, f, "1.2.4", nil, nil)
	mustRun(t, f, "update", "--provider", "ollama", "--trust-download", "--rocm")
	rec, err := f.b.ollamaVerify(ctx, "1.2.4", f.b.ollamaVersion("1.2.4"))
	if err != nil || rec.Schema != 2 || rec.ROCm == nil {
		t.Fatalf("ROCm receipt: %+v %v", rec, err)
	}
	if !bytes.Equal(baseReceipt, integrationRead(t, filepath.Join(f.b.ollamaVersion("1.2.3"), "receipt.json"))) {
		t.Fatal("old base receipt changed")
	}
	rocmFixtureRelease(t, f, "1.2.5", nil, nil)
	before := f.calls
	mustRun(t, f, "update", "--provider", "ollama", "--trust-download")
	if f.calls-before != 3 {
		t.Fatalf("expected one metadata and two payload requests; got %d", f.calls-before)
	}
	rec, err = f.b.ollamaVerify(ctx, "1.2.5", f.b.ollamaVersion("1.2.5"))
	if err != nil || rec.ROCm == nil {
		t.Fatalf("update lost variant: %+v %v", rec, err)
	}
	if err := f.run(ctx, "update", "--provider", "ollama", "--trust-download", "--version", "1.2.4"); err == nil || !strings.Contains(err.Error(), "downgrade") {
		t.Fatalf("downgrade: %v", err)
	}
	before = f.calls
	mustRun(t, f, "update", "--provider", "ollama", "--trust-download")
	if f.calls != before+1 {
		t.Fatal("same variant unexpectedly downloaded")
	}
	before = f.calls
	var logs []string
	f.b.log = func(s string) { logs = append(logs, s) }
	mustRun(t, f, "status")
	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "1.2.3: base;") || !strings.Contains(joined, "1.2.5: base + ROCm companion;") {
		t.Fatalf("variant status: %s", joined)
	}
	mustRun(t, f, "use", "--provider", "ollama", "--version", "1.2.3")
	mustRun(t, f, "use", "--provider", "ollama", "--version", "1.2.5")
	if got := integrationRead(t, filepath.Join(f.b.root("ollama"), "current/lib/ollama/rocm_v7/libhip.so")); string(got) != "synthetic ROCm library" {
		t.Fatal("companion symlink or selection lost")
	}
	if !bytes.Equal(profile, integrationRead(t, filepath.Join(os.Getenv("HOME"), ".profile"))) {
		t.Fatal("update/use modified enrolled profile")
	}
	mustRun(t, f, "remove", "--provider", "ollama", "--version", "1.2.4")
	mustRun(t, f, "uninstall", "--provider", "ollama")
	if f.calls != before {
		t.Fatal("offline lifecycle fetched metadata")
	}
	vs, selected, err := f.b.ollamaInspect(ctx)
	if err != nil || len(vs) != 0 || selected != "" {
		t.Fatalf("incomplete uninstall: %v %q %v", vs, selected, err)
	}
}

func TestOllamaROCmSameVersionMismatchPreservesTree(t *testing.T) {
	for _, installedROCm := range []bool{false, true} {
		f := newFixture(t, "amd64")
		rocmFixtureRelease(t, f, "1.2.3", nil, nil)
		args := []string{"--provider", "ollama", "--trust-download"}
		if installedROCm {
			args = append(args, "--rocm")
		}
		mustRun(t, f, "install", args...)
		before, err := f.b.ollamaVerify(context.Background(), "1.2.3", f.b.ollamaVersion("1.2.3"))
		if err != nil {
			t.Fatal(err)
		}
		original, err := os.Lstat(f.b.ollamaVersion("1.2.3"))
		if err != nil {
			t.Fatal(err)
		}
		args = []string{"--provider", "ollama", "--trust-download"}
		if !installedROCm {
			args = append(args, "--rocm")
		}
		calls := f.calls
		if err := f.run(context.Background(), "install", args...); err == nil || !strings.Contains(err.Error(), "same-version variant mismatch") {
			t.Fatalf("variant mismatch: %v", err)
		}
		after, err := f.b.ollamaVerify(context.Background(), "1.2.3", f.b.ollamaVersion("1.2.3"))
		now, statErr := os.Lstat(f.b.ollamaVersion("1.2.3"))
		if err != nil || statErr != nil || !reflect.DeepEqual(before, after) || !os.SameFile(original, now) || f.calls != calls+1 {
			t.Fatalf("variant mismatch mutated/downloaded: %v %v", err, statErr)
		}
	}
}

func TestOllamaROCmNoOpAndUsePreserveReceiptAndPayload(t *testing.T) {
	f := integrationFixture(t, "ollama")
	rocmFixtureRelease(t, f, "1.2.4", nil, nil)
	mustRun(t, f, "install", "--provider", "ollama", "--trust-download", "--rocm")
	dir := f.b.ollamaVersion("1.2.4")
	before, err := f.b.ollamaVerify(context.Background(), "1.2.4", dir)
	if err != nil {
		t.Fatal(err)
	}
	receipt := integrationRead(t, filepath.Join(dir, "receipt.json"))
	identities := map[string]os.FileInfo{}
	if err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil {
			identities[p] = info
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	calls := f.calls
	mustRun(t, f, "install", "--provider", "ollama", "--trust-download", "--rocm")
	mustRun(t, f, "update", "--provider", "ollama", "--trust-download")
	mustRun(t, f, "use", "--provider", "ollama", "--version", "1.2.3")
	mustRun(t, f, "use", "--provider", "ollama", "--version", "1.2.4")
	after, err := f.b.ollamaVerify(context.Background(), "1.2.4", dir)
	if err != nil || !reflect.DeepEqual(before, after) || !bytes.Equal(receipt, integrationRead(t, filepath.Join(dir, "receipt.json"))) || f.calls != calls+2 {
		t.Fatalf("no-op/use changed evidence or downloaded payload: %v", err)
	}
	for p, before := range identities {
		after, err := os.Lstat(p)
		if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
			t.Fatalf("no-op/use changed entry %s: %v", p, err)
		}
	}
}

func TestOllamaROCmArchiveCollisionsAndUnsafeCompanions(t *testing.T) {
	cases := map[string]func(*[]ollamaTarItem, *[]ollamaTarItem){
		"binary":  func(b, c *[]ollamaTarItem) { *c = append(*c, (*b)[2]) },
		"file":    func(b, c *[]ollamaTarItem) { *b = append(*b, (*c)[4]) },
		"symlink": func(b, c *[]ollamaTarItem) { *b = append(*b, (*c)[4], (*c)[5]) },
		"file-ancestor": func(b, c *[]ollamaTarItem) {
			*b = append(*b, ollamaTarItem{"lib/ollama/rocm_v7", "", tar.TypeReg, 0600, []byte("file")})
		},
		"link-ancestor": func(b, c *[]ollamaTarItem) {
			*b = append(*b, ollamaTarItem{"lib/ollama/rocm_v7", ".", tar.TypeSymlink, 0777, nil})
		},
		"late-ancestor": func(b, c *[]ollamaTarItem) {
			(*c)[3] = ollamaTarItem{"lib/ollama/rocm_v7", "", tar.TypeReg, 0600, []byte("file")}
			*b = append(*b, ollamaTarItem{"lib/ollama/rocm_v7/existing", "", tar.TypeReg, 0600, []byte("x")})
		},
		"traversal":           func(b, c *[]ollamaTarItem) { (*c)[4].name = "lib/ollama/rocm_v7/../../escape" },
		"escape-link":         func(b, c *[]ollamaTarItem) { (*c)[5].link = "../../../../outside" },
		"cycle":               func(b, c *[]ollamaTarItem) { (*c)[5].link = "libhip.so" },
		"duplicate":           func(b, c *[]ollamaTarItem) { *c = append(*c, (*c)[4]) },
		"hardlink":            func(b, c *[]ollamaTarItem) { (*c)[5].kind = tar.TypeLink },
		"empty":               func(b, c *[]ollamaTarItem) { *c = (*c)[:4] },
		"foreign-layout":      func(b, c *[]ollamaTarItem) { (*c)[4].name = "lib/ollama/foreign.so" },
		"wrong-arch":          func(b, c *[]ollamaTarItem) { (*b)[2].data = fakeELF("arm64", "1.2.3", false) },
		"missing-base-binary": func(b, c *[]ollamaTarItem) { *b = append((*b)[:2], (*b)[3:]...) },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, "amd64")
			base, companion := ollamaFixtureItems("amd64", "1.2.3"), rocmFixtureItems()
			change(&base, &companion)
			rocmFixtureRelease(t, f, "1.2.3", base, companion)
			if err := f.run(context.Background(), "install", "--provider", "ollama", "--trust-download", "--rocm"); err == nil {
				t.Fatal("unsafe combined bundle accepted")
			}
			integrationMissing(t, f.b.root("ollama"))
		})
	}
}

func TestOllamaROCmReceiptAndTreeTampering(t *testing.T) {
	cases := map[string]func(*ollamaReceipt){
		"missing":      func(r *ollamaReceipt) { r.ROCm = nil },
		"base-schema":  func(r *ollamaReceipt) { r.Schema = 1 },
		"version":      func(r *ollamaReceipt) { r.ROCm.Version = "1.2.4" },
		"architecture": func(r *ollamaReceipt) { r.ROCm.Arch = "arm64" },
		"url":          func(r *ollamaReceipt) { r.ROCm.URL = r.Artifact.URL },
		"digest":       func(r *ollamaReceipt) { r.ROCm.SHA256 = "" },
		"size":         func(r *ollamaReceipt) { r.ROCm.Size = 0 },
		"format":       func(r *ollamaReceipt) { r.ROCm.Format = "tar.gz" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, "amd64")
			rocmFixtureRelease(t, f, "1.2.3", nil, nil)
			mustRun(t, f, "install", "--provider", "ollama", "--trust-download", "--rocm")
			r, err := ollamaReadReceipt(f.b.ollamaVersion("1.2.3"))
			if err != nil {
				t.Fatal(err)
			}
			mutate(&r)
			data, err := json.Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			writeFixture(t, filepath.Join(f.b.ollamaVersion("1.2.3"), "receipt.json"), data, 0600)
			if _, err := f.b.ollamaVerify(context.Background(), "1.2.3", f.b.ollamaVersion("1.2.3")); err == nil {
				t.Fatal("tampered companion accepted")
			}
		})
	}
	for _, op := range []string{"status", "use", "remove", "uninstall", "integrate"} {
		t.Run("modified-library-"+op, func(t *testing.T) {
			f := integrationFixture(t)
			rocmFixtureRelease(t, f, "1.2.3", nil, nil)
			mustRun(t, f, "install", "--provider", "ollama", "--trust-download", "--rocm")
			writeFixture(t, filepath.Join(f.b.ollamaVersion("1.2.3"), "tree/lib/ollama/rocm_v7/libhip.so.1"), []byte("modified"), 0600)
			args := []string{"--provider", "ollama"}
			if op == "status" {
				args = nil
			} else if op == "use" || op == "remove" {
				args = append(args, "--version", "1.2.3")
			}
			if err := f.run(context.Background(), op, args...); err == nil || !strings.Contains(err.Error(), "bundle changed") {
				t.Fatalf("%s accepted modified companion: %v", op, err)
			}
		})
	}
}

func TestOllamaROCmReceiptDuplicateArtifactFields(t *testing.T) {
	for _, field := range []string{"ROCm", "rocm", "Artifact", "artifact", "url"} {
		t.Run(field, func(t *testing.T) {
			f := newFixture(t, "amd64")
			rocmFixtureRelease(t, f, "1.2.3", nil, nil)
			mustRun(t, f, "install", "--provider", "ollama", "--trust-download", "--rocm")
			p := filepath.Join(f.b.ollamaVersion("1.2.3"), "receipt.json")
			data := bytes.TrimSpace(integrationRead(t, p))
			if field == "url" {
				data = bytes.Replace(data, []byte(`"URL":`), []byte(`"url":"ignored","URL":`), 1)
			} else {
				data = append(data[:len(data)-1], []byte(fmt.Sprintf(",%q:null}", field))...)
			}
			writeFixture(t, p, data, 0600)
			if _, err := ollamaReadReceipt(f.b.ollamaVersion("1.2.3")); err == nil {
				t.Fatal("duplicate or alias artifact record accepted")
			}
		})
	}
}

func TestOllamaROCmSharedDirectoriesAndCrossArchiveLinks(t *testing.T) {
	f := newFixture(t, "amd64")
	base, companion := ollamaFixtureItems("amd64", "1.2.3"), rocmFixtureItems()
	base = append(base, ollamaTarItem{"lib/ollama/rocm_v7/base-only", "", tar.TypeReg, 0600, []byte("base data")})
	companion = append(companion, ollamaTarItem{"lib/ollama/rocm_v7/base-link", "base-only", tar.TypeSymlink, 0777, nil})
	rocmFixtureRelease(t, f, "1.2.3", base, companion)
	mustRun(t, f, "install", "--provider", "ollama", "--trust-download", "--rocm")
	if got := integrationRead(t, filepath.Join(f.b.root("ollama"), "current/lib/ollama/rocm_v7/base-link")); string(got) != "base data" {
		t.Fatal("valid combined link target lost")
	}
}

func TestOllamaROCmStagingFaults(t *testing.T) {
	for _, point := range []string{"download", "cancel-download", "ollama-verified", "ollama-published", "ollama-before-activation"} {
		t.Run(point, func(t *testing.T) {
			f := integrationFixture(t, "ollama")
			before, err := f.b.ollamaVerify(context.Background(), "1.2.3", f.b.ollamaVersion("1.2.3"))
			if err != nil {
				t.Fatal(err)
			}
			_, companion := rocmFixtureRelease(t, f, "1.2.4", nil, nil)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			transport := f.b.client.Transport
			f.b.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() == companion.URL {
					if point == "download" {
						return nil, errors.New("injected companion download failure")
					}
					if point == "cancel-download" {
						cancel()
					}
				}
				return transport.RoundTrip(r)
			})
			f.b.checkpoint = func(p string) error {
				if p == point {
					return errors.New("injected publication fault")
				}
				return nil
			}
			if err := f.run(ctx, "update", "--provider", "ollama", "--trust-download", "--rocm"); err == nil {
				t.Fatal("fault ignored")
			}
			after, err := f.b.ollamaVerify(context.Background(), "1.2.3", f.b.ollamaVersion("1.2.3"))
			current, currentErr := f.b.ollamaCurrent()
			if err != nil || currentErr != nil || current != "1.2.3" || !reflect.DeepEqual(before, after) {
				t.Fatalf("old version changed: %s %v %v", current, err, currentErr)
			}
			if point == "ollama-published" || point == "ollama-before-activation" {
				if _, err := f.b.ollamaVerify(context.Background(), "1.2.4", f.b.ollamaVersion("1.2.4")); err != nil {
					t.Fatalf("inactive complete version not retained: %v", err)
				}
			} else {
				integrationMissing(t, f.b.ollamaVersion("1.2.4"))
			}
		})
	}
}

func TestOllamaROCmCombinedExtractionBudgets(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(root, "base.zst")
	companion := filepath.Join(root, "companion.zst")
	writeFixture(t, base, ollamaFixtureArchive(t, ollamaFixtureItems("amd64", "1.2.3")), 0600)
	writeFixture(t, companion, ollamaFixtureArchive(t, rocmFixtureItems()), 0600)
	tree := filepath.Join(root, "tree")
	if err := os.Mkdir(tree, 0700); err != nil {
		t.Fatal(err)
	}
	files := map[string]ollamaEntry{}
	remaining := int64(1 << 20)
	if err := ollamaExtractPart(context.Background(), base, tree, files, &remaining, false); err != nil {
		t.Fatal(err)
	}
	if remaining >= 1<<20 {
		t.Fatal("base expanded stream did not consume shared budget")
	}
	remaining = 512 // A valid companion alone exceeds the remaining shared budget.
	if err := ollamaExtractPart(context.Background(), companion, tree, files, &remaining, true); err == nil {
		t.Fatal("companion reset shared expanded budget")
	}
	for len(files) <= ollamaMaxEntries {
		files[fmt.Sprintf("lib/ollama/base-%d", len(files))] = ollamaEntry{Mode: 0600}
	}
	remaining = 1 << 20
	if err := ollamaExtractPart(context.Background(), companion, tree, files, &remaining, true); err == nil || !strings.Contains(err.Error(), "inventory entry limit") {
		t.Fatalf("combined entry limit: %v", err)
	}
}
