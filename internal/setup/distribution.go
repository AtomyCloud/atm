package setup

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/AtomyCloud/atm/internal/coreupdate"
	"github.com/AtomyCloud/atm/internal/plugin"
	"github.com/AtomyCloud/atm/internal/version"
)

const maxFile = 256 << 20
const maxTotal = 1 << 30
const maxEntries = 20000

type release struct {
	Schema    int               `json:"schema"`
	Version   string            `json:"version"`
	Commit    string            `json:"commit"`
	OS        string            `json:"os"`
	Arch      string            `json:"arch"`
	Migration bool              `json:"migration"`
	Files     map[string]string `json:"files"`
}

type payloadFile struct {
	hash string
	size int64
	mode fs.FileMode
}
type distribution struct{ files map[string]payloadFile }

func digestOK(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == sha256.Size && strings.ToLower(s) == s
}

func payloadPath(name string) bool {
	if !fs.ValidPath(name) || name == "." || len(name) > 240 || strings.Count(name, "/") > 15 {
		return false
	}
	parts := strings.Split(name, "/")
	for _, p := range parts {
		if strings.HasPrefix(p, ".") || strings.HasSuffix(p, ".") || strings.HasSuffix(p, " ") || p == "tests" || p == "__pycache__" {
			return false
		}
		base, _, _ := strings.Cut(strings.ToUpper(p), ".")
		if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || (len(base) == 4 && (base[:3] == "COM" || base[:3] == "LPT") && base[3] >= '1' && base[3] <= '9') {
			return false
		}
		for _, r := range p {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_-+.@() ", r)) {
				return false
			}
		}
	}
	switch parts[0] {
	case "bin", "lib", "libexec", "plugins", "lang":
		return true
	case "atm", "atm.exe", "core-api.json", "LICENSE", "README.md", "release.json":
		return len(parts) == 1
	}
	return false
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(b)
}

func readRegular(ctx context.Context, root *os.Root, name string, limit int64) ([]byte, error) {
	if err := checkPath(root, filepath.FromSlash(name)); err != nil {
		return nil, err
	}
	i, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !i.Mode().IsRegular() || i.Size() > limit {
		return nil, fmt.Errorf("unsafe or oversized file: %s", name)
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(contextReader{ctx, f}, limit+1))
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("file exceeds limit: %s", name)
	}
	return b, err
}

func verify(ctx context.Context, source, pin string) (distribution, error) {
	return verifyPayload(ctx, source, pin, false, false)
}

const receiptName = ".atm-setup-receipt.json"

type setupReceipt struct {
	Schema         int    `json:"schema"`
	ManifestSHA256 string `json:"manifest_sha256"`
	Destination    string `json:"destination"`
}

// Installed command integration verifies the pinned manifest and every non-plugin
// payload file. Plugins have independent update receipts and are not PATH inputs.
func verifyInstalled(ctx context.Context, source, pin string) (distribution, error) {
	key, err := coreupdate.OfficialPublicKey()
	if err != nil {
		return distribution{}, err
	}
	return verifyInstalledWithKey(ctx, source, pin, key)
}

