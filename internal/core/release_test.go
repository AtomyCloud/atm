package core

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
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
	"github.com/AtomyCloud/atm/internal/coreupdate"
	"github.com/AtomyCloud/atm/internal/install"
	"github.com/AtomyCloud/atm/internal/updates"
)

func releaseTestRoot(t *testing.T) (string, config.Config) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"runtime", "config", "state", "apps", "inputs"} {
		dir := filepath.Join(root, name)
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "sentinel"), []byte("preserve fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	c := config.Config{Home: filepath.Join(root, "runtime"), ConfigDir: filepath.Join(root, "config"), StateDir: filepath.Join(root, "state"), AppsDir: filepath.Join(root, "apps")}
	c.PluginsDir = filepath.Join(c.Home, "plugins")
	return root, c
}

func releaseTestSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	snapshot := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		var content []byte
		if info.Mode()&os.ModeSymlink != 0 {
			link, e := os.Readlink(path)
			if e != nil {
				return e
			}
			content = []byte(link)
		} else if info.Mode().IsRegular() {
			content, err = os.ReadFile(path)
			if err != nil {
				return err
			}
		}
		snapshot[path] = fmt.Sprintf("%s:%s:%x", info.Mode(), info.ModTime().UTC(), sha256.Sum256(content))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestReleaseRoutingAndArgumentOwnership(t *testing.T) {
	for _, tc := range []struct {
		args, want []string
		mutating   bool
	}{
		{[]string{"updates", "check-core"}, []string{"updates", "check-core"}, false},
		{[]string{"updates", "core"}, []string{"updates", "core"}, true},
		{[]string{"self-update"}, []string{"updates", "core"}, true},
		{[]string{"updates", "core", "manifest", "signature", "archive"}, []string{"updates", "core", "manifest", "signature", "archive"}, true},
		{[]string{"updates", "enroll-core", "manifest", "signature", "archive"}, []string{"updates", "enroll-core", "manifest", "signature", "archive"}, true},
		{[]string{"updates", "verify-package", "manifest", "signature", "archive"}, []string{"updates", "verify-package", "manifest", "signature", "archive"}, false},
	} {
		p, err := Prepare(config.Config{}, tc.args, nil)
		if err != nil || p.Executor != "native-core" || p.Mutating != tc.mutating || !reflect.DeepEqual(p.Arguments, tc.want) || !IsCommand(tc.args) {
			t.Fatal(tc, p, err)
		}
		tc.args[0] = "changed"
		if !reflect.DeepEqual(p.Arguments, tc.want) {
			t.Fatal("plan retained caller's argument slice")
		}
	}
	for _, args := range [][]string{
		{"updates", "check-core", "extra"}, {"updates", "enroll-core"}, {"updates", "core", "manifest"},
		{"updates", "core", "manifest", "sig"}, {"updates", "core", "manifest", "sig", "archive", "extra"},
		{"updates", "enroll-core", "", "sig", "archive"}, {"updates", "core", "bad\npath", "sig", "archive"},
		{"updates", "core", "manifest", "bad\x00sig", "archive"}, {"updates", "core", "manifest", "sig", "bad\rarchive"},
	} {
		if _, err := Prepare(config.Config{}, args, nil); err == nil {
			t.Fatal("invalid release route accepted", args)
		}
	}
}

func TestReleaseDryRunDoesNotReadInputsOrCreateState(t *testing.T) {
	root, c := releaseTestRoot(t)
	c.Home = filepath.Join(root, "absent-installation")
	before := releaseTestSnapshot(t, root)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, args := range [][]string{
		{"updates", "check-core"}, {"updates", "core"}, {"self-update"},
		{"updates", "core", "missing.json", "missing.sig", "missing.archive"},
		{"updates", "enroll-core", "missing.json", "missing.sig", "missing.archive"},
	} {
		for _, configDry := range []bool{false, true} {
			c.DryRun = configDry
			p, err := Prepare(c, args, nil)
			if err != nil {
				t.Fatal(err)
			}
			p.DryRun = !configDry
			e, err := Resolve(ctx, c, p)
			if err != nil || e == nil || e.release != nil || e.Preview.Release != nil {
				t.Fatal("dry-run resolved release inputs", e, err)
			}
			if _, err = Execute(ctx, c, e, true); err == nil || !strings.Contains(err.Error(), "dry-run") {
				t.Fatal("dry-run execution not refused", err)
			}
		}
	}
	if !reflect.DeepEqual(before, releaseTestSnapshot(t, root)) {
		t.Fatal("dry-run modified fixture roots")
	}
}

