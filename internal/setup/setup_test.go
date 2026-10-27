package setup

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/AtomyCloud/atm/internal/config"
	"github.com/AtomyCloud/atm/internal/plugin"
	"github.com/AtomyCloud/atm/internal/version"
)

func put(t *testing.T, path string, b []byte, mode fs.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func fixture(t *testing.T) Options {
	t.Helper()
	home := filepath.Join(t.TempDir(), "user space \u00e7 ' $(no-execution)")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		executableName(): b,
		"core-api.json":  []byte(`{"schema":1,"plugin_api":1,"update_protocol":1}`),
		"LICENSE":        []byte("test license"), "README.md": []byte("fixture only"),
		"lang/en-us.lang":             []byte("TEXT='test'\n"),
		"bin/atm":                     []byte("exit 99\n"),
		"lib/atm_core.sh":             []byte("exit 99\n"),
		"libexec/atm-legacy.sh":       []byte("exit 99\n"),
		"plugins/fixture/plugin.json": []byte(`{"schema":1,"id":"fixture","name":"Fixture","version":"0.0.1","min_core_api":1,"platforms":["linux"],"executor":"legacy-bash","protocol":"tty-v1","operations":["status"]}`),
		"plugins/fixture/plugin.sh":   []byte("exit 99\n"),
	}
	m := release{Schema: 1, Version: version.Version, Commit: "development", OS: runtime.GOOS, Arch: runtime.GOARCH, Migration: true, Files: map[string]string{}}
	for name, data := range files {
		mode := fs.FileMode(0644)
		if name == executableName() {
			mode = 0755
		}
		put(t, filepath.Join(source, filepath.FromSlash(name)), data, mode)
		m.Files[name] = fmt.Sprintf("%x", sha256.Sum256(data))
	}
	b, err = json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(source, "release.json"), b, 0644)
	return Options{UserHome: home, Source: source, TrustedManifestSHA256: fmt.Sprintf("%x", sha256.Sum256(b)), Consent: true}
}

func editManifest(t *testing.T, o *Options, fn func(*release)) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(o.Source, "release.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m release
	if err = json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	fn(&m)
	b, err = json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(o.Source, "release.json"), b, 0644)
	o.TrustedManifestSHA256 = fmt.Sprintf("%x", sha256.Sum256(b))
}

func tree(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		value := info.Mode().String()
		if info.Mode().IsRegular() {
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			value += fmt.Sprintf(" %x", sha256.Sum256(b))
		}
		if info.Mode()&os.ModeSymlink != 0 {
			s, err := os.Readlink(path)
			if err != nil {
				return err
			}
			value += " " + s
		}
		result[rel] = value
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestDefaultsAndReadOnlyPlan(t *testing.T) {
	home := t.TempDir()
	c := config.Config{Home: t.TempDir(), AssumeYes: true, DryRun: true}
	o := DefaultOptions(c, home)
	if o.Source != c.Home || o.AppsDir != filepath.Join(home, "Apps") || o.Consent || !o.DryRun {
		t.Fatalf("bad defaults: %+v", o)
	}
	before := tree(t, home)
	block, err := PathBlock(o)
	if err != nil || !strings.Contains(block, filepath.Join(home, "Apps", "atm")) || strings.Contains(block, c.Home) {
		t.Fatalf("block=%q err=%v", block, err)
	}
	status, err := Inspect(context.Background(), o)
	if err != nil || status.Installed {
		t.Fatalf("%+v %v", status, err)
	}
	if !reflect.DeepEqual(before, tree(t, home)) {
		t.Fatal("plan wrote files")
	}
	if !errors.Is(DesktopSupport(plugin.Plugin{Manifest: plugin.Manifest{ID: "fixture"}}), ErrUnavailable) {
		t.Fatal("desktop claimed support")
	}
	o.AppsDir = t.TempDir()
	if _, err = PathBlock(o); err == nil {
		t.Fatal("outside-home target accepted")
	}
}

func TestDefaultShellSelection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows uses HKCU, not SHELL")
	}
	for _, name := range []string{"bash", "zsh", "fish", "sh", "unknown"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("SHELL", "/usr/bin/"+name)
			o := DefaultOptions(config.Config{}, t.TempDir())
			if name == "unknown" {
				if o.Shell != "" {
					t.Fatal("unknown shell selected")
				}
			} else if o.Shell != name {
				t.Fatalf("selected %q, want %q", o.Shell, name)
			}
		})
	}
}

