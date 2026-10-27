package catalog

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/AtomyCloud/atm/internal/plugin"
)

func downloadFixture(t *testing.T) *fixture {
	f := newFixture(t)
	f.add("plugins/alpha/plugin.json", manifest("alpha", "Alpha", 1))
	f.add("plugins/alpha/plugin.metadata", []byte("ATM_PLUGIN_ID=alpha\nATM_PLUGIN_NAME_VALUE=Alpha\nATM_PLUGIN_VERSION_VALUE=0.0.1\nATM_PLUGIN_ENTRYPOINT=plugin.sh\n"))
	f.add("plugins/alpha/plugin.sh", []byte("#!/bin/bash\nprintf 'must never run'\ntouch sentinel\n"))
	f.tree[len(f.tree)-1].Mode = "100755"
	f.add("plugins/alpha/plugin.conf", []byte("SETTING='a literal value'\n"))
	f.add("plugins/alpha/lang/en-us.lang", []byte("HELLO='Hello'\n"))
	f.add("plugins/alpha/assets/icon.bin", []byte{0, 0xff, 0x1b, 10})
	return f
}

func prepared(t *testing.T, f *fixture) *DownloadPlan {
	t.Helper()
	p, err := f.client.Prepare(context.Background(), "alpha", nil)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDownloadPublishesCompleteVerifiedPlugin(t *testing.T) {
	f := downloadFixture(t)
	p := prepared(t, f)
	root := filepath.Join(t.TempDir(), "Plugins with spaces")
	t.Setenv("PATH", "")
	result, err := f.client.Download(context.Background(), p, root)
	if err != nil {
		t.Fatal(err)
	}
	if result.ID != "alpha" || result.Commit != f.revision || !result.RestartRequired {
		t.Fatal(result)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 1 || entries[0].Name() != "alpha" {
		t.Fatal("staging or lock leaked", entries)
	}
	var receipt struct {
		Schema                         int
		Source, Branch, Commit, Plugin string
		Files                          map[string]struct{ SHA, Mode string }
	}
	b, err := os.ReadFile(filepath.Join(root, "alpha", ".atm-origin.json"))
	if err != nil || json.Unmarshal(b, &receipt) != nil {
		t.Fatal(err)
	}
	if receipt.Schema != 1 || receipt.Source != Source || receipt.Branch != "main" || receipt.Commit != f.revision || receipt.Plugin != "alpha" || len(receipt.Files) != len(p.files) {
		t.Fatal("incompatible receipt", receipt)
	}
	for name, e := range p.files {
		b, err := os.ReadFile(filepath.Join(root, "alpha", filepath.FromSlash(name)))
		if err != nil || !bytes.Equal(b, f.blobs[e.SHA]) || receipt.Files[name].SHA != e.SHA || receipt.Files[name].Mode != e.Mode {
			t.Fatal("payload or receipt mismatch", name, err)
		}
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(filepath.Join(root, "alpha", "plugin.sh"))
		if info.Mode().Perm() != 0755 {
			t.Fatal("executable mode lost")
		}
	}
	if _, err := os.Stat(filepath.Join(root, "alpha", "sentinel")); !os.IsNotExist(err) {
		t.Fatal("downloaded script ran")
	}
}

func TestPrepareUsesSearchCommitAndWritesNothing(t *testing.T) {
	f := downloadFixture(t)
	s, err := f.client.Search(context.Background(), "alpha")
	if err != nil {
		t.Fatal(err)
	}
	original := f.revision
	f.revision = strings.Repeat("b", 40)
	calls := len(f.calls)
	p, err := f.client.Prepare(context.Background(), "alpha", &s)
	if err != nil || p.Info().Commit != original {
		t.Fatal(p, err)
	}
	for _, call := range f.calls[calls:] {
		if strings.Contains(call, "commits/") || strings.Contains(call, "trees/") {
			t.Fatal("repinned searched snapshot")
		}
	}
	if _, err := f.client.Prepare(context.Background(), "alpha", &Snapshot{Commit: original}); err == nil {
		t.Fatal("accepted forged snapshot")
	}
	if _, err := f.client.Prepare(context.Background(), "../alpha", nil); err == nil {
		t.Fatal("accepted unsafe ID")
	}
}

func TestDownloadPreservesExistingTargetsAndLocks(t *testing.T) {
	for _, kind := range []string{"empty-directory", "populated-directory", "file", "broken-symlink", "lock", "case-collision"} {
		t.Run(kind, func(t *testing.T) {
			f := downloadFixture(t)
			p := prepared(t, f)
			root := t.TempDir()
			target := filepath.Join(root, "alpha")
			switch kind {
			case "empty-directory":
				os.Mkdir(target, 0700)
			case "populated-directory":
				os.Mkdir(target, 0700)
				os.WriteFile(filepath.Join(target, "keep"), []byte("mine"), 0600)
			case "file":
				os.WriteFile(target, []byte("mine"), 0600)
			case "case-collision":
				os.Mkdir(filepath.Join(root, "ALPHA"), 0700)
			case "broken-symlink":
				if err := os.Symlink("missing", target); err != nil {
					t.Skip("symlink privileges unavailable")
				}
			case "lock":
				os.Mkdir(filepath.Join(root, ".atm-download-lock-alpha"), 0700)
			}
			before := len(f.calls)
			if _, err := f.client.Download(context.Background(), p, root); err == nil {
				t.Fatal("replaced existing object")
			}
			if len(f.calls) != before {
				t.Fatal("fetched payload before local guard")
			}
			entries, _ := os.ReadDir(root)
			if len(entries) != 1 {
				t.Fatal("modified destination", entries)
			}
		})
	}
}

func TestPrepareRejectsUnsafePayloadInventory(t *testing.T) {
	for _, name := range []string{".atm-origin.json", "sub/.atm-owned", ".git/config", "../escape", "file:stream", "NUL.txt", "COM1", "folder./file", "dir/file ", "dir\\file", "link", "submodule", "huge", "collision", "parent-case"} {
		t.Run(name, func(t *testing.T) {
			f := downloadFixture(t)
			f.add("plugins/alpha/"+name, []byte("payload"))
			e := &f.tree[len(f.tree)-1]
			switch name {
			case "link":
				e.Mode = "120000"
			case "submodule":
				e.Type = "commit"
				e.Mode = "160000"
			case "huge":
				n := int64(maxDownloadFile + 1)
				e.Size = &n
			case "collision":
				f.add("plugins/alpha/collision/child", []byte("child"))
			case "parent-case":
				f.add("plugins/alpha/DIR/a", []byte("a"))
				f.add("plugins/alpha/dir/b", []byte("b"))
			}
			if _, err := f.client.Prepare(context.Background(), "alpha", nil); err == nil {
				t.Fatal("accepted unsafe inventory")
			}
		})
	}
}

func TestDownloadCleansStagingOnFailureAndCancellation(t *testing.T) {
	for _, kind := range []string{"corrupt", "bad-shell", "missing-required", "cancel-before", "cancel-during"} {
		t.Run(kind, func(t *testing.T) {
			f := downloadFixture(t)
			if kind == "bad-shell" {
				f.add("plugins/alpha/bad.sh", []byte("if then\n"))
			}
			if kind == "missing-required" {
				for i, e := range f.tree {
					if strings.HasSuffix(e.Path, "plugin.conf") {
						f.tree = append(f.tree[:i], f.tree[i+1:]...)
						break
					}
				}
			}
			p := prepared(t, f)
			root := filepath.Join(t.TempDir(), "plugins")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if kind == "corrupt" {
				f.corrupt = true
			}
			if kind == "cancel-before" {
				cancel()
			}
			if kind == "cancel-during" {
				underlying := f.client.http.Transport
				f.client.http.Transport = transportFunc(func(r *http.Request) (*http.Response, error) { cancel(); return underlying.RoundTrip(r) })
			}
			if _, err := f.client.Download(ctx, p, root); err == nil {
				t.Fatal("reported failed payload as success")
			}
			entries, err := os.ReadDir(root)
			if !os.IsNotExist(err) && (err != nil || len(entries) != 0) {
				t.Fatal("partial publication or staging leak", entries, err)
			}
			if kind == "cancel-before" {
				if _, err := os.Stat(root); !os.IsNotExist(err) {
					t.Fatal("cancel created root")
				}
			}
		})
	}
}

func TestAtomicPublicationRefusesConcurrentDestination(t *testing.T) {
	for _, kind := range []string{"empty", "file", "populated"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			os.Mkdir(filepath.Join(root, "stage"), 0700)
			os.Mkdir(filepath.Join(root, "stage", "payload"), 0700)
			os.WriteFile(filepath.Join(root, "stage", "payload", "new"), []byte("new"), 0600)
			dest := filepath.Join(root, "alpha")
			if kind == "file" {
				os.WriteFile(dest, []byte("old"), 0600)
			} else {
				os.Mkdir(dest, 0700)
				if kind == "populated" {
					os.WriteFile(filepath.Join(dest, "old"), []byte("old"), 0600)
				}
			}
			from, err := os.Open(filepath.Join(root, "stage"))
			if err != nil {
				t.Fatal(err)
			}
			defer from.Close()
			to, err := os.Open(root)
			if err != nil {
				t.Fatal(err)
			}
			defer to.Close()
			if err := publishNoReplace(from, to, "payload", "alpha"); err == nil {
				t.Fatal("overwrote concurrent destination")
			}
			if _, err := os.Stat(filepath.Join(root, "stage", "payload", "new")); err != nil {
				t.Fatal("lost staged source", err)
			}
			if kind == "empty" {
				entries, _ := os.ReadDir(dest)
				if len(entries) != 0 {
					t.Fatal("filled existing directory")
				}
			}
		})
	}
}

