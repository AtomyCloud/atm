package coreupdate

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

type discoveryTransport func(*http.Request) (*http.Response, error)

func (f discoveryTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type discoveryFixture struct {
	manifest PackageManifest
	listing  releaseListing
	raw, sig []byte
	archive  []byte
	key      ed25519.PublicKey
	client   *http.Client
	requests []string
	bodies   map[string][]byte
}

func newDiscoveryFixture(t *testing.T, goos, arch string) *discoveryFixture {
	t.Helper()
	f := &discoveryFixture{manifest: packageFixtureManifest(), archive: []byte("fixture package bytes; never extracted")}
	f.manifest.OS, f.manifest.Arch = goos, arch
	stem := "atm-" + f.manifest.Version + "-" + goos + "-" + arch
	base := OfficialSource + "/releases/download/v" + f.manifest.Version + "/" + stem
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	f.manifest.Source = base + ext
	hash := sha256.Sum256(f.archive)
	f.manifest.Archive.Size = int64(len(f.archive))
	f.manifest.Archive.SHA256 = hex.EncodeToString(hash[:])
	f.raw, _ = json.Marshal(f.manifest)
	key, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	f.key = key
	f.sig = ed25519.Sign(private, append([]byte(PackageSignatureDomain), f.raw...))
	no := false
	f.listing = releaseListing{Tag: "v" + f.manifest.Version, Draft: &no, Prerelease: &no, Assets: []releaseAsset{
		{Name: stem + ".manifest.json", URL: base + ".manifest.json", Size: int64(len(f.raw))},
		{Name: stem + ".manifest.sig", URL: base + ".manifest.sig", Size: int64(len(f.sig))},
		{Name: stem + ext, URL: base + ext, Size: int64(len(f.archive))},
	}}
	f.bodies = map[string][]byte{base + ".manifest.json": f.raw, base + ".manifest.sig": f.sig, base + ext: f.archive}
	f.client = newDiscoveryClient(discoveryTransport(func(r *http.Request) (*http.Response, error) {
		f.requests = append(f.requests, r.URL.String())
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Fatalf("unsafe request: %v", r)
		}
		body, found := f.bodies[r.URL.String()]
		if r.URL.String() == officialLatestReleaseAPI {
			body, _ = json.Marshal(f.listing)
			found = true
		}
		if !found {
			t.Fatalf("unexpected network request: %s", r.URL)
		}
		return discoveryOK(r, body), nil
	}))
	return f
}

func discoveryOK(r *http.Request, b []byte) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(b)), ContentLength: int64(len(b)), Request: r}
}

