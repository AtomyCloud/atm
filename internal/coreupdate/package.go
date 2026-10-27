package coreupdate

import (
	"crypto/ed25519"
	"fmt"

	"github.com/AtomyCloud/atm/internal/install"
)

const (
	PackageScope                 = "compiled-core-package-v1"
	PackageSignatureDomain       = "ATM compiled CORE package manifest v1\x00"
	MaxPackageBytes        int64 = 256 << 20
)

// PackageManifest authenticates the whole archive and its inner payload index.
// It is deliberately incompatible with the earlier single-binary experiment.
type PackageManifest struct {
	Schema        int          `json:"schema"`
	Scope         string       `json:"scope"`
	Source        string       `json:"source"`
	Version       string       `json:"version"`
	Commit        string       `json:"commit"`
	OS            string       `json:"os"`
	Arch          string       `json:"arch"`
	Migration     bool         `json:"migration"`
	Archive       install.File `json:"archive"`
	ReleaseSHA256 string       `json:"release_sha256"`
}

// Validate checks structure and the fixed official source, not authenticity.
func (m PackageManifest) Validate() error {
	if m.Schema != 1 || m.Scope != PackageScope || !m.Migration ||
		!supported(m.OS, m.Arch) || !versionPattern.MatchString(m.Version) || !commitPattern.MatchString(m.Commit) {
		return fmt.Errorf("invalid full-package release identity")
	}
	name := "atm-" + m.Version + "-" + m.OS + "-" + m.Arch + ".tar.gz"
	if m.OS == "windows" {
		name = "atm-" + m.Version + "-" + m.OS + "-" + m.Arch + ".zip"
	}
	if m.Source != OfficialSource+"/releases/download/v"+m.Version+"/"+name ||
		!digest(m.Archive.SHA256) || m.Archive.Size <= 0 || m.Archive.Size > MaxPackageBytes ||
		m.Archive.Mode != 0644 || !digest(m.ReleaseSHA256) {
		return fmt.Errorf("invalid full-package source, size or hashes")
	}
	return nil
}

// VerifyPackageManifest verifies exact signed bytes. It makes no freshness or
// installed-baseline claim and does not enable replacement. Key is supplied by
// trusted code only; the CLI must always use OfficialPublicKey.
func VerifyPackageManifest(raw, signature []byte, key ed25519.PublicKey, goos, arch string) (PackageManifest, error) {
	var m PackageManifest
	if len(key) != ed25519.PublicKeySize {
		return m, ErrNoTrustAnchor
	}
	if len(raw) == 0 || len(raw) > MaxManifestBytes || len(signature) != ed25519.SignatureSize ||
		!ed25519.Verify(key, append([]byte(PackageSignatureDomain), raw...), signature) {
		return m, fmt.Errorf("invalid signed package manifest")
	}
	if err := strictJSON(raw, &m); err != nil {
		return PackageManifest{}, err
	}
	if err := m.Validate(); err != nil {
		return PackageManifest{}, err
	}
	if m.OS != goos || m.Arch != arch {
		return PackageManifest{}, fmt.Errorf("package target mismatch")
	}
	return m, nil
}
