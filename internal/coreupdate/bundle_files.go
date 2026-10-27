package coreupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/AtomyCloud/atm/internal/install"
)

type bundleTree struct {
	Files map[string]install.File `json:"files"`
	Dirs  map[string]uint32       `json:"dirs"`
}

type bundleInstalledState struct {
	manifest PackageManifest
	raw, sig []byte
	owned    map[string]string
	tree     bundleTree
}

func bundleName(name string) bool {
	if !fs.ValidPath(name) || name == "." || !utf8.ValidString(name) || len(name) > 1024 || strings.Count(name, "/") > 32 {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
			return false
		}
		base, _, _ := strings.Cut(strings.ToUpper(part), ".")
		if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || len(base) == 4 && (base[:3] == "COM" || base[:3] == "LPT") && base[3] >= '1' && base[3] <= '9' {
			return false
		}
		for _, c := range part {
			if c < 32 || c == 127 || strings.ContainsRune(`\:*?"<>|`, c) {
				return false
			}
		}
	}
	return true
}

// Bound the entire portable tree, including unowned data. Huge application/data
// trees are deliberately refused instead of partially preserving their contents.
func bundleScan(ctx context.Context, r *os.Root) (bundleTree, error) {
	t := bundleTree{Files: map[string]install.File{}, Dirs: map[string]uint32{}}
	seen := map[string]string{}
	var total int64
	err := fs.WalkDir(r.FS(), ".", func(name string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if name == "." {
			i, err := r.Stat(".")
			if err != nil {
				return err
			}
			t.Dirs["."] = uint32(i.Mode().Perm())
			return nil
		}
		if !bundleName(name) {
			return fmt.Errorf("unsafe installed path: %q", name)
		}
		base := path.Base(name)
		if strings.EqualFold(base, ".git") || strings.HasPrefix(base, ".atm-update-") || strings.HasPrefix(base, ".atm-component-stage-") || base == ".atm-core-update-pending" || base == transaction || strings.HasPrefix(base, ".atm-download-") || base == ".atm-setup-lock" {
			return fmt.Errorf("checkout or pending component transaction: %s", name)
		}
		if strings.HasPrefix(strings.ToLower(base), ".atm-package-") && name != BundleManifestName && name != BundleSignatureName {
			return fmt.Errorf("reserved package metadata: %s", name)
		}
		key := strings.ToLower(name)
		if old, ok := seen[key]; ok && old != name {
			return fmt.Errorf("installed case collision: %s", name)
		}
		seen[key] = name
		if len(seen) > packageEntries {
			return fmt.Errorf("installed tree entry limit exceeded")
		}
		i, err := r.Lstat(name)
		if err != nil {
			return err
		}
		if i.Mode()&(os.ModeSymlink|os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
			return fmt.Errorf("unsafe installed mode: %s", name)
		}
		if i.IsDir() {
			t.Dirs[name] = uint32(i.Mode().Perm())
			return nil
		}
		if !i.Mode().IsRegular() {
			return fmt.Errorf("special installed file: %s", name)
		}
		f, err := fingerprint(ctx, r, name, MaxPackageBytes)
		if err != nil {
			return err
		}
		if f.Size > packageExpandedBytes-total {
			return fmt.Errorf("installed tree size limit exceeded")
		}
		total += f.Size
		t.Files[name] = f
		return nil
	})
	return t, err
}

func bundleInstalled(ctx context.Context, r *os.Root, p BundlePolicy, raw, sig []byte) (bundleInstalledState, error) {
	var s bundleInstalledState
	var err error
	if raw == nil {
		_, manifestErr := r.Lstat(BundleManifestName)
		_, signatureErr := r.Lstat(BundleSignatureName)
		if errors.Is(manifestErr, os.ErrNotExist) && errors.Is(signatureErr, os.ErrNotExist) {
			return s, ErrBundleUnenrolled
		}
		raw, err = readFile(r, BundleManifestName, MaxManifestBytes)
		if err != nil {
			return s, fmt.Errorf("signed package baseline required: %w", err)
		}
		sig, err = readFile(r, BundleSignatureName, ed25519SignatureSize)
		if err != nil {
			return s, err
		}
	}
	s.manifest, err = VerifyPackageManifest(raw, sig, p.PublicKey, p.OS, p.Arch)
	if err != nil {
		return s, err
	}
	s.raw, s.sig = append([]byte(nil), raw...), append([]byte(nil), sig...)
	s.tree, err = bundleScan(ctx, r)
	if err != nil {
		return s, err
	}
	b, err := readFile(r, "release.json", packageReleaseBytes)
	if err != nil {
		return s, err
	}
	if fmt.Sprintf("%x", sha256.Sum256(b)) != s.manifest.ReleaseSHA256 {
		return s, fmt.Errorf("modified installed release index")
	}
	var release packageRelease
	if err = packageReleaseJSON(b, &release); err != nil {
		return s, err
	}
	m := s.manifest
	if release.Schema != m.Schema || release.Version != m.Version || release.Commit != m.Commit || release.OS != m.OS || release.Arch != m.Arch || release.Migration != m.Migration || len(release.Files) == 0 || len(release.Files) > packageEntries {
		return s, fmt.Errorf("installed release identity mismatch")
	}
	s.owned = map[string]string{"release.json": m.ReleaseSHA256}
	for name, hash := range release.Files {
		if !packagePayloadPath(name) || name == "release.json" || !digest(hash) {
			return s, fmt.Errorf("invalid signed ownership path: %s", name)
		}
		s.owned[name] = hash
	}
	exe := "atm"
	if p.OS == "windows" {
		exe += ".exe"
	}
	for _, name := range []string{exe, "core-api.json", "LICENSE", "README.md"} {
		if _, ok := s.owned[name]; !ok {
			return s, fmt.Errorf("missing signed ownership: %s", name)
		}
	}
	for name, hash := range s.owned {
		f, ok := s.tree.Files[name]
		if !ok || f.SHA256 != hash || runtime.GOOS != "windows" && (f.Mode != 0644 && f.Mode != 0755 || name == exe && f.Mode != 0755) {
			return s, fmt.Errorf("modified owned package file: %s", name)
		}
	}
	return s, nil
}

