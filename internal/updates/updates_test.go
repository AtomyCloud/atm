package updates

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
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
	"time"

	"github.com/AtomyCloud/atm/internal/catalog"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type fixture struct {
	s        *Service
	trees    map[string][]map[string]any
	blobs    map[string][]byte
	current  string
	relation string
	calls    []string
	status   int
	corrupt  bool
	onBlob   func()
}

const oldCommit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const newCommit = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func fixtureService(t *testing.T) *fixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "runtime space")
	if err := os.MkdirAll(filepath.Join(home, "plugins"), 0700); err != nil {
		t.Fatal(err)
	}
	f := &fixture{trees: map[string][]map[string]any{}, blobs: map[string][]byte{}, current: oldCommit, relation: "ahead"}
	original := http.DefaultTransport
	http.DefaultTransport = transport(func(r *http.Request) (*http.Response, error) {
		if err := r.Context().Err(); err != nil {
			return nil, err
		}
		if r.URL.Host != "api.github.com" || r.URL.Scheme != "https" {
			t.Fatalf("nonofficial request: %s", r.URL)
		}
		f.calls = append(f.calls, r.URL.Path)
		var value any
		switch {
		case strings.HasSuffix(r.URL.Path, "/commits/main"):
			value = map[string]any{"sha": f.current}
		case strings.Contains(r.URL.Path, "/git/trees/"):
			sha := filepath.Base(r.URL.Path)
			tree, ok := f.trees[sha]
			if !ok {
				return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader(`{}`)), Header: http.Header{}}, nil
			}
			value = map[string]any{"truncated": false, "tree": tree}
		case strings.Contains(r.URL.Path, "/compare/"):
			value = map[string]any{"status": f.relation}
		case strings.Contains(r.URL.Path, "/git/blobs/"):
			if f.onBlob != nil {
				f.onBlob()
			}
			b, ok := f.blobs[filepath.Base(r.URL.Path)]
			if !ok {
				t.Fatalf("unknown blob")
			}
			if f.corrupt {
				b = []byte("corrupt")
			}
			value = map[string]any{"encoding": "base64", "content": base64.StdEncoding.EncodeToString(b)}
		default:
			t.Fatalf("unexpected request %s", r.URL)
		}
		b, _ := json.Marshal(value)
		status := 200
		if f.status != 0 {
			status = f.status
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(b)), Header: http.Header{}}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = original })
	f.s, err = New(Options{Home: home, PluginsDir: filepath.Join(home, "plugins"), StateDir: filepath.Join(root, "state"), Client: catalog.New("")})
	if err != nil {
		t.Fatal(err)
	}
	f.addPlugin(oldCommit, "alpha", "old")
	f.addPlugin(newCommit, "alpha", "new")
	return f
}
func (f *fixture) add(commit, id, name, content string) {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(content))
	h.Write([]byte(content))
	sha := fmt.Sprintf("%x", h.Sum(nil))
	f.blobs[sha] = []byte(content)
	f.trees[commit] = append(f.trees[commit], map[string]any{"path": "plugins/" + id + "/" + name, "type": "blob", "mode": "100644", "sha": sha, "size": len(content)})
}
func (f *fixture) addPlugin(commit, id, version string) {
	f.add(commit, id, "plugin.metadata", fmt.Sprintf("ATM_PLUGIN_ID=%s\nATM_PLUGIN_NAME_VALUE=Fixture\nATM_PLUGIN_VERSION_VALUE=0.0.1\n", id))
	f.add(commit, id, "plugin.sh", "#!/usr/bin/env bash\n# "+version+"\n")
	f.add(commit, id, "plugin.conf", "SETTING=default\n")
	f.add(commit, id, "lang/en-us.lang", "LABEL=fixture\n")
}
func (f *fixture) install(t *testing.T, id string) {
	t.Helper()
	p, err := f.s.client.Prepare(context.Background(), id, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.client.Download(context.Background(), p, f.s.options.PluginsDir); err != nil {
		t.Fatal(err)
	}
}
func write(t *testing.T, name, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(value), 0644); err != nil {
		t.Fatal(err)
	}
}
func status(t *testing.T, s *Service, id, want string) {
	t.Helper()
	r, err := s.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range r.Rows {
		if row.Component == id {
			if row.Status != want {
				t.Fatalf("%s: got %s want %s (%s)", id, row.Status, want, row.Detail)
			}
			return
		}
	}
	t.Fatalf("missing %s", id)
}
func prepare(t *testing.T, f *fixture) *Plan {
	t.Helper()
	f.current = newCommit
	p, err := f.s.Prepare(context.Background(), "alpha")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestInspectionNativePinnedAndNoAdoption(t *testing.T) {
	f := fixtureService(t)
	f.install(t, "alpha")
	t.Setenv("PATH", t.TempDir())
	t.Setenv("ATM_PLUGIN_ALPHA_ENABLED", "0")
	status(t, f.s, "core", "unknown")
	status(t, f.s, "plugin:alpha", "current")
	if _, err := os.Stat(f.s.state); !os.IsNotExist(err) {
		t.Fatal("inspection wrote state")
	}
	os.Remove(filepath.Join(f.s.root("alpha"), ".atm-origin.json"))
	status(t, f.s, "plugin:alpha", "unmanaged")
	if _, err := os.Stat(f.s.state); !os.IsNotExist(err) {
		t.Fatal("unreceipted inspection adopted installation")
	}
}
func TestApplyRestorePreservesUnknownAndCentralReceipt(t *testing.T) {
	f := fixtureService(t)
	f.install(t, "alpha")
	t.Setenv("PATH", t.TempDir())
	write(t, filepath.Join(f.s.root("alpha"), "custom/settings.txt"), "keep me")
	before, err := scan(f.s.root("alpha"))
	if err != nil {
		t.Fatal(err)
	}
	p := prepare(t, f)
	if p.Info().Commit != newCommit || len(p.Changes()) != 1 {
		t.Fatal(p.Info(), p.Changes())
	}
	f.current = oldCommit // Publication must keep the confirmed plan's snapshot.
	result, err := f.s.Apply(context.Background(), p, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "updated" || !result.RestartRequired {
		t.Fatal(result)
	}
	b, _ := os.ReadFile(filepath.Join(f.s.root("alpha"), "custom/settings.txt"))
	if string(b) != "keep me" {
		t.Fatal("unknown file lost")
	}
	backups, err := f.s.Backups()
	if err != nil || len(backups) != 1 {
		t.Fatal(backups, err)
	}
	if _, err = f.s.Restore(context.Background(), result.BackupID, true); err != nil {
		t.Fatal(err)
	}
	after, err := scan(f.s.root("alpha"))
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("restore mismatch", err)
	}
	if b, err := readOptional(f.s.receiptPath("alpha")); err != nil || b != nil {
		t.Fatal("central receipt was not removed", err)
	}
}
func TestReceiptPrecedenceAndCommitOnly(t *testing.T) {
	f := fixtureService(t)
	f.install(t, "alpha")
	b, err := readOptional(filepath.Join(f.s.root("alpha"), ".atm-origin.json"))
	if err != nil {
		t.Fatal(err)
	}
	var o origin
	json.Unmarshal(b, &o)
	o.Files = nil
	if err = writeJSON(f.s.receiptPath("alpha"), o); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(f.s.root("alpha"), ".atm-origin.json"), "invalid embedded receipt")
	p := prepare(t, f)
	if _, err = f.s.Apply(context.Background(), p, true); err != nil {
		t.Fatal(err)
	}
	status(t, f.s, "plugin:alpha", "current")
}
func TestStatesAndOwnershipGuards(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"dirty", "modified"}, {"mode", "modified"}, {"unmanaged", "unmanaged"}, {"bad-origin", "unknown"}, {"symlink", "unknown"}, {"removed", "removed"}, {"ahead", "ahead"}, {"diverged", "diverged"}, {"collision", "modified"}, {"case-collision", "modified"}, {"directory-collision", "modified"}, {"unsafe-receipt", "unknown"}, {"offline", "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := fixtureService(t)
			f.install(t, "alpha")
			f.current = newCommit
			switch tc.name {
			case "dirty":
				write(t, filepath.Join(f.s.root("alpha"), "plugin.conf"), "USER=edited\n")
			case "mode":
				if runtime.GOOS == "windows" {
					t.Skip("Windows does not expose POSIX executable modes")
				}
				if err := os.Chmod(filepath.Join(f.s.root("alpha"), "plugin.sh"), 0755); err != nil {
					t.Fatal(err)
				}
			case "unmanaged":
				os.Remove(filepath.Join(f.s.root("alpha"), ".atm-origin.json"))
			case "bad-origin":
				write(t, filepath.Join(f.s.root("alpha"), ".atm-origin.json"), `{"schema":1,"source":"https://evil.test"}`)
			case "symlink":
				if err := os.Symlink("plugin.sh", filepath.Join(f.s.root("alpha"), "link")); err != nil {
					t.Skip(err)
				}
			case "removed":
				f.trees[newCommit] = []map[string]any{}
			case "ahead":
				f.relation = "behind"
			case "diverged":
				f.relation = "diverged"
			case "collision":
				write(t, filepath.Join(f.s.root("alpha"), "new.txt"), "private")
				f.add(newCommit, "alpha", "new.txt", "upstream")
			case "case-collision":
				write(t, filepath.Join(f.s.root("alpha"), "NEW.TXT"), "private")
				f.add(newCommit, "alpha", "new.txt", "upstream")
			case "directory-collision":
				write(t, filepath.Join(f.s.root("alpha"), "new.txt/child"), "private")
				f.add(newCommit, "alpha", "new.txt", "upstream")
			case "unsafe-receipt":
				var o origin
				b, _ := readOptional(filepath.Join(f.s.root("alpha"), ".atm-origin.json"))
				json.Unmarshal(b, &o)
				o.Files["../escape"] = catalog.Fingerprint{SHA: oldCommit, Mode: "100644"}
				writeJSON(f.s.receiptPath("alpha"), o)
			case "offline":
				f.status = 503
			}
			status(t, f.s, "plugin:alpha", tc.want)
			if _, err := f.s.Prepare(context.Background(), "alpha"); err == nil {
				t.Fatal("unsafe update prepared")
			}
		})
	}
}
func TestMutationRefusalsAndStalePlan(t *testing.T) {
	for _, name := range []string{"no-consent", "git-home", "git-parent", "git-worktree", "stale-file", "stale-receipt", "corrupt-download", "cancel", "legacy-lock", "native-lock", "pending-core", "pending-plugin", "tampered-backup", "edited-after"} {
		t.Run(name, func(t *testing.T) {
			f := fixtureService(t)
			f.install(t, "alpha")
			p := prepare(t, f)
			ctx := context.Background()
			consent := true
			before, _ := scan(f.s.root("alpha"))
			switch name {
			case "no-consent":
				consent = false
			case "git-home":
				os.Mkdir(filepath.Join(f.s.options.Home, ".git"), 0700)
			case "git-parent":
				write(t, filepath.Join(filepath.Dir(f.s.options.Home), ".git", "HEAD"), "ref: refs/heads/main")
			case "git-worktree":
				write(t, filepath.Join(f.s.options.Home, ".git"), "gitdir: elsewhere")
			case "stale-file":
				write(t, filepath.Join(f.s.root("alpha"), "custom"), "new edit")
				before, _ = scan(f.s.root("alpha"))
			case "stale-receipt":
				write(t, f.s.receiptPath("alpha"), "{}")
			case "corrupt-download":
				f.corrupt = true
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "legacy-lock":
				os.MkdirAll(filepath.Join(f.s.state, "lock"), 0700)
			case "native-lock":
				unlock, err := f.s.lock(false)
				if err != nil {
					t.Fatal(err)
				}
				defer unlock()
			case "pending-core":
				write(t, filepath.Join(f.s.options.Home, ".atm-core-update-pending"), "{}")
			case "pending-plugin":
				write(t, f.s.pendingPath(), "{}")
			case "tampered-backup", "edited-after":
				r, err := f.s.Apply(ctx, p, true)
				if err != nil {
					t.Fatal(err)
				}
				if name == "tampered-backup" {
					write(t, filepath.Join(f.s.backupDir(r.BackupID), "before/plugin.sh"), "tampered")
				} else {
					write(t, filepath.Join(f.s.root("alpha"), "plugin.conf"), "edited")
				}
				before, _ = scan(f.s.root("alpha"))
				if _, err = f.s.Restore(ctx, r.BackupID, true); err == nil {
					t.Fatal("unsafe restore succeeded")
				}
				after, _ := scan(f.s.root("alpha"))
				if !reflect.DeepEqual(before, after) {
					t.Fatal("restore changed files")
				}
				return
			}
			if _, err := f.s.Apply(ctx, p, consent); err == nil {
				t.Fatal("unsafe update succeeded")
			}
			after, _ := scan(f.s.root("alpha"))
			if !reflect.DeepEqual(before, after) {
				t.Fatal("refused operation changed plugin")
			}
		})
	}
}
func TestPrepareAllIncludesDisabledOnlyInstalled(t *testing.T) {
	f := fixtureService(t)
	f.addPlugin(oldCommit, "disabled", "old")
	f.addPlugin(newCommit, "disabled", "new")
	f.addPlugin(newCommit, "absent", "new")
	f.install(t, "alpha")
	f.install(t, "disabled")
	t.Setenv("ATM_PLUGIN_DISABLED_ENABLED", "0")
	f.current = newCommit
	f.calls = nil
	plans, skips, err := f.s.PrepareAll(context.Background())
	if err != nil || len(plans) != 2 || len(skips) != 0 {
		t.Fatal(len(plans), skips, err)
	}
	tips := 0
	for _, route := range f.calls {
		if strings.HasSuffix(route, "/commits/main") {
			tips++
		}
	}
	if tips != 1 {
		t.Fatal("main resolved more than once", tips)
	}
	for _, p := range plans {
		if p.Info().Commit != newCommit {
			t.Fatal("unpinned plan")
		}
		if _, err = f.s.Apply(context.Background(), p, true); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = os.Stat(f.s.root("absent")); !os.IsNotExist(err) {
		t.Fatal("ALL downloaded absent plugin")
	}
}
func TestDryRunNoWritesOrNetwork(t *testing.T) {
	f := fixtureService(t)
	f.install(t, "alpha")
	f.s.options.DryRun = true
	f.calls = nil
	before, _ := scan(f.s.root("alpha"))
	ctx := context.Background()
	f.s.Check(ctx)
	f.s.Prepare(ctx, "alpha")
	f.s.PrepareAll(ctx)
	f.s.UpdateAll(ctx, true)
	f.s.Restore(ctx, strings.Repeat("a", 32), true)
	f.s.Recover(ctx, true)
	f.s.Verify(ctx, "alpha", oldCommit, true)
	if len(f.calls) != 0 {
		t.Fatal("dry-run contacted network")
	}
	if _, err := os.Stat(f.s.state); !os.IsNotExist(err) {
		t.Fatal("dry-run wrote state")
	}
	after, _ := scan(f.s.root("alpha"))
	if !reflect.DeepEqual(before, after) {
		t.Fatal("dry-run changed plugin")
	}
}
func TestExplicitVerifyAndChanges(t *testing.T) {
	f := fixtureService(t)
	f.install(t, "alpha")
	os.Remove(filepath.Join(f.s.root("alpha"), ".atm-origin.json"))
	if _, err := f.s.Verify(context.Background(), "alpha", oldCommit, false); err == nil {
		t.Fatal("verify without consent")
	}
	if _, err := f.s.Verify(context.Background(), "alpha", oldCommit, true); err != nil {
		t.Fatal(err)
	}
	status(t, f.s, "plugin:alpha", "current")
	write(t, filepath.Join(f.s.root("alpha"), "plugin.conf"), "edited\n")
	changes, err := f.s.Changes("alpha")
	if err != nil || len(changes) != 1 || changes[0].Kind != "modified" {
		t.Fatal(changes, err)
	}
	if _, err = f.s.Verify(context.Background(), "alpha", oldCommit, true); err == nil {
		t.Fatal("verified dirty baseline")
	}
}

func TestForgedReceiptsNeverAuthorizeOwnership(t *testing.T) {
	for _, name := range []string{"invented-commit", "forged-files", "missing-owned-file", "extra-owned-file"} {
		t.Run(name, func(t *testing.T) {
			f := fixtureService(t)
			f.install(t, "alpha")
			b, _ := readOptional(filepath.Join(f.s.root("alpha"), ".atm-origin.json"))
			var o origin
			json.Unmarshal(b, &o)
			switch name {
			case "invented-commit":
				o.Commit = strings.Repeat("c", 40)
			case "forged-files":
				write(t, filepath.Join(f.s.root("alpha"), "plugin.conf"), "CUSTOM=setting\n")
				inv, _ := scan(f.s.root("alpha"))
				o.Files["plugin.conf"] = inv["plugin.conf"]
			case "missing-owned-file":
				delete(o.Files, "plugin.conf")
			case "extra-owned-file":
				write(t, filepath.Join(f.s.root("alpha"), "external.txt"), "private")
				inv, _ := scan(f.s.root("alpha"))
				o.Files["external.txt"] = inv["external.txt"]
			}
			writeJSON(f.s.receiptPath("alpha"), o)
			status(t, f.s, "plugin:alpha", "unknown")
			f.current = newCommit
			if _, err := f.s.Prepare(context.Background(), "alpha"); err == nil {
				t.Fatal("forged origin authorized update")
			}
		})
	}
}

func TestCheckPropagatesCancellation(t *testing.T) {
	for _, mode := range []string{"canceled", "deadline", "during-metadata"} {
		t.Run(mode, func(t *testing.T) {
			f := fixtureService(t)
			f.install(t, "alpha")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			want := context.Canceled
			if mode == "canceled" {
				cancel()
			}
			if mode == "deadline" {
				var end context.CancelFunc
				ctx, end = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
				defer end()
				want = context.DeadlineExceeded
			}
			if mode == "during-metadata" {
				f.onBlob = cancel
			}
			if _, err := f.s.Check(ctx); !errors.Is(err, want) {
				t.Fatalf("got %v want %v", err, want)
			}
		})
	}
}

func TestAllReportsIndependentFailure(t *testing.T) {
	f := fixtureService(t)
	f.addPlugin(oldCommit, "dirty", "old")
	f.addPlugin(newCommit, "dirty", "new")
	f.install(t, "alpha")
	f.install(t, "dirty")
	write(t, filepath.Join(f.s.root("dirty"), "plugin.conf"), "USER=custom\n")
	f.current = newCommit
	rows, err := f.s.UpdateAll(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]string{}
	for _, r := range rows {
		states[r.Component] = r.Status
	}
	if states["plugin:alpha"] != "updated" || states["plugin:dirty"] != "modified" {
		t.Fatal(rows)
	}
	b, _ := os.ReadFile(filepath.Join(f.s.root("dirty"), "plugin.conf"))
	if string(b) != "USER=custom\n" {
		t.Fatal("dirty configuration replaced")
	}
}

func TestStateRecordMustRemainRecoverable(t *testing.T) {
	name := filepath.Join(t.TempDir(), "metadata.json")
	if err := writeJSON(name, map[string]string{"oversized": strings.Repeat("x", 4<<20)}); err == nil {
		t.Fatal("unreadable recovery record written")
	}
	if _, err := os.Stat(name); !os.IsNotExist(err) {
		t.Fatal("oversized state was published", err)
	}
}
