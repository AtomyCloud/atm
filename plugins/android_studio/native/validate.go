package main

import (
	"context"
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/AtomyCloud/atm/internal/data"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type identity struct {
	ProductVersion string `json:"product_version"`
	Build          string `json:"build"`
	Launcher       string `json:"launcher"`
	Icon           string `json:"icon"`
}

var buildRE = regexp.MustCompile(`^[0-9]+(\.[0-9]+){1,6}$`)

func readRegular(root *os.Root, name string, max int64) ([]byte, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > max {
		return nil, fmt.Errorf("regular bounded %s required", name)
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if int64(len(data)) > max {
		return nil, fmt.Errorf("file exceeds limit")
	}
	return data, err
}
func validateELF(root *os.Root, name string) error {
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 || info.Size() > limits.FileBytes {
		return fmt.Errorf("regular executable %s required", name)
	}
	f, err := root.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	bin, err := elf.NewFile(f)
	if err != nil {
		return err
	}
	if bin.Class != elf.ELFCLASS64 || bin.Data != elf.ELFDATA2LSB || bin.Machine != elf.EM_X86_64 || (bin.Type != elf.ET_EXEC && bin.Type != elf.ET_DYN) {
		return fmt.Errorf("Linux amd64 executable required")
	}
	return nil
}
func validatePayload(root *os.Root, a artifact) (identity, error) {
	var id identity
	for _, name := range []string{"bin", "jbr", "jbr/bin"} {
		info, err := root.Lstat(name)
		if err != nil || !info.IsDir() {
			return id, fmt.Errorf("real %s directory required", name)
		}
	}
	data, err := readRegular(root, "product-info.json", 1<<20)
	if err != nil {
		return id, err
	}
	var p struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Build   string `json:"buildNumber"`
		Code    string `json:"productCode"`
		Launch  []struct {
			OS   string `json:"os"`
			Arch string `json:"arch"`
			Path string `json:"launcherPath"`
		} `json:"launch"`
	}
	if err = json.Unmarshal(data, &p); err != nil {
		return id, err
	}
	if p.Code != "AI" || p.Name != "Android Studio" {
		return id, fmt.Errorf("payload is not Android Studio")
	}
	parts := strings.Split(a.Version, ".")
	if len(parts) != 4 {
		return id, fmt.Errorf("invalid distribution version")
	}
	base := strings.Join(parts[:3], ".")
	id.ProductVersion = p.Version
	id.Build = strings.TrimPrefix(p.Build, "AI-")
	if !buildRE.MatchString(id.Build) {
		return id, fmt.Errorf("invalid Android Studio build number")
	}
	// Google's studio/stamper.py writes version=build.txt (AI-<build>) and
	// buildNumber without AI-. Accept that exact pair, not arbitrary AI-* text.
	if p.Version != base && p.Version != a.Version && p.Version != "AI-"+id.Build {
		return id, fmt.Errorf("IDE product version does not match distribution or stamped build identity")
	}
	build, err := readRegular(root, "build.txt", 4096)
	if err != nil {
		return id, err
	}
	if strings.TrimSpace(string(build)) != "AI-"+id.Build {
		return id, fmt.Errorf("build.txt/product-info identity mismatch")
	}
	for _, l := range p.Launch {
		if l.OS == "Linux" && (l.Arch == "amd64" || l.Arch == "x86_64") && (l.Path == "bin/studio.sh" || l.Path == "bin/studio") {
			id.Launcher = l.Path
			break
		}
	}
	if id.Launcher == "" {
		return id, fmt.Errorf("no compatible Linux launcher")
	}
	// Prefer the legacy-compatible script when included, keeping current/bin stable.
	if info, err := root.Lstat("bin/studio.sh"); err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
			return id, fmt.Errorf("unsafe studio.sh")
		}
		id.Launcher = "bin/studio.sh"
	} else if !os.IsNotExist(err) {
		return id, err
	}
	if id.Launcher == "bin/studio.sh" {
		data, err := readRegular(root, id.Launcher, 1<<20)
		if err != nil {
			return id, err
		}
		if !strings.HasPrefix(string(data), "#!") {
			return id, fmt.Errorf("invalid studio.sh launcher")
		}
	} else if err := validateELF(root, id.Launcher); err != nil {
		return id, err
	}
	if err := validateELF(root, "jbr/bin/java"); err != nil {
		return id, err
	}
	for _, name := range []string{"bin/studio.svg", "bin/studio.png"} {
		if _, err := readRegular(root, name, 8<<20); err == nil {
			id.Icon = name
			break
		}
	}
	return id, nil
}
func (b *backend) readReceipt(v string) (receipt, error) {
	var rec receipt
	if !slotRE.MatchString(v) {
		return rec, fmt.Errorf("invalid installation directory")
	}
	root, err := os.OpenRoot(filepath.Join(b.root, v))
	if err != nil {
		return rec, err
	}
	defer root.Close()
	data, err := readRegular(root, receiptName, 32<<20)
	if err != nil {
		return rec, err
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&rec); err != nil {
		return rec, err
	}
	if dec.Decode(new(any)) != io.EOF {
		return rec, fmt.Errorf("trailing receipt data")
	}
	if rec.Schema != 1 || rec.Plugin != "android_studio" || rec.Artifact.dir() != v || len(rec.Files) == 0 {
		return rec, fmt.Errorf("invalid Android Studio receipt")
	}
	return rec, rec.Artifact.validate()
}
func (b *backend) legacySpec(v string) (artifact, error) {
	root, err := os.OpenRoot(filepath.Join(b.root, v))
	if err != nil {
		return artifact{}, err
	}
	defer root.Close()
	dataBytes, err := readRegular(root, ".atm-meta", 64<<10)
	if os.IsNotExist(err) {
		for _, a := range catalog {
			if a.dir() == v {
				return a, nil
			}
		}
		return artifact{}, fmt.Errorf("unowned installation lacks identifiable literal metadata")
	}
	if err != nil {
		return artifact{}, err
	}
	fields, err := data.ParseAssignments(dataBytes, ".atm-meta")
	if err != nil {
		return artifact{}, err
	}
	for key := range fields {
		switch key {
		case "ATM_ANDROID_STUDIO_ID", "ATM_ANDROID_STUDIO_DISPLAY_NAME", "ATM_ANDROID_STUDIO_VERSION", "ATM_ANDROID_STUDIO_PACKAGE", "ATM_ANDROID_STUDIO_URL":
		default:
			return artifact{}, fmt.Errorf("unsupported legacy metadata key")
		}
	}
	a := artifact{ID: fields["ATM_ANDROID_STUDIO_ID"], Name: fields["ATM_ANDROID_STUDIO_DISPLAY_NAME"], Version: fields["ATM_ANDROID_STUDIO_VERSION"]}
	if !idRE.MatchString(a.ID) || !versionRE.MatchString(a.Version) || !safeText(a.Name, 120) || a.dir() != v {
		return a, fmt.Errorf("legacy identity does not match directory")
	}
	return a, nil
}
func (b *backend) spec(v string) (artifact, error) {
	if _, err := os.Lstat(filepath.Join(b.root, v, receiptName)); os.IsNotExist(err) {
		return b.legacySpec(v)
	} else if err != nil {
		return artifact{}, err
	}
	rec, err := b.readReceipt(v)
	return rec.Artifact, err
}
func (b *backend) validInstalled(v string) error {
	if !slotRE.MatchString(v) {
		return fmt.Errorf("invalid installation directory")
	}
	dir := filepath.Join(b.root, v)
	if err := realDirs(dir, false); err != nil {
		return err
	}
	a, err := b.spec(v)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	id, err := validatePayload(root, a)
	if err != nil {
		return err
	}
	if b.hasReceipt(v) {
		rec, err := b.readReceipt(v)
		if err != nil {
			return err
		}
		if rec.Identity != id {
			return fmt.Errorf("IDE identity changed; refusing selection")
		}
		for _, name := range []string{"product-info.json", "build.txt", id.Launcher} {
			ctx := b.ctx
			if ctx == nil {
				ctx = context.Background()
			}
			hash, err := hashFile(ctx, root, name)
			if err != nil {
				return err
			}
			if rec.Files[name].SHA256 != hash {
				return fmt.Errorf("IDE identity or launcher changed")
			}
		}
	}
	return nil
}
func hashFile(ctx context.Context, root *os.Root, name string) (string, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > limits.FileBytes {
		return "", fmt.Errorf("regular bounded identity file required")
	}
	f, err := root.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(contextReader{ctx, f}, info.Size()+1))
	if err != nil {
		return "", err
	}
	if n != info.Size() {
		return "", fmt.Errorf("identity file changed while hashing")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func (b *backend) resolveTarget(target string) (string, error) {
	id, version, ok := strings.Cut(target, "/")
	if !ok || !idRE.MatchString(id) || !versionRE.MatchString(version) {
		return "", fmt.Errorf("expected id/version, e.g. panda4/2025.3.4.6")
	}
	dirs, err := b.versionDirs(false)
	if err != nil {
		return "", err
	}
	found := ""
	for _, v := range dirs {
		a, err := b.spec(v)
		if err != nil {
			continue
		}
		if a.target() == target {
			if found != "" {
				return "", fmt.Errorf("ambiguous id/version; preserve and inspect duplicate installs")
			}
			found = v
		}
	}
	if found == "" {
		return "", fmt.Errorf("Android Studio target not installed")
	}
	return found, nil
}
