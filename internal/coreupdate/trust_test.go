package coreupdate

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

func TestApprovedTrustIdentity(t *testing.T) {
	key, fingerprint, err := parseSSHPublicKey(OfficialSSHPublicKey)
	if err != nil || fingerprint != OfficialKeyFingerprint || len(key) != 32 {
		t.Fatal(key, fingerprint, err)
	}
	first, err := OfficialPublicKey()
	if err != nil || !bytes.Equal(first, key) {
		t.Fatal(err)
	}
	first[0] ^= 0xff
	second, _ := OfficialPublicKey()
	if !bytes.Equal(second, key) {
		t.Fatal("caller changed trusted key")
	}
	identity, err := OfficialTrust()
	if err != nil || identity.Fingerprint != fingerprint || identity.ReplacementEnabled || identity.Scope != PackageScope {
		t.Fatal(identity, err)
	}
	_, fp, err := parseSSHPublicKey(strings.ReplaceAll(OfficialSSHPublicKey, "atomy@atomy.cloud", "different comment"))
	if err != nil || fp != fingerprint {
		t.Fatal("comment must not affect identity", err)
	}
}

func TestMalformedSSHTrustRejected(t *testing.T) {
	fields := strings.Fields(OfficialSSHPublicKey)
	blob, _ := base64.StdEncoding.DecodeString(fields[1])
	for _, line := range []string{"", "ssh-rsa " + fields[1], "ssh-ed25519 !", OfficialSSHPublicKey + "\nother", strings.Repeat("x", 1025),
		"ssh-ed25519 " + base64.StdEncoding.EncodeToString(append(blob, 0)),
		"ssh-ed25519 " + base64.StdEncoding.EncodeToString(blob[:len(blob)-1]),
		"ssh-ed25519 " + base64.StdEncoding.EncodeToString([]byte{255, 255, 255, 255}),
		"ssh-ed25519 " + base64.StdEncoding.EncodeToString(bytes.Replace(blob, []byte("ssh-ed25519"), []byte("ssh-invalid"), 1)),
	} {
		if _, _, err := parseSSHPublicKey(line); err == nil {
			t.Fatalf("accepted %q", line)
		}
	}
}
