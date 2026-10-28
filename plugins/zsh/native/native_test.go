//go:build linux

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/AtomyCloud/atm/internal/protocol"
)

const testCommit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const secondCommit = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

type fixture struct {
	b     *backend
	calls [][]string
	logs  []string
	acct  account
	root  string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	must(t, os.Chmod(root, 0700))
	f := &fixture{root: root}
	b := &backend{home: filepath.Join(root, "home"), state: filepath.Join(root, "state"), backups: filepath.Join(root, "Backups"), rename: renameAbsent}
	f.b = b
	b.dot = b.home
	b.omz = filepath.Join(b.home, ".oh-my-zsh")
	b.custom = filepath.Join(b.omz, "custom")
	b.req = protocol.Request{Protocol: 1, RequestID: strings.Repeat("0", 32), PluginID: "zsh", Home: filepath.Join(root, "runtime"), AppsDir: filepath.Join(root, "Apps")}
	for _, p := range []string{b.home, b.req.Home, b.req.AppsDir} {
		must(t, os.Mkdir(p, 0700))
	}
	f.acct = account{"fixture", os.Getuid(), b.home, "/bin/bash"}
	b.account = func() (account, error) { return f.acct, nil }
	b.removalSafe = func() error { return nil }
	b.shells = func() ([]string, error) { return []string{"/bin/bash", "/usr/bin/zsh"}, nil }
	b.resolve = func(name string) (string, error) { return "/trusted/" + name, nil }
	b.run = func(_ context.Context, p string, args []string) (string, error) {
		f.calls = append(f.calls, append([]string{p}, args...))
		return "", nil
	}
	b.log = func(s string) { f.logs = append(f.logs, s) }
	b.fetch = func(_ context.Context, commit string) ([]byte, error) { return officialFixture(t, commit, nil), nil }
	b.discover = func(context.Context) (string, error) { return testCommit, nil }
	must(t, b.validatePaths())
	return f
}
func must(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func put(t *testing.T, p, s string)      { t.Helper(); must(t, os.WriteFile(p, []byte(s), 0600)) }
func read(t *testing.T, p string) []byte { t.Helper(); v, e := os.ReadFile(p); must(t, e); return v }
func absent(t *testing.T, p string) {
	t.Helper()
	if _, e := os.Lstat(p); !os.IsNotExist(e) {
		t.Fatalf("expected absent %s: %v", p, e)
	}
}
func tree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	must(t, filepath.Walk(root, func(p string, i os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		s := i.Mode().String()
		if i.Mode().IsRegular() {
			s += " " + string(read(t, p))
		}
		if i.Mode()&os.ModeSymlink != 0 {
			v, e := os.Readlink(p)
			must(t, e)
			s += " " + v
		}
		out[strings.TrimPrefix(p, root)] = s
		return nil
	}))
	return out
}
func officialFixture(t *testing.T, commit string, extra []*tar.Header) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	prefix := "ohmyzsh-" + commit
	must(t, tw.WriteHeader(&tar.Header{Name: "pax_global_header", Typeflag: tar.TypeXGlobalHeader, PAXRecords: map[string]string{"comment": commit}}))
	must(t, tw.WriteHeader(&tar.Header{Name: prefix + "/", Typeflag: tar.TypeDir, Mode: 0755}))
	data := []byte("synthetic framework " + commit)
	must(t, tw.WriteHeader(&tar.Header{Name: prefix + "/oh-my-zsh.sh", Typeflag: tar.TypeReg, Mode: 0644, Size: int64(len(data))}))
	_, e := tw.Write(data)
	must(t, e)
	must(t, tw.WriteHeader(&tar.Header{Name: prefix + "/custom/", Typeflag: tar.TypeDir, Mode: 0755}))
	for _, h := range extra {
		copy := *h
		copy.Name = prefix + "/" + h.Name
		must(t, tw.WriteHeader(&copy))
		if h.Size > 0 {
			_, e = tw.Write(bytes.Repeat([]byte("x"), int(h.Size)))
			must(t, e)
		}
	}
	must(t, tw.Close())
	must(t, gz.Close())
	return buf.Bytes()
}
func installArgs() []string {
	return []string{"--commit", testCommit, "--trust-official-omz", "--accept-system-packages", "--accept-snapshot"}
}
func latestSnapshot(t *testing.T, f *fixture) []string {
	t.Helper()
	for i := len(f.logs) - 1; i >= 0; i-- {
		if strings.HasPrefix(f.logs[i], "Private unencrypted snapshot: ") {
			p := strings.TrimPrefix(f.logs[i], "Private unencrypted snapshot: ")
			return []string{"restore", p, "--sha256", digest(read(t, p)), "--accept-restore", "--accept-snapshot"}
		}
	}
	t.Fatal("missing snapshot log")
	return nil
}
func execute(t *testing.T, f *fixture, op string, args ...string) []protocol.Choice {
	t.Helper()
	f.b.req.Operation = op
	f.b.req.Arguments = args
	cs, e := f.b.execute(context.Background())
	must(t, e)
	return cs
}