func TestReleaseConsentAndProductionGateDoNotWrite(t *testing.T) {
	root, c := releaseTestRoot(t)
	before := releaseTestSnapshot(t, root)
	for _, operation := range []string{"core", "enroll-core"} {
		p, err := Prepare(c, []string{"updates", operation, "absent.json", "absent.sig", "absent.archive"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		e := &Execution{Preview: Preview{Plan: p}}
		missing := c
		missing.Home = filepath.Join(root, "missing")
		if _, err := Execute(context.Background(), missing, e, false); err == nil || !strings.Contains(err.Error(), "consent") {
			t.Fatal("consent was not checked before accessing installation", err)
		}
		if !coreupdate.ProductionEnabled {
			result, err := Execute(context.Background(), c, e, true)
			if !errors.Is(err, coreupdate.ErrProductionDisabled) || result.RestartRequired || len(result.ChangedPlugins) != 0 {
				t.Fatal("production gate bypassed", result, err)
			}
		}
	}
	if !reflect.DeepEqual(before, releaseTestSnapshot(t, root)) {
		t.Fatal("unconfirmed/disabled release wrote files")
	}
}

func TestReleaseUnenrolledBaselineIsReadOnlyAndNotCurrent(t *testing.T) {
	root, c := releaseTestRoot(t)
	before := releaseTestSnapshot(t, root)
	p, err := Prepare(c, []string{"updates", "core"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	e, err := Resolve(context.Background(), c, p)
	if err == nil || !strings.Contains(err.Error(), "signed installed baseline required") {
		t.Fatal("unenrolled root reached candidate discovery", err)
	}
	if e != nil && (e.Preview.Release != nil || e.release != nil && (e.release.remote != nil || e.release.bundle != nil)) {
		t.Fatal("unenrolled root acquired a release plan")
	}
	p, err = Prepare(c, []string{"updates", "check-core"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	e, err = Resolve(context.Background(), c, p)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Execute(context.Background(), c, e, false)
	row, ok := r.Value.(updates.Row)
	if err != nil || !ok || row.Component != "core" || row.Status != "unmanaged" || row.Commit != "" || !strings.Contains(row.Detail, "Signed installed baseline unavailable") {
		t.Fatal("unsigned installation represented as current", r, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Resolve(ctx, c, p); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := checkCoreRelease(ctx, c); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, releaseTestSnapshot(t, root)) {
		t.Fatal("baseline inspection changed fixture roots")
	}
}

func TestReleaseParentRejectsFixtureSignerBeforeArchiveInspection(t *testing.T) {
	root, c := releaseTestRoot(t)
	key, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	stem := "atm-0.0.3-" + runtime.GOOS + "-" + runtime.GOARCH
	ext := ".tar.gz"
	if runtime.GOOS == "windows" {
		ext = ".zip"
	}
	m := coreupdate.PackageManifest{Schema: 1, Scope: coreupdate.PackageScope, Source: coreupdate.OfficialSource + "/releases/download/v0.0.3/" + stem + ext, Version: "0.0.3", Commit: strings.Repeat("a", 40), OS: runtime.GOOS, Arch: runtime.GOARCH, Migration: true, Archive: install.File{SHA256: strings.Repeat("b", 64), Size: 1024, Mode: 0644}, ReleaseSHA256: strings.Repeat("c", 64)}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	sig := ed25519.Sign(private, append([]byte(coreupdate.PackageSignatureDomain), raw...))
	if _, err := coreupdate.VerifyPackageManifest(raw, sig, key, runtime.GOOS, runtime.GOARCH); err != nil {
		t.Fatal("invalid fixture", err)
	}
	manifest, signature := filepath.Join(root, "inputs", "manifest.json"), filepath.Join(root, "inputs", "manifest.sig")
	if err := os.WriteFile(manifest, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(signature, sig, 0600); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "inputs", "must-not-be-read.archive")
	before := releaseTestSnapshot(t, root)
	for _, operation := range []string{"core", "enroll-core", "verify-package"} {
		p, err := Prepare(c, []string{"updates", operation, manifest, signature, archive}, nil)
		if err != nil {
			t.Fatal(err)
		}
		e, err := Resolve(context.Background(), c, p)
		if operation == "verify-package" && err == nil {
			_, err = Execute(context.Background(), c, e, false)
		}
		if err == nil || !strings.Contains(err.Error(), "invalid signed package manifest") {
			t.Fatal(operation, "fixture key was trusted or archive was read first", err)
		}
	}
	p, err := officialBundlePolicy(c.Home)
	if err != nil {
		t.Fatal(err)
	}
	official, err := coreupdate.OfficialPublicKey()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(p.PublicKey, official) || bytes.Equal(p.PublicKey, key) || p.Root != c.Home || p.OS != runtime.GOOS || p.Arch != runtime.GOARCH {
		t.Fatal("parent did not pin official trust/target")
	}
	if !reflect.DeepEqual(before, releaseTestSnapshot(t, root)) {
		t.Fatal("rejected signature wrote files")
	}
}

func TestReleaseInputBoundsAndLinkRejection(t *testing.T) {
	root, _ := releaseTestRoot(t)
	valid := filepath.Join(root, "inputs", "valid")
	empty := filepath.Join(root, "inputs", "empty")
	oversize := filepath.Join(root, "inputs", "oversize")
	for path, data := range map[string][]byte{valid: []byte("1234"), empty: {}, oversize: []byte("12345")} {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	invalid := []string{empty, oversize, filepath.Join(root, "inputs"), filepath.Join(root, "missing")}
	if runtime.GOOS != "windows" {
		link := filepath.Join(root, "inputs", "link")
		if err := os.Symlink(valid, link); err != nil {
			t.Fatal(err)
		}
		invalid = append(invalid, link)
	}
	before := releaseTestSnapshot(t, root)
	got, err := releaseInput(valid, 4)
	if err != nil || string(got) != "1234" {
		t.Fatal(got, err)
	}
	got[0] = 'x'
	for _, path := range invalid {
		if _, err := releaseInput(path, 4); err == nil {
			t.Fatal("unsafe release input accepted", path)
		}
	}
	if !reflect.DeepEqual(before, releaseTestSnapshot(t, root)) {
		t.Fatal("release input inspection changed files")
	}
}

func TestReleaseCheckDistinguishesDamageAndRecovery(t *testing.T) {
	for _, state := range []string{"unmanaged", "unknown", "blocked"} {
		t.Run(state, func(t *testing.T) {
			root, c := releaseTestRoot(t)
			switch state {
			case "unknown":
				if err := os.WriteFile(filepath.Join(c.Home, coreupdate.BundleManifestName), []byte("invalid receipt"), 0600); err != nil {
					t.Fatal(err)
				}
			case "blocked":
				if err := os.Mkdir(c.Home+coreupdate.BundleTransactionSuffix, 0700); err != nil {
					t.Fatal(err)
				}
			}
			before := releaseTestSnapshot(t, root)
			row, err := checkCoreRelease(context.Background(), c)
			if row.Status != state || (err == nil) != (state == "unmanaged") {
				t.Fatal(row, err)
			}
			if !reflect.DeepEqual(before, releaseTestSnapshot(t, root)) {
				t.Fatal("inspection changed state")
			}
		})
	}
}