// The public entrypoint always supplies the embedded key. A separate pure trust
// parameter keeps fixture signatures out of the production identity.
func verifyInstalledWithKey(ctx context.Context, source, pin string, key ed25519.PublicKey) (distribution, error) {
	if err := coreupdate.RuntimeGuard(source); err != nil {
		return distribution{}, err
	}
	r, err := os.OpenRoot(source)
	if err != nil {
		return distribution{}, err
	}
	defer r.Close()
	// Signed full-package enrollment remains authoritative after an update.
	// The old local setup receipt may retain its original release hash; never
	// adopt a new hash merely by reading release.json from the installation.
	signed, signedErr := readRegular(ctx, r, coreupdate.BundleManifestName, coreupdate.MaxManifestBytes)
	if signedErr == nil {
		sig, err := readRegular(ctx, r, coreupdate.BundleSignatureName, 64)
		if err != nil {
			return distribution{}, err
		}
		m, err := coreupdate.VerifyPackageManifest(signed, sig, key, runtime.GOOS, runtime.GOARCH)
		if err != nil {
			return distribution{}, err
		}
		if pin != "" && pin != m.ReleaseSHA256 {
			return distribution{}, fmt.Errorf("explicit pin conflicts with signed installed release")
		}
		return verifyPayloadScope(ctx, source, m.ReleaseSHA256, true, true, true)
	}
	if !os.IsNotExist(signedErr) {
		return distribution{}, signedErr
	}
	if _, err := r.Lstat(coreupdate.BundleSignatureName); !os.IsNotExist(err) {
		return distribution{}, fmt.Errorf("orphaned or unreadable signed package receipt")
	}
	b, err := readRegular(ctx, r, receiptName, 4096)
	hasReceipt := err == nil
	if err != nil && !os.IsNotExist(err) {
		return distribution{}, err
	}
	if hasReceipt {
		var rec setupReceipt
		if err = json.Unmarshal(b, &rec); err != nil {
			return distribution{}, err
		}
		if rec.Schema != 1 || !digestOK(rec.ManifestSHA256) || filepath.Clean(rec.Destination) != filepath.Clean(source) || (pin != "" && pin != rec.ManifestSHA256) {
			return distribution{}, fmt.Errorf("invalid or conflicting setup receipt")
		}
		pin = rec.ManifestSHA256
	}
	return verifyPayload(ctx, source, pin, hasReceipt, true)
}

func verifyPayload(ctx context.Context, source, pin string, allowReceipt, coreOnly bool) (distribution, error) {
	return verifyPayloadScope(ctx, source, pin, allowReceipt, coreOnly, false)
}