func TestNativePayloadHeaderAndModeValidation(t *testing.T) {
	for _, bad := range []string{"", "header", "mode", "missing"} {
		t.Run("native-"+bad, func(t *testing.T) {
			f := newFixture(t)
			m := plugin.Manifest{Schema: 2, ID: "alpha", Name: "Alpha", Version: "0.0.1", MinAPI: 2, Platforms: []string{"linux"}, Executor: "native", Protocol: "jsonl-v1", Operations: []string{"status"}, Executables: map[string]string{"linux/amd64": "bin/alpha"}}
			b, _ := json.Marshal(m)
			f.add("plugins/alpha/plugin.json", b)
			if bad != "missing" {
				b = make([]byte, 64)
				copy(b, []byte("\x7fELF"))
				b[4] = 2
				b[5] = 1
				binary.LittleEndian.PutUint16(b[18:20], 62)
				if bad == "header" {
					b[0] = 0
				}
				f.add("plugins/alpha/bin/alpha", b)
				if bad != "mode" {
					f.tree[len(f.tree)-1].Mode = "100755"
				}
			}
			p := prepared(t, f)
			_, err := f.client.Download(context.Background(), p, t.TempDir())
			if (bad == "") != (err == nil) {
				t.Fatal("binary validation", bad, err)
			}
		})
	}
}

