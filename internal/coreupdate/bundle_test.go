package coreupdate

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// This deterministic key is only a fixture. It is not the approved signing key
// and is never exposed through application policy or written to an installation.
func bundleFixtureKey() ed25519.PrivateKey {
	seed := sha256.Sum256([]byte("ATM full-package tests fixturekeys only"))
	return ed25519.NewKeyFromSeed(seed[:])
}

type bundleFixturePackage struct {
	raw, sig []byte
	archive  string
	entries  []paEntry
	manifest PackageManifest
}

func bundlePackage(t *testing.T, version string) bundleFixturePackage {
	t.Helper()
	m, entries := paFixture(t, runtime.GOOS, runtime.GOARCH)
	m.Version = version
	ext := ".tar.gz"
	if runtime.GOOS == "windows" {
		ext = ".zip"
	}
	m.Source = OfficialSource + "/releases/download/v" + version + "/atm-" + version + "-" + m.OS + "-" + m.Arch + ext
	for i := range entries {
		if entries[i].name == "LICENSE" {
			entries[i].data = []byte("fixture license " + version + "\n")
		}
	}
	name := "lib/new.txt"
	if version == "0.0.2" {
		name = "lib/obsolete.txt"
	}
	entries = paIndex(t, &m, append(entries, paEntry{name: name, data: []byte(version), mode: 0644}), nil)
	archive := paWrite(t, &m, paArchive(t, m, entries))
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	sig := ed25519.Sign(bundleFixtureKey(), append([]byte(PackageSignatureDomain), raw...))
	return bundleFixturePackage{raw, sig, archive, entries, m}
}

func bundlePut(t *testing.T, name string, b []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, b, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(name, mode); err != nil {
		t.Fatal(err)
	}
}

func bundleFixture(t *testing.T, enroll bool) (BundlePolicy, bundleFixturePackage) {
	t.Helper()
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(parent, 0700); err != nil {
		t.Fatal(err)
	}
	p := BundlePolicy{Root: filepath.Join(parent, "portable ATM"), PublicKey: bundleFixtureKey().Public().(ed25519.PublicKey), OS: runtime.GOOS, Arch: runtime.GOARCH}
	if err = os.Mkdir(p.Root, 0700); err != nil {
		t.Fatal(err)
	}
	old := bundlePackage(t, "0.0.2")
	for _, e := range old.entries {
		bundlePut(t, filepath.Join(p.Root, filepath.FromSlash(e.name)), e.data, e.mode)
	}
	bundlePut(t, filepath.Join(p.Root, "settings", "user.json"), []byte("user data\n"), 0600)
	bundlePut(t, filepath.Join(p.Root, ".atm-setup-receipt.json"), []byte("preserve setup metadata\n"), 0600)
	if enroll {
		result, e := EnrollBundle(context.Background(), p, old.raw, old.sig, old.archive, true)
		if e != nil || !result.Changed || result.State != "enrolled" {
			t.Fatalf("enroll: %+v %v", result, e)
		}
	}
	return p, old
}

