//go:build linux

package main

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/AtomyCloud/atm/internal/protocol"
	"github.com/klauspost/compress/zstd"
)

type ollamaTarItem struct {
	name, link string
	kind       byte
	mode       int64
	data       []byte
}

func ollamaFixtureItems(arch, version string) []ollamaTarItem {
	return []ollamaTarItem{
		{"./", "", tar.TypeDir, 0755, nil},
		{"./bin/", "", tar.TypeDir, 0755, nil},
		{"./bin/ollama", "", tar.TypeReg, 0755, fakeELF(arch, version, false)},
		{"./lib/ollama/", "", tar.TypeDir, 0755, nil},
		{"./lib/ollama/lib.so.1", "", tar.TypeReg, 0644, []byte("library-" + version)},
		{"./lib/ollama/lib.so", "lib.so.1", tar.TypeSymlink, 0777, nil},
		{"./lib/ollama/runner", "", tar.TypeReg, 0755, []byte("helper never executed")},
		{"./lib/ollama/LICENSE", "", tar.TypeReg, 0644, []byte("fixture license")},
	}
}

func ollamaFixtureArchive(t *testing.T, items []ollamaTarItem) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw, err := zstd.NewWriter(&buf, zstd.WithEncoderConcurrency(1), zstd.WithEncoderLevel(zstd.SpeedFastest))
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(zw)
	for _, item := range items {
		if err = tw.WriteHeader(&tar.Header{Name: item.name, Linkname: item.link, Typeflag: item.kind, Mode: item.mode, Size: int64(len(item.data))}); err != nil {
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

func ollamaFixtureRelease(t *testing.T, f *fixture, v string) artifact {
	t.Helper()
	return f.release(t, "ollama", v, ollamaFixtureArchive(t, ollamaFixtureItems(f.b.arch, v)))
}

func TestOllamaLifecycleBundleBothArchitectures(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		t.Run(arch, func(t *testing.T) {
			f := newFixture(t, arch)
			legacy := filepath.Join(filepath.Dir(f.b.root("ollama")), "legacy-sentinel")
			writeFixture(t, legacy, []byte("keep"), 0600)
			profile := filepath.Join(f.b.discovery.home, ".profile")
			writeFixture(t, profile, []byte("unchanged profile"), 0600)
			ollamaFixtureRelease(t, f, "1.2.3")
			mustRun(t, f, "install", "--provider", "ollama", "--trust-download", "--version", "1.2.3")
			root := f.b.root("ollama")
			if target, err := os.Readlink(filepath.Join(root, "current")); err != nil || target != "versions/1.2.3/tree" {
				t.Fatalf("current %q %v", target, err)
			}
			for _, name := range []string{"bin/ollama", "lib/ollama/lib.so", "lib/ollama/runner", "lib/ollama/LICENSE"} {
				if _, err := os.Stat(filepath.Join(root, "current", name)); err != nil {
					t.Fatal(err)
				}
			}
			if i, err := os.Stat(filepath.Join(root, "current/lib/ollama/runner")); err != nil || i.Mode().Perm() != 0755 {
				t.Fatalf("helper executable mode: %v", err)
			}
			calls := f.calls
			mustRun(t, f, "status")
			choices, err := f.b.menu(context.Background(), "ollama")
			if err != nil {
				t.Fatal(err)
			}
			if err = protocol.ValidateChoices(choices); err != nil {
				t.Fatal(err)
			}
			integrationOffered := false
			for _, c := range choices {
				if c.ID == "integrate" && c.Operation == "integrate" {
					integrationOffered = true
				}
				if c.ID == "remove-path" {
					t.Fatal("PATH removal offered without explicit integration")
				}
			}
			if !integrationOffered {
				t.Fatal("explicit Ollama PATH action missing")
			}
			integrationMissing(t, filepath.Join(root, "path.json"))
			integrationMissing(t, filepath.Join(root, "bin", "ollama"))
			if calls != f.calls {
				t.Fatal("offline menu/status fetched")
			}
			ollamaFixtureRelease(t, f, "1.2.4")
			mustRun(t, f, "update", "--provider", "ollama", "--trust-download")
			if err = f.run(context.Background(), "remove", "--provider", "ollama", "--version", "1.2.4"); err == nil {
				t.Fatal("removed active version")
			}
			mustRun(t, f, "use", "--provider", "ollama", "--version", "1.2.3")
			mustRun(t, f, "remove", "--provider", "ollama", "--version", "1.2.4")
			mustRun(t, f, "uninstall", "--provider", "ollama")
			vs, current, err := f.b.ollamaInspect(context.Background())
			if err != nil || len(vs) != 0 || current != "" {
				t.Fatalf("not uninstalled: %v %s %v", vs, current, err)
			}
			for p, want := range map[string]string{legacy: "keep", profile: "unchanged profile"} {
				got, err := os.ReadFile(p)
				if err != nil || string(got) != want {
					t.Fatalf("external bytes changed %s: %v", p, err)
				}
			}
		})
	}
}

func TestOllamaCompiledELFFixtureNeverExecuted(t *testing.T) {
	f := newFixture(t, runtime.GOARCH)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	items := ollamaFixtureItems("amd64", "1.2.3")
	items[2].data = data // A real compiled ELF, read as data; no provider subprocess.
	f.release(t, "ollama", "1.2.3", ollamaFixtureArchive(t, items))
	mustRun(t, f, "install", "--provider", "ollama", "--trust-download")
	got, err := os.ReadFile(filepath.Join(f.b.root("ollama"), "current/bin/ollama"))
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("compiled binary not preserved: %v", err)
	}
	mustRun(t, f, "uninstall", "--provider", "ollama")
}

