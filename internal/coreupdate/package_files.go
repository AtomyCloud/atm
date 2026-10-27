package coreupdate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
)

// Capability does not imply enrollment, a newer release or target-host acceptance.
const ProductionStatus = "enabled: signed full-package enrollment/update/recovery require explicit consent; Windows requires an external trusted executable"

// The maintainer approved code activation after validating the signed Linux
// release on 2026-10-27. No runtime key/gate override or automatic installation.
const ProductionEnabled = true

var ErrProductionDisabled = errors.New("full-package production mutation is disabled pending explicit release/cutover authorization")

type VerifiedPackage struct {
	Manifest           PackageManifest `json:"manifest"`
	Fingerprint        string          `json:"fingerprint"`
	Files              int             `json:"verified_files"`
	ReplacementEnabled bool            `json:"replacement_enabled"`
}

// VerifyOfficialPackageFiles is offline and read-only. No key override, external
// program, extraction, installation or acceptance of unsigned packages occurs.
func VerifyOfficialPackageFiles(ctx context.Context, manifest, signature, archive, goos, arch string) (VerifiedPackage, error) {
	var result VerifiedPackage
	if err := ctx.Err(); err != nil {
		return result, err
	}
	key, err := OfficialPublicKey()
	if err != nil {
		return result, err
	}
	raw, err := readVerificationFile(manifest, MaxManifestBytes)
	if err != nil {
		return result, err
	}
	sig, err := readVerificationFile(signature, 64)
	if err != nil {
		return result, err
	}
	m, err := VerifyPackageManifest(raw, sig, key, goos, arch)
	if err != nil {
		return result, err
	}
	files, err := InspectPackage(ctx, archive, m)
	if err != nil {
		return result, err
	}
	return VerifiedPackage{Manifest: m, Fingerprint: OfficialKeyFingerprint, Files: len(files), ReplacementEnabled: ProductionEnabled}, nil
}

func readVerificationFile(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > limit {
		return nil, fmt.Errorf("verification input must be a bounded regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, opened) {
		return nil, fmt.Errorf("verification input changed while opening")
	}
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("verification input exceeds limit")
	}
	return b, nil
}
