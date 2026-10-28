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
	"strings"
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
	Artifact artifact         `json:"artifact"`
	Files    map[string]entry `json:"files"`
}

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
	if _, err := b.current(); err != nil {
		return err
	}
	lock := filepath.Join(b.root, ".atm-native-lock")
	if err := os.Mkdir(lock, 0700); err != nil {
		return fmt.Errorf("Java installation locked or interrupted; inspect %s before manually clearing it: %w", lock, err)
	}
	defer os.Remove(lock)
	return fn()
}
func (b *backend) installed() ([]string, error) {
	return b.versionDirs(true)
}
func (b *backend) validInstalled(v string) error {
	normalized, err := normalizeTarget(v)
	if err != nil {
		return fmt.Errorf("invalid installed version")
	}
	v = normalized
	dir := filepath.Join(b.root, v)
	if err := realDirs(filepath.Join(dir, "bin"), false); err != nil {
		return err
	}
	r, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer r.Close()
	_, err = validatePayload(r, b.platform)
	return err
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
	v, err := filepath.Rel(b.root, target)
	if err != nil {
		return "", err
	}
	canonical, err := normalizeTarget(v)
	if err != nil || canonical != v {
		return "", fmt.Errorf("current points outside canonical Java vendor/version directories")
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
			if err == nil {
				_, err = r.Stat(name)
			}
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
func (b *backend) readReceipt(v string) (receipt, error) {
	if err := b.validInstalled(v); err != nil {
		return receipt{}, err
	}
	name := filepath.Join(b.root, v, receiptName)
	info, err := os.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 8<<20 {
		return receipt{}, fmt.Errorf("version %s has no safe native ownership receipt; legacy/user files are preserved", v)
	}
	f, err := os.Open(name)
	if err != nil {
		return receipt{}, err
	}
	defer f.Close()
	dec := json.NewDecoder(io.LimitReader(f, (8<<20)+1))
	dec.DisallowUnknownFields()
	var rec receipt
	if err = dec.Decode(&rec); err != nil {
		return receipt{}, err
	}
	if dec.Decode(new(any)) != io.EOF {
		return receipt{}, fmt.Errorf("trailing receipt data")
	}
	if rec.Schema != 1 || rec.Plugin != "java" || rec.Artifact.Version != v || len(rec.Files) == 0 {
		return receipt{}, fmt.Errorf("invalid Java ownership receipt")
	}
	return rec, nil
}
func (b *backend) verifyOwned(ctx context.Context, v string) error {
	rec, err := b.readReceipt(v)
	if err != nil {
		return err
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
func (b *backend) activate(ctx context.Context, v string) error {
	if err := b.validInstalled(v); err != nil {
		return err
	}
	if _, err := b.current(); err != nil {
		return err
	}
	// Native payloads must still match their receipt even when only switching.
	if b.hasReceipt(v) {
		if err := b.verifyOwned(ctx, v); err != nil {
			return err
		}
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
	b.log("Selected Java " + v + ". Refresh this shell: " + b.shellRefresh())
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
	vs, err := b.versionDirs(false)
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