func verifyPayloadScope(ctx context.Context, source, pin string, allowReceipt, coreOnly, allowUnowned bool) (distribution, error) {
	d := distribution{files: map[string]payloadFile{}}
	if !digestOK(pin) {
		return d, fmt.Errorf("independently trusted release.json SHA-256 required")
	}
	i, err := os.Lstat(source)
	if err != nil {
		return d, err
	}
	if !i.IsDir() || i.Mode()&os.ModeSymlink != 0 {
		return d, fmt.Errorf("distribution must be a real directory")
	}
	r, err := os.OpenRoot(source)
	if err != nil {
		return d, err
	}
	defer r.Close()
	b, err := readRegular(ctx, r, "release.json", 2<<20)
	if err != nil {
		return d, err
	}
	if fmt.Sprintf("%x", sha256.Sum256(b)) != pin {
		return d, fmt.Errorf("release manifest hash mismatch")
	}
	var m release
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&m); err != nil {
		return d, err
	}
	if dec.Decode(new(any)) != io.EOF {
		return d, fmt.Errorf("trailing manifest data")
	}
	if m.Schema != 1 || m.Version != version.Version || !m.Migration || m.Commit == "" || m.OS != runtime.GOOS || m.Arch != runtime.GOARCH || len(m.Files) == 0 || len(m.Files) > maxEntries {
		return d, fmt.Errorf("incompatible release identity or target")
	}
	seen := map[string]bool{}
	components := map[string]string{}
	expectedFiles := 1 // release.json itself is pinned separately.
	for name, hash := range m.Files {
		if !payloadPath(name) || name == "release.json" || !digestOK(hash) || seen[strings.ToLower(name)] {
			return d, fmt.Errorf("unsafe or duplicate payload path: %s", name)
		}
		seen[strings.ToLower(name)] = true
		parts := strings.Split(name, "/")
		for i := range parts {
			prefix := strings.Join(parts[:i+1], "/")
			key := strings.ToLower(prefix)
			if prior, ok := components[key]; ok && prior != prefix {
				return d, fmt.Errorf("case-aliased payload directory: %s", prefix)
			}
			components[key] = prefix
		}
		if !coreOnly || !strings.HasPrefix(name, "plugins/") {
			expectedFiles++
		}
	}
	for _, name := range []string{executableName(), "core-api.json", "LICENSE", "README.md"} {
		if _, ok := m.Files[name]; !ok {
			return d, fmt.Errorf("missing required payload: %s", name)
		}
	}
	var total int64
	visited := 0
	err = fs.WalkDir(r.FS(), ".", func(name string, e fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		visited++
		if visited > maxEntries*2 {
			return fmt.Errorf("too many payload entries")
		}
		if name == "." {
			return nil
		}
		if allowReceipt && (name == receiptName || name == coreupdate.BundleManifestName || name == coreupdate.BundleSignatureName) {
			return nil
		} // Already bounded and checked by verifyInstalled.
		if allowUnowned && name != "release.json" && name != "plugins" {
			_, listed := m.Files[name]
			if !listed {
				ancestor := components[strings.ToLower(name)] == name
				if !ancestor {
					if e.IsDir() {
						return fs.SkipDir
					}
					return nil
				}
			}
		}
		if !payloadPath(name) {
			return fmt.Errorf("non-distribution file refused: %s", name)
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("payload symlink refused: %s", name)
		}
		if coreOnly && name == "plugins" {
			if !e.IsDir() {
				return fmt.Errorf("plugin root must remain a real directory")
			}
			return fs.SkipDir
		}
		if e.IsDir() {
			return nil
		}
		hash, ok := m.Files[name]
		if name == "release.json" {
			hash, ok = pin, true
		}
		if !ok || !info.Mode().IsRegular() || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 || info.Size() < 0 || info.Size() > maxFile {
			return fmt.Errorf("unlisted or unsafe payload: %s", name)
		}
		total += info.Size()
		if total > maxTotal {
			return fmt.Errorf("payload exceeds size limit")
		}
		f, err := r.Open(name)
		if err != nil {
			return err
		}
		h := sha256.New()
		n, copyErr := io.Copy(h, io.LimitReader(contextReader{ctx, f}, info.Size()+1))
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if n != info.Size() || hex.EncodeToString(h.Sum(nil)) != hash {
			return fmt.Errorf("payload hash mismatch: %s", name)
		}
		mode := fs.FileMode(0644)
		if info.Mode().Perm()&0111 != 0 || name == executableName() {
			mode = 0755
		}
		d.files[name] = payloadFile{hash, n, mode}
		return nil
	})
	if err != nil {
		return d, err
	}
	if len(d.files) != expectedFiles {
		return d, fmt.Errorf("payload files missing")
	}
	if err = plugin.CheckBinary(filepath.Join(source, executableName()), m.OS+"/"+m.Arch); err != nil {
		return d, err
	}
	api, err := readRegular(ctx, r, "core-api.json", 64<<10)
	if err != nil {
		return d, err
	}
	var contract struct {
		Schema         int `json:"schema"`
		PluginAPI      int `json:"plugin_api"`
		UpdateProtocol int `json:"update_protocol"`
	}
	if err = json.Unmarshal(api, &contract); err != nil || contract.Schema != 1 || contract.PluginAPI != 1 || contract.UpdateProtocol != 1 {
		return d, fmt.Errorf("incompatible core-api.json")
	}
	if coreOnly {
		return d, nil
	}
	for name := range m.Files {
		parts := strings.Split(name, "/")
		if len(parts) != 3 || parts[0] != "plugins" || parts[2] != "plugin.json" {
			continue
		}
		b, err := readRegular(ctx, r, name, 64<<10)
		if err != nil {
			return d, err
		}
		var manifest plugin.Manifest
		if err = json.Unmarshal(b, &manifest); err != nil {
			return d, err
		}
		if err = manifest.Validate(parts[1]); err != nil {
			return d, err
		}
		for target, executable := range manifest.Executables {
			rel := "plugins/" + parts[1] + "/" + executable
			if _, ok := m.Files[rel]; !ok {
				return d, fmt.Errorf("native plugin executable missing: %s", rel)
			}
			if err = plugin.CheckBinary(filepath.Join(source, filepath.FromSlash(rel)), target); err != nil {
				return d, err
			}
		}
	}
	return d, nil
}

