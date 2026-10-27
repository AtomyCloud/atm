package coreupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/AtomyCloud/atm/internal/install"
	"github.com/AtomyCloud/atm/internal/plugin"
)

const (
	packageEntries             = 20000
	packageExpandedBytes int64 = 1 << 30
	packageReleaseBytes        = 2 << 20
)

type packageRelease struct {
	Schema    int               `json:"schema"`
	Version   string            `json:"version"`
	Commit    string            `json:"commit"`
	OS        string            `json:"os"`
	Arch      string            `json:"arch"`
	Migration bool              `json:"migration"`
	Files     map[string]string `json:"files"`
}

type packageInspector struct {
	ctx                     context.Context
	prefix, executable      string
	entries                 int
	expanded                int64
	seen, explicit          map[string]string
	files                   map[string]install.File
	release, executableData []byte
}

// InspectPackage reads, but never extracts or executes, a complete runtime
// package. It verifies consistency with expected, not its signature or freshness:
// callers must authenticate expected independently before trusting the result.
// Returned modes are normalized to 0644/0755 as in portable setup; release.json
// is included. Compressed input is snapshotted (at most MaxPackageBytes) so the
// hash and archive parser always see the same bytes, even if the file changes.
// The root binary is also retained (at most 256 MiB) for plugin's ReaderAt format
// checks, including Mach-O tables outside the header. Other payloads are streamed.
// Limits: 20,000 entries, 256 MiB per file, 1 GiB of payload, 2 MiB release.json,
// 240-byte runtime paths and at most 15 path separators. Parser overhead is extra.
func InspectPackage(ctx context.Context, archivePath string, expected PackageManifest) (map[string]install.File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := expected.Validate(); err != nil {
		return nil, err
	}
	raw, err := packageSnapshot(ctx, archivePath, expected.Archive)
	if err != nil {
		return nil, err
	}
	x := packageInspector{
		ctx: ctx, prefix: "atm-" + expected.Version + "-" + expected.OS + "-" + expected.Arch,
		executable: "atm", seen: map[string]string{}, explicit: map[string]string{}, files: map[string]install.File{},
	}
	if expected.OS == "windows" {
		x.executable += ".exe"
	}
	if expected.OS == "windows" {
		err = x.zip(raw)
	} else {
		err = x.tar(raw)
	}
	if err != nil {
		return nil, err
	}
	if err = x.verify(expected); err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return x.files, nil
}

func packageSnapshot(ctx context.Context, name string, expected install.File) ([]byte, error) {
	name, err := filepath.Abs(name)
	if err != nil {
		return nil, err
	}
	for parent := filepath.Dir(name); ; parent = filepath.Dir(parent) {
		i, err := os.Lstat(parent)
		if err != nil {
			return nil, err
		}
		if !i.IsDir() || i.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("linked/non-directory archive parent")
		}
		if filepath.Dir(parent) == parent {
			break
		}
	}
	r, err := os.OpenRoot(filepath.Dir(name))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	f, err := regular(r, filepath.Base(name), os.O_RDONLY, expected.Size)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() != expected.Size {
		return nil, fmt.Errorf("package archive size mismatch")
	}
	// One extra byte detects growth; do not allocate from untrusted archive headers.
	b := make([]byte, int(expected.Size)+1)
	n, err := io.ReadFull(contextReader{ctx, f}, b)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, err
	}
	if int64(n) != expected.Size {
		return nil, fmt.Errorf("package archive size mismatch")
	}
	b = b[:n]
	if fmt.Sprintf("%x", sha256.Sum256(b)) != expected.SHA256 {
		return nil, fmt.Errorf("package archive SHA-256 mismatch")
	}
	return b, ctx.Err()
}

