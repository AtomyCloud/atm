package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

const markerName = ".atm-native-sdk.json"
const receiptName = ".atm-android-sdk-package.json"

type rootMarker struct {
	Schema     int    `json:"schema"`
	Root       string `json:"root"`
	Integrated bool   `json:"integrated"`
	Block      string `json:"block,omitempty"`
}
type entry struct {
	Mode   uint32 `json:"mode"`
	Size   int64  `json:"size,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
	Link   string `json:"link,omitempty"`
}
type receipt struct {
	Schema      int              `json:"schema"`
	Plugin      string           `json:"plugin"`
	Spec        packageSpec      `json:"spec"`
	LocalSHA256 string           `json:"local_sha256"`
	Files       map[string]entry `json:"files"`
}

func realDirs(p string, create bool) error {
	if !filepath.IsAbs(p) {
		return fmt.Errorf("absolute path required")
	}
	cur := "/"
	for _, part := range strings.Split(filepath.Clean(p), "/") {
		if part == "" {
			continue
		}
		cur = filepath.Join(cur, part)
		i, err := os.Lstat(cur)
		if os.IsNotExist(err) && create {
			if err = os.Mkdir(cur, 0700); err != nil {
				return err
			}
			i, err = os.Lstat(cur)
		}
		if err != nil {
			return err
		}
		if !i.IsDir() || i.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe directory %s", cur)
		}
	}
	return nil
}
func decodeStrict(data []byte, value any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return err
	}
	if d.Decode(new(any)) != io.EOF {
		return fmt.Errorf("trailing JSON")
	}
	return nil
}
func (b *backend) ownedRoot(required bool) (bool, error) {
	if err := realDirs(b.root, false); os.IsNotExist(err) {
		if required {
			return false, fmt.Errorf("no native-managed SDK installed")
		}
		return false, nil
	} else if err != nil {
		return false, err
	}
	data, _, exists, err := readProfile(filepath.Join(b.root, markerName))
	if err != nil {
		return false, err
	}
	if exists {
		var m rootMarker
		if err = decodeStrict(data, &m); err != nil {
			return false, err
		}
		if m.Schema != 1 || m.Root != b.root {
			return false, fmt.Errorf("invalid native SDK marker")
		}
		return true, nil
	}
	f, err := os.Open(b.root)
	if err != nil {
		return false, err
	}
	defer f.Close()
	names, err := f.Readdirnames(1)
	if err != nil && err != io.EOF {
		return false, err
	}
	if len(names) != 0 {
		return false, fmt.Errorf("existing unmanaged SDK detected at %s; preserving it, no automatic adoption", b.root)
	}
	if required {
		return false, fmt.Errorf("SDK has no native ownership")
	}
	return false, nil
}
func (b *backend) withLock(fn func() error) error {
	owned, err := b.ownedRoot(false)
	if err != nil {
		return err
	}
	if err = realDirs(b.root, true); err != nil {
		return err
	}
	lock := filepath.Join(b.root, ".atm-native-lock")
	if err = os.Mkdir(lock, 0700); err != nil {
		return fmt.Errorf("SDK locked/interrupted; inspect %s before clearing: %w", lock, err)
	}
	defer os.Remove(lock)
	if !owned {
		entries, err := os.ReadDir(b.root)
		if err != nil {
			return err
		}
		if len(entries) != 1 || entries[0].Name() != ".atm-native-lock" {
			return fmt.Errorf("SDK changed before ownership initialization; preserving it")
		}
		data, _ := json.Marshal(rootMarker{Schema: 1, Root: b.root})
		f, err := os.OpenFile(filepath.Join(b.root, markerName), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		_, err = f.Write(data)
		if err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return fn()
}
func inventory(ctx context.Context, dir string) (map[string]entry, error) {
	if err := realDirs(dir, false); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	result := map[string]entry{}
	var total int64
	err = fs.WalkDir(root.FS(), ".", func(name string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if name == "." || name == receiptName {
			return nil
		}
		if !safePath(name) || len(result) >= maxEntries*2 {
			return fmt.Errorf("unsafe/excessive package entries")
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		e := entry{Mode: uint32(info.Mode())}
		switch {
		case info.IsDir():
		case info.Mode()&os.ModeSymlink != 0:
			e.Link, err = root.Readlink(name)
			if err == nil {
				_, err = root.Stat(name)
			}
		case info.Mode().IsRegular():
			e.Size = info.Size()
			if e.Size > maxFile || e.Size > maxExpanded-total {
				return fmt.Errorf("package inventory limit")
			}
			total += e.Size
			f, openErr := root.Open(name)
			if openErr != nil {
				return openErr
			}
			h := sha256.New()
			n, readErr := io.Copy(h, io.LimitReader(contextReader{ctx, f}, e.Size+1))
			f.Close()
			if readErr != nil {
				return readErr
			}
			if n != e.Size {
				return fmt.Errorf("package changed during inventory")
			}
			e.SHA256 = hex.EncodeToString(h.Sum(nil))
		default:
			return fmt.Errorf("special package file")
		}
		if err != nil {
			return err
		}
		result[name] = e
		return nil
	})
	return result, err
}
func readRegular(root *os.Root, name string, max int64) ([]byte, error) {
	i, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !i.Mode().IsRegular() || i.Size() > max {
		return nil, fmt.Errorf("unsafe/oversized file %s", name)
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if int64(len(data)) > max {
		return nil, fmt.Errorf("file grew beyond limit")
	}
	return data, err
}
func (b *backend) readReceipt(id string) (receipt, error) {
	var rec receipt
	dir, err := packageDir(id)
	if err != nil {
		return rec, err
	}
	full := filepath.Join(b.root, dir)
	if err = realDirs(full, false); err != nil {
		return rec, err
	}
	root, err := os.OpenRoot(full)
	if err != nil {
		return rec, err
	}
	defer root.Close()
	data, err := readRegular(root, receiptName, 64<<20)
	if err != nil {
		return rec, fmt.Errorf("unowned package preserved: %s: %w", id, err)
	}
	if err = decodeStrict(data, &rec); err != nil {
		return rec, err
	}
	if rec.Schema != 1 || rec.Plugin != "android_sdk" || rec.Spec.ID != id || rec.Spec.Directory != dir || !rec.Spec.Revision.valid() || !sha256RE.MatchString(rec.LocalSHA256) || !approvedURL(rec.Spec.URL) || len(rec.Files) == 0 || len(rec.Files) > maxEntries*2 {
		return rec, fmt.Errorf("invalid package ownership receipt")
	}
	return rec, nil
}
func (b *backend) verifyOwned(ctx context.Context, id string) (receipt, error) {
	rec, err := b.readReceipt(id)
	if err != nil {
		return rec, err
	}
	actual, err := inventory(ctx, filepath.Join(b.root, rec.Spec.Directory))
	if err != nil {
		return rec, err
	}
	if !reflect.DeepEqual(rec.Files, actual) {
		return rec, fmt.Errorf("package %s changed or contains unknown files; preserving it", id)
	}
	return rec, nil
}
func (b *backend) receipts() ([]receipt, error) {
	if _, err := b.ownedRoot(true); err != nil {
		return nil, err
	}
	ids := []string{"platform-tools", "emulator", toolsID}
	for _, family := range []string{"platforms", "build-tools", "cmake", "ndk", "patcher"} {
		dir := filepath.Join(b.root, family)
		if err := realDirs(dir, false); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return nil, err
		}
		f, err := os.Open(dir)
		if err != nil {
			return nil, err
		}
		entries, err := f.ReadDir(257)
		f.Close()
		if err != nil && err != io.EOF {
			return nil, err
		}
		if len(entries) > 256 {
			return nil, fmt.Errorf("too many SDK package directories")
		}
		for _, e := range entries {
			if _, err := packageDir(family + ";" + e.Name()); err == nil {
				ids = append(ids, family+";"+e.Name())
			}
		}
	}
	var out []receipt
	for _, id := range ids {
		dir, _ := packageDir(id)
		name := filepath.Join(b.root, dir, receiptName)
		if _, err := os.Lstat(name); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return nil, err
		}
		r, err := b.readReceipt(id)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Spec.ID < out[j].Spec.ID })
	return out, nil
}
func (b *backend) removePackage(ctx context.Context, id string) error {
	records, err := b.receipts()
	if err != nil {
		return err
	}
	for _, r := range records {
		for _, d := range r.Spec.Dependencies {
			if d.Path == id {
				return fmt.Errorf("%s is required by native package %s", id, r.Spec.ID)
			}
		}
	}
	if strings.HasPrefix(id, "cmake;") {
		// Validate the target before changing integration. Edited blocks abort
		// before deletion; successful refresh avoids a stale version-specific PATH.
		if _, err = b.verifyOwned(ctx, id); err != nil {
			return err
		}
		if err = b.integrateExcluding(ctx, false, id); err != nil {
			return err
		}
	}
	return b.deleteOwned(ctx, id)
}
func (b *backend) deleteOwned(ctx context.Context, id string) error {
	rec, err := b.verifyOwned(ctx, id)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(filepath.Join(b.root, rec.Spec.Directory))
	if err != nil {
		return err
	}
	defer root.Close()
	names := make([]string, 0, len(rec.Files))
	for name := range rec.Files {
		names = append(names, name)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	for _, name := range names {
		if err = ctx.Err(); err != nil {
			return err
		}
		if !safePath(name) || name == receiptName {
			return fmt.Errorf("unsafe receipt deletion path")
		}
		if err = root.Remove(name); err != nil {
			return err
		}
	}
	if err = root.Remove(receiptName); err != nil {
		return err
	}
	return os.Remove(filepath.Join(b.root, rec.Spec.Directory))
}
func (b *backend) uninstall(ctx context.Context) error {
	records, err := b.receipts()
	if err != nil {
		return err
	}
	for _, r := range records {
		if _, err = b.verifyOwned(ctx, r.Spec.ID); err != nil {
			return err
		}
	}
	if err = b.checkLatest(false); err != nil {
		return err
	}
	if err = b.integrate(ctx, true); err != nil {
		return err
	}
	if target, err := os.Readlink(filepath.Join(b.root, "cmdline-tools/latest")); err == nil && target == toolsDirectory {
		if err = os.Remove(filepath.Join(b.root, "cmdline-tools/latest")); err != nil {
			return err
		}
	}
	for _, r := range records {
		if err = b.deleteOwned(ctx, r.Spec.ID); err != nil {
			return err
		}
	}
	b.log("Native packages removed. SDK marker, licenses, retained update history, unknown files and all user/AVD data are preserved.")
	return nil
}