// Positive tests authenticate with ephemeral fixture keys only in test code.
// Production discoverPackage always uses OfficialPublicKey and rejects these.
func (f *discoveryFixture) plan(t *testing.T) *ReleasePlan {
	t.Helper()
	c, err := fetchPackageCandidate(context.Background(), f.manifest.OS, f.manifest.Arch, f.client)
	if err != nil {
		t.Fatal(err)
	}
	m, err := VerifyPackageManifest(c.raw, c.signature, f.key, c.goos, c.arch)
	if err != nil {
		t.Fatal(err)
	}
	p, err := c.pin(context.Background(), m, f.client)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDiscoveryTargetsCopiesAndPinnedDownload(t *testing.T) {
	for _, target := range [][2]string{{"linux", "amd64"}, {"linux", "arm64"}, {"darwin", "amd64"}, {"darwin", "arm64"}, {"windows", "amd64"}} {
		t.Run(target[0]+"-"+target[1], func(t *testing.T) {
			f := newDiscoveryFixture(t, target[0], target[1])
			p := f.plan(t)
			if len(f.requests) != 3 {
				t.Fatal("discovery fetched archive", f.requests)
			}
			info := p.Info()
			if info.Status != "authenticated-candidate" || info.Freshness != "unknown" || info.Fingerprint != OfficialKeyFingerprint || info.Tag != "v"+info.Version {
				t.Fatal(info)
			}
			info.Version = "999.0.0"
			m := p.Manifest()
			m.Source = "https://example.invalid/archive"
			p.Raw()[0] ^= 1
			p.Signature()[0] ^= 1
			if p.Manifest() != f.manifest || !bytes.Equal(p.Raw(), f.raw) || !bytes.Equal(p.Signature(), f.sig) || p.Info().Version != f.manifest.Version {
				t.Fatal("accessor leaked mutable metadata")
			}
			// A later latest-listing mutation cannot retarget a pinned download.
			f.listing.Tag = "v999.0.0"
			path := filepath.Join(tempRoot(t), "archive")
			if err := p.Download(context.Background(), path); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(got, f.archive) || len(f.requests) != 4 || f.requests[3] != f.manifest.Source {
				t.Fatal("download was not pinned", err, f.requests)
			}
			st, _ := os.Stat(path)
			if runtime.GOOS != "windows" && st.Mode().Perm() != 0600 {
				t.Fatal("download permissions", st.Mode())
			}
		})
	}
}

func TestDiscoveryOfficialTrustCannotBeOverridden(t *testing.T) {
	f := newDiscoveryFixture(t, "linux", "amd64")
	t.Setenv("GITHUB_TOKEN", "fixture-secret-never-send")
	t.Setenv("GH_TOKEN", "fixture-secret-never-send")
	t.Setenv("ATM_RELEASE_PUBLIC_KEY", hex.EncodeToString(f.key))
	if p, err := discoverPackage(context.Background(), "linux", "amd64", f.client); err == nil || p != nil || !strings.Contains(err.Error(), "invalid signed package") {
		t.Fatal("fixture signer accepted as production trust", p, err)
	}
	if len(f.requests) != 3 {
		t.Fatal(f.requests)
	}
}

func TestDiscoveryRejectsListingIdentityAndAssets(t *testing.T) {
	yes := true
	for name, mutate := range map[string]func(*releaseListing){
		"draft":              func(r *releaseListing) { r.Draft = &yes },
		"prerelease":         func(r *releaseListing) { r.Prerelease = &yes },
		"missing draft":      func(r *releaseListing) { r.Draft = nil },
		"missing prerelease": func(r *releaseListing) { r.Prerelease = nil },
		"no v":               func(r *releaseListing) { r.Tag = "0.0.3" },
		"leading zero":       func(r *releaseListing) { r.Tag = "v00.0.3" },
		"beta":               func(r *releaseListing) { r.Tag = "v0.0.3-beta" },
		"build":              func(r *releaseListing) { r.Tag = "v0.0.3+build" },
		"traversal":          func(r *releaseListing) { r.Tag = "v../../etc" },
		"missing":            func(r *releaseListing) { r.Assets = r.Assets[:2] },
		"duplicate":          func(r *releaseListing) { r.Assets = append(r.Assets, r.Assets[0]) },
		"wrong target":       func(r *releaseListing) { r.Assets[0].Name = "atm-0.0.3-windows-amd64.manifest.json" },
		"foreign repo": func(r *releaseListing) {
			r.Assets[0].URL = strings.Replace(r.Assets[0].URL, "AtomyCloud", "attacker", 1)
		},
		"http":            func(r *releaseListing) { r.Assets[0].URL = strings.Replace(r.Assets[0].URL, "https:", "http:", 1) },
		"query":           func(r *releaseListing) { r.Assets[0].URL += "?token=secret" },
		"fragment":        func(r *releaseListing) { r.Assets[0].URL += "#secret" },
		"manifest large":  func(r *releaseListing) { r.Assets[0].Size = MaxManifestBytes + 1 },
		"signature large": func(r *releaseListing) { r.Assets[1].Size = 65 },
		"signature short": func(r *releaseListing) { r.Assets[1].Size = 63 },
		"archive large":   func(r *releaseListing) { r.Assets[2].Size = MaxPackageBytes + 1 },
		"archive empty":   func(r *releaseListing) { r.Assets[2].Size = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			f := newDiscoveryFixture(t, "linux", "amd64")
			mutate(&f.listing)
			if _, err := fetchPackageCandidate(context.Background(), "linux", "amd64", f.client); err == nil {
				t.Fatal("invalid listing accepted")
			}
			if len(f.requests) != 1 {
				t.Fatal("download after invalid listing", f.requests)
			}
		})
	}
}