func (x *packageInspector) zip(raw []byte) error {
	z, err := zip.NewReader(packageContextReaderAt{x.ctx, bytes.NewReader(raw)}, int64(len(raw)))
	if err != nil {
		return err
	}
	if len(z.File) > packageEntries {
		return fmt.Errorf("too many package entries")
	}
	for _, f := range z.File {
		if f.UncompressedSize64 > uint64(MaxPackageBytes) {
			return fmt.Errorf("package file too large")
		}
		r, err := f.Open()
		if err != nil {
			return err
		}
		err = x.entry(f.Name, f.Mode(), int64(f.UncompressedSize64), r)
		closeErr := r.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func (x *packageInspector) tar(raw []byte) error {
	source := bytes.NewReader(raw)
	gz, err := gzip.NewReader(source)
	if err != nil {
		return err
	}
	defer gz.Close()
	gz.Multistream(false)
	// Headers and padding have their own bounded allowance, including PAX data
	// consumed internally by archive/tar before it returns an entry.
	bounded := &io.LimitedReader{R: contextReader{x.ctx, gz}, N: packageExpandedBytes + packageEntries*4096 + 1025}
	tr := tar.NewReader(bounded)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if (h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA && h.Typeflag != tar.TypeDir) || len(h.PAXRecords) != 0 || h.Linkname != "" {
			return fmt.Errorf("package links, special files and PAX extensions refused")
		}
		if err = x.entry(h.Name, h.FileInfo().Mode(), h.Size, tr); err != nil {
			return err
		}
	}
	// tar EOF is not gzip EOF: read through the trailer to verify CRC/truncation.
	// Only zero padding is permitted after the tar end marker.
	buf := make([]byte, 32<<10)
	for {
		n, err := bounded.Read(buf)
		for _, b := range buf[:n] {
			if b != 0 {
				return fmt.Errorf("unexpected data after package tar")
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	if bounded.N <= 0 || source.Len() != 0 {
		return fmt.Errorf("oversized or trailing package stream")
	}
	return nil
}

func (x *packageInspector) entry(name string, mode fs.FileMode, size int64, r io.Reader) error {
	if err := x.ctx.Err(); err != nil {
		return err
	}
	x.entries++
	if x.entries > packageEntries {
		return fmt.Errorf("too many package entries")
	}
	if (!mode.IsRegular() && !mode.IsDir()) || mode&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 || size < 0 || size > MaxPackageBytes {
		return fmt.Errorf("unsafe or oversized package entry: %q", name)
	}
	if mode.IsDir() {
		name = strings.TrimSuffix(name, "/")
		if size != 0 {
			return fmt.Errorf("nonempty package directory")
		}
	}
	key := strings.ToLower(name)
	if _, ok := x.explicit[key]; ok {
		return fmt.Errorf("duplicate package entry: %q", name)
	}
	x.explicit[key] = name
	if name == x.prefix && mode.IsDir() {
		return nil
	}
	rel, ok := strings.CutPrefix(name, x.prefix+"/")
	if !ok || !packagePayloadPath(rel) {
		return fmt.Errorf("unsafe package path: %q", name)
	}
	if mode.IsDir() && !packageRuntimeTree(rel) {
		return fmt.Errorf("invalid package directory: %q", rel)
	}
	parts := strings.Split(rel, "/")
	for i := range parts {
		p := strings.Join(parts[:i+1], "/")
		kind := "d:"
		if i == len(parts)-1 && !mode.IsDir() {
			kind = "f:"
		}
		key := strings.ToLower(p)
		if old, ok := x.seen[key]; ok && old != kind+p {
			return fmt.Errorf("package path/type/case collision: %q", rel)
		}
		x.seen[key] = kind + p
	}
	if mode.IsDir() {
		return nil
	}
	if size > packageExpandedBytes-x.expanded {
		return fmt.Errorf("expanded package exceeds limit")
	}
	x.expanded += size
	if rel == "release.json" && size > packageReleaseBytes {
		return fmt.Errorf("release.json exceeds limit")
	}
	h := sha256.New()
	src := io.TeeReader(io.LimitReader(contextReader{x.ctx, r}, size+1), h)
	var captured []byte
	var err error
	if rel == "release.json" {
		captured, err = io.ReadAll(src)
	}
	if rel == x.executable {
		captured = make([]byte, int(size))
		_, err = io.ReadFull(src, captured)
	}
	if err != nil {
		return err
	}
	n, err := io.Copy(io.Discard, src)
	if err != nil {
		return err
	}
	if n+int64(len(captured)) != size {
		return fmt.Errorf("package file size mismatch: %q", rel)
	}
	perm := uint32(0644)
	if mode.Perm()&0111 != 0 || rel == x.executable {
		perm = 0755
	}
	x.files[rel] = install.File{SHA256: hex.EncodeToString(h.Sum(nil)), Size: size, Mode: perm}
	if rel == "release.json" {
		x.release = captured
	}
	if rel == x.executable {
		x.executableData = captured
	}
	return nil
}

// Keep the runtime boundary aligned with internal/setup/distribution.go.
func packageRuntimeTree(name string) bool {
	root, _, _ := strings.Cut(name, "/")
	return root == "bin" || root == "lib" || root == "libexec" || root == "plugins" || root == "lang"
}

func packagePayloadPath(name string) bool {
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
	if packageRuntimeTree(name) {
		return true
	}
	switch name {
	case "atm", "atm.exe", "core-api.json", "LICENSE", "README.md", "release.json":
		return true
	}
	return false
}

func (x *packageInspector) verify(expected PackageManifest) error {
	if x.files["release.json"].SHA256 != expected.ReleaseSHA256 {
		return fmt.Errorf("release.json SHA-256 mismatch")
	}
	var release packageRelease
	if err := packageReleaseJSON(x.release, &release); err != nil {
		return err
	}
	if release.Schema != expected.Schema || release.Version != expected.Version || release.Commit != expected.Commit || release.OS != expected.OS || release.Arch != expected.Arch || release.Migration != expected.Migration {
		return fmt.Errorf("package release identity mismatch")
	}
	if len(release.Files) == 0 || len(release.Files) > packageEntries || len(x.files) != len(release.Files)+1 {
		return fmt.Errorf("package payload index is not exact")
	}
	for name, hash := range release.Files {
		f, ok := x.files[name]
		if !packagePayloadPath(name) || name == "release.json" || !digest(hash) || !ok || f.SHA256 != hash {
			return fmt.Errorf("package payload hash/path mismatch: %q", name)
		}
	}
	for _, name := range []string{x.executable, "core-api.json", "LICENSE", "README.md"} {
		if _, ok := release.Files[name]; !ok {
			return fmt.Errorf("missing required package payload: %s", name)
		}
	}
	return plugin.CheckBinaryReader(packageContextReaderAt{x.ctx, bytes.NewReader(x.executableData)}, expected.OS+"/"+expected.Arch)
}

type packageContextReaderAt struct {
	ctx context.Context
	r   io.ReaderAt
}

func (r packageContextReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.ReadAt(p, off)
}

// strictJSON intentionally refuses uppercase map keys; release.json instead has
// case-sensitive runtime paths such as LICENSE. Reject duplicates before decoding
// its exact schema, without folding those paths or permitting schema aliases.
func packageReleaseJSON(raw []byte, result *packageRelease) error {
	if !utf8.Valid(raw) {
		return fmt.Errorf("invalid release JSON UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	var value func(int) error
	value = func(depth int) error {
		if depth > 8 {
			return fmt.Errorf("release JSON nesting limit")
		}
		t, err := d.Token()
		if err != nil {
			return err
		}
		if delim, ok := t.(json.Delim); ok {
			if delim != '{' {
				return fmt.Errorf("invalid release JSON structure")
			}
			seen := map[string]bool{}
			for d.More() {
				k, err := d.Token()
				if err != nil {
					return err
				}
				key, ok := k.(string)
				if !ok || seen[key] {
					return fmt.Errorf("duplicate release JSON key")
				}
				if depth == 0 {
					switch key {
					case "schema", "version", "commit", "os", "arch", "migration", "files":
					default:
						return fmt.Errorf("unknown release JSON key")
					}
				}
				seen[key] = true
				if err := value(depth + 1); err != nil {
					return err
				}
			}
			_, err = d.Token()
			return err
		}
		return nil
	}
	if err := value(0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("trailing release JSON")
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode(result)
}
