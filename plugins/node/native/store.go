package main

import (
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

	"github.com/AtomyCloud/atm/internal/install"
)

type entry struct {
	Mode   uint32 `json:"mode"`
	Size   int64  `json:"size,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
	Link   string `json:"link,omitempty"`
}
type receipt struct {
	Schema   int              `json:"schema"`
	Plugin   string           `json:"plugin"`
	Artifact install.Spec     `json:"artifact"`
	Files    map[string]entry `json:"files"`
}

func relativeLink(from, to string) (string, error) { return filepath.Rel(from, to) }

// Refuse symlinked ancestors, including an AppsDir alias, rather than adopting it.
func realDirs(p string, create bool) error {
	if !filepath.IsAbs(p) {
		return fmt.Errorf("absolute directory required")
	}
	cur := string(filepath.Separator)
	for _, part := range strings.Split(filepath.Clean(p), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		if os.IsNotExist(err) && create {
			if err = os.Mkdir(cur, 0700); err != nil {
				return err
			}
			info, err = os.Lstat(cur)
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe directory: %s", cur)
		}
	}
	return nil
}
func (b *backend) withLock(fn func() error) error {
	if err := realDirs(b.root, true); err != nil {
		return err
	}
	lock := filepath.Join(b.root, ".atm-native-lock")
	if err := os.Mkdir(lock, 0700); err != nil {
		return fmt.Errorf("Node installation locked or interrupted; inspect %s before manually clearing it: %w", lock, err)
	}
	defer os.Remove(lock)
	return fn()
}
func (b *backend) installed() ([]string, error) {
	if err := realDirs(b.root, false); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	dir, err := os.Open(b.root)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(10001)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if len(entries) > 10000 {
		return nil, fmt.Errorf("too many Node root entries")
	}
	out := []string{}
	for _, e := range entries {
		if !versionRE.MatchString(e.Name()) || !e.IsDir() {
			continue
		}
		if b.validInstalled(e.Name()) == nil {
			out = append(out, e.Name())
		}
	}
	sort.Slice(out, func(i, j int) bool { return newer(out[i], out[j]) })
	return out, nil
}
func (b *backend) validInstalled(v string) error {
	if !versionRE.MatchString(v) {
		return fmt.Errorf("invalid installed version")
	}
	dir := filepath.Join(b.root, v)
	if err := realDirs(filepath.Join(dir, "bin"), false); err != nil {
		return err
	}
	info, err := os.Lstat(filepath.Join(dir, "bin/node"))
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return fmt.Errorf("version %s lacks regular executable bin/node", v)
	}
	return nil
}
func (b *backend) current() (string, error) {
	if err := realDirs(b.root, false); os.IsNotExist(err) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	target, err := os.Readlink(filepath.Join(b.root, "current"))
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("current is not an ATM version symlink: %w", err)
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(b.root, target)
	}
	target = filepath.Clean(target)
	v := filepath.Base(target)
	if filepath.Dir(target) != b.root || !versionRE.MatchString(v) {
		return "", fmt.Errorf("current points outside ATM Node versions; refusing mutation")
	}
	if err := b.validInstalled(v); err != nil {
		return "", err
	}
	return v, nil
}
func (b *backend) hasReceipt(v string) bool {
	info, err := os.Lstat(filepath.Join(b.root, v, receiptName))
	return err == nil && info.Mode().IsRegular()
}

func inventory(ctx context.Context, dir string) (map[string]entry, error) {
	r, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	files := map[string]entry{}
	var total int64
	err = fs.WalkDir(r.FS(), ".", func(name string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if name == "." || name == receiptName {
			return nil
		}
		if !safeArchivePath(name) || len(files) >= limits.Entries*2 {
			return fmt.Errorf("unsafe or excessive installed entries")
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		e := entry{Mode: uint32(info.Mode())}
		switch {
		case info.IsDir():
		case info.Mode()&os.ModeSymlink != 0:
			e.Link, err = r.Readlink(name)
		case info.Mode().IsRegular():
			e.Size = info.Size()
			if e.Size > limits.FileBytes || e.Size > limits.ExpandedBytes-total {
				return fmt.Errorf("installed payload too large")
			}
			total += e.Size
			f, openErr := r.Open(name)
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
				return fmt.Errorf("installed file changed while reading")
			}
			e.SHA256 = hex.EncodeToString(h.Sum(nil))
		default:
			return fmt.Errorf("special installed file")
		}
		if err != nil {
			return err
		}
		files[name] = e
		return nil
	})
	return files, err
}
func (b *backend) verifyOwned(ctx context.Context, v string) error {
	if err := b.validInstalled(v); err != nil {
		return err
	}
	name := filepath.Join(b.root, v, receiptName)
	info, err := os.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 8<<20 {
		return fmt.Errorf("version %s has no safe native ownership receipt; legacy/user files are preserved", v)
	}
	f, err := os.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	dec := json.NewDecoder(io.LimitReader(f, (8<<20)+1))
	dec.DisallowUnknownFields()
	var rec receipt
	if err = dec.Decode(&rec); err != nil {
		return err
	}
	if dec.Decode(new(any)) != io.EOF {
		return fmt.Errorf("trailing receipt data")
	}
	if rec.Schema != 1 || rec.Plugin != "node" || rec.Artifact.Version != v || len(rec.Files) == 0 {
		return fmt.Errorf("invalid Node ownership receipt")
	}
	actual, err := inventory(ctx, filepath.Join(b.root, v))
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(actual, rec.Files) {
		return fmt.Errorf("version %s contains changed or user-added files; preserving it", v)
	}
	return nil
}
func (b *backend) install(ctx context.Context, o options) error {
	if _, err := b.current(); err != nil {
		return err
	}
	v := o.version
	if v == "" {
		major := o.major
		if major == "" {
			major = "24"
		}
		vs, err := b.latest(ctx, major)
		if err != nil {
			return err
		}
		v = vs[0]
	}
	dest := filepath.Join(b.root, v)
	if _, err := os.Lstat(dest); err == nil {
		if err = b.validInstalled(v); err != nil {
			return err
		}
		b.log("Existing version preserved; selecting " + v)
		return b.activate(ctx, v)
	} else if !os.IsNotExist(err) {
		return err
	}
	spec, err := b.spec(ctx, v)
	if err != nil {
		return err
	}
	stage, err := os.MkdirTemp(b.root, ".atm-stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	archive := filepath.Join(stage, "archive.tar.gz")
	f, err := os.OpenFile(archive, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	h := sha256.New()
	b.log("Downloading official Node.js " + v)
	err = b.fetch(ctx, strings.TrimPrefix(spec.URL, b.base), io.MultiWriter(f, h), limits.DownloadBytes)
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
	if hex.EncodeToString(h.Sum(nil)) != spec.SHA256 {
		return fmt.Errorf("official artifact SHA-256 mismatch")
	}
	payload := filepath.Join(stage, "payload")
	if err = os.Mkdir(payload, 0700); err != nil {
		return err
	}
	r, err := os.OpenRoot(payload)
	if err != nil {
		return err
	}
	err = extractArchive(ctx, archive, r, spec.StripPrefix)
	r.Close()
	if err != nil {
		return err
	}
	files, err := inventory(ctx, payload)
	if err != nil {
		return err
	}
	data, err := json.Marshal(receipt{1, "node", spec, files})
	if err != nil {
		return err
	}
	if len(data) > 8<<20 {
		return fmt.Errorf("receipt too large")
	}
	if err = os.WriteFile(filepath.Join(payload, receiptName), data, 0600); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if _, err = os.Lstat(dest); !os.IsNotExist(err) {
		return fmt.Errorf("version destination appeared during download")
	}
	if err = os.Rename(payload, dest); err != nil {
		return err
	}
	// Published versions survive activation failure for inspection; old current remains.
	return b.activate(ctx, v)
}
func (b *backend) activate(ctx context.Context, v string) error {
	if err := b.validInstalled(v); err != nil {
		return err
	}
	if _, err := b.current(); err != nil {
		return err
	}
	plans, err := b.profilePlans(false)
	if err != nil {
		return err
	}
	stage, err := os.MkdirTemp(b.root, ".atm-switch-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	link := filepath.Join(stage, "current")
	if err = os.Symlink(filepath.Join(b.root, v), link); err != nil {
		return err
	}
	rollback, err := applyProfiles(ctx, plans)
	if err != nil {
		return err
	}
	if err = ctx.Err(); err == nil {
		err = os.Rename(link, filepath.Join(b.root, "current"))
	}
	if err != nil {
		if undoErr := rollback(); undoErr != nil {
			return fmt.Errorf("activation failed: %v; profile rollback failed: %w", err, undoErr)
		}
		return err
	}
	b.log("Selected Node.js " + v + ". Refresh this shell: export PATH=" + shellQuote(filepath.Join(b.root, "current/bin")) + ":\"$PATH\"; hash -r")
	return nil
}
func (b *backend) remove(ctx context.Context, v string) error {
	current, err := b.current()
	if err != nil {
		return err
	}
	if current == v {
		return fmt.Errorf("cannot remove current version; select another version first")
	}
	if err = b.verifyOwned(ctx, v); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(b.root, v))
}
func (b *backend) uninstall(ctx context.Context) error {
	current, err := b.current()
	if err != nil {
		return err
	}
	vs, err := b.installed()
	if err != nil {
		return err
	}
	owned := []string{}
	for _, v := range vs {
		if !b.hasReceipt(v) {
			b.log("Preserving legacy/unowned version " + v)
			continue
		}
		if err = b.verifyOwned(ctx, v); err != nil {
			return err
		}
		owned = append(owned, v)
	}
	clear := false
	for _, v := range owned {
		if v == current {
			clear = true
		}
	}
	if current == "" || clear {
		plans, err := b.profilePlans(true)
		if err != nil {
			return err
		}
		rollback, err := applyProfiles(ctx, plans)
		if err != nil {
			return err
		}
		if clear {
			if err = os.Remove(filepath.Join(b.root, "current")); err != nil {
				_ = rollback()
				return err
			}
		}
	}
	for _, v := range owned {
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = os.RemoveAll(filepath.Join(b.root, v)); err != nil {
			return err
		}
	}
	b.log("Removed unchanged native-owned versions only; external installations and unrelated files preserved")
	return nil
}
