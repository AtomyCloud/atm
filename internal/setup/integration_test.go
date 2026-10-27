package setup

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"mvdan.cc/sh/v3/syntax"
)

func installed(t *testing.T) Options {
	t.Helper()
	o := fixture(t)
	if _, err := Install(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	o.TrustedManifestSHA256 = ""
	return o
}

func TestProfileIntegrationAndPrivateBackups(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix profiles; Windows tested with fake user registry")
	}
	for _, shell := range []string{"bash", "zsh", "sh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			o := installed(t)
			o.Shell = shell
			o.LinkCLI = true
			t.Setenv("PATH", t.TempDir())
			home, err := openHome(o)
			if err != nil {
				t.Fatal(err)
			}
			names, err := profileNames(home, shell)
			home.Close()
			if err != nil {
				t.Fatal(err)
			}
			original := []byte("# personal settings\r\nexport KEEP='literal $()'\r\n")
			for _, name := range names {
				put(t, filepath.Join(o.UserHome, name), original, 0640)
			}
			before := tree(t, o.UserHome)
			o.DryRun = true
			o.Consent = false
			result, err := Integrate(context.Background(), o)
			if err != nil || len(result.Planned) != len(names)+1 || len(result.Changed) != 0 {
				t.Fatalf("%+v %v", result, err)
			}
			if !reflect.DeepEqual(before, tree(t, o.UserHome)) {
				t.Fatal("dry-run changed profile")
			}
			o.DryRun = false
			if _, err = Integrate(context.Background(), o); !errors.Is(err, ErrConsent) {
				t.Fatal(err)
			}
			o.Consent = true
			result, err = Integrate(context.Background(), o)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Backups) != len(names) || len(result.Changed) != len(names)+1 {
				t.Fatalf("%+v", result)
			}
			for _, backup := range result.Backups {
				b, err := os.ReadFile(backup)
				if err != nil || string(b) != string(original) {
					t.Fatal("bad profile backup")
				}
				info, err := os.Stat(backup)
				if err != nil || info.Mode().Perm() != 0600 {
					t.Fatal("nonprivate backup")
				}
				info, err = os.Stat(filepath.Dir(backup))
				if err != nil || info.Mode().Perm() != 0700 {
					t.Fatal("nonprivate backup directory")
				}
			}
			for _, name := range names {
				b, err := os.ReadFile(filepath.Join(o.UserHome, name))
				if err != nil {
					t.Fatal(err)
				}
				if !strings.HasPrefix(string(b), string(original)) || strings.Count(string(b), blockBegin) != 1 {
					t.Fatal("profile content lost")
				}
				info, err := os.Stat(filepath.Join(o.UserHome, name))
				if err != nil || info.Mode().Perm() != 0640 {
					t.Fatal("profile mode changed")
				}
			}
			link, err := os.Readlink(filepath.Join(o.UserHome, ".local/bin/atm"))
			if err != nil || link != result.Executable {
				t.Fatal("CLI link must target native executable")
			}
			before = tree(t, o.UserHome)
			result, err = Integrate(context.Background(), o)
			if err != nil || len(result.Changed) != 0 || len(result.Backups) != 0 {
				t.Fatalf("not idempotent: %+v %v", result, err)
			}
			if !reflect.DeepEqual(before, tree(t, o.UserHome)) {
				t.Fatal("idempotent integration wrote files")
			}
		})
	}
}