func bundleRead(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func bundleSnapshot(t *testing.T, root string) bundleTree {
	t.Helper()
	r, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	tree, err := bundleScan(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func TestBundleLifecycle(t *testing.T) {
	p, _ := bundleFixture(t, true)
	next := bundlePackage(t, "0.0.3")
	info, err := InspectBundle(context.Background(), p)
	if err != nil || info.Manifest.Version != "0.0.2" || info.Baseline != info.Manifest {
		t.Fatalf("inspect: %+v %v", info, err)
	}
	plan, err := PrepareBundle(context.Background(), p, next.raw, next.sig, next.archive)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Info().Baseline != info.Manifest || plan.Info().TargetVersion != "0.0.3" || plan.Info().PreservedFiles != 2 {
		t.Fatalf("preview: %+v", plan.Info())
	}
	if _, err = json.Marshal(plan.Info()); err != nil {
		t.Fatal(err)
	}
	res, err := ApplyBundle(context.Background(), p, plan, true)
	if err != nil || !res.Changed || res.State != "applied" {
		t.Fatalf("apply: %+v %v", res, err)
	}
	if string(bundleRead(t, filepath.Join(p.Root, "settings/user.json"))) != "user data\n" {
		t.Fatal("user data changed")
	}
	if _, err = os.Stat(filepath.Join(p.Root, "lib/obsolete.txt")); !os.IsNotExist(err) {
		t.Fatal("obsolete owned file retained")
	}
	if string(bundleRead(t, filepath.Join(res.Backup, "lib/obsolete.txt"))) != "0.0.2" {
		t.Fatal("backup missing")
	}
	if err = BundleStartupGuard(p.Root); err != nil {
		t.Fatal(err)
	}
	// Ordinary user data may change between completed transactions.
	bundlePut(t, filepath.Join(p.Root, "settings/user.json"), []byte("updated preference"), 0600)
	third := bundlePackage(t, "0.0.4")
	plan, err = PrepareBundle(context.Background(), p, third.raw, third.sig, third.archive)
	if err != nil {
		t.Fatal(err)
	}
	res, err = ApplyBundle(context.Background(), p, plan, true)
	if err != nil || !res.Changed {
		t.Fatalf("repeat: %+v %v", res, err)
	}
	history, err := filepath.Glob(p.Root + ".atm-package-history-*")
	if err != nil || len(history) != 2 {
		t.Fatalf("history=%v %v", history, err)
	}
	// Recovery preserves post-publication edits in rejected instead of deleting.
	bundlePut(t, filepath.Join(p.Root, "settings/user.json"), []byte("latest preference"), 0600)
	bundlePut(t, filepath.Join(p.Root, "LICENSE"), []byte("local owned modification"), 0644)
	res, err = RecoverBundle(context.Background(), p, true)
	if err != nil || !res.Changed || res.State != "recovered" {
		t.Fatalf("recover: %+v %v", res, err)
	}
	info, err = InspectBundle(context.Background(), p)
	if err != nil || info.Manifest.Version != "0.0.3" {
		t.Fatalf("restored: %+v %v", info, err)
	}
	if string(bundleRead(t, filepath.Join(p.Root+BundleTransactionSuffix, "rejected/settings/user.json"))) != "latest preference" {
		t.Fatal("displaced changes lost")
	}
	if string(bundleRead(t, filepath.Join(p.Root, "settings/user.json"))) != "updated preference" {
		t.Fatal("old data not restored")
	}
	res, err = RecoverBundle(context.Background(), p, true)
	if err != nil || res.Changed {
		t.Fatalf("idempotent recovery: %+v %v", res, err)
	}
	plan, err = PrepareBundle(context.Background(), p, third.raw, third.sig, third.archive)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ApplyBundle(context.Background(), p, plan, true); err != nil {
		t.Fatalf("update after rollback: %v", err)
	}
}

func TestBundleReadOnlyAndPinning(t *testing.T) {
	p, old := bundleFixture(t, false)
	before := bundleSnapshot(t, p.Root)
	parentBefore, err := os.ReadDir(filepath.Dir(p.Root))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PrepareEnrollment(context.Background(), p, old.raw, old.sig, old.archive)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ApplyBundle(context.Background(), p, plan, false); !errors.Is(err, ErrConsentRequired) {
		t.Fatal(err)
	}
	p.DryRun = true
	res, err := ApplyBundle(context.Background(), p, plan, false)
	if err != nil || res.Changed {
		t.Fatalf("dry: %+v %v", res, err)
	}
	if _, err = RecoverBundle(context.Background(), p, false); err != nil {
		t.Fatal(err)
	}
	if !bundleSame(before, bundleSnapshot(t, p.Root)) {
		t.Fatal("preview modified installation")
	}
	parentAfter, err := os.ReadDir(filepath.Dir(p.Root))
	if err != nil || !reflect.DeepEqual(parentBefore, parentAfter) {
		t.Fatal("preview created sibling state")
	}
	p.DryRun = false
	bundlePut(t, filepath.Join(p.Root, "settings/user.json"), []byte("changed since preview"), 0600)
	res, err = ApplyBundle(context.Background(), p, plan, true)
	if err == nil || res.Changed {
		t.Fatalf("accepted stale preview: %+v %v", res, err)
	}
	if _, err = os.Stat(p.Root + BundleTransactionSuffix); !os.IsNotExist(err) {
		t.Fatal("stale preview wrote transaction")
	}
}

func TestBundleRejectsUnsafeAndModifiedTrees(t *testing.T) {
	for _, kind := range []string{"owned", "plugin", "unowned-collision", "symlink", "hardlink", "case", "type", "git", "nested-git", "pending-plugin", "pending-core", "staging-plugin", "reserved", "signature", "receipt-half"} {
		t.Run(kind, func(t *testing.T) {
			p, _ := bundleFixture(t, true)
			next := bundlePackage(t, "0.0.3")
			switch kind {
			case "owned":
				bundlePut(t, filepath.Join(p.Root, "LICENSE"), []byte("edited"), 0644)
			case "plugin":
				bundlePut(t, filepath.Join(p.Root, "plugins/demo/plugin.json"), []byte("independent plugin update"), 0644)
			case "unowned-collision":
				bundlePut(t, filepath.Join(p.Root, "lib/new.txt"), []byte("unowned"), 0644)
			case "symlink":
				if err := os.Symlink("settings/user.json", filepath.Join(p.Root, "linked")); err != nil {
					t.Skip(err)
				}
			case "hardlink":
				if err := os.Link(filepath.Join(p.Root, "LICENSE"), filepath.Join(p.Root, "linked")); err != nil {
					t.Skip(err)
				}
			case "case":
				bundlePut(t, filepath.Join(p.Root, "license"), []byte("alias"), 0644)
			case "type":
				if err := os.Mkdir(filepath.Join(p.Root, "lib/new.txt"), 0700); err != nil {
					t.Fatal(err)
				}
			case "git":
				bundlePut(t, filepath.Join(p.Root, ".git"), []byte("gitdir: elsewhere"), 0600)
			case "nested-git":
				bundlePut(t, filepath.Join(p.Root, "settings/.git"), []byte("gitdir: elsewhere"), 0600)
			case "pending-plugin":
				bundlePut(t, filepath.Join(p.Root, "plugins/.atm-update-pending.json"), []byte("{}"), 0600)
			case "pending-core":
				bundlePut(t, filepath.Join(p.Root, ".atm-core-update-pending"), []byte("{}"), 0600)
			case "staging-plugin":
				if err := os.Mkdir(filepath.Join(p.Root, "plugins/.atm-update-abandoned"), 0700); err != nil {
					t.Fatal(err)
				}
			case "reserved":
				bundlePut(t, filepath.Join(p.Root, "settings/.atm-package-journal"), []byte("{}"), 0600)
			case "signature":
				bundlePut(t, filepath.Join(p.Root, BundleSignatureName), make([]byte, 64), 0600)
			case "receipt-half":
				if err := os.Remove(filepath.Join(p.Root, BundleSignatureName)); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := PrepareBundle(context.Background(), p, next.raw, next.sig, next.archive); err == nil {
				t.Fatalf("accepted %s", kind)
			}
		})
	}
}

func TestBundleEnrollmentTrustAndVersions(t *testing.T) {
	p, old := bundleFixture(t, false)
	bad := append([]byte(nil), old.sig...)
	bad[0] ^= 1
	if _, err := EnrollBundle(context.Background(), p, old.raw, bad, old.archive, true); err == nil {
		t.Fatal("bad signature enrolled")
	}
	bundlePut(t, filepath.Join(p.Root, "LICENSE"), []byte("edited"), 0644)
	if _, err := EnrollBundle(context.Background(), p, old.raw, old.sig, old.archive, true); err == nil {
		t.Fatal("modified baseline enrolled")
	}
	for _, e := range old.entries {
		if e.name == "LICENSE" {
			bundlePut(t, filepath.Join(p.Root, e.name), e.data, e.mode)
		}
	}
	if _, err := EnrollBundle(context.Background(), p, old.raw, old.sig, old.archive, true); err != nil {
		t.Fatal(err)
	}
	if _, err := EnrollBundle(context.Background(), p, old.raw, old.sig, old.archive, true); err == nil {
		t.Fatal("re-enrolled existing baseline")
	}
	for _, v := range []string{"0.0.1", "0.0.2"} {
		candidate := bundlePackage(t, v)
		if _, err := PrepareBundle(context.Background(), p, candidate.raw, candidate.sig, candidate.archive); err == nil {
			t.Fatalf("version %s accepted", v)
		}
	}
	next := bundlePackage(t, "0.0.3")
	plan, err := PrepareBundle(context.Background(), p, next.raw, next.sig, next.archive)
	if err != nil {
		t.Fatal(err)
	}
	bundlePut(t, next.archive, []byte("corrupt archive"), 0600)
	if _, err = ApplyBundle(context.Background(), p, plan, true); err == nil {
		t.Fatal("corrupt archive applied")
	}
	if info, err := InspectBundle(context.Background(), p); err != nil || info.Manifest.Version != "0.0.2" {
		t.Fatal("corruption affected installed tree")
	}
}

func TestBundleFailureBoundaries(t *testing.T) {
	for _, enroll := range []bool{false, true} {
		for _, boundary := range []string{"journal-ready", "created", "candidate-ready", "before-backup", "after-backup", "after-activate", "committed"} {
			t.Run(fmt.Sprintf("enroll=%v/%s", enroll, boundary), func(t *testing.T) {
				p, old := bundleFixture(t, !enroll)
				before := bundleSnapshot(t, p.Root)
				candidate := old
				if !enroll {
					candidate = bundlePackage(t, "0.0.3")
				}
				plan, err := prepareBundle(context.Background(), p, candidate.raw, candidate.sig, candidate.archive, enroll)
				if err != nil {
					t.Fatal(err)
				}
				injected := errors.New("injected boundary failure")
				plan.checkpoint = func(name string) error {
					if name == boundary {
						return injected
					}
					return nil
				}
				res, err := ApplyBundle(context.Background(), p, plan, true)
				if !errors.Is(err, injected) {
					t.Fatalf("boundary not hit: %+v %v", res, err)
				}
				changed := boundary == "after-backup" || boundary == "after-activate" || boundary == "committed"
				if res.Changed != changed {
					t.Fatalf("incorrect Changed: %+v", res)
				}
				if boundary == "journal-ready" {
					if err = BundleStartupGuard(p.Root); err != nil {
						t.Fatal(err)
					}
					return
				}
				if boundary != "committed" && !errors.Is(BundleStartupGuard(p.Root), ErrRecoveryRequired) {
					t.Fatal("interruption not guarded")
				}
				if _, err = RecoverBundle(context.Background(), p, true); err != nil {
					t.Fatal(err)
				}
				if !bundleSame(before, bundleSnapshot(t, p.Root)) {
					t.Fatal("rollback did not restore exact original tree")
				}
				if err = BundleStartupGuard(p.Root); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestBundleCancellationAndCorruptBackup(t *testing.T) {
	p, _ := bundleFixture(t, true)
	next := bundlePackage(t, "0.0.3")
	plan, err := PrepareBundle(context.Background(), p, next.raw, next.sig, next.archive)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	plan.checkpoint = func(name string) error {
		if name == "created" {
			cancel()
		}
		return nil
	}
	res, err := ApplyBundle(ctx, p, plan, true)
	if !errors.Is(err, context.Canceled) || res.Changed {
		t.Fatalf("cancel: %+v %v", res, err)
	}
	if _, err = RecoverBundle(context.Background(), p, true); err != nil {
		t.Fatal(err)
	}
	plan, err = PrepareBundle(context.Background(), p, next.raw, next.sig, next.archive)
	if err != nil {
		t.Fatal(err)
	}
	res, err = ApplyBundle(context.Background(), p, plan, true)
	if err != nil {
		t.Fatal(err)
	}
	bundlePut(t, filepath.Join(res.Backup, "LICENSE"), []byte("corrupt backup"), 0644)
	before := bundleSnapshot(t, p.Root)
	if _, err = RecoverBundle(context.Background(), p, true); err == nil {
		t.Fatal("corrupt backup restored")
	}
	if !bundleSame(before, bundleSnapshot(t, p.Root)) {
		t.Fatal("corrupt backup recovery changed active tree")
	}
}

func TestBundleGuardAndLocks(t *testing.T) {
	p, _ := bundleFixture(t, true)
	checkout := t.TempDir()
	if err := os.Mkdir(filepath.Join(checkout, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := BundleStartupGuard(checkout); err != nil {
		t.Fatalf("source root guard: %v", err)
	}
	if err := BundleStartupGuard(filepath.Join(checkout, "absent")); err != nil {
		t.Fatalf("absent root guard: %v", err)
	}
	next := bundlePackage(t, "0.0.3")
	plan, err := PrepareBundle(context.Background(), p, next.raw, next.sig, next.archive)
	if err != nil {
		t.Fatal(err)
	}
	s, err := bundleOpen(p)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ApplyBundle(context.Background(), p, plan, true)
	s.close()
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("lock: %v", err)
	}
	bundlePut(t, filepath.Join(p.Root+BundleTransactionSuffix, "committed"), []byte(strings.Repeat("0", 64)), 0600)
	if !errors.Is(BundleStartupGuard(p.Root), ErrRecoveryRequired) {
		t.Fatal("corrupt marker accepted")
	}
}

func TestBundleUnenrolledIsNotDamaged(t *testing.T) {
	p, old := bundleFixture(t, false)
	if _, err := InspectBundle(context.Background(), p); !errors.Is(err, ErrBundleUnenrolled) {
		t.Fatalf("unsigned install: %v", err)
	}
	bundlePut(t, filepath.Join(p.Root, BundleManifestName), old.raw, 0600)
	if _, err := InspectBundle(context.Background(), p); err == nil || errors.Is(err, ErrBundleUnenrolled) {
		t.Fatalf("partial receipt: %v", err)
	}
	if err := os.Remove(filepath.Join(p.Root, BundleManifestName)); err != nil {
		t.Fatal(err)
	}
	bundlePut(t, filepath.Join(p.Root, BundleSignatureName), old.sig, 0600)
	if _, err := InspectBundle(context.Background(), p); err == nil || errors.Is(err, ErrBundleUnenrolled) {
		t.Fatalf("orphan signature: %v", err)
	}
	if err := os.Remove(filepath.Join(p.Root, BundleSignatureName)); err != nil {
		t.Fatal(err)
	}
	if _, err := EnrollBundle(context.Background(), p, old.raw, old.sig, old.archive, true); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(p.Root, "LICENSE")); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectBundle(context.Background(), p); err == nil || errors.Is(err, ErrBundleUnenrolled) {
		t.Fatalf("missing owned file: %v", err)
	}
}

func TestBundlePreservesOrdinaryModesAndEmptyDirectories(t *testing.T) {
	p, _ := bundleFixture(t, true)
	name := "settings/notes-\u00e9.txt"
	bundlePut(t, filepath.Join(p.Root, name), []byte("personal setting"), 0600)
	if err := os.Mkdir(filepath.Join(p.Root, "settings/empty"), 0700); err != nil {
		t.Fatal(err)
	}
	next := bundlePackage(t, "0.0.3")
	plan, err := PrepareBundle(context.Background(), p, next.raw, next.sig, next.archive)
	if err != nil {
		t.Fatal(err)
	}
	want := bundleSnapshot(t, p.Root)
	if _, err = ApplyBundle(context.Background(), p, plan, true); err != nil {
		t.Fatal(err)
	}
	got := bundleSnapshot(t, p.Root)
	if got.Files[name] != want.Files[name] || got.Dirs["settings/empty"] != want.Dirs["settings/empty"] || got.Dirs["."] != want.Dirs["."] {
		t.Fatal("unowned modes or empty directory lost")
	}
}
