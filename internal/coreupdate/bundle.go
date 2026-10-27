package coreupdate

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/AtomyCloud/atm/internal/install"
)

const (
	BundleManifestName      = ".atm-package-manifest.json"
	BundleSignatureName     = ".atm-package-signature"
	BundleTransactionSuffix = ".atm-package-update"
)

// ErrBundleUnenrolled means both signed receipt paths are absent. A partial or
// damaged receipt, or a missing signed payload, is not an unenrolled install.
var ErrBundleUnenrolled = errors.New("installed package has no signed baseline")

// BundlePolicy is supplied by trusted application code. CLI callers must use
// OfficialPublicKey, never a key from a downloaded package or command flag.
type BundlePolicy struct {
	Root      string
	PublicKey ed25519.PublicKey
	OS, Arch  string
	DryRun    bool
}

// BundleInfo is a serializable preview or verified installed identity.
type BundleInfo struct {
	Root           string          `json:"root"`
	State          string          `json:"state"`
	Manifest       PackageManifest `json:"manifest"`
	Baseline       PackageManifest `json:"baseline"`
	CurrentVersion string          `json:"current_version"`
	TargetVersion  string          `json:"target_version"`
	Files          int             `json:"files"`
	PreservedFiles int             `json:"preserved_files"`
	Transaction    string          `json:"transaction"`
}

type BundleResult struct {
	State   string     `json:"state"`
	Root    string     `json:"root"`
	Backup  string     `json:"backup,omitempty"`
	Changed bool       `json:"changed"`
	Info    BundleInfo `json:"info"`
}

// BundlePlan is opaque: callers cannot change the signed target or the pinned
// installed snapshot after displaying consent. Apply rechecks both before writes.
type BundlePlan struct {
	p                        BundlePolicy
	info                     BundleInfo
	raw, sig, oldRaw, oldSig []byte
	archive                  string
	files                    map[string]install.File
	before                   bundleTree
	enroll                   bool
	checkpoint               func(string) error // failure/crash injection in package tests only
}

func (p *BundlePlan) Info() BundleInfo {
	if p == nil {
		return BundleInfo{}
	}
	return p.info
}

func bundlePolicy(p BundlePolicy) error {
	if len(p.PublicKey) != ed25519.PublicKeySize {
		return ErrNoTrustAnchor
	}
	if !filepath.IsAbs(p.Root) || filepath.Clean(p.Root) != p.Root || filepath.Dir(p.Root) == p.Root || !supported(p.OS, p.Arch) {
		return fmt.Errorf("invalid full-package root or platform")
	}
	return nil
}

// PrepareBundle is offline and read-only, including when DryRun is false.
func PrepareBundle(ctx context.Context, p BundlePolicy, manifest, sig []byte, archive string) (*BundlePlan, error) {
	return prepareBundle(ctx, p, manifest, sig, archive, false)
}

// PrepareEnrollment is the read-only, pinned preview for baseline consent.
func PrepareEnrollment(ctx context.Context, p BundlePolicy, manifest, sig []byte, archive string) (*BundlePlan, error) {
	return prepareBundle(ctx, p, manifest, sig, archive, true)
}

// InspectBundle authenticates the installed receipt and all signed owned file
// hashes. It does not adopt unsigned installs or execute installed code.
func InspectBundle(ctx context.Context, p BundlePolicy) (BundleInfo, error) {
	var info BundleInfo
	if err := bundlePolicy(p); err != nil {
		return info, err
	}
	if err := BundleStartupGuard(p.Root); err != nil {
		return info, err
	}
	r, err := openRoot(p.Root)
	if err != nil {
		return info, err
	}
	defer r.Close()
	state, err := bundleInstalled(ctx, r, p, nil, nil)
	if err != nil {
		return info, err
	}
	return BundleInfo{Root: p.Root, State: "installed", Manifest: state.manifest, Baseline: state.manifest, CurrentVersion: state.manifest.Version, TargetVersion: state.manifest.Version, Files: len(state.owned), PreservedFiles: len(state.tree.Files) - len(state.owned) - 2, Transaction: p.Root + BundleTransactionSuffix}, nil
}

// EnrollBundle requires a signed package matching the complete installed owned
// payload. It preserves unlisted data; even receipt registration is recoverable.
func EnrollBundle(ctx context.Context, p BundlePolicy, manifest, sig []byte, archive string, consent bool) (BundleResult, error) {
	plan, err := prepareBundle(ctx, p, manifest, sig, archive, true)
	if err != nil {
		return BundleResult{}, err
	}
	return ApplyBundle(ctx, p, plan, consent)
}

// ApplyBundle publishes with two directory renames, not a globally atomic swap.
// The sibling journal guards the gap; recovery requires an external trusted copy
// when the installed root is absent. No installed executable is launched.
func ApplyBundle(ctx context.Context, p BundlePolicy, plan *BundlePlan, consent bool) (BundleResult, error) {
	return applyBundle(ctx, p, plan, consent)
}

// RecoverBundle explicitly restores this transaction's verified old tree. It
// retains the displaced candidate, including post-update edits, for inspection.
func RecoverBundle(ctx context.Context, p BundlePolicy, consent bool) (BundleResult, error) {
	return recoverBundle(ctx, p, consent, nil)
}