func TestAllLegacyShellSourcesParseWithoutBash(t *testing.T) {
	err := filepath.WalkDir("../../plugins", func(name string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			return nil
		}
		b, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		return checkShell(name, b)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDownloadUnverifiedPlanAndRootSymlink(t *testing.T) {
	f := downloadFixture(t)
	if _, err := f.client.Download(context.Background(), &DownloadPlan{}, t.TempDir()); err == nil {
		t.Fatal("unverified plan accepted")
	}
	root := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(root, link); err != nil {
		t.Skip("symlink unavailable")
	}
	_, err := f.client.Download(context.Background(), prepared(t, f), link)
	if err == nil || errors.Is(err, context.Canceled) {
		t.Fatal("root symlink accepted", err)
	}
}

func TestConcurrentDestinationAndStagedTampering(t *testing.T) {
	for _, kind := range []string{"destination", "staging"} {
		t.Run(kind, func(t *testing.T) {
			f := downloadFixture(t)
			p := prepared(t, f)
			root := t.TempDir()
			underlying := f.client.http.Transport
			trigger := p.files["plugin.sh"].SHA
			f.client.http.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				if strings.HasSuffix(r.URL.Path, trigger) {
					if kind == "destination" {
						if err := os.Mkdir(filepath.Join(root, "alpha"), 0700); err != nil {
							t.Fatal(err)
						}
					} else {
						if err := os.WriteFile(filepath.Join(root, ".atm-download-lock-alpha", "payload", "assets", "icon.bin"), []byte{1, 2, 3, 4}, 0644); err != nil {
							t.Fatal(err)
						}
					}
				}
				return underlying.RoundTrip(r)
			})
			if _, err := f.client.Download(context.Background(), p, root); err == nil {
				t.Fatal("published after race/tampering")
			}
			entries, err := os.ReadDir(root)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "destination" {
				if len(entries) != 1 || entries[0].Name() != "alpha" {
					t.Fatal("lost concurrent directory")
				}
				files, _ := os.ReadDir(filepath.Join(root, "alpha"))
				if len(files) != 0 {
					t.Fatal("replaced empty directory")
				}
			} else if len(entries) != 0 {
				t.Fatal("staging leaked")
			}
		})
	}
}