func TestInstallAndReceiptWithoutShells(t *testing.T) {
	o := fixture(t)
	t.Setenv("PATH", t.TempDir())
	result, err := Install(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Installed || len(result.Changed) != 1 || !strings.Contains(result.Desktop, "unavailable") {
		t.Fatalf("%+v", result)
	}
	if _, err = os.Stat(filepath.Join(o.UserHome, ".bashrc")); !os.IsNotExist(err) {
		t.Fatal("installation edited profile")
	}
	o.TrustedManifestSHA256 = ""
	status, err := Inspect(context.Background(), o)
	if err != nil || !status.Verified {
		t.Fatalf("receipt not usable: %+v %v", status, err)
	}
	before := tree(t, o.UserHome)
	if _, err = Install(context.Background(), o); !errors.Is(err, ErrExists) {
		t.Fatalf("existing target accepted: %v", err)
	}
	if !reflect.DeepEqual(before, tree(t, o.UserHome)) {
		t.Fatal("existing install changed")
	}
	put(t, filepath.Join(result.Destination, "README.md"), []byte("modified"), 0644)
	if _, err = Inspect(context.Background(), o); err == nil {
		t.Fatal("changed installed payload accepted")
	}
}

func TestDryRunConsentCancellation(t *testing.T) {
	o := fixture(t)
	before := tree(t, o.UserHome)
	o.Consent = false
	if _, err := Install(context.Background(), o); !errors.Is(err, ErrConsent) {
		t.Fatal(err)
	}
	o.DryRun = true
	result, err := Install(context.Background(), o)
	if err != nil || !result.DryRun || len(result.Planned) == 0 {
		t.Fatalf("%+v %v", result, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = Install(ctx, o); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, tree(t, o.UserHome)) {
		t.Fatal("dry run/denial/cancel wrote files")
	}
	o.DryRun, o.Consent = false, true
	ctx, cancel = context.WithCancel(context.Background())
	o.checkpoint = func(phase string) error {
		if phase == "before-publish" {
			cancel()
		}
		return nil
	}
	if _, err = Install(ctx, o); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(o.UserHome, "Apps"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("partial publication left behind: %v %v", entries, err)
	}
	if _, err = os.Lstat(filepath.Join(o.UserHome, ".atm-setup-lock")); !os.IsNotExist(err) {
		t.Fatal("lock leaked")
	}
}

func TestRejectDistributionPayloads(t *testing.T) {
	for _, name := range []string{"no-pin", "wrong-pin", "wrong-target", "wrong-version", "missing", "modified", "modified-plugin", "private", "private-listed", "traversal", "case-directory", "reserved", "bad-binary", "bad-api", "bad-plugin", "symlink", "unlisted", "oversize"} {
		t.Run(name, func(t *testing.T) {
			o := fixture(t)
			switch name {
			case "no-pin":
				o.TrustedManifestSHA256 = ""
			case "wrong-pin":
				o.TrustedManifestSHA256 = strings.Repeat("0", 64)
			case "wrong-target":
				editManifest(t, &o, func(m *release) { m.OS = "unsupported" })
			case "wrong-version":
				editManifest(t, &o, func(m *release) { m.Version = "99" })
			case "missing":
				if err := os.Remove(filepath.Join(o.Source, "LICENSE")); err != nil {
					t.Fatal(err)
				}
			case "modified":
				put(t, filepath.Join(o.Source, "README.md"), []byte("dirty"), 0644)
			case "modified-plugin":
				put(t, filepath.Join(o.Source, "plugins/fixture/plugin.sh"), []byte("updated after packaging"), 0644)
			case "private":
				put(t, filepath.Join(o.Source, ".codex", "private.toml"), []byte("never copy"), 0600)
			case "private-listed":
				editManifest(t, &o, func(m *release) { m.Files["internal-docs/MEMORY.md"] = strings.Repeat("0", 64) })
			case "traversal":
				editManifest(t, &o, func(m *release) { m.Files["plugins/../secret"] = strings.Repeat("0", 64) })
			case "case-directory":
				editManifest(t, &o, func(m *release) {
					m.Files["lib/Foo/a"] = strings.Repeat("0", 64)
					m.Files["lib/foo/b"] = strings.Repeat("0", 64)
				})
			case "reserved":
				editManifest(t, &o, func(m *release) { m.Files["lib/CON.txt"] = strings.Repeat("0", 64) })
			case "bad-binary", "bad-api", "bad-plugin":
				path := executableName()
				if name == "bad-api" {
					path = "core-api.json"
				}
				if name == "bad-plugin" {
					path = "plugins/fixture/plugin.json"
				}
				b := []byte("{}")
				put(t, filepath.Join(o.Source, filepath.FromSlash(path)), b, 0755)
				editManifest(t, &o, func(m *release) { m.Files[path] = fmt.Sprintf("%x", sha256.Sum256(b)) })
			case "symlink":
				if runtime.GOOS == "windows" {
					t.Skip("symlink privilege not assumed")
				}
				if err := os.Remove(filepath.Join(o.Source, "README.md")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(o.Source, "LICENSE"), filepath.Join(o.Source, "README.md")); err != nil {
					t.Fatal(err)
				}
			case "unlisted":
				put(t, filepath.Join(o.Source, "lib", "extra.txt"), []byte("extra"), 0644)
			case "oversize":
				f, err := os.OpenFile(filepath.Join(o.Source, "README.md"), os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				err = f.Truncate(maxFile + 1)
				f.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			before := tree(t, o.UserHome)
			if _, err := Install(context.Background(), o); err == nil {
				t.Fatal("unsafe distribution accepted")
			}
			if !reflect.DeepEqual(before, tree(t, o.UserHome)) {
				t.Fatal("verification failure wrote files")
			}
		})
	}
}

func TestPublicationPreservesRacingTarget(t *testing.T) {
	o := fixture(t)
	dest, _, _ := o.paths()
	o.checkpoint = func(phase string) error {
		if phase == "before-publish" {
			return os.Mkdir(dest, 0700)
		}
		return nil
	}
	if _, err := Install(context.Background(), o); err == nil {
		t.Fatal("replaced racing empty directory")
	}
	entries, err := os.ReadDir(dest)
	if err != nil || len(entries) != 0 {
		t.Fatalf("racing directory changed: %v %v", entries, err)
	}
}

func TestCopyRechecksSourceAndLock(t *testing.T) {
	o := fixture(t)
	o.checkpoint = func(phase string) error {
		if phase == "source-verified" {
			return os.WriteFile(filepath.Join(o.Source, "README.md"), []byte("changed after validation"), 0644)
		}
		return nil
	}
	if _, err := Install(context.Background(), o); err == nil {
		t.Fatal("changed source copied")
	}
	dest, _, _ := o.paths()
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("partial install published")
	}
	o = fixture(t)
	if err := os.Mkdir(filepath.Join(o.UserHome, ".atm-setup-lock"), 0700); err != nil {
		t.Fatal(err)
	}
	before := tree(t, o.UserHome)
	if _, err := Install(context.Background(), o); err == nil {
		t.Fatal("busy install accepted")
	}
	if !reflect.DeepEqual(before, tree(t, o.UserHome)) {
		t.Fatal("stale lock removed")
	}
}

func TestSymlinkedDestinationParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privilege not assumed")
	}
	o := fixture(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(o.UserHome, "Apps")); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(context.Background(), o); err == nil {
		t.Fatal("symlinked apps accepted")
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatal("escaped destination")
	}
}

// Optional real-package smoke test, still installing only into t.TempDir.
// Both values are supplied by the local package-build verification harness.
func TestPackagedDistribution(t *testing.T) {
	source := os.Getenv("ATM_SETUP_TEST_DISTRIBUTION")
	if source == "" {
		t.Skip("no local package fixture supplied")
	}
	o := Options{UserHome: t.TempDir(), Source: source, Consent: true, TrustedManifestSHA256: os.Getenv("ATM_SETUP_TEST_MANIFEST_SHA256")}
	result, err := Install(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	o.TrustedManifestSHA256 = ""
	s, err := Inspect(context.Background(), o)
	if err != nil || !s.Verified || s.Destination != result.Destination {
		t.Fatalf("%+v %v", s, err)
	}
	if runtime.GOOS != "windows" {
		o.Shell = "bash"
		if _, err = Integrate(context.Background(), o); err != nil {
			t.Fatal(err)
		}
	}
}
