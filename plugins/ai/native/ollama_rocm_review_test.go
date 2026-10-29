//go:build linux

package main

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func reviewROCmRelease(t *testing.T, f *fixture, version string) (artifact, artifact) {
	t.Helper()
	base := ollamaFixtureRelease(t, f, version)
	body := ollamaFixtureArchive(t, []ollamaTarItem{
		{"lib/ollama/rocm_v7/", "", tar.TypeDir, 0755, nil},
		{"lib/ollama/rocm_v7/libhip.so.7", "", tar.TypeReg, 0644, []byte("synthetic ROCm " + version)},
		{"lib/ollama/rocm_v7/libhip.so", "libhip.so.7", tar.TypeSymlink, 0777, nil},
	})
	companion := artifact{Provider: "ollama", Version: version, Arch: "amd64",
		URL:    "https://github.com/ollama/ollama/releases/download/v" + version + "/" + ollamaROCmName,
		SHA256: fmt.Sprintf("%x", sha256.Sum256(body)), Format: "tar.zst", Entry: "lib/ollama", Size: int64(len(body))}
	var r release
	if err := json.Unmarshal(f.responses[f.latest], &r); err != nil {
		t.Fatal(err)
	}
	r.Assets = append(r.Assets, releaseAsset{Name: ollamaROCmName, URL: companion.URL, Digest: "sha256:" + companion.SHA256, Size: companion.Size})
	encoded, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	f.responses[f.latest] = encoded
	f.responses["https://api.github.com/repos/ollama/ollama/releases/tags/v"+version] = encoded
	f.responses[companion.URL] = body
	return base, companion
}

func TestReviewROCmPairRejectsMixedReleaseEvidence(t *testing.T) {
	for _, kind := range []string{"valid", "missing-base-digest", "missing-companion-digest", "duplicate-companion", "other-version", "other-origin", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t, "amd64")
			base, companion := reviewROCmRelease(t, f, "1.2.3")
			var r release
			if err := json.Unmarshal(f.responses[f.latest], &r); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "missing-base-digest":
				r.Assets[0].Digest = ""
			case "missing-companion-digest":
				r.Assets[1].Digest = ""
			case "duplicate-companion":
				r.Assets = append(r.Assets, r.Assets[1])
			case "other-version":
				r.Assets[1].URL = strings.Replace(r.Assets[1].URL, "/v1.2.3/", "/v1.2.4/", 1)
			case "other-origin":
				r.Assets[1].URL = strings.Replace(r.Assets[1].URL, "github.com", "example.invalid", 1)
			case "oversized":
				r.Assets[1].Size = ollamaMaxDownload + 1
			}
			encoded, err := json.Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			f.responses[f.latest] = encoded
			a, c, err := f.b.resolveOllama(context.Background(), "latest", true)
			if kind == "valid" {
				if err != nil || a != base || c == nil || *c != companion {
					t.Fatalf("wrong paired artifacts: %+v %+v %v", a, c, err)
				}
			} else if err == nil {
				t.Fatal("invalid pair accepted")
			}
			if f.calls != 1 {
				t.Fatalf("expected one pinned metadata response, got %d", f.calls)
			}
			if _, err := os.Lstat(f.b.req.AppsDir); !os.IsNotExist(err) {
				t.Fatalf("metadata resolution created Apps: %v", err)
			}
		})
	}
}

func TestReviewROCmFailedUpdateKeepsActiveBase(t *testing.T) {
	f := newFixture(t, "amd64")
	ollamaFixtureRelease(t, f, "1.2.3")
	mustRun(t, f, "install", "--provider", "ollama", "--version", "1.2.3", "--trust-download")
	root := f.b.ollamaVersion("1.2.3")
	before := hermesFixtureTreeSnapshot(t, root)
	_, companion := reviewROCmRelease(t, f, "1.2.4")
	f.responses[companion.URL][0] ^= 1
	err := f.run(context.Background(), "update", "--provider", "ollama", "--rocm", "--trust-download")
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("wanted companion checksum refusal, got %v", err)
	}
	hermesFixtureTreeUnchanged(t, root, before)
	versions, active, err := f.b.ollamaInspect(context.Background())
	if err != nil || active != "1.2.3" || len(versions) != 1 {
		t.Fatalf("failed pair changed active version: %v %s %v", versions, active, err)
	}
}

func TestReviewROCmStageReceiptCannotDropCompanion(t *testing.T) {
	f := newFixture(t, "amd64")
	reviewROCmRelease(t, f, "1.2.3")
	f.b.checkpoint = func(point string) error {
		if point != "ollama-verified" {
			return nil
		}
		matches, err := filepath.Glob(filepath.Join(filepath.Dir(f.b.root("ollama")), ".atm-ollama-stage-*", "version"))
		if err != nil || len(matches) != 1 {
			t.Fatalf("unexpected staged versions: %v %v", matches, err)
		}
		rec, err := ollamaReadReceipt(matches[0])
		if err != nil {
			return err
		}
		rec.ROCm, rec.Schema = nil, 1
		data, err := json.Marshal(rec)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(matches[0], "receipt.json"), data, 0600)
	}
	if err := f.run(context.Background(), "install", "--provider", "ollama", "--rocm", "--trust-download"); err == nil {
		t.Fatal("coherently changed staged receipt was published")
	}
	if _, err := os.Lstat(f.b.ollamaVersion("1.2.3")); !os.IsNotExist(err) {
		t.Fatalf("changed stage published: %v", err)
	}
}

// Public metadata only; this gate never downloads or executes either archive.
func TestOfficialOllamaROCmMetadata(t *testing.T) {
	if os.Getenv("ATM_TEST_AI_PUBLIC_ROCM") != "1" {
		t.Skip("set ATM_TEST_AI_PUBLIC_ROCM=1 for public paired metadata verification")
	}
	f := newFixture(t, "amd64")
	b, err := newBackend(f.b.req, func(s string) { t.Log(s) })
	if err != nil {
		t.Fatal(err)
	}
	b.arch = "amd64"
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	a, c, err := b.resolveOllama(ctx, "latest", true)
	if err != nil || c == nil {
		t.Fatalf("official paired metadata: %v", err)
	}
	if a.Version != c.Version || a.Arch != c.Arch {
		t.Fatal("mixed release pair")
	}
	if _, err := os.Lstat(b.req.AppsDir); !os.IsNotExist(err) {
		t.Fatalf("metadata created Apps: %v", err)
	}
	t.Logf("official %s amd64: base %d bytes, ROCm %d bytes; both digests verified as metadata only", a.Version, a.Size, c.Size)
}