func TestOllamaArchiveRejectsUnsafeTrees(t *testing.T) {
	cases := map[string]func([]ollamaTarItem) []ollamaTarItem{
		"traversal":         func(a []ollamaTarItem) []ollamaTarItem { a[4].name = "./lib/ollama/../../escape"; return a },
		"absolute":          func(a []ollamaTarItem) []ollamaTarItem { a[4].name = "/lib/ollama/escape"; return a },
		"double-dot-prefix": func(a []ollamaTarItem) []ollamaTarItem { a[4].name = "././lib/ollama/escape"; return a },
		"control":           func(a []ollamaTarItem) []ollamaTarItem { a[4].name = "lib/ollama/\nfile"; return a },
		"outside-layout":    func(a []ollamaTarItem) []ollamaTarItem { a[4].name = "etc/service"; return a },
		"duplicate": func(a []ollamaTarItem) []ollamaTarItem {
			return append(a, ollamaTarItem{"lib/ollama/lib.so.1", "", tar.TypeReg, 0600, []byte("x")})
		},
		"root-duplicate": func(a []ollamaTarItem) []ollamaTarItem {
			return append(a, ollamaTarItem{".", "", tar.TypeDir, 0755, nil})
		},
		"hardlink": func(a []ollamaTarItem) []ollamaTarItem {
			a[5].kind = tar.TypeLink
			a[5].link = "lib/ollama/lib.so.1"
			return a
		},
		"fifo":          func(a []ollamaTarItem) []ollamaTarItem { a[5].kind = tar.TypeFifo; return a },
		"device":        func(a []ollamaTarItem) []ollamaTarItem { a[5].kind = tar.TypeChar; return a },
		"special-mode":  func(a []ollamaTarItem) []ollamaTarItem { a[2].mode = 04755; return a },
		"wrong-arch":    func(a []ollamaTarItem) []ollamaTarItem { a[2].data = fakeELF("arm64", "x", false); return a },
		"shell":         func(a []ollamaTarItem) []ollamaTarItem { a[2].data = []byte("#!/bin/sh\nexit 42\n"); return a },
		"no-exec":       func(a []ollamaTarItem) []ollamaTarItem { a[2].mode = 0644; return a },
		"absolute-link": func(a []ollamaTarItem) []ollamaTarItem { a[5].link = "/etc/passwd"; return a },
		"escaping-link": func(a []ollamaTarItem) []ollamaTarItem { a[5].link = "../../../outside"; return a },
		"dangling-link": func(a []ollamaTarItem) []ollamaTarItem { a[5].link = "missing.so"; return a },
		"cycle":         func(a []ollamaTarItem) []ollamaTarItem { a[5].link = "lib.so"; return a },
		"ancestor-link": func(a []ollamaTarItem) []ollamaTarItem { a[5].link = "."; return a },
		"link-child": func(a []ollamaTarItem) []ollamaTarItem {
			return append(a, ollamaTarItem{"lib/ollama/lib.so/child", "", tar.TypeReg, 0600, []byte("x")})
		},
		"linked-parent-late": func(a []ollamaTarItem) []ollamaTarItem {
			a = append(a, ollamaTarItem{"lib/ollama/parent/child", "", tar.TypeReg, 0600, []byte("x")})
			return append(a, ollamaTarItem{"lib/ollama/parent", "lib.so.1", tar.TypeSymlink, 0777, nil})
		},
		"trailing-dot-link": func(a []ollamaTarItem) []ollamaTarItem { a[5].link = "lib.so.1/."; return a },
		"directory-cycle": func(a []ollamaTarItem) []ollamaTarItem {
			return append(a, ollamaTarItem{"lib/ollama/a/next", "../b", tar.TypeSymlink, 0777, nil}, ollamaTarItem{"lib/ollama/b/next", "../a", tar.TypeSymlink, 0777, nil})
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, "amd64")
			f.release(t, "ollama", "1.2.3", ollamaFixtureArchive(t, change(ollamaFixtureItems("amd64", "1.2.3"))))
			if err := f.run(context.Background(), "install", "--provider", "ollama", "--trust-download"); err == nil {
				t.Fatal("unsafe bundle accepted")
			}
			if _, err := os.Lstat(f.b.root("ollama")); !os.IsNotExist(err) {
				t.Fatalf("unsafe bundle published: %v", err)
			}
		})
	}
}

