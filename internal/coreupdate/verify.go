package coreupdate

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/AtomyCloud/atm/internal/install"
)

const (
	OfficialSource         = "https://github.com/AtomyCloud/atm"
	Scope                  = "compiled-core-binary-v1"
	MaxManifestBytes       = 16 << 10
	MaxBinaryBytes   int64 = 256 << 20
	// Sign the exact manifest bytes after this domain separator.
	SignatureDomain = "ATM compiled CORE manifest v1\x00"
)

var (
	ErrNoTrustAnchor    = errors.New("compiled CORE update requires an explicitly approved Ed25519 trust anchor")
	ErrRecoveryRequired = errors.New("compiled CORE update requires explicit recovery or verified completion before startup")
	ErrUnsupported      = errors.New("compiled CORE replacement unsupported on this platform; verified staging only")
	ErrBusy             = errors.New("compiled CORE update is busy")
	versionPattern      = regexp.MustCompile(`^(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})$`)
	commitPattern       = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// Manifest is a new signed binary-only contract, not the existing unsigned
// tools/package release.json. Runtime archives include plugins and are refused.
// Source is the fixed official raw release-asset URL, not a configurable origin.
type Manifest struct {
	Schema    int                     `json:"schema"`
	Scope     string                  `json:"scope"`
	Source    string                  `json:"source"`
	Version   string                  `json:"version"`
	Commit    string                  `json:"commit"`
	OS        string                  `json:"os"`
	Arch      string                  `json:"arch"`
	Migration bool                    `json:"migration"`
	Files     map[string]install.File `json:"files"`
}

// Policy comes from trusted application code, never downloaded metadata or
// arbitrary CLI flags. BaselineSHA256 comes from an independently verified
// installed receipt, not a hash silently calculated to adopt the executable.
// Versions are canonical MAJOR.MINOR.PATCH triples, each component at most nine
// digits. Target must increase numerically: no v prefix, prerelease, build
// metadata, same-version rebuild or general downgrade. Recover only restores
// this transaction's pinned baseline. Consent belongs to the caller.
type Policy struct {
	Root                          string
	PublicKey                     ed25519.PublicKey
	OS, Arch                      string
	CurrentVersion, TargetVersion string
	BaselineSHA256                string
	MaxBytes                      int64
	DryRun                        bool
}

type Service struct {
	p          Policy
	checkpoint func(string) error
}

func New(p Policy) (*Service, error) {
	if len(p.PublicKey) == 0 {
		return nil, ErrNoTrustAnchor
	}
	if len(p.PublicKey) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid Ed25519 trust anchor")
	}
	if !filepath.IsAbs(p.Root) || filepath.Clean(p.Root) != p.Root || filepath.Dir(p.Root) == p.Root {
		return nil, fmt.Errorf("a clean absolute installation root is required")
	}
	if !supported(p.OS, p.Arch) || !newer(p.TargetVersion, p.CurrentVersion) || !digest(p.BaselineSHA256) {
		return nil, fmt.Errorf("unsupported target, non-increasing version or missing verified baseline")
	}
	if p.MaxBytes == 0 {
		p.MaxBytes = MaxBinaryBytes
	}
	if p.MaxBytes < 64 || p.MaxBytes > MaxBinaryBytes {
		return nil, fmt.Errorf("invalid binary limit")
	}
	p.PublicKey = append(ed25519.PublicKey(nil), p.PublicKey...)
	return &Service{p: p}, nil
}

func supported(goos, arch string) bool {
	return ((goos == "linux" || goos == "darwin") && (arch == "amd64" || arch == "arm64")) || goos == "windows" && arch == "amd64"
}

func newer(a, b string) bool {
	x, y := versionPattern.FindStringSubmatch(a), versionPattern.FindStringSubmatch(b)
	if x == nil || y == nil {
		return false
	}
	for i := 1; i <= 3; i++ {
		xi, _ := strconv.Atoi(x[i])
		yi, _ := strconv.Atoi(y[i])
		if xi != yi {
			return xi > yi
		}
	}
	return false
}

func digest(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == 32 && hex.EncodeToString(b) == s
}

func (s *Service) name() string {
	if s.p.OS == "windows" {
		return "atm.exe"
	}
	return "atm"
}

func (s *Service) trust() error {
	if s == nil || len(s.p.PublicKey) != ed25519.PublicKeySize {
		return ErrNoTrustAnchor
	}
	return nil
}

// Verify is pure: no filesystem, network, process execution or trust discovery.
func (s *Service) Verify(raw, signature []byte) (Manifest, error) {
	var m Manifest
	if err := s.trust(); err != nil {
		return m, err
	}
	if len(raw) == 0 || len(raw) > MaxManifestBytes || len(signature) != ed25519.SignatureSize ||
		!ed25519.Verify(s.p.PublicKey, append([]byte(SignatureDomain), raw...), signature) {
		return m, fmt.Errorf("invalid signed CORE manifest")
	}
	if err := strictJSON(raw, &m); err != nil {
		return m, err
	}
	wantSource := OfficialSource + "/releases/download/v" + s.p.TargetVersion + "/atm-" + s.p.TargetVersion + "-" + s.p.OS + "-" + s.p.Arch
	if s.p.OS == "windows" {
		wantSource += ".exe"
	}
	if m.Schema != 1 || m.Scope != Scope || m.Source != wantSource || m.Version != s.p.TargetVersion ||
		m.OS != s.p.OS || m.Arch != s.p.Arch || !m.Migration || !commitPattern.MatchString(m.Commit) || len(m.Files) != 1 {
		return m, fmt.Errorf("CORE manifest identity, source or payload boundary mismatch")
	}
	f, ok := m.Files[s.name()]
	if !ok || !digest(f.SHA256) || f.Size < 64 || f.Size > s.p.MaxBytes || f.Mode != 0755 {
		return m, fmt.Errorf("CORE manifest requires exactly one bounded executable")
	}
	return m, nil
}

// Exact signed bytes must have one interpretation, including nested records.
func strictJSON(raw []byte, v any) error {
	if !utf8.Valid(raw) {
		return fmt.Errorf("invalid JSON UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	var value func(int) error
	value = func(depth int) error {
		if depth > 8 {
			return fmt.Errorf("JSON nesting limit")
		}
		t, err := d.Token()
		if err != nil {
			return err
		}
		delim, ok := t.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return err
				}
				k, ok := key.(string)
				if !ok || seen[k] || strings.ToLower(k) != k {
					return fmt.Errorf("duplicate or noncanonical JSON key")
				}
				// encoding/json folds some Unicode letters to ASCII field names.
				// Our fixed schema needs no Unicode keys or case aliases.
				for _, c := range k {
					if c < 0x20 || c > 0x7e {
						return fmt.Errorf("non-ASCII JSON key")
					}
				}
				seen[k] = true
				if err := value(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for d.More() {
				if err := value(depth + 1); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("invalid JSON delimiter")
		}
		_, err = d.Token()
		return err
	}
	if err := value(0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("trailing JSON data")
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode(v)
}
