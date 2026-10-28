package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/AtomyCloud/atm/internal/protocol"
)

func privateFixture(t *testing.T) string {
	t.Helper()
	p := t.TempDir()
	if e := os.Chmod(p, 0700); e != nil {
		t.Fatal(e)
	}
	return p
}
func fixtureFile(t *testing.T, p, s string) {
	t.Helper()
	if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(p, []byte(s), 0600); e != nil {
		t.Fatal(e)
	}
}
func testBackend(t *testing.T) *backend {
	t.Helper()
	p := privateFixture(t)
	b := &backend{profile: filepath.Join(p, "profile"), backups: filepath.Join(p, "backups"), state: filepath.Join(p, "state/native-operation.json"), arch: "amd64", log: func(string) {}, closed: func(context.Context, string) error { return nil }, manager: func() (string, error) { return "apt-get", nil }, uid: func() int { return 1000 }, sources: func() ([]sourceFile, error) {
		return []sourceFile{{"system.list", "deb https://example.invalid stable main\n"}}, nil
	}, resolve: func(n string) (string, error) { return "/trusted/" + n, nil }}
	b.fetch = func(context.Context, systemPlan) ([]byte, error) { return []byte("fixture public key"), nil }
	b.preferences = func() ([]sourceFile, error) {
		return []sourceFile{{"vendor.pref", "Package: dependency\nPin: version 1.2\nPin-Priority: 1001\n"}, {"preferences", "Package: another-dependency\nPin: version 2.0\nPin-Priority: 990\n"}}, nil
	}
	b.stageCheck = func(string) error { return nil }
	b.inputCheck = func(string, []byte) error { return nil }
	b.stageValid = func(string) bool { return true }
	return b
}
func bookmarkInventory(t *testing.T, p string) map[string]string {
	t.Helper()
	f, e := os.Open(p)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	gz, e := gzip.NewReader(f)
	if e != nil {
		t.Fatal(e)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	m := map[string]string{}
	for {
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		if h.Typeflag == tar.TypeReg {
			v, e := io.ReadAll(tr)
			if e != nil {
				t.Fatal(e)
			}
			m[h.Name] = string(v)
		}
	}
	return m
}
func backupFiles(t *testing.T, b *backend) []string {
	t.Helper()
	v, e := filepath.Glob(filepath.Join(b.backups, "*.tar.gz"))
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func fullBackup(t *testing.T, b *backend) (string, string) {
	t.Helper()
	fixtureFile(t, filepath.Join(b.profile, "Default/Bookmarks"), "original")
	fixtureFile(t, filepath.Join(b.profile, "Local State"), "state")
	if e := b.backup(context.Background(), false); e != nil {
		t.Fatal(e)
	}
	files := backupFiles(t, b)
	if len(files) != 1 {
		t.Fatal(files)
	}
	v, e := os.ReadFile(files[0])
	if e != nil {
		t.Fatal(e)
	}
	return filepath.Base(files[0]), digest(v)
}

func TestBookmarksAllProfilesPrivateNoLinks(t *testing.T) {
	b := testBackend(t)
	for _, n := range []string{"Default", "Profile 0", "Profile 1", "Profile 12", "Profile abc", "Guest Profile"} {
		fixtureFile(t, filepath.Join(b.profile, n, "Bookmarks"), n)
	}
	fixtureFile(t, filepath.Join(b.profile, "Default/History"), "private history")
	for range 2 {
		if e := b.backup(context.Background(), true); e != nil {
			t.Fatal(e)
		}
	}
	files := backupFiles(t, b)
	if len(files) != 2 {
		t.Fatal(files)
	}
	for _, p := range files {
		i, e := os.Stat(p)
		if e != nil || i.Mode().Perm() != 0600 {
			t.Fatal(i, e)
		}
		m := bookmarkInventory(t, p)
		if len(m) != 4 || m["Profile 12/Bookmarks"] != "Profile 12" || m["Default/History"] != "" {
			t.Fatal(m)
		}
	}
	choices, e := b.backupMenu(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if len(choices) != 2 {
		t.Fatal("bookmarks offered for restore", choices)
	}
	if e = os.Remove(filepath.Join(b.profile, "Profile 12/Bookmarks")); e != nil {
		t.Fatal(e)
	}
	if e = os.Symlink(filepath.Join(b.profile, "Default/Bookmarks"), filepath.Join(b.profile, "Profile 12/Bookmarks")); e != nil {
		t.Fatal(e)
	}
	if e = b.backup(context.Background(), true); e == nil {
		t.Fatal("linked bookmarks accepted")
	}
	if len(backupFiles(t, b)) != 2 {
		t.Fatal("published unsafe backup")
	}
}
func TestEmptyBookmarksAndClosedGuardNoWrites(t *testing.T) {
	b := testBackend(t)
	fixtureFile(t, filepath.Join(b.profile, "Guest Profile/Bookmarks"), "guest")
	if e := b.backup(context.Background(), true); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(b.backups); !os.IsNotExist(e) {
		t.Fatal("empty backup wrote")
	}
	b.closed = func(context.Context, string) error { return errors.New("running") }
	if e := b.backup(context.Background(), false); e == nil {
		t.Fatal("running accepted")
	}
	if _, e := os.Stat(filepath.Dir(b.state)); !os.IsNotExist(e) {
		t.Fatal("closed refusal wrote state")
	}
}
func TestSnapshotMutationRefusesPublication(t *testing.T) {
	for _, mode := range []string{"new", "same-size", "remove", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			b := testBackend(t)
			p := filepath.Join(b.profile, "Default/Bookmarks")
			fixtureFile(t, p, "before")
			i, _ := os.Stat(p)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			b.closed = func(context.Context, string) error {
				calls++
				if calls == 3 {
					switch mode {
					case "new":
						fixtureFile(t, filepath.Join(b.profile, "Default/New"), "new")
					case "same-size":
						fixtureFile(t, p, "after!")
						if e := os.Chtimes(p, i.ModTime(), i.ModTime()); e != nil {
							t.Fatal(e)
						}
					case "remove":
						if e := os.Remove(p); e != nil {
							t.Fatal(e)
						}
					case "cancel":
						cancel()
					}
				}
				return nil
			}
			if e := b.backup(ctx, false); e == nil {
				t.Fatal("changed snapshot published")
			}
			if len(backupFiles(t, b)) != 0 {
				t.Fatal("archive published")
			}
		})
	}
}
func TestRestoreFingerprintRecoveryAndPrivateModes(t *testing.T) {
	b := testBackend(t)
	name, h := fullBackup(t, b)
	fixtureFile(t, filepath.Join(b.profile, "Default/Bookmarks"), "newer")
	fixtureFile(t, filepath.Join(b.profile, "only-old"), "retain")
	if e := b.restore(context.Background(), name, strings.Repeat("0", 64)); e == nil {
		t.Fatal("wrong digest accepted")
	}
	if e := b.restore(context.Background(), name, h); e != nil {
		t.Fatal(e)
	}
	v, _ := os.ReadFile(filepath.Join(b.profile, "Default/Bookmarks"))
	if string(v) != "original" {
		t.Fatal(string(v))
	}
	copies, _ := filepath.Glob(b.profile + ".before-restore.*")
	if len(copies) != 1 {
		t.Fatal(copies)
	}
	v, _ = os.ReadFile(filepath.Join(copies[0], "only-old"))
	if string(v) != "retain" {
		t.Fatal("old data lost")
	}
	if _, e := os.Stat(filepath.Join(b.profile, "only-old")); !os.IsNotExist(e) {
		t.Fatal("restore overlaid tree")
	}
	i, _ := os.Stat(filepath.Join(b.profile, "Default/Bookmarks"))
	if i.Mode().Perm() != 0600 {
		t.Fatal(i.Mode())
	}
	r, _, e := readRecord(b.state)
	if e != nil || r.Status != "completed" || r.Recovery != copies[0] {
		t.Fatal(r, e)
	}
}
func TestRestoreIdentityConflictAndInterruptedJournal(t *testing.T) {
	t.Run("replacement", func(t *testing.T) {
		b := testBackend(t)
		name, h := fullBackup(t, b)
		calls := 0
		b.closed = func(context.Context, string) error {
			calls++
			if calls == 3 {
				if e := os.Rename(b.profile, b.profile+"-saved"); e != nil {
					t.Fatal(e)
				}
				fixtureFile(t, filepath.Join(b.profile, "foreign"), "preserve")
			}
			return nil
		}
		if e := b.restore(context.Background(), name, h); e == nil || !strings.Contains(e.Error(), "identity") {
			t.Fatal(e)
		}
		v, _ := os.ReadFile(filepath.Join(b.profile, "foreign"))
		if string(v) != "preserve" {
			t.Fatal("replaced foreign tree")
		}
	})
	t.Run("interruption-after-move", func(t *testing.T) {
		b := testBackend(t)
		name, h := fullBackup(t, b)
		calls := 0
		b.closed = func(context.Context, string) error {
			calls++
			if calls == 3 {
				fixtureFile(t, b.state, `{"schema":1,"plugin":"brave","action":"restore","status":"running","phase":"external","plan":"","updated":""}`)
			}
			return nil
		}
		if e := b.restore(context.Background(), name, h); e == nil {
			t.Fatal("changed journal accepted")
		}
		copies, _ := filepath.Glob(b.profile + ".before-restore.*")
		if len(copies) != 1 {
			t.Fatal("recovery lost", copies)
		}
		if _, e := os.Stat(filepath.Join(filepath.Dir(b.state), ".native-lock")); e != nil {
			t.Fatal("interrupted lock missing", e)
		}
		if _, _, e := openJournal(b.state); e == nil {
			t.Fatal("interrupted operation reopened")
		}
	})
}
func TestJournalChangedForeignAndInterrupted(t *testing.T) {
	b := testBackend(t)
	j, release, e := openJournal(b.state)
	if e != nil {
		t.Fatal(e)
	}
	defer release()
	r := operationRecord{Schema: 1, Plugin: "brave", Action: "restore", Status: "running", Phase: "test"}
	if e = j.save(r); e != nil {
		t.Fatal(e)
	}
	old, _ := os.ReadFile(b.state)
	fixtureFile(t, b.state, strings.Replace(string(old), `"test"`, `"changed"`, 1))
	r.Status = "completed"
	if e = j.save(r); e == nil {
		t.Fatal("journal overwritten")
	}
	release()
	if _, _, e = openJournal(b.state); e == nil {
		t.Fatal("running journal accepted")
	}
	fixtureFile(t, b.state, `{"schema":1,"schema":1}`)
	if _, _, e = readRecord(b.state); e == nil {
		t.Fatal("duplicate journal key")
	}
}
func TestContractConsentDryRunAndUnsupported(t *testing.T) {
	for _, args := range [][]string{nil, {"--accept-removal"}, {"--accept-system-packages", "--accept-system-packages"}, {"--accept-system-packages", "--plan", "bad"}, {"--accept-system-packages", "--unknown"}} {
		if _, e := systemOptions("install", args); e == nil {
			t.Fatal(args)
		}
	}
	if _, e := systemOptions("remove", []string{"--accept-system-packages"}); e == nil {
		t.Fatal("removal consent bypass")
	}
	b := testBackend(t)
	b.req = protocol.Request{Operation: "use"}
	if _, e := b.execute(context.Background()); e == nil {
		t.Fatal("use supported")
	}
	b.req.Operation = "menu"
	b.req.Arguments = []string{"restore", "x"}
	if _, e := b.execute(context.Background()); e == nil {
		t.Fatal("invalid restore")
	}
	t.Setenv("ATM_DRY_RUN", "1")
	b.req.Operation = "install"
	if _, e := b.execute(context.Background()); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Dir(b.state)); !os.IsNotExist(e) {
		t.Fatal("dry run wrote")
	}
}
func TestPlanAndCommandPolicy(t *testing.T) {
	b := testBackend(t)
	p, e := b.prepareSystem("install")
	if e != nil {
		t.Fatal(e)
	}
	q := p
	q.Arch = "arm64"
	if planHash(q) == planHash(p) {
		t.Fatal("plan omits arch")
	}
	choices, e := b.systemMenu("install")
	if e != nil || len(choices) != 1 {
		t.Fatal(choices, e)
	}
	if e = protocol.ValidateChoices(choices); e != nil {
		t.Fatal(e)
	}
	for _, m := range []string{"apt-get", "dnf"} {
		p.Manager = m
		for _, cmd := range systemCommands(p, "/run/atm-brave-abcdefghij") {
			s := strings.Join(cmd, " ")
			for _, bad := range []string{"--nogpgcheck", "--no-gpgchecks", "allow-unauthenticated", "sudo -v", "bash", "python"} {
				if strings.Contains(s, bad) {
					t.Fatal(s)
				}
			}
		}
		p.Action = "remove"
		commands := systemCommands(p, "")
		if len(commands) != 1 || strings.Contains(strings.Join(commands[0], " "), "update") {
			t.Fatal(commands)
		}
		p.Action = "install"
	}
	d := systemCommands(systemPlan{Manager: "dnf", Action: "install", Arch: "arm64"}, "/stage")
	s := strings.Join(d[0], " ")
	for _, required := range []string{".gpgcheck=1", ".repo_gpgcheck=0", ".gpgkey=file:///stage/brave.asc", "aarch64", "*.excludepkgs=brave-browser"} {
		if !strings.Contains(s, required) {
			t.Fatal(s)
		}
	}
	if !reflect.DeepEqual(commandEnvironment(), []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "DEBIAN_FRONTEND=noninteractive", "TERM=dumb"}) {
		t.Fatal("ambient environment")
	}
}