const ed25519SignatureSize = 64

func prepareBundle(ctx context.Context, p BundlePolicy, raw, sig []byte, archive string, enroll bool) (*BundlePlan, error) {
	if err := bundlePolicy(p); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := BundleStartupGuard(p.Root); err != nil {
		return nil, err
	}
	m, err := VerifyPackageManifest(raw, sig, p.PublicKey, p.OS, p.Arch)
	if err != nil {
		return nil, err
	}
	files, err := InspectPackage(ctx, archive, m)
	if err != nil {
		return nil, err
	}
	r, err := openRoot(p.Root)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	var old bundleInstalledState
	if enroll {
		for _, name := range []string{BundleManifestName, BundleSignatureName} {
			if _, e := r.Lstat(name); !errors.Is(e, os.ErrNotExist) {
				return nil, fmt.Errorf("existing or unreadable package baseline; enrollment refused")
			}
		}
		old, err = bundleInstalled(ctx, r, p, raw, sig)
		if err == nil {
			for name, want := range files {
				if err = match(ctx, r, name, want); err != nil {
					break
				}
			}
		}
	} else {
		old, err = bundleInstalled(ctx, r, p, nil, nil)
		if err == nil && !newer(m.Version, old.manifest.Version) {
			err = fmt.Errorf("package version must strictly increase")
		}
	}
	if err != nil {
		return nil, err
	}
	if err = bundleCollision(old.tree, old.owned, files); err != nil {
		return nil, err
	}
	p.PublicKey = append([]byte(nil), p.PublicKey...)
	archive, err = filepath.Abs(archive)
	if err != nil {
		return nil, err
	}
	preserved := len(old.tree.Files) - len(old.owned)
	if !enroll {
		preserved -= 2
	}
	return &BundlePlan{p: p, info: BundleInfo{Root: p.Root, State: "planned", Manifest: m, Baseline: old.manifest, CurrentVersion: old.manifest.Version, TargetVersion: m.Version, Files: len(files), PreservedFiles: preserved, Transaction: p.Root + BundleTransactionSuffix}, raw: append([]byte(nil), raw...), sig: append([]byte(nil), sig...), oldRaw: old.raw, oldSig: old.sig, archive: archive, files: files, before: old.tree, enroll: enroll}, nil
}

func bundleCollision(tree bundleTree, owned map[string]string, next map[string]install.File) error {
	seen := map[string]string{}
	add := func(name, kind string) error {
		key := strings.ToLower(name)
		value := kind + name
		if old, ok := seen[key]; ok && old != value {
			return fmt.Errorf("package path/type/case collision: %s", name)
		}
		seen[key] = value
		return nil
	}
	for name := range tree.Dirs {
		if err := add(name, "d:"); err != nil {
			return err
		}
	}
	for name := range tree.Files {
		if _, ok := owned[name]; !ok {
			if err := add(name, "f:"); err != nil {
				return err
			}
		}
	}
	for name := range next {
		if _, ok := tree.Files[name]; ok {
			if _, owned := owned[name]; !owned {
				return fmt.Errorf("package would overwrite unowned file: %s", name)
			}
		}
		if err := add(name, "f:"); err != nil {
			return err
		}
		for dir := path.Dir(name); dir != "."; dir = path.Dir(dir) {
			if err := add(dir, "d:"); err != nil {
				return err
			}
		}
	}
	return nil
}

func bundleSame(a, b bundleTree) bool { return reflect.DeepEqual(a, b) }

func bundleCopy(ctx context.Context, from, to *os.Root, name string, want install.File) error {
	if err := to.MkdirAll(path.Dir(name), 0700); err != nil {
		return err
	}
	f, err := regular(from, name, os.O_RDONLY, want.Size)
	if err != nil {
		return err
	}
	defer f.Close()
	got, err := writeFile(ctx, to, name, f, want.Size, os.FileMode(want.Mode))
	if err != nil {
		return err
	}
	if got.SHA256 != want.SHA256 || got.Size != want.Size {
		return fmt.Errorf("installed data changed during copy: %s", name)
	}
	return nil
}