func TestDiscoverySignedCandidateBinding(t *testing.T) {
	for _, field := range []string{"version", "tag", "os", "arch", "source", "size"} {
		t.Run(field, func(t *testing.T) {
			f := newDiscoveryFixture(t, "linux", "amd64")
			c, err := fetchPackageCandidate(context.Background(), "linux", "amd64", f.client)
			if err != nil {
				t.Fatal(err)
			}
			switch field {
			case "version":
				c.version = "0.0.4"
			case "tag":
				c.tag = "v0.0.4"
			case "os":
				c.goos = "windows"
			case "arch":
				c.arch = "arm64"
			case "source":
				c.archive.URL += "?token=bad"
			case "size":
				c.archive.Size++
			}
			if _, err := c.pin(context.Background(), f.manifest, f.client); err == nil {
				t.Fatal("mismatch accepted")
			}
		})
	}
}

func TestDiscoveryMetadataBodyLimits(t *testing.T) {
	for _, source := range []string{"listing", "manifest", "signature"} {
		for _, declared := range []bool{false, true} {
			t.Run(source+"/"+map[bool]string{false: "chunked", true: "declared"}[declared], func(t *testing.T) {
				f := newDiscoveryFixture(t, "linux", "amd64")
				target, limit := officialLatestReleaseAPI, maxReleaseMetadataBytes
				if source == "manifest" {
					target, limit = f.listing.Assets[0].URL, MaxManifestBytes
				}
				if source == "signature" {
					target, limit = f.listing.Assets[1].URL, ed25519.SignatureSize
				}
				original := f.client.Transport
				f.client.Transport = discoveryTransport(func(r *http.Request) (*http.Response, error) {
					if r.URL.String() != target {
						return original.RoundTrip(r)
					}
					resp := discoveryOK(r, bytes.Repeat([]byte("x"), limit+1))
					if !declared {
						resp.ContentLength = -1
					}
					return resp, nil
				})
				if _, err := fetchPackageCandidate(context.Background(), "linux", "amd64", f.client); err == nil {
					t.Fatal("oversized body accepted")
				}
			})
		}
	}
}

func TestDiscoveryRejectsAmbiguousAPIJSON(t *testing.T) {
	for _, body := range []string{
		`{"tag_name":"v0.0.3","tag_name":"v0.0.4","draft":false,"prerelease":false}`,
		`{"tag_name":"v0.0.3","draft":true,"draft":false,"prerelease":false}`,
		`{"tag_name":"v0.0.3","Draft":false,"prerelease":false}`,
		`{"tag_name":"v0.0.3","draft":false,"prerelease":false} {}`,
		`{"tag_name":`, `null`, `[]`,
	} {
		client := newDiscoveryClient(discoveryTransport(func(r *http.Request) (*http.Response, error) {
			if r.URL.String() != officialLatestReleaseAPI {
				t.Fatal("invalid API triggered asset request")
			}
			return discoveryOK(r, []byte(body)), nil
		}))
		if _, err := fetchPackageCandidate(context.Background(), "linux", "amd64", client); err == nil {
			t.Fatal("ambiguous JSON accepted", body)
		}
	}
}

