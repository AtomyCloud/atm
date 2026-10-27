package coreupdate

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	officialLatestReleaseAPI = "https://api.github.com/repos/AtomyCloud/atm/releases/latest"
	maxReleaseMetadataBytes  = 1 << 20
	discoveryTimeout         = 45 * time.Second
	packageDownloadTimeout   = 10 * time.Minute
)

// ReleaseInfo describes an authenticated candidate, not proof that it is the
// newest release. Freshness and installed-version policy belong to the caller.
type ReleaseInfo struct {
	Status      string `json:"status"`
	Freshness   string `json:"freshness"`
	Tag         string `json:"tag"`
	Version     string `json:"version"`
	Commit      string `json:"commit"`
	OS          string `json:"os"`
	Arch        string `json:"arch"`
	Source      string `json:"source"`
	Fingerprint string `json:"fingerprint"`
}

// ReleasePlan pins authenticated metadata. It neither extracts nor installs.
// Its zero value cannot download. Accessors return independent values/copies.
type ReleasePlan struct {
	info      ReleaseInfo
	manifest  PackageManifest
	raw       []byte
	signature []byte
	client    *http.Client
}

func (p *ReleasePlan) Info() ReleaseInfo         { return p.info }
func (p *ReleasePlan) Manifest() PackageManifest { return p.manifest }
func (p *ReleasePlan) Raw() []byte               { return bytes.Clone(p.raw) }
func (p *ReleasePlan) Signature() []byte         { return bytes.Clone(p.signature) }

// DiscoverPackage is read-only and needs no credentials. GitHub's unauthenticated
// latest endpoint selects a candidate; only the embedded release key establishes
// authenticity. No environment token, cookies, key override or custom URL is used.
func DiscoverPackage(ctx context.Context, goos, arch string) (*ReleasePlan, error) {
	client := newDiscoveryClient(nil)
	defer client.CloseIdleConnections()
	return discoverPackage(ctx, goos, arch, client)
}

func discoverPackage(ctx context.Context, goos, arch string, client *http.Client) (*ReleasePlan, error) {
	ctx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()
	candidate, err := fetchPackageCandidate(ctx, goos, arch, client)
	if err != nil {
		return nil, err
	}
	key, err := OfficialPublicKey()
	if err != nil {
		return nil, err
	}
	m, err := VerifyPackageManifest(candidate.raw, candidate.signature, key, goos, arch)
	if err != nil {
		return nil, err
	}
	return candidate.pin(ctx, m, client)
}

type releaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

type releaseListing struct {
	Tag        string         `json:"tag_name"`
	Draft      *bool          `json:"draft"`
	Prerelease *bool          `json:"prerelease"`
	Assets     []releaseAsset `json:"assets"`
}

type packageCandidate struct {
	tag, version, goos, arch string
	archive                  releaseAsset
	raw, signature           []byte
}

func fetchPackageCandidate(ctx context.Context, goos, arch string, client *http.Client) (packageCandidate, error) {
	var c packageCandidate
	if err := ctx.Err(); err != nil {
		return c, err
	}
	if !supported(goos, arch) {
		return c, fmt.Errorf("unsupported package discovery target")
	}
	raw, err := fetchDiscoveryBytes(ctx, client, officialLatestReleaseAPI, maxReleaseMetadataBytes)
	if err != nil {
		return c, err
	}
	// GitHub adds fields to its API. Allow unknown fields, but reject duplicate
	// keys/case aliases before decoding the fields used for candidate selection.
	var envelope map[string]json.RawMessage
	if err := strictJSON(raw, &envelope); err != nil {
		return c, fmt.Errorf("invalid release listing: %w", err)
	}
	var listing releaseListing
	if err := json.Unmarshal(raw, &listing); err != nil {
		return c, fmt.Errorf("invalid release listing: %w", err)
	}
	version := strings.TrimPrefix(listing.Tag, "v")
	if listing.Tag != "v"+version || !versionPattern.MatchString(version) ||
		listing.Draft == nil || *listing.Draft || listing.Prerelease == nil || *listing.Prerelease {
		return c, fmt.Errorf("release listing is not a canonical stable release")
	}
	stem := "atm-" + version + "-" + goos + "-" + arch
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	names := []string{stem + ".manifest.json", stem + ".manifest.sig", stem + ext}
	assets := make([]releaseAsset, len(names))
	for i, name := range names {
		count := 0
		for _, asset := range listing.Assets {
			if asset.Name == name {
				count++
				assets[i] = asset
			}
		}
		if count != 1 || assets[i].URL != OfficialSource+"/releases/download/"+listing.Tag+"/"+name {
			return c, fmt.Errorf("missing, duplicate or unofficial release asset: %s", name)
		}
	}
	if assets[0].Size <= 0 || assets[0].Size > MaxManifestBytes || assets[1].Size != ed25519.SignatureSize ||
		assets[2].Size <= 0 || assets[2].Size > MaxPackageBytes {
		return c, fmt.Errorf("release asset size outside limits")
	}
	c = packageCandidate{tag: listing.Tag, version: version, goos: goos, arch: arch, archive: assets[2]}
	c.raw, err = fetchDiscoveryBytes(ctx, client, assets[0].URL, MaxManifestBytes)
	if err == nil {
		c.signature, err = fetchDiscoveryBytes(ctx, client, assets[1].URL, ed25519.SignatureSize)
	}
	if err != nil {
		return packageCandidate{}, err
	}
	if int64(len(c.raw)) != assets[0].Size || int64(len(c.signature)) != assets[1].Size {
		return packageCandidate{}, fmt.Errorf("release metadata size mismatch")
	}
	return c, nil
}