func TestConsentAndReadonlyRoutes(t *testing.T) {
	f := newFixture(t)
	put(t, filepath.Join(f.b.dot, ".zshrc"), "private manual config")
	must(t, os.Mkdir(f.b.omz, 0700))
	put(t, filepath.Join(f.b.omz, "manual"), "keep")
	before := tree(t, f.root)
	f.b.fetch = func(context.Context, string) ([]byte, error) { t.Fatal("network"); return nil, nil }
	f.b.discover = func(context.Context) (string, error) { t.Fatal("discovery"); return "", nil }
	for _, op := range []string{"status", "menu"} {
		execute(t, f, op)
	}
	for _, op := range []string{"install", "update", "remove", "uninstall", "use"} {
		f.b.req.Operation = op
		f.b.req.Arguments = nil
		if _, e := f.b.execute(context.Background()); e == nil {
			t.Fatal(op, "accepted missing consent")
		}
	}
	for _, view := range []string{"install", "update", "remove", "backup"} {
		execute(t, f, "menu", view)
	}
	if len(f.calls) != 0 || !reflect.DeepEqual(before, tree(t, f.root)) {
		t.Fatal("readonly routes changed state")
	}
	t.Setenv("ATM_DRY_RUN", "1")
	for _, op := range []string{"install", "update", "remove", "uninstall", "use", "menu", "status"} {
		execute(t, f, op, "anything")
	}
	if !reflect.DeepEqual(before, tree(t, f.root)) {
		t.Fatal("dry run wrote")
	}
}
func TestArgumentGrammarAndAPTPolicy(t *testing.T) {
	for _, action := range []string{"install", "update", "remove"} {
		args := installArgs()
		if action == "remove" {
			args = []string{"--accept-system-packages", "--accept-removal", "--accept-snapshot"}
		}
		_, e := parseOptions(action, args)
		must(t, e)
		for _, bad := range [][]string{append(append([]string{}, args...), args[0]), {"--wat"}, {"--commit", "master"}, {"--accept-snapshot"}} {
			if _, e = parseOptions(action, bad); e == nil {
				t.Fatalf("accepted %v", bad)
			}
		}
		commands := aptArguments(action)
		for _, cmd := range commands {
			s := strings.Join(cmd, " ")
			for _, policy := range []string{"APT::Get::AllowUnauthenticated=false", "Acquire::AllowInsecureRepositories=false", "Acquire::AllowDowngradeToInsecureRepositories=false", "APT::Get::AutomaticRemove=false", "APT::Get::Purge=false"} {
				if !strings.Contains(s, policy) {
					t.Fatal("missing", policy)
				}
			}
			if strings.Contains(s, " install ") && !strings.Contains(s, "--no-remove") {
				t.Fatal("implicit removals allowed")
			}
		}
		if action != "remove" && !strings.Contains(strings.Join(commands[0], " "), "APT::Update::Error-Mode=any") {
			t.Fatal("refresh error mode")
		}
	}
}
func TestNativeLifecycleAndLiteralCommands(t *testing.T) {
	f := newFixture(t)
	put(t, filepath.Join(f.b.dot, ".zsh_history"), "private history")
	execute(t, f, "install", installArgs()...)
	r, e := f.b.ownedReceipt(context.Background())
	must(t, e)
	if r.Commit != testCommit || r.RC == "" {
		t.Fatal("missing provenance/loader")
	}
	loader := string(read(t, filepath.Join(f.b.dot, ".zshrc")))
	for _, s := range []string{"zstyle ':omz:update' mode disabled", "ZSH_CACHE_DIR=", "ZSH_COMPDUMP="} {
		if !strings.Contains(loader, s) {
			t.Fatal("missing loader setting", s)
		}
	}
	args := installArgs()
	args[1] = secondCommit
	execute(t, f, "update", args...)
	r, e = f.b.ownedReceipt(context.Background())
	must(t, e)
	if r.Commit != secondCommit {
		t.Fatal("update not activated")
	}
	execute(t, f, "remove", "--accept-system-packages", "--accept-removal", "--accept-snapshot")
	absent(t, f.b.omz)
	absent(t, filepath.Join(f.b.dot, ".zshrc"))
	absent(t, filepath.Join(f.b.state, "receipt.json"))
	if string(read(t, filepath.Join(f.b.dot, ".zsh_history"))) != "private history" {
		t.Fatal("history lost")
	}
	if len(f.calls) != 5 {
		t.Fatal(f.calls)
	}
	for _, c := range f.calls {
		if c[0] != "/trusted/sudo" || c[1] != "-n" || c[2] != "--" || c[3] != "/trusted/apt-get" {
			t.Fatal("unexpected command", c)
		}
	}
	cs := execute(t, f, "menu", "backups")
	if len(cs) != 3 {
		t.Fatal("missing snapshots", len(cs))
	}
}
func TestExistingRCAndManualFrameworkPreserved(t *testing.T) {
	t.Run("rc", func(t *testing.T) {
		f := newFixture(t)
		p := filepath.Join(f.b.dot, ".zshrc")
		put(t, p, "user config")
		execute(t, f, "install", installArgs()...)
		if string(read(t, p)) != "user config" {
			t.Fatal("rc overwritten")
		}
		if !strings.Contains(strings.Join(f.logs, " "), "manual activation required") {
			t.Fatal("missing activation warning")
		}
		execute(t, f, "remove", "--accept-system-packages", "--accept-removal", "--accept-snapshot")
		if string(read(t, p)) != "user config" {
			t.Fatal("manual rc removed")
		}
	})
	t.Run("framework", func(t *testing.T) {
		f := newFixture(t)
		must(t, os.Mkdir(f.b.omz, 0700))
		put(t, filepath.Join(f.b.omz, "oh-my-zsh.sh"), "manual")
		before := tree(t, f.root)
		if e := f.b.apply(context.Background(), "install", options{testCommit}); e == nil {
			t.Fatal("adopted manual framework")
		}
		if len(f.calls) != 0 || !reflect.DeepEqual(before, tree(t, f.root)) {
			t.Fatal("manual state touched")
		}
	})
}
func TestEditedManagedDataBlocksReplacement(t *testing.T) {
	for _, which := range []string{"framework", "custom", "loader", "receipt"} {
		t.Run(which, func(t *testing.T) {
			f := newFixture(t)
			execute(t, f, "install", installArgs()...)
			switch which {
			case "framework":
				put(t, filepath.Join(f.b.omz, "oh-my-zsh.sh"), "user edit")
			case "custom":
				put(t, filepath.Join(f.b.custom, "local.zsh"), "user custom")
			case "loader":
				put(t, filepath.Join(f.b.dot, ".zshrc"), "edited loader")
			case "receipt":
				must(t, os.Chmod(filepath.Join(f.b.state, "receipt.json"), 0644))
			}
			before := tree(t, f.root)
			calls := len(f.calls)
			for _, action := range []string{"update", "remove"} {
				if e := f.b.apply(context.Background(), action, options{secondCommit}); e == nil {
					t.Fatal("accepted edited ", which)
				}
			}
			if len(f.calls) != calls || !reflect.DeepEqual(before, tree(t, f.root)) {
				t.Fatal("edited data mutated")
			}
		})
	}
}
func TestSnapshotRestoreAndOwnershipInvalidation(t *testing.T) {
	f := newFixture(t)
	execute(t, f, "install", installArgs()...)
	put(t, filepath.Join(f.b.dot, ".zsh_history"), "first")
	execute(t, f, "menu", "backup", "--accept-snapshot")
	restore := latestSnapshot(t, f)
	put(t, filepath.Join(f.b.dot, ".zsh_history"), "second")
	put(t, filepath.Join(f.b.omz, "oh-my-zsh.sh"), "post-snapshot edit")
	execute(t, f, "menu", restore...)
	if string(read(t, filepath.Join(f.b.dot, ".zsh_history"))) != "first" {
		t.Fatal("restore mismatch")
	}
	absent(t, filepath.Join(f.b.state, "receipt.json"))
	if _, e := f.b.ownedReceipt(context.Background()); e == nil {
		t.Fatal("restored data gained authority")
	}
	found := false
	for p, v := range tree(t, f.b.home) {
		if strings.Contains(p, "recovery") && strings.Contains(v, "post-snapshot edit") {
			found = true
		}
	}
	if !found {
		t.Fatal("displaced framework missing")
	}
}
func archiveBytes(t *testing.T, s any) []byte {
	t.Helper()
	v, e := json.Marshal(s)
	must(t, e)
	return gzipBytes(t, v)
}
func gzipBytes(t *testing.T, v []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	_, e := gz.Write(v)
	must(t, e)
	must(t, gz.Close())
	return buf.Bytes()
}
func TestMalformedSnapshotsRefuseBeforeWrites(t *testing.T) {
	good := entry{Root: "zshrc", Path: ".", Mode: 0600, Hash: digest([]byte("hello")), Data: []byte("hello")}
	cases := map[string][]byte{}
	for name, change := range map[string]func(*entry){"traversal": func(e *entry) { e.Path = "../escape" }, "root": func(e *entry) { e.Root = "passwd" }, "directory-dotfile": func(e *entry) { e.Dir = true; e.Data = nil; e.Hash = "" }, "dot-subtree": func(e *entry) { e.Path = "child" }, "hash": func(e *entry) { e.Hash = strings.Repeat("0", 64) }, "mode": func(e *entry) { e.Mode = 0666 }, "file-tree": func(e *entry) { e.Root = "omz" }} {
		en := good
		change(&en)
		cases[name] = archiveBytes(t, snapshot{snapshotFormat, []entry{en}})
	}
	cases["duplicate-entry"] = archiveBytes(t, snapshot{snapshotFormat, []entry{good, good}})
	cases["legacy"] = archiveBytes(t, snapshot{"atm-zsh-backup-v1", nil})
	cases["unknown-field"] = gzipBytes(t, []byte(`{"format":"atm-zsh-native-v1","entries":[],"unknown":1}`))
	cases["duplicate-key"] = gzipBytes(t, []byte(`{"format":"bad","format":"atm-zsh-native-v1","entries":[]}`))
	cases["duplicate-nested-key"] = gzipBytes(t, []byte(`{"format":"atm-zsh-native-v1","entries":[{"root":"omz","root":"zshrc"}]}`))
	valid := archiveBytes(t, snapshot{snapshotFormat, []entry{good}})
	cases["trailing"] = append(append([]byte{}, valid...), []byte("trailer")...)
	bad := append([]byte{}, valid...)
	bad[len(bad)-5] ^= 255
	cases["crc"] = bad
	cases["two-gzip"] = append(append([]byte{}, valid...), valid...)
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			must(t, os.Mkdir(f.b.backups, 0700))
			p := filepath.Join(f.b.backups, "zsh-native-bad.json.gz")
			must(t, os.WriteFile(p, data, 0600))
			before := tree(t, f.root)
			if e := f.b.restore(context.Background(), p, digest(data)); e == nil {
				t.Fatal("accepted malformed archive")
			}
			if !reflect.DeepEqual(before, tree(t, f.root)) {
				t.Fatal("malformed archive wrote state")
			}
		})
	}
}
func TestPrivatePathsLinksAndTypes(t *testing.T) {
	for _, kind := range []string{"file-symlink", "dir-symlink", "hardlink", "fifo", "writable", "directory-dotfile", "symlink-ancestor"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			p := filepath.Join(f.b.dot, ".zshrc")
			other := filepath.Join(f.root, "external")
			put(t, other, "external")
			switch kind {
			case "file-symlink":
				must(t, os.Symlink(other, p))
			case "dir-symlink":
				must(t, os.Symlink(f.root, f.b.omz))
			case "hardlink":
				must(t, os.Link(other, p))
			case "fifo":
				must(t, makeFIFO(p))
			case "writable":
				put(t, p, "x")
				must(t, os.Chmod(p, 0666))
			case "directory-dotfile":
				must(t, os.Mkdir(p, 0700))
			case "symlink-ancestor":
				f.b.dot = filepath.Join(f.root, "linked")
				must(t, os.Symlink(f.b.home, f.b.dot))
			}
			if e := f.b.backup(context.Background()); e == nil {
				t.Fatal("accepted unsafe data")
			}
			absent(t, f.b.state)
			if string(read(t, other)) != "external" {
				t.Fatal("external data touched")
			}
		})
	}
	f := newFixture(t)
	f.b.backups = f.b.omz
	if e := f.b.validatePaths(); e == nil {
		t.Fatal("overlap accepted")
	}
}
func TestAPTFailureAndCancellationRetainRecovery(t *testing.T) {
	for _, stage := range []string{"refresh", "install", "cancel"} {
		t.Run(stage, func(t *testing.T) {
			f := newFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			f.b.run = func(context.Context, string, []string) (string, error) {
				calls++
				if stage == "cancel" {
					cancel()
					return "", context.Canceled
				}
				if stage == "refresh" || calls == 2 {
					return "", errors.New("synthetic APT failure")
				}
				return "", nil
			}
			if e := f.b.apply(ctx, "install", options{testCommit}); e == nil {
				t.Fatal("failure reported success")
			}
			absent(t, f.b.omz)
			absent(t, filepath.Join(f.b.dot, ".zshrc"))
			absent(t, filepath.Join(f.b.state, "receipt.json"))
			journal := read(t, filepath.Join(f.b.state, "operation", "journal.json"))
			if !bytes.Contains(journal, []byte("apt-running")) || !bytes.Contains(journal, []byte("staging")) {
				t.Fatal("missing recovery evidence")
			}
			if _, e := f.b.begin("retry"); e == nil {
				t.Fatal("interrupted attempt retried")
			}
		})
	}
}
func TestAccountParsingAndRemovalChecks(t *testing.T) {
	good := "fixture:x:123:123::/home/fixture:/bin/bash\n"
	a, e := parseAccount([]byte(good), 123)
	must(t, e)
	if a.Name != "fixture" {
		t.Fatal(a)
	}
	for _, v := range []string{good + good, "bad", strings.Replace(good, "fixture:x", "-evil:x", 1), strings.Replace(good, "/home/fixture", "relative", 1)} {
		if _, e = parseAccount([]byte(v), 123); e == nil {
			t.Fatal("bad account accepted")
		}
	}
	must(t, noLocalZsh([]byte(good)))
	if noLocalZsh([]byte(good+"other:x:456:456::/home/other:/usr/bin/zsh\n")) == nil {
		t.Fatal("other zsh account ignored")
	}
	f := newFixture(t)
	execute(t, f, "install", installArgs()...)
	n := len(f.calls)
	f.b.removalSafe = func() error { return errors.New("other account uses zsh") }
	if e = f.b.apply(context.Background(), "remove", options{}); e == nil || len(f.calls) != n {
		t.Fatal("unsafe removal executed")
	}
	f.b.removalSafe = func() error { return nil }
	f.acct.Shell = "/usr/bin/zsh"
	if e = f.b.apply(context.Background(), "remove", options{}); e == nil || len(f.calls) != n {
		t.Fatal("current zsh removed")
	}
}
func TestShellChangeVerification(t *testing.T) {
	f := newFixture(t)
	execute(t, f, "use", "/bin/bash", "--accept-shell-change")
	absent(t, f.b.state)
	f.b.run = func(_ context.Context, p string, args []string) (string, error) {
		want := []string{"-n", "--", "/trusted/chsh", "-s", "/usr/bin/zsh", "fixture"}
		if p != "/trusted/sudo" || !reflect.DeepEqual(want, args) {
			t.Fatal(p, args)
		}
		f.acct.Shell = "/usr/bin/zsh"
		return "", nil
	}
	execute(t, f, "use", "/usr/bin/zsh", "--accept-shell-change")
	if !strings.Contains(strings.Join(f.logs, " "), "log out and log back in") {
		t.Fatal("missing login notice")
	}
	g := newFixture(t)
	if e := g.b.changeShell(context.Background(), "/tmp/custom"); e == nil {
		t.Fatal("unregistered shell accepted")
	}
	absent(t, g.b.state)
	if e := g.b.changeShell(context.Background(), "/usr/bin/zsh"); e == nil {
		t.Fatal("unverified shell change accepted")
	}
	if _, e := os.Stat(filepath.Join(g.b.state, "operation")); e != nil {
		t.Fatal("missing failed chsh journal")
	}
	h := newFixture(t)
	h.acct.Home = "/other"
	if e := h.b.changeShell(context.Background(), "/usr/bin/zsh"); e == nil {
		t.Fatal("wrong account home accepted")
	}
	absent(t, h.b.state)
}
func TestLatestDiscoveryIsExplicitAndPinned(t *testing.T) {
	f := newFixture(t)
	n := 0
	f.b.discover = func(context.Context) (string, error) { n++; return testCommit, nil }
	execute(t, f, "menu")
	execute(t, f, "menu", "install")
	execute(t, f, "menu", "update")
	if n != 0 {
		t.Fatal("implicit discovery")
	}
	cs := execute(t, f, "menu", "install", "latest")
	if n != 1 || len(cs) != 1 || !reflect.DeepEqual(cs[0].Arguments, installArgs()) {
		t.Fatal("not pinned", cs)
	}
	absent(t, f.b.state)
	for _, v := range []string{`{"sha":"master"}`, `{"sha":"` + testCommit + `","sha":"` + secondCommit + `"}`, `{"sha":"` + testCommit + `"} {}`} {
		if _, e := parseCommit([]byte(v)); e == nil {
			t.Fatal("bad metadata accepted")
		}
	}
	commit, e := parseCommit([]byte(`{"sha":"` + testCommit + `","commit":{"message":"fixture"}}`))
	must(t, e)
	if commit != testCommit {
		t.Fatal(commit)
	}
}
func TestOfficialArchiveAliasesAndAttacks(t *testing.T) {
	valid := []*tar.Header{{Name: "custom/alias", Typeflag: tar.TypeSymlink, Linkname: "../oh-my-zsh.sh", Mode: 0777}, {Name: "alias", Typeflag: tar.TypeSymlink, Linkname: "./oh-my-zsh.sh", Mode: 0777}}
	es, e := officialEntries(context.Background(), officialFixture(t, testCommit, valid), testCommit)
	must(t, e)
	for _, en := range es {
		if en.Path == "alias" || en.Path == "custom/alias" {
			if en.Hash == "" || en.Mode != 0600 || en.Dir {
				t.Fatal("alias not materialized")
			}
		}
	}
	for name, extra := range map[string][]*tar.Header{
		"escape": {{Name: "alias", Typeflag: tar.TypeSymlink, Linkname: "../outside"}}, "absolute": {{Name: "alias", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"}},
		"cycle": {{Name: "alias", Typeflag: tar.TypeSymlink, Linkname: "alias"}}, "directory": {{Name: "alias", Typeflag: tar.TypeSymlink, Linkname: "custom"}},
		"chain":    {{Name: "alias", Typeflag: tar.TypeSymlink, Linkname: "second"}, {Name: "second", Typeflag: tar.TypeSymlink, Linkname: "oh-my-zsh.sh"}},
		"hardlink": {{Name: "alias", Typeflag: tar.TypeLink, Linkname: "oh-my-zsh.sh"}}, "duplicate": {{Name: "oh-my-zsh.sh", Typeflag: tar.TypeReg, Mode: 0600}},
		"traversal": {{Name: "../escape", Typeflag: tar.TypeReg, Mode: 0600}}, "device": {{Name: "device", Typeflag: tar.TypeChar, Mode: 0600}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, e := officialEntries(context.Background(), officialFixture(t, testCommit, extra), testCommit); e == nil {
				t.Fatal("accepted bad archive")
			}
		})
	}
	v := officialFixture(t, testCommit, nil)
	for _, bad := range [][]byte{append(append([]byte{}, v...), []byte("extra")...), append(append([]byte{}, v...), v...), v[:len(v)-4]} {
		if _, e = officialEntries(context.Background(), bad, testCommit); e == nil {
			t.Fatal("accepted corrupt/trailing archive")
		}
	}
}
func TestRestoreFailureRetainsReceiptAndAllRecovery(t *testing.T) {
	f := newFixture(t)
	execute(t, f, "install", installArgs()...)
	execute(t, f, "menu", "backup", "--accept-snapshot")
	a := latestSnapshot(t, f)
	oldReceipt := read(t, filepath.Join(f.b.state, "receipt.json"))
	put(t, filepath.Join(f.b.omz, "oh-my-zsh.sh"), "latest user edit")
	original := f.b.rename
	f.b.rename = func(from, to string) error {
		if to == filepath.Join(f.b.dot, ".zshrc") && strings.Contains(from, "stage") {
			return errors.New("synthetic replacement failure")
		}
		return original(from, to)
	}
	if e := f.b.restore(context.Background(), a[1], a[3]); e == nil {
		t.Fatal("replacement failure hidden")
	}
	absent(t, filepath.Join(f.b.state, "receipt.json"))
	if !bytes.Equal(oldReceipt, read(t, filepath.Join(f.b.state, "operation", "invalidated-receipt.json"))) {
		t.Fatal("prior receipt lost")
	}
	journal := read(t, filepath.Join(f.b.state, "operation", "journal.json"))
	if !bytes.Contains(journal, []byte("staging")) || !bytes.Contains(journal, []byte("recovery")) {
		t.Fatal("recovery paths unrecorded")
	}
	found := false
	for _, v := range tree(t, f.b.home) {
		if strings.Contains(v, "latest user edit") {
			found = true
		}
	}
	if !found {
		t.Fatal("partial restore discarded old data")
	}
}
func TestJSONLServeAndCancellation(t *testing.T) {
	f := newFixture(t)
	t.Setenv("HOME", f.b.home)
	t.Setenv("ATM_STATE_DIR", f.b.state)
	t.Setenv("ATM_ZSH_USER_HOME", f.b.home)
	t.Setenv("ATM_ZSH_DOT_DIR", f.b.dot)
	t.Setenv("ATM_ZSH_OMZ_DIR", f.b.omz)
	t.Setenv("ATM_ZSH_CUSTOM_DIR", f.b.custom)
	t.Setenv("ATM_ZSH_BACKUP_DIR", f.b.backups)
	for _, op := range []string{"menu", "status"} {
		f.b.req.Operation = op
		v, e := json.Marshal(f.b.req)
		must(t, e)
		var out bytes.Buffer
		must(t, serve(context.Background(), bytes.NewReader(v), &out))
		s := protocol.Stream{RequestID: f.b.req.RequestID}
		for _, line := range bytes.Split(bytes.TrimSpace(out.Bytes()), []byte("\n")) {
			_, e = s.Accept(line)
			must(t, e)
		}
		if s.Result == nil || s.Result.Status != "success" {
			t.Fatal(out.String())
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := tree(t, f.root)
	if _, e := f.b.execute(ctx); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, tree(t, f.root)) {
		t.Fatal("cancellation wrote")
	}
}
func TestBackupModesAndContentBounds(t *testing.T) {
	f := newFixture(t)
	put(t, filepath.Join(f.b.dot, ".zshrc"), "private")
	execute(t, f, "menu", "backup", "--accept-snapshot")
	cs := execute(t, f, "menu", "backups")
	p := cs[0].Arguments[1]
	i, e := os.Stat(p)
	must(t, e)
	if i.Mode().Perm() != 0600 {
		t.Fatal(i.Mode())
	}
	i, e = os.Stat(f.b.backups)
	must(t, e)
	if i.Mode().Perm() != 0700 {
		t.Fatal(i.Mode())
	}
	must(t, os.Chmod(p, 0644))
	if _, e = f.b.readSnapshot(context.Background(), p, digest(read(t, p))); e == nil {
		t.Fatal("public snapshot accepted")
	}
	g := newFixture(t)
	file, e := os.Create(filepath.Join(g.b.dot, ".zsh_history"))
	must(t, e)
	must(t, file.Chmod(0600))
	must(t, file.Truncate(maxFile+1))
	must(t, file.Close())
	if e = g.b.backup(context.Background()); e == nil {
		t.Fatal("oversized file accepted")
	}
	absent(t, g.b.state)
}

func TestGeneratedLoaderOriginalPathBound(t *testing.T) {
	f := newFixture(t)
	execute(t, f, "install", installArgs()...)
	original := filepath.Join(f.b.dot, ".zshrc")
	other := filepath.Join(f.b.home, "other-dot")
	must(t, os.Mkdir(other, 0700))
	must(t, os.WriteFile(filepath.Join(other, ".zshrc"), read(t, original), 0600))
	f.b.dot = other
	before := tree(t, f.root)
	n := len(f.calls)
	for _, action := range []string{"update", "remove"} {
		if e := f.b.apply(context.Background(), action, options{secondCommit}); e == nil {
			t.Fatal("new DOTDIR gained loader ownership")
		}
	}
	if n != len(f.calls) || !reflect.DeepEqual(before, tree(t, f.root)) {
		t.Fatal("original/new loader changed")
	}
}
func TestRestorePreservesValidatedModes(t *testing.T) {
	f := newFixture(t)
	must(t, os.Mkdir(f.b.omz, 0750))
	must(t, os.Chmod(f.b.omz, 0750))
	must(t, os.Mkdir(f.b.custom, 0750))
	must(t, os.Chmod(f.b.custom, 0750))
	put(t, filepath.Join(f.b.custom, "local"), "custom")
	p := filepath.Join(f.b.dot, ".zsh_history")
	put(t, p, "history")
	must(t, os.Chmod(p, 0640))
	execute(t, f, "menu", "backup", "--accept-snapshot")
	args := latestSnapshot(t, f)
	must(t, os.Chmod(p, 0600))
	must(t, os.Chmod(f.b.custom, 0700))
	execute(t, f, "menu", args...)
	for path, mode := range map[string]os.FileMode{p: 0640, f.b.omz: 0750, f.b.custom: 0750} {
		i, e := os.Stat(path)
		must(t, e)
		if i.Mode().Perm() != mode {
			t.Fatalf("mode %s got %o want %o", path, i.Mode().Perm(), mode)
		}
	}
}
func TestLoginShellAliasesFailClosed(t *testing.T) {
	input := []byte("other:x:456:456::/home/other:/usr/local/bin/login-shell\n")
	no := func(string) (bool, error) { return false, nil }
	if e := verifyLoginShells(input, func(string) (string, error) { return "/usr/bin/zsh", nil }, no); e == nil {
		t.Fatal("zsh alias accepted")
	}
	if e := verifyLoginShells(input, func(string) (string, error) { return "", os.ErrNotExist }, no); e == nil {
		t.Fatal("unresolved alias accepted")
	}
	if e := verifyLoginShells(input, func(p string) (string, error) { return p, nil }, func(string) (bool, error) { return true, nil }); e == nil {
		t.Fatal("zsh hardlink accepted")
	}
	must(t, verifyLoginShells(input, func(string) (string, error) { return "/usr/bin/bash", nil }, no))
}
func TestRestoreCancellationReportsPartialEffects(t *testing.T) {
	f := newFixture(t)
	execute(t, f, "install", installArgs()...)
	put(t, filepath.Join(f.b.dot, ".zsh_history"), "archived")
	execute(t, f, "menu", "backup", "--accept-snapshot")
	args := latestSnapshot(t, f)
	put(t, filepath.Join(f.b.omz, "oh-my-zsh.sh"), "displaced edit")
	put(t, filepath.Join(f.b.dot, ".zsh_history"), "current")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	original := f.b.rename
	f.b.rename = func(from, to string) error {
		e := original(from, to)
		if e == nil && to == f.b.omz && strings.Contains(from, "stage") {
			cancel()
		}
		return e
	}
	e := f.b.restore(ctx, args[1], args[3])
	if !errors.Is(e, context.Canceled) || !strings.Contains(e.Error(), "earlier roots may already be replaced") || !strings.Contains(e.Error(), filepath.Join(f.b.state, "operation", "journal.json")) {
		t.Fatal("partial effects not reported", e)
	}
	if string(read(t, filepath.Join(f.b.dot, ".zsh_history"))) != "current" {
		t.Fatal("unreached root changed")
	}
	if string(read(t, filepath.Join(f.b.omz, "oh-my-zsh.sh"))) == "displaced edit" {
		t.Fatal("test did not interrupt partial publication")
	}
	retained := false
	for _, v := range tree(t, f.b.home) {
		if strings.Contains(v, "displaced edit") {
			retained = true
		}
	}
	if !retained {
		t.Fatal("displaced data lost")
	}
	if _, e = os.Stat(filepath.Join(f.b.state, "operation", "invalidated-receipt.json")); e != nil {
		t.Fatal("prior receipt lost", e)
	}
}