func TestDiscoveryMetadataLengthMismatchAndHTTPFailures(t *testing.T) {
	for _, failure := range []string{"short manifest", "short signature", "base64 signature", "forbidden", "rate limited", "not found", "partial", "encoded"} {
		t.Run(failure, func(t *testing.T) {
			f := newDiscoveryFixture(t, "linux", "amd64")
			original := f.client.Transport
			f.client.Transport = discoveryTransport(func(r *http.Request) (*http.Response, error) {
				resp, err := original.RoundTrip(r)
				if err != nil {
					return nil, err
				}
				switch failure {
				case "short manifest":
					if r.URL.String() == f.listing.Assets[0].URL {
						resp.Body = io.NopCloser(bytes.NewReader(f.raw[:len(f.raw)-1]))
						resp.ContentLength--
					}
				case "short signature":
					if r.URL.String() == f.listing.Assets[1].URL {
						resp.Body = io.NopCloser(bytes.NewReader(f.sig[:63]))
						resp.ContentLength--
					}
				case "base64 signature":
					if r.URL.String() == f.listing.Assets[1].URL {
						resp.Body = io.NopCloser(strings.NewReader(strings.Repeat("A", 88)))
						resp.ContentLength = -1
					}
				case "forbidden":
					resp.StatusCode = 403
				case "rate limited":
					resp.StatusCode = 429
				case "not found":
					resp.StatusCode = 404
				case "partial":
					resp.StatusCode = 206
				case "encoded":
					resp.Header.Set("Content-Encoding", "gzip")
				}
				return resp, nil
			})
			if _, err := fetchPackageCandidate(context.Background(), "linux", "amd64", f.client); err == nil {
				t.Fatal("invalid metadata accepted")
			}
		})
	}
}