func TestProfileConflictsPreserved(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix profiles")
	}
	for _, kind := range []string{"legacy-block", "edited-block", "dangling-marker", "symlink", "link-file", "link-other", "linked-parent", "profile-directory"} {
		t.Run(kind, func(t *testing.T) {
			o := installed(t)
			o.Shell = "bash"
			switch kind {
			case "legacy-block":
				put(t, filepath.Join(o.UserHome, ".bashrc"), []byte("# >>> ATM command >>>\nold\n# <<< ATM command <<<\n"), 0600)
			case "edited-block":
				put(t, filepath.Join(o.UserHome, ".bashrc"), []byte(blockBegin+"\ncustom\n"+blockEnd+"\n"), 0600)
			case "dangling-marker":
				put(t, filepath.Join(o.UserHome, ".bashrc"), []byte(blockBegin+"\n"), 0600)
			case "symlink":
				outside := filepath.Join(t.TempDir(), "profile")
				put(t, outside, []byte("private"), 0600)
				if err := os.Symlink(outside, filepath.Join(o.UserHome, ".bashrc")); err != nil {
					t.Fatal(err)
				}
			case "link-file":
				o.LinkCLI = true
				put(t, filepath.Join(o.UserHome, ".local/bin/atm"), []byte("unmanaged"), 0700)
			case "link-other":
				o.LinkCLI = true
				if err := os.MkdirAll(filepath.Join(o.UserHome, ".local/bin"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("/not-present", filepath.Join(o.UserHome, ".local/bin/atm")); err != nil {
					t.Fatal(err)
				}
			case "linked-parent":
				o.Shell = "fish"
				if err := os.Symlink(t.TempDir(), filepath.Join(o.UserHome, ".config")); err != nil {
					t.Fatal(err)
				}
			case "profile-directory":
				if err := os.Mkdir(filepath.Join(o.UserHome, ".bashrc"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			before := tree(t, o.UserHome)
			if _, err := Integrate(context.Background(), o); err == nil {
				t.Fatal("conflict accepted")
			}
			if !reflect.DeepEqual(before, tree(t, o.UserHome)) {
				t.Fatal("conflict changed user files")
			}
		})
	}
}

func TestBashLoginSelectionAndPathQuoting(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix profiles")
	}
	o := installed(t)
	o.Shell = "bash"
	put(t, filepath.Join(o.UserHome, ".bash_profile"), []byte("# login without newline"), 0600)
	put(t, filepath.Join(o.UserHome, ".profile"), []byte("untouched"), 0600)
	result, err := Integrate(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Changed) != 2 {
		t.Fatalf("%+v", result)
	}
	b, err := os.ReadFile(filepath.Join(o.UserHome, ".profile"))
	if err != nil || string(b) != "untouched" {
		t.Fatal("inactive login profile modified")
	}
	block, err := PathBlock(o)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(block, "'\"'\"'") || strings.Contains(block, "export ATM_HOME") || strings.Contains(block, "/bin:") {
		t.Fatalf("unsafe/native wrong block: %s", block)
	}
}

func TestProfileFailureAndConcurrentChange(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix profiles")
	}
	for _, concurrent := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "concurrent"}[concurrent], func(t *testing.T) {
			o := installed(t)
			o.Shell = "sh"
			path := filepath.Join(o.UserHome, ".profile")
			put(t, path, []byte("original"), 0600)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			o.checkpoint = func(phase string) error {
				if phase == "before-profile-write" {
					if concurrent {
						return os.WriteFile(path, []byte("editor change"), 0600)
					}
					cancel()
				}
				return nil
			}
			result, err := Integrate(ctx, o)
			if err == nil || len(result.Backups) != 1 || len(result.Changed) != 0 {
				t.Fatalf("%+v %v", result, err)
			}
			want := "original"
			if concurrent {
				want = "editor change"
			}
			b, err := os.ReadFile(path)
			if err != nil || string(b) != want {
				t.Fatal("original/editor content lost")
			}
			entries, err := os.ReadDir(o.UserHome)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), ".atm-profile-") || e.Name() == ".atm-setup-lock" {
					t.Fatal("temporary file or lock leaked")
				}
			}
		})
	}
}

type fakePath struct {
	value        pathValue
	writes       int
	reads        int
	changeOnRead int
	failWrite    bool
}

func (f *fakePath) Read() (pathValue, error) {
	f.reads++
	if f.reads == f.changeOnRead {
		f.value.Value += ";changed"
	}
	return f.value, nil
}
func (f *fakePath) Write(v pathValue) error {
	if f.failWrite {
		return errors.New("registry denied")
	}
	f.value = v
	f.writes++
	return nil
}
func (f *fakePath) Close() error { return nil }