// pin accepts only a previously verified manifest; there is no runtime verifier
// or trust-key injection. Kept separate so fixture-key tests can exercise binding.
func (c packageCandidate) pin(ctx context.Context, m PackageManifest, client *http.Client) (*ReleasePlan, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	if m.Version != c.version || c.tag != "v"+m.Version || m.OS != c.goos || m.Arch != c.arch ||
		m.Source != c.archive.URL || m.Archive.Size != c.archive.Size {
		return nil, fmt.Errorf("signed package does not match release candidate")
	}
	return &ReleasePlan{
		info: ReleaseInfo{Status: "authenticated-candidate", Freshness: "unknown", Tag: c.tag,
			Version: m.Version, Commit: m.Commit, OS: m.OS, Arch: m.Arch, Source: m.Source, Fingerprint: OfficialKeyFingerprint},
		manifest: m, raw: bytes.Clone(c.raw), signature: bytes.Clone(c.signature), client: client,
	}, nil
}

func newDiscoveryClient(transport http.RoundTripper) *http.Client {
	if transport == nil {
		transport = &http.Transport{
			Proxy:               http.ProxyFromEnvironment,
			DialContext:         (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 20 * time.Second,
			IdleConnTimeout: 30 * time.Second, MaxIdleConns: 4, MaxIdleConnsPerHost: 2,
			DisableCompression: true,
		}
	}
	return &http.Client{Transport: transport, Timeout: packageDownloadTimeout, CheckRedirect: discoveryRedirect}
}

func discoveryRedirect(req *http.Request, via []*http.Request) error {
	if len(via) == 0 || len(via) >= 5 || via[0].URL.String() == officialLatestReleaseAPI {
		return fmt.Errorf("release API redirect or redirect limit refused")
	}
	u := req.URL
	if u.Scheme != "https" || u.User != nil || u.Opaque != "" || u.Fragment != "" || u.RawPath != "" ||
		u.Host != u.Hostname() || u.Path == "" {
		return fmt.Errorf("unsafe release asset redirect")
	}
	switch u.Host {
	case "github.com":
		if u.String() != via[0].URL.String() {
			return fmt.Errorf("release asset redirect changed official path")
		}
	case "release-assets.githubusercontent.com", "objects.githubusercontent.com", "github-releases.githubusercontent.com":
	default:
		return fmt.Errorf("unapproved release asset redirect host")
	}
	// Never carry caller headers, cookies or authorization across redirects.
	req.Header = make(http.Header)
	req.Header.Set("Accept", "application/octet-stream")
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("User-Agent", "atm-core-package-discovery")
	return nil
}

func discoveryResponse(ctx context.Context, client *http.Client, source string, limit int64) (*http.Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/octet-stream")
	if source == officialLatestReleaseAPI {
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	}
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("User-Agent", "atm-core-package-discovery")
	resp, err := client.Do(req)
	if err != nil {
		// Do not echo redirect query strings, which may contain CDN credentials.
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("official package request failed")
	}
	if resp.StatusCode != http.StatusOK || resp.ContentLength > limit ||
		(resp.Header.Get("Content-Encoding") != "" && resp.Header.Get("Content-Encoding") != "identity") {
		resp.Body.Close()
		return nil, fmt.Errorf("official package response rejected (HTTP %d or size/encoding limit)", resp.StatusCode)
	}
	return resp, nil
}

func fetchDiscoveryBytes(ctx context.Context, client *http.Client, source string, limit int64) ([]byte, error) {
	resp, err := discoveryResponse(ctx, client, source, limit)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(contextReader{ctx, resp.Body}, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("official package metadata exceeds limit")
	}
	return b, ctx.Err()
}

// Download creates destination exclusively, mode 0600, in an existing directory
// with no symlink parents (resolve system temporary-directory aliases first).
// It verifies the pinned archive's size/hash, removes failed partial downloads,
// and never extracts, executes, installs, creates directories or overwrites files.
// The caller should use a private temporary directory and exclude other writers.
func (p *ReleasePlan) Download(ctx context.Context, destination string) (err error) {
	if p == nil || p.client == nil || p.info.Status != "authenticated-candidate" {
		return fmt.Errorf("authenticated release plan required")
	}
	if err := p.manifest.Validate(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, packageDownloadTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	if !filepath.IsAbs(destination) || filepath.Clean(destination) != destination || filepath.Dir(destination) == destination {
		return fmt.Errorf("download requires a clean absolute new file path")
	}
	parent := filepath.Dir(destination)
	for dir := parent; ; dir = filepath.Dir(dir) {
		info, err := os.Lstat(dir)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("linked or non-directory download parent")
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		return err
	}
	defer root.Close()
	name := filepath.Base(destination)
	f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() {
		f.Close()
		if err != nil {
			if removeErr := root.Remove(name); removeErr != nil {
				err = fmt.Errorf("%w; partial download cleanup failed: %v", err, removeErr)
			}
		}
	}()
	resp, err := discoveryResponse(ctx, p.client, p.manifest.Source, p.manifest.Archive.Size)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(contextReader{ctx, resp.Body}, p.manifest.Archive.Size+1))
	if err != nil {
		return err
	}
	if n != p.manifest.Archive.Size || hex.EncodeToString(h.Sum(nil)) != p.manifest.Archive.SHA256 {
		return fmt.Errorf("downloaded package size or SHA-256 mismatch")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return f.Close()
}