func TestOllamaLegitimateLibraryLinks(t *testing.T) {
	f := newFixture(t, "amd64")
	items := append(ollamaFixtureItems("amd64", "1.2.3"),
		ollamaTarItem{"lib/ollama/chain.so", "./lib.so", tar.TypeSymlink, 0777, nil},
		ollamaTarItem{"lib/ollama/sub/other.so", "../chain.so", tar.TypeSymlink, 0777, nil},
		ollamaTarItem{"lib/ollama/dir-alias", "sub", tar.TypeSymlink, 0777, nil})
	f.release(t, "ollama", "1.2.3", ollamaFixtureArchive(t, items))
	mustRun(t, f, "install", "--provider", "ollama", "--trust-download")
	p := filepath.Join(f.b.root("ollama"), "current/lib/ollama/dir-alias/other.so")
	got, err := os.ReadFile(p)
	if err != nil || string(got) != "library-1.2.3" {
		t.Fatalf("legitimate link lost: %q %v", got, err)
	}
	mustRun(t, f, "uninstall", "--provider", "ollama")
}

func TestOllamaExactDownloadAndDecoderBounds(t *testing.T) {
	for _, kind := range []string{"digest", "short", "long", "reported-short", "chunked-short", "window", "truncated", "nonzero-trailer", "declared-expanded"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t, "amd64")
			body := ollamaFixtureArchive(t, ollamaFixtureItems("amd64", "1.2.3"))
			switch kind {
			case "window":
				body = []byte{0x28, 0xb5, 0x2f, 0xfd, 0, 0x90, 1, 0, 0}
			case "truncated":
				body = body[:len(body)-1]
			case "nonzero-trailer", "declared-expanded":
				var raw bytes.Buffer
				if kind == "nonzero-trailer" {
					raw.Write(make([]byte, 1024))
					raw.WriteString("not tar padding")
				} else {
					tw := tar.NewWriter(&raw)
					if err := tw.WriteHeader(&tar.Header{Name: "lib/ollama/huge", Typeflag: tar.TypeReg, Mode: 0644, Size: ollamaMaxExpanded + 1}); err != nil {
						t.Fatal(err)
					}
				}
				zw, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1))
				if err != nil {
					t.Fatal(err)
				}
				body = zw.EncodeAll(raw.Bytes(), nil)
				zw.Close()
			}
			a := f.release(t, "ollama", "1.2.3", body)
			switch kind {
			case "digest":
				f.responses[a.URL] = bytes.Repeat([]byte{42}, len(body))
			case "short", "chunked-short":
				f.responses[a.URL] = body[:len(body)-1]
			case "long":
				f.responses[a.URL] = append(body, 0)
			}
			if kind == "chunked-short" || kind == "reported-short" {
				base := f.b.client.Transport
				f.b.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
					resp, err := base.RoundTrip(r)
					if err == nil && r.URL.String() == a.URL {
						if kind == "chunked-short" {
							resp.ContentLength = -1
						} else {
							resp.ContentLength--
						}
					}
					return resp, err
				})
			}
			if err := f.run(context.Background(), "install", "--provider", "ollama", "--trust-download"); err == nil {
				t.Fatal("invalid bounded stream installed")
			}
			if _, err := os.Lstat(f.b.root("ollama")); !os.IsNotExist(err) {
				t.Fatal("bad stream published")
			}
		})
	}
	f := newFixture(t, "amd64")
	a := ollamaFixtureRelease(t, f, "1.2.3")
	a.Size = ollamaMaxDownload
	if err := a.validate(); err != nil {
		t.Fatal(err)
	}
	a.Size++
	if err := a.validate(); err == nil {
		t.Fatal("over 4 GiB accepted")
	}
}

