package coreupdate

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

type bundleCrashInput struct {
	Policy            BundlePolicy
	Raw, Sig          []byte
	Archive, Boundary string
	Enroll, Recover   bool
}

func TestBundleCrashHelper(t *testing.T) {
	name := os.Getenv("ATM_BUNDLE_CRASH_FIXTURE")
	if name == "" {
		return
	}
	var in bundleCrashInput
	if err := json.Unmarshal(bundleRead(t, name), &in); err != nil {
		t.Fatal(err)
	}
	hook := func(boundary string) error {
		if boundary == in.Boundary {
			os.Exit(71)
		}
		return nil
	}
	if in.Recover {
		if _, err := recoverBundle(context.Background(), in.Policy, true, hook); err != nil {
			t.Fatal(err)
		}
	} else {
		plan, err := prepareBundle(context.Background(), in.Policy, in.Raw, in.Sig, in.Archive, in.Enroll)
		if err != nil {
			t.Fatal(err)
		}
		plan.checkpoint = hook
		if _, err = ApplyBundle(context.Background(), in.Policy, plan, true); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("crash boundary not reached")
}

func bundleCrash(t *testing.T, in bundleCrashInput) {
	t.Helper()
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(t.TempDir(), "crash-input.json")
	bundlePut(t, name, b, 0600)
	cmd := exec.Command(os.Args[0], "-test.run=^TestBundleCrashHelper$", "-test.count=1")
	cmd.Env = append(os.Environ(), "ATM_BUNDLE_CRASH_FIXTURE="+name)
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 71 {
		t.Fatalf("crash helper: %v\n%s", err, out)
	}
}

func TestBundleSubprocessCrashes(t *testing.T) {
	for _, enroll := range []bool{false, true} {
		for _, boundary := range []string{"journal-ready", "created", "candidate-ready", "before-backup", "after-backup", "after-activate", "committed"} {
			kind := "update/"
			if enroll {
				kind = "enroll/"
			}
			t.Run(kind+boundary, func(t *testing.T) {
				p, old := bundleFixture(t, !enroll)
				before := bundleSnapshot(t, p.Root)
				next := old
				if !enroll {
					next = bundlePackage(t, "0.0.3")
				}
				bundleCrash(t, bundleCrashInput{Policy: p, Raw: next.raw, Sig: next.sig, Archive: next.archive, Boundary: boundary, Enroll: enroll})
				if boundary == "journal-ready" {
					if err := BundleStartupGuard(p.Root); err != nil {
						t.Fatal(err)
					}
					if !bundleSame(before, bundleSnapshot(t, p.Root)) {
						t.Fatal("unpublished journal changed active root")
					}
					// A pre-publication reservation can remain after process death;
					// it is not active state and is never silently deleted/adopted.
					return
				}
				if boundary != "committed" && !errors.Is(BundleStartupGuard(p.Root), ErrRecoveryRequired) {
					t.Fatal("crash not guarded")
				}
				if _, err := RecoverBundle(context.Background(), p, true); err != nil {
					t.Fatal(err)
				}
				if !bundleSame(before, bundleSnapshot(t, p.Root)) {
					t.Fatal("crash rollback changed original data")
				}
				if err := BundleStartupGuard(p.Root); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestBundleRecoverySubprocessCrashes(t *testing.T) {
	for _, boundary := range []string{"recovering", "before-quarantine", "after-quarantine", "after-restore", "recovered"} {
		t.Run(boundary, func(t *testing.T) {
			p, _ := bundleFixture(t, true)
			before := bundleSnapshot(t, p.Root)
			next := bundlePackage(t, "0.0.3")
			plan, err := PrepareBundle(context.Background(), p, next.raw, next.sig, next.archive)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = ApplyBundle(context.Background(), p, plan, true); err != nil {
				t.Fatal(err)
			}
			bundleCrash(t, bundleCrashInput{Policy: p, Boundary: boundary, Recover: true})
			if boundary != "recovered" && !errors.Is(BundleStartupGuard(p.Root), ErrRecoveryRequired) {
				t.Fatal("recovery interruption not guarded")
			}
			if _, err = RecoverBundle(context.Background(), p, true); err != nil {
				t.Fatal(err)
			}
			if !bundleSame(before, bundleSnapshot(t, p.Root)) {
				t.Fatal("repeated recovery changed original data")
			}
			if err = BundleStartupGuard(p.Root); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestBundleStageCorruption(t *testing.T) {
	for _, boundary := range []string{"candidate-ready", "before-backup", "after-backup", "after-activate"} {
		t.Run(boundary, func(t *testing.T) {
			p, _ := bundleFixture(t, true)
			before := bundleSnapshot(t, p.Root)
			next := bundlePackage(t, "0.0.3")
			plan, err := PrepareBundle(context.Background(), p, next.raw, next.sig, next.archive)
			if err != nil {
				t.Fatal(err)
			}
			plan.checkpoint = func(name string) error {
				if name == boundary {
					root := filepath.Join(p.Root+BundleTransactionSuffix, "candidate")
					if name == "after-activate" {
						root = p.Root
					}
					bundlePut(t, filepath.Join(root, "LICENSE"), []byte("corrupted stage"), 0644)
				}
				return nil
			}
			res, err := ApplyBundle(context.Background(), p, plan, true)
			if err == nil || res.State != "recovery-required" {
				t.Fatalf("corruption not guarded: %+v %v", res, err)
			}
			if _, err = RecoverBundle(context.Background(), p, true); err != nil {
				t.Fatal(err)
			}
			if !bundleSame(before, bundleSnapshot(t, p.Root)) {
				t.Fatal("corrupt stage rollback failed")
			}
		})
	}
}

func TestBundleMidStageCancellation(t *testing.T) {
	p, _ := bundleFixture(t, true)
	before := bundleSnapshot(t, p.Root)
	next := bundlePackage(t, "0.0.3")
	plan, err := PrepareBundle(context.Background(), p, next.raw, next.sig, next.archive)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	plan.checkpoint = func(name string) error {
		if name == "candidate-file" {
			cancel()
		}
		return nil
	}
	res, err := ApplyBundle(ctx, p, plan, true)
	if !errors.Is(err, context.Canceled) || res.Changed {
		t.Fatalf("mid-stage cancel: %+v %v", res, err)
	}
	count := 0
	err = filepath.WalkDir(filepath.Join(p.Root+BundleTransactionSuffix, "candidate"), func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			count++
		}
		return nil
	})
	if err != nil || count == 0 {
		t.Fatalf("cancellation did not interrupt a partial stage: %d %v", count, err)
	}
	if _, err = RecoverBundle(context.Background(), p, true); err != nil {
		t.Fatal(err)
	}
	if !bundleSame(before, bundleSnapshot(t, p.Root)) {
		t.Fatal("partial-stage rollback changed original tree")
	}
}

func TestBundleUnexpectedRootCollision(t *testing.T) {
	p, _ := bundleFixture(t, true)
	next := bundlePackage(t, "0.0.3")
	plan, err := PrepareBundle(context.Background(), p, next.raw, next.sig, next.archive)
	if err != nil {
		t.Fatal(err)
	}
	plan.checkpoint = func(name string) error {
		if name == "after-backup" {
			return os.Mkdir(p.Root, 0700)
		}
		return nil
	}
	res, err := ApplyBundle(context.Background(), p, plan, true)
	if err == nil || !res.Changed || res.State != "recovery-required" {
		t.Fatalf("unexpected destination replaced: %+v %v", res, err)
	}
	entries, err := os.ReadDir(p.Root)
	if err != nil || len(entries) != 0 {
		t.Fatal("unexpected destination contents changed")
	}
	if _, err = RecoverBundle(context.Background(), p, true); err == nil {
		t.Fatal("unknown root displaced during recovery")
	}
	// Only the test-created empty obstacle is removed, never updater data.
	if err = os.Remove(p.Root); err != nil {
		t.Fatal(err)
	}
	if _, err = RecoverBundle(context.Background(), p, true); err != nil {
		t.Fatal(err)
	}
}
