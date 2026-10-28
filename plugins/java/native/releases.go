package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/AtomyCloud/atm/internal/install"
	"github.com/AtomyCloud/atm/internal/protocol"
)

type choice = protocol.Choice

var vendors = []string{"openjdk", "corretto", "temurin", "microsoft", "graalvm", "custom"}
var versionRE = regexp.MustCompile(`^[1-9][0-9]{0,3}(\.(0|[1-9][0-9]{0,5})){0,3}$`)
var labelRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`)
var actualVersionRE = regexp.MustCompile(`^[1-9][0-9]{0,3}(\.(0|[1-9][0-9]{0,5})){0,3}(_[0-9]{1,6})?(-ea)?(\+[0-9]{1,6})?$`)
var limits = install.Limits{DownloadBytes: 512 << 20, ExpandedBytes: 2 << 30, FileBytes: 512 << 20, Entries: 50000}

func vendorSlug(s string) (string, error) {
	switch strings.ToLower(s) {
	case "openjdk", "jdk":
		return "openjdk", nil
	case "corretto", "amazon", "amazon-corretto", "amazon_corretto":
		return "corretto", nil
	case "temurin", "eclipse", "eclipse-temurin", "eclipse_temurin":
		return "temurin", nil
	case "microsoft", "microsoft-openjdk", "microsoft_openjdk", "ms":
		return "microsoft", nil
	case "graalvm", "graal":
		return "graalvm", nil
	case "custom":
		return "custom", nil
	}
	return "", fmt.Errorf("unknown Java vendor")
}
func normalizeVersion(s string) (string, error) {
	for _, prefix := range []string{"jdk-", "jdk", "java-", "java"} {
		s = strings.TrimPrefix(s, prefix)
	}
	if !versionRE.MatchString(s) {
		return "", fmt.Errorf("numeric Java version required (up to four components)")
	}
	return s, nil
}
func safeLabel(s string) bool { return labelRE.MatchString(s) && !strings.HasSuffix(s, ".") }
func normalizeTarget(s string) (string, error) {
	vendor, label, ok := strings.Cut(s, "/")
	if !ok || !safeLabel(label) {
		return "", fmt.Errorf("expected safe vendor/version target")
	}
	slug, err := vendorSlug(vendor)
	if err != nil {
		return "", err
	}
	return slug + "/" + label, nil
}
func compatible(actual, requested string) bool {
	base := strings.TrimSuffix(strings.Split(strings.Split(actual, "+")[0], "_")[0], "-ea")
	return base == requested || (!strings.Contains(requested, ".") && strings.HasPrefix(base, requested+"."))
}
func newer(a, b string) bool {
	parts := func(s string) []int {
		fields := strings.FieldsFunc(s, func(r rune) bool { return r == '.' || r == '_' || r == '+' })
		out := []int{}
		for _, f := range fields {
			n, _ := strconv.Atoi(f)
			out = append(out, n)
		}
		return out
	}
	aa, bb := parts(a), parts(b)
	for i := 0; i < max(len(aa), len(bb)); i++ {
		x, y := 0, 0
		if i < len(aa) {
			x = aa[i]
		}
		if i < len(bb) {
			y = bb[i]
		}
		if x != y {
			return x > y
		}
	}
	return false
}

type options struct {
	vendor, version, url, hash      string
	explicitVendor, explicitVersion bool
}

func parseOptions(args []string) (options, error) {
	o := options{vendor: "openjdk"}
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		key, val, eq := strings.Cut(args[i], "=")
		if seen[key] {
			return o, fmt.Errorf("duplicate option")
		}
		seen[key] = true
		if key != "--vendor" && key != "--version" && key != "--url" && key != "--sha256" {
			return o, fmt.Errorf("unknown option %s", key)
		}
		if !eq {
			i++
			if i >= len(args) {
				return o, fmt.Errorf("missing option value")
			}
			val = args[i]
		}
		if val == "" {
			return o, fmt.Errorf("empty option value")
		}
		switch key {
		case "--vendor":
			var err error
			o.vendor, err = vendorSlug(val)
			if err != nil {
				return o, err
			}
			o.explicitVendor = true
		case "--version":
			o.version = val
			o.explicitVersion = true
		case "--url":
			o.url = val
		case "--sha256":
			o.hash = strings.ToLower(val)
		}
	}
	if o.version == "" {
		switch o.vendor {
		case "openjdk":
			o.version = "26.0.1"
		case "corretto", "temurin":
			o.version = "26"
		case "microsoft", "graalvm":
			o.version = "25"
		}
	}
	if o.vendor == "custom" {
		if !o.explicitVersion || !safeLabel(o.version) {
			return o, fmt.Errorf("custom JDK requires --version with a safe exact label")
		}
	} else {
		var err error
		o.version, err = normalizeVersion(o.version)
		if err != nil {
			return o, err
		}
	}
	if o.url != "" {
		if _, err := httpsURL(o.url); err != nil {
			return o, err
		}
		if !validHash(o.hash) {
			return o, fmt.Errorf("--url requires an exact --sha256")
		}
	} else if o.hash != "" || o.vendor == "custom" {
		return o, fmt.Errorf("custom JDK requires --url and --sha256")
	}
	return o, nil
}
func validHash(s string) bool {
	raw, err := hex.DecodeString(s)
	return err == nil && len(raw) == sha256.Size
}
func httpsURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 4096 || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || strings.ContainsAny(raw, "\x00\r\n") {
		return nil, fmt.Errorf("HTTPS URL without credentials or fragment required")
	}
	return u, nil
}

type policy struct {
	vendor, origin string
	custom         bool
}

func storageHost(h string) bool {
	return h == "release-assets.githubusercontent.com" || h == "objects.githubusercontent.com" || h == "github-releases.githubusercontent.com"
}
func (p policy) allowed(u *url.URL) bool {
	if u.Scheme != "https" || u.User != nil || u.Fragment != "" {
		return false
	}
	if p.custom {
		return u.Scheme+"://"+u.Host == p.origin
	}
	switch p.vendor {
	case "openjdk":
		return u.Host == "download.java.net"
	case "corretto":
		return u.Host == "corretto.aws"
	case "microsoft":
		return u.Host == "aka.ms" || u.Host == "download.visualstudio.microsoft.com"
	case "graalvm":
		return u.Host == "download.oracle.com"
	case "temurin":
		return u.Host == "api.adoptium.net" || (u.Host == "github.com" && strings.HasPrefix(u.Path, "/adoptium/temurin26-binaries/releases/download/")) || storageHost(u.Host)
	}
	return false
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}
func (b *backend) fetch(ctx context.Context, raw string, p policy, dst io.Writer, limit int64) (string, int64, error) {
	u, err := httpsURL(raw)
	if err != nil {
		return "", 0, err
	}
	if !p.allowed(u) {
		return "", 0, fmt.Errorf("URL is outside this vendor's allowed origins")
	}
	c := *b.client
	c.Timeout = 5 * time.Minute
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !p.allowed(req.URL) {
			return fmt.Errorf("redirect rejected; custom downloads must stay on the explicitly supplied HTTPS origin")
		}
		if !p.custom && p.vendor == "temurin" && via[0].URL.Host == "api.adoptium.net" {
			return fmt.Errorf("Adoptium metadata redirect refused")
		}
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, "GET", raw, nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("User-Agent", "ATM/0.0.2")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := c.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || resp.ContentLength > limit {
		return "", 0, fmt.Errorf("vendor response rejected (status %d); no offline fallback", resp.StatusCode)
	}
	if ce := resp.Header.Get("Content-Encoding"); ce != "" && ce != "identity" {
		return "", 0, fmt.Errorf("encoded vendor response refused")
	}
	n, err := io.Copy(dst, io.LimitReader(contextReader{ctx, resp.Body}, limit+1))
	if err != nil {
		return "", n, err
	}
	if n > limit {
		return "", n, fmt.Errorf("download size limit exceeded")
	}
	return resp.Request.URL.String(), n, ctx.Err()
}

type artifact struct {
	install.Spec
	Vendor           string `json:"vendor"`
	Requested        string `json:"requested"`
	ActualVersion    string `json:"actual_version"`
	ResolvedURL      string `json:"resolved_url"`
	ChecksumURL      string `json:"checksum_url,omitempty"`
	ChecksumFilename string `json:"checksum_filename,omitempty"`
	Size             int64  `json:"size"`
	Custom           bool   `json:"custom_url,omitempty"`
}

func (a artifact) policy() policy {
	u, _ := url.Parse(a.URL)
	return policy{a.Vendor, u.Scheme + "://" + u.Host, a.Custom}
}

const openjdkBase = "https://download.java.net/java/GA/jdk26.0.1/458fda22e4c54d5ba572ab8d2b22eb83/8/GPL/"

func (b *backend) resolve(ctx context.Context, o options) (artifact, error) {
	a := artifact{Spec: install.Spec{Format: "tar.gz"}, Vendor: o.vendor, Requested: o.version, Custom: o.url != ""}
	if a.Custom {
		a.URL = o.url
		a.SHA256 = o.hash
		u, _ := url.Parse(o.url)
		a.Origins = []string{u.Scheme + "://" + u.Host}
		return a, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	switch o.vendor + ":" + o.version {
	case "openjdk:26", "openjdk:26.0.1":
		a.Requested = "26.0.1"
		a.URL = openjdkBase + "openjdk-26.0.1_linux-" + b.platform + "_bin.tar.gz"
		a.ChecksumURL = a.URL + ".sha256"
	case "corretto:26":
		name := "amazon-corretto-26-" + b.platform + "-linux-jdk.tar.gz"
		a.URL = "https://corretto.aws/downloads/latest/" + name
		a.ChecksumURL = "https://corretto.aws/downloads/latest_sha256/" + name
	case "microsoft:25":
		a.URL = "https://aka.ms/download-jdk/microsoft-jdk-25-linux-" + b.platform + ".tar.gz"
		a.ChecksumURL = a.URL + ".sha256sum.txt"
	case "graalvm:25":
		a.URL = "https://download.oracle.com/graalvm/25/latest/graalvm-jdk-25_linux-" + b.platform + "_bin.tar.gz"
		a.ChecksumURL = a.URL + ".sha256"
	case "temurin:26":
		return b.temurin(ctx, a)
	default:
		return a, fmt.Errorf("no official URL configured for %s %s; use an explicit HTTPS --url with --sha256", o.vendor, o.version)
	}
	p := a.policy()
	var data strings.Builder
	resolved, _, err := b.fetch(ctx, a.ChecksumURL, p, &data, 64<<10)
	if err != nil {
		return a, err
	}
	a.ChecksumURL = resolved
	fields := strings.Fields(data.String())
	if len(fields) < 1 || len(fields) > 2 || !validHash(fields[0]) {
		return a, fmt.Errorf("invalid or ambiguous SHA-256 sidecar")
	}
	// Some vendor sidecars name the resolved, version-specific archive. Require
	// a single safe basename; the payload is bound by the complete SHA-256.
	if len(fields) == 2 {
		filename := strings.TrimPrefix(fields[1], "*")
		if path.Base(filename) != filename || !strings.HasSuffix(filename, ".tar.gz") {
			return a, fmt.Errorf("invalid checksum filename")
		}
		a.ChecksumFilename = filename
	}
	a.SHA256 = strings.ToLower(fields[0])
	a.Origins = []string{p.origin}
	return a, nil
}

type temurinRow struct {
	ReleaseName string `json:"release_name"`
	Vendor      string `json:"vendor"`
	Version     struct {
		Major  int    `json:"major"`
		Semver string `json:"semver"`
	} `json:"version"`
	Binary struct {
		Architecture string `json:"architecture"`
		OS           string `json:"os"`
		ImageType    string `json:"image_type"`
		JVM          string `json:"jvm_impl"`
		Heap         string `json:"heap_size"`
		Package      struct {
			Name     string `json:"name"`
			Link     string `json:"link"`
			Checksum string `json:"checksum"`
			Size     int64  `json:"size"`
		} `json:"package"`
	} `json:"binary"`
}

func (b *backend) temurin(ctx context.Context, a artifact) (artifact, error) {
	endpoint := "https://api.adoptium.net/v3/assets/latest/26/hotspot?architecture=" + b.platform + "&image_type=jdk&os=linux&vendor=eclipse"
	var data strings.Builder
	if _, _, err := b.fetch(ctx, endpoint, policy{vendor: "temurin"}, &data, 4<<20); err != nil {
		return a, err
	}
	var rows []temurinRow
	if err := json.Unmarshal([]byte(data.String()), &rows); err != nil {
		return a, err
	}
	if len(rows) != 1 {
		return a, fmt.Errorf("expected one unambiguous Adoptium JDK asset")
	}
	row := rows[0]
	bin := row.Binary
	pkg := bin.Package
	release, build, ok := strings.Cut(strings.TrimPrefix(row.ReleaseName, "jdk-"), "+")
	if !ok || !strings.HasPrefix(row.ReleaseName, "jdk-") || !versionRE.MatchString(release) || !compatible(release, "26") || build == "" || strings.Trim(build, "0123456789") != "" || row.Vendor != "eclipse" || row.Version.Major != 26 || bin.Architecture != b.platform || bin.OS != "linux" || bin.ImageType != "jdk" || bin.JVM != "hotspot" || bin.Heap != "normal" {
		return a, fmt.Errorf("Adoptium response is not the requested stable Linux JDK")
	}
	u, err := httpsURL(pkg.Link)
	prefix := "OpenJDK26U-jdk_" + b.platform + "_linux_hotspot_"
	if err != nil || u.Host != "github.com" || !strings.HasPrefix(u.Path, "/adoptium/temurin26-binaries/releases/download/"+row.ReleaseName+"/") || path.Base(u.Path) != pkg.Name || !strings.HasPrefix(pkg.Name, prefix) || !strings.HasSuffix(pkg.Name, ".tar.gz") || !validHash(pkg.Checksum) || pkg.Size <= 0 || pkg.Size > limits.DownloadBytes {
		return a, fmt.Errorf("invalid Adoptium archive/hash/size")
	}
	a.URL = pkg.Link
	a.SHA256 = strings.ToLower(pkg.Checksum)
	a.Size = pkg.Size
	a.Origins = []string{"https://github.com", "https://release-assets.githubusercontent.com", "https://objects.githubusercontent.com", "https://github-releases.githubusercontent.com"}
	a.ChecksumURL = endpoint
	return a, nil
}