func TestOllamaFaultsAndCancellationPreserveSelection(t *testing.T) {
	for _, at := range []string{"ollama-verified", "ollama-published", "ollama-before-activation"} {
		for _, cancelRun := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%t", at, cancelRun), func(t *testing.T) {
				f := newFixture(t, "amd64")
				ollamaFixtureRelease(t, f, "1.2.3")
				mustRun(t, f, "install", "--provider", "ollama", "--trust-download")
				ollamaFixtureRelease(t, f, "1.2.4")
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				f.b.checkpoint = func(name string) error {
					if name == at {
						if cancelRun {
							cancel()
						} else {
							return errors.New("fixture fault")
						}
					}
					return nil
				}
				if err := f.run(ctx, "update", "--provider", "ollama", "--trust-download"); err == nil {
					t.Fatal("fault ignored")
				}
				if v, err := f.b.ollamaCurrent(); err != nil || v != "1.2.3" {
					t.Fatalf("selection changed: %s %v", v, err)
				}
			})
		}
	}
}

func TestOllamaPostDetachAndPartialCleanupRetainRemainder(t *testing.T) {
	for _, at := range []string{"ollama-after-removal-detach", "ollama-removed-lib/ollama/runner"} {
		t.Run(at, func(t *testing.T) {
			f := newFixture(t, "amd64")
			ollamaFixtureRelease(t, f, "1.2.3")
			mustRun(t, f, "install", "--provider", "ollama", "--trust-download")
			ollamaFixtureRelease(t, f, "1.2.4")
			mustRun(t, f, "update", "--provider", "ollama", "--trust-download")
			f.b.checkpoint = func(name string) error {
				if name == at {
					return errors.New("cleanup fault")
				}
				return nil
			}
			err := f.run(context.Background(), "remove", "--provider", "ollama", "--version", "1.2.3")
			if err == nil || !strings.Contains(err.Error(), "retained for inspection") {
				t.Fatalf("missing retained-state error: %v", err)
			}
			stages, err := filepath.Glob(filepath.Join(filepath.Dir(f.b.root("ollama")), ".atm-ollama-remove-*"))
			if err != nil || len(stages) != 1 {
				t.Fatalf("lost remainder %v %v", stages, err)
			}
			if _, err = os.Stat(filepath.Join(stages[0], "version/receipt.json")); err != nil {
				t.Fatal(err)
			}
			if v, err := f.b.ollamaCurrent(); err != nil || v != "1.2.4" {
				t.Fatalf("selection changed %s %v", v, err)
			}
			if _, err = os.Lstat(f.b.root("ollama") + ".lock"); !os.IsNotExist(err) {
				t.Fatal("live lock retained after completed error")
			}
		})
	}
}