func privateDir(root *os.Root, prefix string) (string, error) {
	name := prefix + hex.EncodeToString(randomBytes())
	if err := root.Mkdir(name, 0700); err != nil {
		return "", err
	}
	if err := secureDirectory(root, name); err != nil {
		_ = root.Remove(name)
		return "", err
	}
	return name, nil
}

func randomBytes() []byte { b := make([]byte, 16); _, _ = rand.Read(b); return b }

// Install copies only a fully verified distribution into a new destination.
// It does not also edit profiles: Integrate is a separate consent-gated action.
func Install(ctx context.Context, o Options) (Result, error) {
	dest, rel, err := o.paths()
	result := Result{Status: baseStatus(dest), DryRun: o.DryRun, Planned: []string{dest}}
	if err != nil {
		return result, err
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if !o.DryRun && !o.Consent {
		return result, ErrConsent
	}
	home, err := openHome(o)
	if err != nil {
		return result, err
	}
	defer home.Close()
	if err = checkPath(home, rel); err != nil {
		return result, err
	}
	if _, err = home.Lstat(rel); err == nil {
		return result, ErrExists
	} else if !os.IsNotExist(err) {
		return result, err
	}
	d, err := verify(ctx, o.Source, o.TrustedManifestSHA256)
	if err != nil {
		return result, err
	}
	if o.DryRun {
		return result, nil
	}
	if err = o.boundary("source-verified"); err != nil {
		return result, err
	}
	unlock, err := lock(home)
	if err != nil {
		return result, err
	}
	defer unlock()
	if err = checkPath(home, rel); err != nil {
		return result, err
	}
	if err = home.MkdirAll(filepath.Dir(rel), 0700); err != nil {
		return result, err
	}
	parent, err := home.OpenRoot(filepath.Dir(rel))
	if err != nil {
		return result, err
	}
	defer parent.Close()
	stage, err := privateDir(parent, ".atm-stage-")
	if err != nil {
		return result, err
	}
	defer parent.RemoveAll(stage)
	staged, err := parent.OpenRoot(stage)
	if err != nil {
		return result, err
	}
	defer staged.Close()
	source, err := os.OpenRoot(o.Source)
	if err != nil {
		return result, err
	}
	defer source.Close()
	for name, file := range d.files {
		if err = ctx.Err(); err != nil {
			return result, err
		}
		if err = checkPath(source, filepath.FromSlash(name)); err != nil {
			return result, err
		}
		if err = staged.MkdirAll(filepath.Dir(name), 0700); err != nil {
			return result, err
		}
		if err = copyPayload(ctx, source, staged, name, file); err != nil {
			return result, err
		}
	}
	if _, err = verify(ctx, filepath.Join(dest, "..", stage), o.TrustedManifestSHA256); err != nil {
		return result, err
	}
	receipt, err := json.Marshal(setupReceipt{1, o.TrustedManifestSHA256, dest})
	if err != nil {
		return result, err
	}
	if err = writeNew(staged, receiptName, receipt, 0600); err != nil {
		return result, err
	}
	if err = o.boundary("before-publish"); err != nil {
		return result, err
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	parentFile, err := parent.Open(".")
	if err != nil {
		return result, err
	}
	defer parentFile.Close()
	if err = publishNoReplace(parentFile, stage, filepath.Base(rel)); err != nil {
		return result, err
	}
	result.Installed, result.Verified = true, true
	result.Changed = []string{dest}
	return result, nil
}

func copyPayload(ctx context.Context, source, dest *os.Root, name string, p payloadFile) error {
	f, err := source.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("payload changed type: %s", name)
	}
	out, err := dest.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, p.mode)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, h), io.LimitReader(contextReader{ctx, f}, p.size+1))
	if err == nil {
		err = out.Chmod(p.mode)
	}
	if err == nil {
		err = out.Sync()
	}
	closeErr := out.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if n != p.size || hex.EncodeToString(h.Sum(nil)) != p.hash {
		return fmt.Errorf("payload changed while copying: %s", name)
	}
	return nil
}
