package coreupdate

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"strings"
)

// Approved by the maintainer on 2026-10-27. The SSH comment is not an identity
// check; the exact public key bytes below are the trust anchor. No runtime override.
const OfficialSSHPublicKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIE19PySbE0RCjKYuFUllXU4meRvy08V0uPaf/rReN6qM atomy@atomy.cloud"

const OfficialKeyFingerprint = "SHA256:jNhv8Ehni4ENGvQaar4/l8vQQPHcVZDnCmcmTvkFXVM"

type TrustIdentity struct {
	Algorithm          string `json:"algorithm"`
	Fingerprint        string `json:"fingerprint"`
	PublicKey          string `json:"public_key"`
	Scope              string `json:"scope"`
	ReplacementEnabled bool   `json:"replacement_enabled"`
}

// OfficialPublicKey returns a fresh raw Ed25519 key, not an OpenSSH key blob.
func OfficialPublicKey() (ed25519.PublicKey, error) {
	key, fingerprint, err := parseSSHPublicKey(OfficialSSHPublicKey)
	if err != nil || fingerprint != OfficialKeyFingerprint {
		return nil, fmt.Errorf("invalid embedded release trust identity")
	}
	return key, nil
}

func OfficialTrust() (TrustIdentity, error) {
	if _, err := OfficialPublicKey(); err != nil {
		return TrustIdentity{}, err
	}
	return TrustIdentity{"Ed25519", OfficialKeyFingerprint, OfficialSSHPublicKey, PackageScope, ProductionEnabled}, nil
}

func parseSSHPublicKey(text string) (ed25519.PublicKey, string, error) {
	if len(text) > 1024 || strings.ContainsAny(text, "\r\n") {
		return nil, "", fmt.Errorf("invalid SSH public key line")
	}
	fields := strings.Fields(text)
	if len(fields) < 2 || fields[0] != "ssh-ed25519" {
		return nil, "", fmt.Errorf("expected an SSH Ed25519 public key")
	}
	blob, err := base64.StdEncoding.Strict().DecodeString(fields[1])
	if err != nil || base64.StdEncoding.EncodeToString(blob) != fields[1] {
		return nil, "", fmt.Errorf("invalid SSH public key encoding")
	}
	remaining := blob
	field := func() ([]byte, bool) {
		if len(remaining) < 4 {
			return nil, false
		}
		n := uint64(binary.BigEndian.Uint32(remaining[:4]))
		remaining = remaining[4:]
		if n > uint64(len(remaining)) {
			return nil, false
		}
		value := remaining[:int(n)]
		remaining = remaining[int(n):]
		return value, true
	}
	algorithm, ok := field()
	if !ok || string(algorithm) != "ssh-ed25519" {
		return nil, "", fmt.Errorf("SSH key algorithm mismatch")
	}
	key, ok := field()
	if !ok || len(key) != ed25519.PublicKeySize || len(remaining) != 0 {
		return nil, "", fmt.Errorf("invalid SSH Ed25519 key payload")
	}
	hash := sha256.Sum256(blob)
	return append(ed25519.PublicKey(nil), key...), "SHA256:" + base64.RawStdEncoding.EncodeToString(hash[:]), nil
}