func TestOllamaOwnedGuardsBeforeWritesAndNetwork(t *testing.T) {
	for _, kind := range []string{"library-mode", "hardlink", "symlink-hardlink", "receipt-unknown", "root-extra", "current-external", "arch"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t, "amd64")
			ollamaFixtureRelease(t, f, "1.2.3")
			mustRun(t, f, "install", "--provider", "ollama", "--trust-download")
			root := f.b.root("ollama")
			dir := f.b.ollamaVersion("1.2.3")
			lib := filepath.Join(dir, "tree/lib/ollama/lib.so.1")
			switch kind {
			case "library-mode":
				if err := os.Chmod(lib, 0644); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(lib, filepath.Join(t.TempDir(), "alias")); err != nil {
					t.Fatal(err)
				}
			case "symlink-hardlink":
				if err := os.Link(filepath.Join(dir, "tree/lib/ollama/lib.so"), filepath.Join(t.TempDir(), "alias")); err != nil {
					t.Fatal(err)
				}
			case "receipt-unknown":
				writeFixture(t, filepath.Join(dir, "receipt.json"), []byte(`{"Schema":1,"unknown":true}`), 0600)
			case "root-extra":
				writeFixture(t, filepath.Join(root, "user-file"), []byte("keep"), 0600)
			case "current-external":
				if err := os.Remove(filepath.Join(root, "current")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("../external", filepath.Join(root, "current")); err != nil {
					t.Fatal(err)
				}
			case "arch":
				f.b.arch = "arm64"
			}
			calls := f.calls
			for _, op := range []string{"use", "uninstall", "update", "status"} {
				args := []string{"--provider", "ollama"}
				if op == "use" {
					args = append(args, "--version", "1.2.3")
				}
				if op == "update" {
					args = append(args, "--trust-download")
				}
				if op == "status" {
					args = nil
				}
				if err := f.run(context.Background(), op, args...); err == nil {
					t.Fatalf("%s accepted modified %s", op, kind)
				}
			}
			if calls != f.calls {
				t.Fatal("modified state fetched network")
			}
			if _, err := os.Lstat(dir); err != nil {
				t.Fatal("modified version removed")
			}
		})
	}
}

func TestOllamaNoAdoptionDowngradeOrImplicitIntegration(t *testing.T) {
	f := newFixture(t, "amd64")
	for _, op := range []string{"install", "update", "use", "remove", "uninstall", "integrate"} {
		args := []string{"--provider", "ollama"}
		if op == "update" {
			args = append(args, "--trust-download")
		}
		if op == "use" || op == "remove" {
			args = append(args, "--version", "1.2.3")
		}
		if err := f.run(context.Background(), op, args...); err == nil {
			t.Fatalf("unsupported/missing/no-trust %s", op)
		}
	}
	if f.calls != 0 {
		t.Fatal("missing ownership fetched")
	}
	if _, err := os.Lstat(f.b.req.AppsDir); !os.IsNotExist(err) {
		t.Fatal("missing ownership wrote")
	}
	ollamaFixtureRelease(t, f, "2.0.0")
	mustRun(t, f, "install", "--provider", "ollama", "--trust-download")
	ollamaFixtureRelease(t, f, "1.9.9")
	if err := f.run(context.Background(), "update", "--provider", "ollama", "--trust-download"); err == nil {
		t.Fatal("downgrade accepted")
	}
	if v, _ := f.b.ollamaCurrent(); v != "2.0.0" {
		t.Fatal("downgrade changed selection")
	}
}