func TestUserRegistryIntegrationWithFakeStore(t *testing.T) {
	for _, kind := range []string{"success", "dry-run", "denied", "concurrent", "write-failure", "idempotent"} {
		t.Run(kind, func(t *testing.T) {
			o := Options{UserHome: t.TempDir(), Consent: true}
			home, err := openHome(o)
			if err != nil {
				t.Fatal(err)
			}
			defer home.Close()
			old := pathValue{`%USERPROFILE%\bin;C:\Other`, 2, true}
			store := &fakePath{value: old}
			result := Result{Status: Status{Destination: `C:\Users\test\Apps\atm`}}
			switch kind {
			case "dry-run":
				o.DryRun = true
			case "denied":
				o.Consent = false
			case "concurrent":
				store.changeOnRead = 2
			case "write-failure":
				store.failWrite = true
			case "idempotent":
				store.value.Value = result.Destination + ";" + old.Value
			}
			before := tree(t, o.UserHome)
			result, err = integrateUserPath(context.Background(), home, o, result, store)
			if kind == "denied" || kind == "concurrent" || kind == "write-failure" {
				if err == nil {
					t.Fatal("failure not reported")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if kind != "success" && store.writes != 0 {
				t.Fatal("unexpected registry write")
			}
			if kind == "success" {
				if store.writes != 1 || store.value.Type != old.Type || !strings.HasSuffix(store.value.Value, ";"+old.Value) || len(result.Backups) != 1 {
					t.Fatalf("%+v %+v", store, result)
				}
				b, err := os.ReadFile(result.Backups[0])
				if err != nil {
					t.Fatal(err)
				}
				var backup pathValue
				if err = json.Unmarshal(b, &backup); err != nil || backup != old {
					t.Fatal("registry backup lost original value/type")
				}
			}
			if kind == "dry-run" || kind == "denied" || kind == "idempotent" {
				if !reflect.DeepEqual(before, tree(t, o.UserHome)) {
					t.Fatal("no-write operation changed files")
				}
			}
		})
	}
}

func TestWindowsPathValidation(t *testing.T) {
	for _, entry := range []string{"", `C:\bad;entry`, `C:\bad"entry`, "bad\nentry", `C:\%expand%`} {
		if _, err := prependUserPath(pathValue{`%USERPROFILE%\bin`, 2, true}, entry); err == nil {
			t.Fatalf("accepted %q", entry)
		}
	}
	old := pathValue{`C:\USERS\test\Apps\atm\;C:\other`, 1, true}
	got, err := prependUserPath(old, `c:\users\test\apps\ATM`)
	if err != nil || got != old {
		t.Fatal("case-insensitive idempotence failed")
	}
	if _, err = prependUserPath(pathValue{"invalid", 7, true}, `C:\safe`); err == nil {
		t.Fatal("unsupported registry type accepted")
	}
}

func TestShellBlocksParseWithoutExecuting(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell syntax")
	}
	for _, shell := range []string{"bash", "zsh", "sh"} {
		for _, suffix := range []string{"plain", "space ' quote $() `literal` * [abc]", "unicode \u00e7"} {
			o := Options{UserHome: filepath.Join(t.TempDir(), suffix), Shell: shell}
			block, err := PathBlock(o)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = syntax.NewParser(syntax.Variant(syntax.LangPOSIX)).Parse(strings.NewReader(block), "profile"); err != nil {
				t.Fatalf("%s: %v\n%s", shell, err, block)
			}
		}
	}
}

func TestUnownedAndChangedReceiptsFailClosed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix integration; no real registry in tests")
	}
	for _, kind := range []string{"missing", "wrong-destination", "conflicting-pin", "linked"} {
		t.Run(kind, func(t *testing.T) {
			o := installed(t)
			dest, _, _ := o.paths()
			name := filepath.Join(dest, receiptName)
			switch kind {
			case "missing":
				if err := os.Remove(name); err != nil {
					t.Fatal(err)
				}
			case "wrong-destination":
				b, err := os.ReadFile(name)
				if err != nil {
					t.Fatal(err)
				}
				var rec setupReceipt
				if err = json.Unmarshal(b, &rec); err != nil {
					t.Fatal(err)
				}
				rec.Destination = t.TempDir()
				b, err = json.Marshal(rec)
				if err != nil {
					t.Fatal(err)
				}
				put(t, name, b, 0600)
			case "conflicting-pin":
				o.TrustedManifestSHA256 = strings.Repeat("0", 64)
			case "linked":
				if err := os.Remove(name); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(o.Source, "release.json"), name); err != nil {
					t.Fatal(err)
				}
			}
			before := tree(t, o.UserHome)
			if _, err := Integrate(context.Background(), o); err == nil {
				t.Fatal("unowned/changed receipt accepted")
			}
			if !reflect.DeepEqual(before, tree(t, o.UserHome)) {
				t.Fatal("invalid receipt caused writes")
			}
		})
	}
}