func TestDiscoveryRedirectPolicy(t *testing.T) {
	initial := OfficialSource + "/releases/download/v0.0.3/atm-0.0.3-linux-amd64.manifest.json"
	for _, target := range []string{
		"https://release-assets.githubusercontent.com/asset?sig=fixture", "https://objects.githubusercontent.com/asset",
		"https://github-releases.githubusercontent.com/asset", "http://release-assets.githubusercontent.com/asset",
		"https://release-assets.githubusercontent.com.evil.invalid/asset", "https://evil.invalid/asset",
		"https://user:secret@objects.githubusercontent.com/asset", "https://objects.githubusercontent.com:443/asset",
		"https://objects.githubusercontent.com/asset#fragment", "https://github.com/attacker/repo/releases/file",
		"https://raw.githubusercontent.com/AtomyCloud/atm/main/file", "https://objects.githubusercontent.com/%2e%2e/asset",
	} {
		t.Run(target, func(t *testing.T) {
			count := 0
			client := newDiscoveryClient(discoveryTransport(func(r *http.Request) (*http.Response, error) {
				count++
				if count == 1 {
					return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{target}, "Set-Cookie": []string{"token=secret"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
				}
				if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("Referer") != "" {
					t.Fatal("redirect leaked headers")
				}
				return discoveryOK(r, []byte("ok")), nil
			}))
			_, err := fetchDiscoveryBytes(context.Background(), client, initial, 8)
			allowed := target == "https://release-assets.githubusercontent.com/asset?sig=fixture" || target == "https://objects.githubusercontent.com/asset" || target == "https://github-releases.githubusercontent.com/asset"
			if (err == nil) != allowed || (!allowed && count != 1) {
				t.Fatal("redirect policy", count, err)
			}
		})
	}
	first, _ := http.NewRequest(http.MethodGet, officialLatestReleaseAPI, nil)
	next, _ := http.NewRequest(http.MethodGet, initial, nil)
	if discoveryRedirect(next, []*http.Request{first}) == nil {
		t.Fatal("API redirect accepted")
	}
	first.URL, _ = url.Parse(initial)
	if discoveryRedirect(next, []*http.Request{first, first, first, first, first}) == nil {
		t.Fatal("redirect loop accepted")
	}
}

func TestDiscoveryCancellationAndUnsupportedAreOffline(t *testing.T) {
	f := newDiscoveryFixture(t, "linux", "amd64")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := discoverPackage(ctx, "linux", "amd64", f.client); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := discoverPackage(context.Background(), "windows", "arm64", f.client); err == nil {
		t.Fatal("unsupported target accepted")
	}
	if len(f.requests) != 0 {
		t.Fatal("unexpected network", f.requests)
	}
	client := newDiscoveryClient(discoveryTransport(func(r *http.Request) (*http.Response, error) {
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > discoveryTimeout {
			t.Fatal("request without bounded deadline")
		}
		<-r.Context().Done()
		return nil, r.Context().Err()
	}))
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	if _, err := discoverPackage(ctx, "linux", "amd64", client); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}

type cancelDiscoveryBody struct {
	cancel context.CancelFunc
	done   bool
}

func (b *cancelDiscoveryBody) Read(p []byte) (int, error) {
	if b.done {
		return 0, io.EOF
	}
	b.done = true
	b.cancel()
	return copy(p, "partial"), nil
}
func (*cancelDiscoveryBody) Close() error { return nil }

func TestDiscoveryDownloadFailuresCleanUp(t *testing.T) {
	for _, failure := range []string{"short", "oversize", "hash", "http", "encoding", "cancel", "network"} {
		t.Run(failure, func(t *testing.T) {
			f := newDiscoveryFixture(t, "linux", "amd64")
			p := f.plan(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p.client = newDiscoveryClient(discoveryTransport(func(r *http.Request) (*http.Response, error) {
				resp := discoveryOK(r, f.archive)
				switch failure {
				case "short":
					resp.Body = io.NopCloser(bytes.NewReader(f.archive[:4]))
				case "oversize":
					resp.Body = io.NopCloser(bytes.NewReader(append(bytes.Clone(f.archive), 'x')))
					resp.ContentLength = -1
				case "hash":
					resp.Body = io.NopCloser(bytes.NewReader(bytes.Repeat([]byte("x"), len(f.archive))))
				case "http":
					resp.StatusCode = 404
				case "encoding":
					resp.Header.Set("Content-Encoding", "gzip")
				case "cancel":
					resp.Body = &cancelDiscoveryBody{cancel: cancel}
					resp.ContentLength = -1
				case "network":
					return nil, errors.New("fixture network failure")
				}
				return resp, nil
			}))
			path := filepath.Join(tempRoot(t), "archive")
			err := p.Download(ctx, path)
			if err == nil {
				t.Fatal("bad download accepted")
			}
			if failure == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("partial file remains", err)
			}
		})
	}
}

func TestDiscoveryDownloadRefusesExistingAndUnsafePaths(t *testing.T) {
	f := newDiscoveryFixture(t, "linux", "amd64")
	p := f.plan(t)
	dir := tempRoot(t)
	existing := filepath.Join(dir, "existing")
	if err := os.WriteFile(existing, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	paths := []string{existing, dir, "relative", filepath.Join(dir, "missing", "archive"), dir + "/../archive"}
	if runtime.GOOS != "windows" {
		link := filepath.Join(dir, "link")
		if err := os.Symlink(existing, link); err != nil {
			t.Fatal(err)
		}
		parentLink := filepath.Join(dir, "parent-link")
		if err := os.Symlink(dir, parentLink); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, link, filepath.Join(parentLink, "archive"))
	}
	before := append([]string(nil), f.requests...)
	for _, path := range paths {
		if err := p.Download(context.Background(), path); err == nil {
			t.Fatal("unsafe destination accepted", path)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := p.Download(ctx, filepath.Join(dir, "canceled")); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := (&ReleasePlan{}).Download(context.Background(), filepath.Join(dir, "zero")); err == nil {
		t.Fatal("zero plan accepted")
	}
	if !reflect.DeepEqual(before, f.requests) {
		t.Fatal("network before destination validation")
	}
	got, _ := os.ReadFile(existing)
	if string(got) != "preserve" {
		t.Fatal("existing file changed")
	}
}