// Extraction consumes a fresh hash-checked snapshot of the already inspected
// archive. Only exact inspected file names are created, through os.Root.
func bundleExtract(ctx context.Context, r *os.Root, plan *BundlePlan) error {
	raw, err := packageSnapshot(ctx, plan.archive, plan.info.Manifest.Archive)
	if err != nil {
		return err
	}
	prefix := "atm-" + plan.info.Manifest.Version + "-" + plan.p.OS + "-" + plan.p.Arch + "/"
	seen := map[string]bool{}
	put := func(name string, mode fs.FileMode, src io.Reader) error {
		if mode.IsDir() {
			return nil
		}
		name, ok := strings.CutPrefix(name, prefix)
		if !ok {
			return fmt.Errorf("unexpected extraction prefix")
		}
		want, ok := plan.files[name]
		if !ok || seen[name] {
			return fmt.Errorf("unexpected extraction file")
		}
		seen[name] = true
		if err := r.MkdirAll(path.Dir(name), 0700); err != nil {
			return err
		}
		got, err := writeFile(ctx, r, name, src, want.Size, os.FileMode(want.Mode))
		if err != nil {
			return err
		}
		if got.SHA256 != want.SHA256 || got.Size != want.Size {
			return fmt.Errorf("extracted file mismatch: %s", name)
		}
		return bundleBoundary(plan.checkpoint, "candidate-file")
	}
	if plan.p.OS == "windows" {
		z, e := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
		if e != nil {
			return e
		}
		for _, f := range z.File {
			src, e := f.Open()
			if e != nil {
				return e
			}
			e = put(f.Name, f.Mode(), src)
			closeErr := src.Close()
			if e != nil {
				return e
			}
			if closeErr != nil {
				return closeErr
			}
		}
	} else {
		gz, e := gzip.NewReader(bytes.NewReader(raw))
		if e != nil {
			return e
		}
		defer gz.Close()
		tr := tar.NewReader(gz)
		for {
			h, e := tr.Next()
			if e == io.EOF {
				break
			}
			if e != nil {
				return e
			}
			if e = put(h.Name, h.FileInfo().Mode(), tr); e != nil {
				return e
			}
		}
	}
	if len(seen) != len(plan.files) {
		return fmt.Errorf("incomplete extraction")
	}
	return ctx.Err()
}

func bundleStage(ctx context.Context, old, candidate *os.Root, plan *BundlePlan) error {
	preserved := map[string]install.File{}
	for name := range plan.before.Dirs {
		if err := candidate.MkdirAll(name, 0700); err != nil {
			return err
		}
	}
	if plan.enroll {
		for name, f := range plan.before.Files {
			if err := bundleCopy(ctx, old, candidate, name, f); err != nil {
				return err
			}
			preserved[name] = f
			if err := bundleBoundary(plan.checkpoint, "candidate-file"); err != nil {
				return err
			}
		}
	} else {
		if err := bundleExtract(ctx, candidate, plan); err != nil {
			return err
		}
		var release packageRelease
		b, err := readFile(old, "release.json", packageReleaseBytes)
		if err != nil {
			return err
		}
		if err = packageReleaseJSON(b, &release); err != nil {
			return err
		}
		for name, f := range plan.before.Files {
			if name == "release.json" || name == BundleManifestName || name == BundleSignatureName {
				continue
			}
			if _, owned := release.Files[name]; owned {
				continue
			}
			if err = bundleCopy(ctx, old, candidate, name, f); err != nil {
				return err
			}
			preserved[name] = f
			if err = bundleBoundary(plan.checkpoint, "candidate-file"); err != nil {
				return err
			}
		}
	}
	for name, b := range map[string][]byte{BundleManifestName: plan.raw, BundleSignatureName: plan.sig} {
		if _, err := writeFile(ctx, candidate, name, bytes.NewReader(b), int64(len(b)), 0600); err != nil {
			return err
		}
	}
	// Final directory modes are applied only after their children are populated.
	dirs := []string{}
	err := fs.WalkDir(candidate.FS(), ".", func(name string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if name != "." && d.IsDir() {
			dirs = append(dirs, name)
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dirs)))
	for _, name := range dirs {
		mode := uint32(0755)
		if oldMode, ok := plan.before.Dirs[name]; ok {
			mode = oldMode
		}
		if err = candidate.Chmod(name, os.FileMode(mode)); err != nil {
			return err
		}
	}
	for name, want := range plan.files {
		if err = match(ctx, candidate, name, want); err != nil {
			return err
		}
	}
	if err = candidate.Chmod(".", os.FileMode(plan.before.Dirs["."])); err != nil {
		return err
	}
	verified, err := bundleInstalled(ctx, candidate, plan.p, nil, nil)
	if err != nil {
		return err
	}
	for name, want := range preserved {
		if got, ok := verified.tree.Files[name]; !ok || got != want {
			return fmt.Errorf("preserved file readback mismatch: %s", name)
		}
	}
	return nil
}