func TestOllamaMetadataExactAndOfflineGuidance(t *testing.T) {
	f := newFixture(t, "amd64")
	a := ollamaFixtureRelease(t, f, "1.2.3")
	for _, mutate := range []func(*artifact){func(a *artifact) { a.URL = strings.Replace(a.URL, ".tar.zst", "-rocm.tar.zst", 1) }, func(a *artifact) { a.Format = "tar.gz" }, func(a *artifact) { a.Arch = "386" }} {
		changed := a
		mutate(&changed)
		if err := changed.validate(); err == nil {
			t.Fatal("unapproved artifact accepted")
		}
	}
	var r release
	if err := json.Unmarshal(f.responses[f.latest], &r); err != nil {
		t.Fatal(err)
	}
	r.Assets = append(r.Assets, r.Assets[0])
	f.responses[f.latest], _ = json.Marshal(r)
	if _, err := f.b.resolve(context.Background(), "ollama", "latest"); err == nil {
		t.Fatal("duplicate architecture asset accepted")
	}
	parent := t.TempDir()
	if err := os.Chmod(parent, 0700); err != nil {
		t.Fatal(err)
	}
	f.b.req.AppsDir = filepath.Join(parent, "Apps '$`literal")
	var logs []string
	f.b.log = func(s string) { logs = append(logs, s) }
	calls := f.calls
	if _, err := f.b.menu(context.Background(), "ollama"); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(logs, "\n")
	quoted := shellQuote(filepath.Join(f.b.root("ollama"), "current/bin/ollama"))
	for _, suffix := range []string{" serve", " run llama3", " pull llama3"} {
		if !strings.Contains(joined, quoted+suffix) {
			t.Fatal("missing literal manual command")
		}
	}
	if f.calls != calls {
		t.Fatal("menu fetched")
	}
	if _, err := os.Lstat(f.b.req.AppsDir); !os.IsNotExist(err) {
		t.Fatal("menu wrote")
	}
}

func TestOllamaCancellationDuringNetwork(t *testing.T) {
	f := newFixture(t, "amd64")
	a := ollamaFixtureRelease(t, f, "1.2.3")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	base := f.b.client.Transport
	f.b.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		resp, err := base.RoundTrip(r)
		if err == nil && r.URL.String() == a.URL {
			cancel()
			resp.Body = io.NopCloser(bytes.NewReader(f.responses[a.URL]))
		}
		return resp, err
	})
	if err := f.run(ctx, "install", "--provider", "ollama", "--trust-download"); !errors.Is(err, context.Canceled) {
		t.Fatalf("wanted cancellation: %v", err)
	}
	if _, err := os.Lstat(f.b.root("ollama")); !os.IsNotExist(err) {
		t.Fatal("cancelled download published")
	}
}

func TestOllamaInventoryReceiptRoundTrip(t *testing.T) {
	f := newFixture(t, "amd64")
	ollamaFixtureRelease(t, f, "1.2.3")
	mustRun(t, f, "install", "--provider", "ollama", "--trust-download")
	dir := f.b.ollamaVersion("1.2.3")
	rec, err := ollamaReadReceipt(dir)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := ollamaInventory(context.Background(), filepath.Join(dir, "tree"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rec.Files, actual) {
		t.Fatal("receipt inventory differs")
	}
	if rec.Files["lib/ollama/lib.so"].Link != "lib.so.1" {
		t.Fatal("raw library link not retained")
	}
}
