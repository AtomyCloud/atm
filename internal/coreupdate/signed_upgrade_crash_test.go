package coreupdate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// This is deliberately separate from fixture-key crash coverage: both packages
// use the embedded official trust root, and recovery runs the real external CLI.
func TestSignedUpgradeCrashRecovery(t *testing.T) {
	candidate := os.Getenv("ATM_TEST_SIGNED_UPGRADE_DIR")
	if candidate == "" {
		t.Skip("set ATM_TEST_SIGNED_UPGRADE_DIR to the private signed 0.0.3 candidate")
	}
	if runtime.GOOS != "linux" || (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64") {
		t.Skip("requires a supported native Linux host")
	}
	baseline := os.Getenv("ATM_TEST_SIGNED_RELEASE_DIR")
	driver := os.Getenv("ATM_TEST_UPDATER_BIN")
	if baseline == "" || !filepath.IsAbs(driver) {
		t.Fatal("require baseline artifacts and an absolute trusted external updater")
	}
	load := func(dir, version, metadata string) bundleFixturePackage {
		t.Helper()
		stem := "atm-" + version + "-linux-" + runtime.GOARCH
		inputs := []string{filepath.Join(dir, metadata, stem+".manifest.json"), filepath.Join(dir, metadata, stem+".manifest.sig"), filepath.Join(dir, stem+".tar.gz")}
		data := make([][]byte, len(inputs))
		for i, name := range inputs {
			var err error
			data[i], err = readVerificationFile(name, []int64{MaxManifestBytes, 64, MaxPackageBytes}[i])
			if err != nil {
				t.Fatal(err)
			}
			before := paHash(data[i])
			t.Cleanup(func() {
				if paHash(bundleRead(t, name)) != before {
					t.Errorf("official artifact changed: %s", name)
				}
			})
		}
		archive := filepath.Join(t.TempDir(), stem+".tar.gz")
		bundlePut(t, archive, data[2], 0600)
		key, err := OfficialPublicKey()
		if err != nil {
			t.Fatal(err)
		}
		m, err := VerifyPackageManifest(data[0], data[1], key, runtime.GOOS, runtime.GOARCH)
		if err != nil || m.Version != version {
			t.Fatalf("invalid signed %s manifest: %v", version, err)
		}
		if _, err := InspectPackage(context.Background(), archive, m); err != nil {
			t.Fatal(err)
		}
		return bundleFixturePackage{raw: data[0], sig: data[1], archive: archive, manifest: m}
	}
	old := load(baseline, "0.0.2", "signatures-linux-"+runtime.GOARCH)
	next := load(candidate, "0.0.3", "signatures")
	fixture := func(t *testing.T) BundlePolicy {
		t.Helper()
		parent, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(parent, 0700); err != nil {
			t.Fatal(err)
		}
		key, err := OfficialPublicKey()
		if err != nil {
			t.Fatal(err)
		}
		p := BundlePolicy{Root: filepath.Join(parent, "signed runtime"), PublicKey: key, OS: runtime.GOOS, Arch: runtime.GOARCH}
		if err := os.Mkdir(p.Root, 0700); err != nil {
			t.Fatal(err)
		}
		files, err := InspectPackage(context.Background(), old.archive, old.manifest)
		if err != nil {
			t.Fatal(err)
		}
		r, err := os.OpenRoot(p.Root)
		if err != nil {
			t.Fatal(err)
		}
		err = bundleExtract(context.Background(), r, &BundlePlan{p: p, archive: old.archive, files: files, info: BundleInfo{Manifest: old.manifest}})
		closeErr := r.Close()
		if err := errors.Join(err, closeErr); err != nil {
			t.Fatal(err)
		}
		bundlePut(t, filepath.Join(p.Root, "settings/user.json"), []byte("preserve unknown user data\n"), 0600)
		if err := os.Mkdir(filepath.Join(p.Root, "empty-user-directory"), 0750); err != nil {
			t.Fatal(err)
		}
		if result, err := EnrollBundle(context.Background(), p, old.raw, old.sig, old.archive, true); err != nil || result.State != "enrolled" {
			t.Fatalf("enrollment: %+v %v", result, err)
		}
		return p
	}
	recoverCLI := func(t *testing.T, p BundlePolicy) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, driver, "--yes", "--json", "recover-core", "--root", p.Root)
		cmd.Dir = filepath.Dir(p.Root)
		// Recovery must work even after the active root has disappeared, without
		// resolving a runtime, configuration, plugins, shell, agent or network token.
		cmd.Env = []string{"PATH=", "HOME=" + filepath.Join(cmd.Dir, "user"), "ATM_HOME=" + filepath.Join(cmd.Dir, "missing"), "ATM_CONFIG_FILE=" + filepath.Join(cmd.Dir, "missing-config"), "SSH_AUTH_SOCK="}
		out, err := cmd.CombinedOutput()
		var result BundleResult
		if err != nil || json.Unmarshal(out, &result) != nil || result.State != "recovered" {
			t.Fatalf("external recovery: %v %s", err, out)
		}
	}
	for _, boundary := range []string{"candidate-ready", "after-backup", "after-activate", "committed"} {
		t.Run("update/"+boundary, func(t *testing.T) {
			p := fixture(t)
			before := bundleSnapshot(t, p.Root)
			bundleCrash(t, bundleCrashInput{Policy: p, Raw: next.raw, Sig: next.sig, Archive: next.archive, Boundary: boundary})
			if boundary != "committed" && !errors.Is(BundleStartupGuard(p.Root), ErrRecoveryRequired) {
				t.Fatal("interrupted signed update not guarded")
			}
			if boundary == "after-backup" {
				if _, err := os.Lstat(p.Root); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("missing-root crash was not exercised", err)
				}
			}
			recoverCLI(t, p)
			if !bundleSame(before, bundleSnapshot(t, p.Root)) {
				t.Fatal("signed crash recovery did not restore the exact original tree")
			}
			info, err := InspectBundle(context.Background(), p)
			if err != nil || info.Manifest != old.manifest {
				t.Fatal("recovered signed baseline mismatch", err)
			}
			if err := BundleStartupGuard(p.Root); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, boundary := range []string{"after-quarantine", "after-restore"} {
		t.Run("recovery/"+boundary, func(t *testing.T) {
			p := fixture(t)
			before := bundleSnapshot(t, p.Root)
			plan, err := PrepareBundle(context.Background(), p, next.raw, next.sig, next.archive)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ApplyBundle(context.Background(), p, plan, true); err != nil {
				t.Fatal(err)
			}
			bundleCrash(t, bundleCrashInput{Policy: p, Boundary: boundary, Recover: true})
			recoverCLI(t, p)
			if !bundleSame(before, bundleSnapshot(t, p.Root)) {
				t.Fatal("interrupted recovery did not restore the exact original tree")
			}
		})
	}
}
