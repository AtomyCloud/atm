package coreupdate

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Optional artifact acceptance: verifies real maintainer signatures without
// opening an agent, executing a package, enrolling it or enabling production.
func TestSignedLinuxReleaseArtifacts(t *testing.T) {
	dir := os.Getenv("ATM_TEST_SIGNED_RELEASE_DIR")
	if dir == "" {
		t.Skip("set ATM_TEST_SIGNED_RELEASE_DIR to reviewed Linux 0.0.2 artifacts")
	}
	for _, arch := range []string{"amd64", "arm64"} {
		t.Run(arch, func(t *testing.T) {
			stem := "atm-0.0.2-linux-" + arch
			meta := filepath.Join(dir, "signatures-linux-"+arch)
			r, err := VerifyOfficialPackageFiles(context.Background(), filepath.Join(meta, stem+".manifest.json"), filepath.Join(meta, stem+".manifest.sig"), filepath.Join(dir, stem+".tar.gz"), "linux", arch)
			if err != nil {
				t.Fatal(err)
			}
			if r.Fingerprint != OfficialKeyFingerprint || r.Manifest.Version != "0.0.2" || r.Files == 0 || r.ReplacementEnabled || ProductionEnabled {
				t.Fatal("invalid acceptance result", r)
			}
			t.Logf("verified %s, commit=%s, files=%d, fingerprint=%s; production remains disabled", arch, r.Manifest.Commit, r.Files, r.Fingerprint)
		})
	}
}