const fixturePolicy = "brave-browser:\n  Installed: (none)\n  Candidate: 1.99.1\n  Version table:\n     1.99.1 1001\n       1001 https://brave-browser-apt-release.s3.brave.com stable/main amd64 Packages\n"

func TestAPTCandidateOriginAmbiguity(t *testing.T) {
	if v, e := officialCandidate(fixturePolicy, "amd64"); e != nil || v != "1.99.1" {
		t.Fatal(v, e)
	}
	for _, s := range []string{strings.ReplaceAll(fixturePolicy, "https://brave-browser-apt-release.s3.brave.com", "https://foreign.invalid"), fixturePolicy + "        500 https://foreign.invalid stable/main amd64 Packages\n", strings.ReplaceAll(fixturePolicy, "Candidate: 1.99.1", "Candidate: (none)"), fixturePolicy + "unexpected\n", strings.ReplaceAll(fixturePolicy, "amd64 Packages", "arm64 Packages")} {
		if _, e := officialCandidate(s, "amd64"); e == nil {
			t.Fatal(s)
		}
	}
}

func TestSystemSyntheticLifecycleFailureCancellation(t *testing.T) {
	for _, mode := range []string{"success", "admin-prohibited", "refresh-failure", "signature-failure", "ambiguous", "copy-changed", "private-checksum", "cancel", "cancel-copy", "cancel-cleanup", "wrong-stage", "sudo-denied", "dnf-failure"} {
		t.Run(mode, func(t *testing.T) {
			b := testBackend(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var calls [][]string
			copied := map[string]string{}
			modes := map[string]string{}
			const adminPin = "Package: brave-browser\nPin: version *\nPin-Priority: -1\n"
			if mode == "admin-prohibited" {
				b.preferences = func() ([]sourceFile, error) {
					return []sourceFile{{"preferences", adminPin}, {"parts/zorin.pref", "Package: *\nPin: release o=Zorin\nPin-Priority: 1001\n"}}, nil
				}
			}
			if mode == "wrong-stage" {
				b.stageCheck = func(string) error { return errors.New("untrusted root stage") }
			}
			if mode == "dnf-failure" {
				b.manager = func() (string, error) { return "dnf", nil }
			}
			b.inputCheck = func(p string, want []byte) error {
				if mode == "copy-changed" {
					return errors.New("copy changed")
				}
				if want == nil {
					if modes[p] != "0600" {
						return fmt.Errorf("private input exposed: %s", p)
					}
					return nil
				}
				if modes[p] != "0644" {
					return fmt.Errorf("public key/config not readable by _apt")
				}
				if copied[p] != string(want) {
					return fmt.Errorf("copy mismatch %s", p)
				}
				return nil
			}
			b.run = func(_ context.Context, ex string, args []string) (string, error) {
				if ex == "/trusted/apt-get" && reflect.DeepEqual(args, []string{"--version"}) {
					return "apt 2.4.1 (amd64)", nil
				}
				if ex != "/trusted/sudo" || len(args) < 4 || args[0] != "-n" || args[2] != "--" {
					t.Fatalf("unsafe elevation %s %v", ex, args)
				}
				name := filepath.Base(args[3])
				a := args[4:]
				calls = append(calls, append([]string{name}, a...))
				if mode == "sudo-denied" {
					return "", errors.New("sudo policy denied")
				}
				switch name {
				case "mktemp":
					return "/run/atm-brave-abcdefghij\n", nil
				case "install":
					if len(a) > 0 && a[0] == "-m" {
						if mode == "cancel-copy" {
							cancel()
							return "", context.Canceled
						}
						v, e := os.ReadFile(a[len(a)-2])
						if e != nil {
							t.Fatal(e)
						}
						copied[a[len(a)-1]] = string(v)
						modes[a[len(a)-1]] = a[1]
					}
				case "apt-cache":
					if mode == "admin-prohibited" {
						if copied["/run/atm-brave-abcdefghij/preferences"] != adminPin || copied["/run/atm-brave-abcdefghij/prefs/zorin.pref"] != "Package: *\nPin: release o=Zorin\nPin-Priority: 1001\n" {
							t.Fatal("administrator preferences changed")
						}
						return strings.ReplaceAll(fixturePolicy, "Candidate: 1.99.1", "Candidate: (none)"), nil
					}
					if mode == "ambiguous" {
						return fixturePolicy + "        500 https://foreign.invalid stable/main amd64 Packages\n", nil
					}
					return fixturePolicy, nil
				case "sha256sum":
					if modes[a[len(a)-1]] != "0600" {
						t.Fatal("private checksum expected")
					}
					if mode == "private-checksum" {
						return "changed", nil
					}
					return digest([]byte(copied[a[len(a)-1]])) + "  " + a[len(a)-1] + "\n", nil
				case "rm":
					if mode == "cancel-cleanup" {
						cancel()
						return "", context.Canceled
					}
				case "apt-get":
					if a[len(a)-1] == "update" && (mode == "refresh-failure" || mode == "signature-failure") {
						return "bad signature", errors.New("update failed")
					}
					if mode == "cancel" {
						cancel()
						return "", context.Canceled
					}
				case "dnf":
					return "", errors.New("DNF failed")
				}
				return "", nil
			}
			e := b.applySystem(ctx, sysOptions{action: "install"})
			if mode == "success" && e != nil {
				t.Fatal(e)
			}
			if mode != "success" && e == nil {
				t.Fatal("failure accepted")
			}
			installs, dnfs, cleans := 0, 0, 0
			for _, call := range calls {
				if call[0] == "dnf" {
					dnfs++
				}
				if call[0] == "rm" {
					cleans++
				}
				if call[0] == "apt-get" && slices.Contains(call[1:], "install") {
					installs++
					if call[len(call)-1] != "brave-browser=1.99.1" {
						t.Fatal("unbound package installation", call)
					}
				}
			}
			if mode == "success" && installs != 1 {
				t.Fatal(calls)
			}
			if mode != "success" && mode != "cancel-cleanup" && installs != 0 {
				t.Fatal("install after failure", calls)
			}
			if mode == "dnf-failure" && dnfs != 1 {
				t.Fatal("DNF fallback", calls)
			}
			r, _, re := readRecord(b.state)
			if re != nil {
				t.Fatal(re)
			}
			if strings.HasPrefix(mode, "cancel") {
				if (mode != "cancel-cleanup" && cleans != 0) || r.Status != "running" {
					t.Fatal(calls, r)
				}
				if _, _, e = openJournal(b.state); e == nil {
					t.Fatal("cancelled lock reopened")
				}
				if _, e = os.Stat(r.Inputs); e != nil {
					t.Fatal("interrupted inputs deleted")
				}
				t.Cleanup(func() { _ = os.RemoveAll(r.Inputs) })
			} else if r.Status == "running" {
				t.Fatal(r)
			}
			if mode == "success" {
				prefs, _ := b.preferences()
				for _, p := range prefs {
					target := "/run/atm-brave-abcdefghij/prefs/" + p.Name
					if p.Name == "preferences" {
						target = "/run/atm-brave-abcdefghij/preferences"
					}
					if !strings.HasSuffix(copied[target], p.Data) {
						t.Fatal("administrator preferences changed", target)
					}
				}
			}
		})
	}
}
