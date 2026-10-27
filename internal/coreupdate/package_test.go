package coreupdate

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AtomyCloud/atm/internal/install"
)

func packageFixtureManifest() PackageManifest {
	return PackageManifest{Schema: 1, Scope: PackageScope, Source: OfficialSource + "/releases/download/v0.0.3/atm-0.0.3-linux-amd64.tar.gz", Version: "0.0.3", Commit: strings.Repeat("a", 40), OS: "linux", Arch: "amd64", Migration: true, Archive: install.File{SHA256: strings.Repeat("b", 64), Size: 1024, Mode: 0644}, ReleaseSHA256: strings.Repeat("c", 64)}
}

func TestPackageSignatureDomainAndIdentity(t *testing.T) {
	pub, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	m := packageFixtureManifest()
	raw, _ := json.Marshal(m)
	sign := func(b []byte) []byte { return ed25519.Sign(private, append([]byte(PackageSignatureDomain), b...)) }
	sig := sign(raw)
	if got, err := VerifyPackageManifest(raw, sig, pub, "linux", "amd64"); err != nil || got != m {
		t.Fatal(got, err)
	}
	other, _, _ := ed25519.GenerateKey(rand.Reader)
	for _, tc := range []struct {
		raw, sig []byte
		key      ed25519.PublicKey
		os, arch string
	}{
		{raw, sig, other, "linux", "amd64"}, {raw, sig, nil, "linux", "amd64"},
		{append(append([]byte{}, raw...), '\n'), sig, pub, "linux", "amd64"},
		{raw, sig, pub, "darwin", "amd64"}, {raw, sig, pub, "linux", "arm64"},
		{raw, ed25519.Sign(private, append([]byte(SignatureDomain), raw...)), pub, "linux", "amd64"},
		{raw, []byte("-----BEGIN SSH SIGNATURE-----"), pub, "linux", "amd64"},
	} {
		if _, err := VerifyPackageManifest(tc.raw, tc.sig, tc.key, tc.os, tc.arch); err == nil {
			t.Fatal("accepted wrong signature, domain or target")
		}
	}
	duplicate := []byte(strings.Replace(string(raw), `"schema":1`, `"schema":1,"schema":1`, 1))
	if _, err := VerifyPackageManifest(duplicate, sign(duplicate), pub, "linux", "amd64"); err == nil {
		t.Fatal("duplicate field accepted")
	}
	for _, mutate := range []func(*PackageManifest){
		func(m *PackageManifest) { m.Scope = Scope }, func(m *PackageManifest) { m.Version = "0.0.3-beta" },
		func(m *PackageManifest) { m.Source += "?key=abc" }, func(m *PackageManifest) { m.Commit += "-dirty" },
		func(m *PackageManifest) { m.Archive.Size = MaxPackageBytes + 1 }, func(m *PackageManifest) { m.Archive.Mode = 0755 },
		func(m *PackageManifest) { m.ReleaseSHA256 = "" }, func(m *PackageManifest) { m.Migration = false },
	} {
		bad := m
		mutate(&bad)
		b, _ := json.Marshal(bad)
		if _, err := VerifyPackageManifest(b, sign(b), pub, "linux", "amd64"); err == nil {
			t.Fatal("accepted invalid manifest", bad)
		}
	}
}

func TestOfficialVerificationRejectsFixtureSignerBeforeArchiveRead(t *testing.T) {
	dir := t.TempDir()
	m := packageFixtureManifest()
	raw, _ := json.Marshal(m)
	_, private, _ := ed25519.GenerateKey(rand.Reader)
	manifest, signature := filepath.Join(dir, "manifest.json"), filepath.Join(dir, "manifest.sig")
	if err := os.WriteFile(manifest, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(signature, ed25519.Sign(private, append([]byte(PackageSignatureDomain), raw...)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyOfficialPackageFiles(context.Background(), manifest, signature, filepath.Join(dir, "missing.tar.gz"), "linux", "amd64"); err == nil || !strings.Contains(err.Error(), "invalid signed") {
		t.Fatal(err)
	}
	if _, err := readVerificationFile(manifest, 1); err == nil {
		t.Fatal("oversize accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := VerifyOfficialPackageFiles(ctx, manifest, signature, "", "linux", "amd64"); err != context.Canceled {
		t.Fatal(err)
	}
}