func TestPluginChangesDoNotBlockCOREIntegration(t *testing.T) {
	for _, change := range []string{"updated", "added", "removed"} {
		t.Run(change, func(t *testing.T) {
			o := installed(t)
			o.Shell = "bash"
			t.Setenv("PATH", t.TempDir())
			dest, _, _ := o.paths()
			switch change {
			case "updated":
				put(t, filepath.Join(dest, "plugins/fixture/plugin.sh"), []byte("updated plugin code; must not execute"), 0644)
				// Plugin contracts and origin receipts belong to the independent
				// plugin validator, not to ATM command PATH integration.
				put(t, filepath.Join(dest, "plugins/fixture/plugin.json"), []byte("not evaluated by setup"), 0644)
				put(t, filepath.Join(dest, "plugins/fixture/.atm-origin.json"), []byte(`{"updated":true}`), 0600)
			case "added":
				put(t, filepath.Join(dest, "plugins/new_plugin/plugin.json"), []byte("new independent plugin"), 0644)
			case "removed":
				if err := os.RemoveAll(filepath.Join(dest, "plugins")); err != nil {
					t.Fatal(err)
				}
			}
			before := tree(t, dest)
			status, err := Inspect(context.Background(), o)
			if err != nil || !status.Verified {
				t.Fatalf("plugin change invalidated CORE: %+v %v", status, err)
			}
			if runtime.GOOS != "windows" {
				result, err := Integrate(context.Background(), o)
				if err != nil || len(result.Changed) != 2 || !result.Verified {
					t.Fatalf("integration after plugin change: %+v %v", result, err)
				}
				if _, err = os.Stat(filepath.Join(o.UserHome, ".bashrc")); err != nil {
					t.Fatal(err)
				}
			}
			if !reflect.DeepEqual(before, tree(t, dest)) {
				t.Fatal("integration changed distribution or plugin files")
			}
		})
	}
}

func TestCOREChangesStillBlockIntegrationAfterPluginUpdate(t *testing.T) {
	for _, name := range []string{executableName(), "core-api.json", "release.json", "bin/atm", "lib/atm_core.sh", "libexec/atm-legacy.sh", "lang/en-us.lang"} {
		t.Run(name, func(t *testing.T) {
			o := installed(t)
			dest, _, _ := o.paths()
			put(t, filepath.Join(dest, "plugins/fixture/plugin.sh"), []byte("independent plugin update"), 0644)
			put(t, filepath.Join(dest, filepath.FromSlash(name)), []byte("changed CORE"), 0755)
			before := tree(t, o.UserHome)
			if status, err := Inspect(context.Background(), o); err == nil || status.Verified {
				t.Fatalf("changed CORE accepted: %+v %v", status, err)
			}
			// Rejection occurs before any Windows registry access too.
			if _, err := Integrate(context.Background(), o); err == nil {
				t.Fatal("changed CORE integrated")
			}
			if !reflect.DeepEqual(before, tree(t, o.UserHome)) {
				t.Fatal("failed integrity check wrote files")
			}
		})
	}
}

func TestCOREFileSetStillCheckedAfterPluginUpdate(t *testing.T) {
	for _, change := range []string{"missing-core", "unlisted-core", "linked-core"} {
		t.Run(change, func(t *testing.T) {
			if runtime.GOOS == "windows" && change == "linked-core" {
				t.Skip("symlink privilege not assumed")
			}
			o := installed(t)
			dest, _, _ := o.paths()
			put(t, filepath.Join(dest, "plugins/fixture/plugin.sh"), []byte("independent plugin update"), 0644)
			switch change {
			case "missing-core":
				if err := os.Remove(filepath.Join(dest, "lib/atm_core.sh")); err != nil {
					t.Fatal(err)
				}
			case "unlisted-core":
				put(t, filepath.Join(dest, "lib/extra.sh"), []byte("unowned runtime file"), 0644)
			case "linked-core":
				if err := os.Remove(filepath.Join(dest, "lib/atm_core.sh")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(o.Source, "lib/atm_core.sh"), filepath.Join(dest, "lib/atm_core.sh")); err != nil {
					t.Fatal(err)
				}
			}
			before := tree(t, o.UserHome)
			if _, err := Integrate(context.Background(), o); err == nil {
				t.Fatal("unsafe CORE file set accepted")
			}
			if !reflect.DeepEqual(before, tree(t, o.UserHome)) {
				t.Fatal("unsafe CORE check wrote files")
			}
		})
	}
}
