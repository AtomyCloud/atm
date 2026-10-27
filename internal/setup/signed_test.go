package setup

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/AtomyCloud/atm/internal/coreupdate"
	"github.com/AtomyCloud/atm/internal/install"
	"github.com/AtomyCloud/atm/internal/version"
)

func TestSignedInstalledPATHIntegrity(t *testing.T) {
	o := fixture(t)
	editManifest(t, &o, func(m *release) { m.Commit = strings.Repeat("a", 40) })
	pub, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ext := ".tar.gz"
	if runtime.GOOS == "windows" {
		ext = ".zip"
	}
	m := coreupdate.PackageManifest{Schema: 1, Scope: coreupdate.PackageScope, Version: version.Version, Commit: strings.Repeat("a", 40), OS: runtime.GOOS, Arch: runtime.GOARCH, Migration: true, Archive: install.File{SHA256: strings.Repeat("b", 64), Size: 100, Mode: 0644}, ReleaseSHA256: o.TrustedManifestSHA256}
	m.Source = coreupdate.OfficialSource + "/releases/download/v" + m.Version + "/atm-" + m.Version + "-" + m.OS + "-" + m.Arch + ext
	raw, _ := json.Marshal(m)
	put(t, filepath.Join(o.Source, coreupdate.BundleManifestName), raw, 0600)
	put(t, filepath.Join(o.Source, coreupdate.BundleSignatureName), ed25519.Sign(private, append([]byte(coreupdate.PackageSignatureDomain), raw...)), 0600)
	// Signed CORE integrity must not depend on the obsolete original setup pin,
	// user data or independently maintained application-plugin code.
	put(t, filepath.Join(o.Source, receiptName), []byte(`{"schema":1,"manifest_sha256":"old"}`), 0600)
	put(t, filepath.Join(o.Source, "settings", "personal.json"), []byte("keep me"), 0600)
	put(t, filepath.Join(o.Source, "plugins", "fixture", "plugin.sh"), []byte("new independent plugin"), 0644)
	before := tree(t, o.Source)
	if _, err := verifyInstalledWithKey(context.Background(), o.Source, "", pub); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, tree(t, o.Source)) {
		t.Fatal("verification wrote files")
	}
	if _, err := verifyInstalled(context.Background(), o.Source, ""); err == nil {
		t.Fatal("fixture key accepted as production identity")
	}
	if _, err := verifyInstalledWithKey(context.Background(), o.Source, strings.Repeat("c", 64), pub); err == nil {
		t.Fatal("conflicting explicit pin accepted")
	}
	put(t, filepath.Join(o.Source, "LICENSE"), []byte("changed core"), 0644)
	if _, err := verifyInstalledWithKey(context.Background(), o.Source, "", pub); err == nil {
		t.Fatal("modified CORE accepted")
	}
	if err := os.Remove(filepath.Join(o.Source, coreupdate.BundleManifestName)); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyInstalledWithKey(context.Background(), o.Source, "", pub); err == nil || !strings.Contains(err.Error(), "orphaned or unreadable signed package receipt") {
		t.Fatal("orphaned signature accepted")
	}
}
