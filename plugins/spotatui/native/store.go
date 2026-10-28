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
		return fmt.Errorf("spotatui installation locked or interrupted; inspect %s before manually clearing it: %w", lock, err)
	}
	defer os.Remove(lock)
	return fn()
}
func (b *backend) installed() ([]string, error) {
	return b.versionDirs(true)
}
func (b *backend) versionDirs(validate bool) ([]string, error) {
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
		return nil, fmt.Errorf("too many spotatui root entries")
	}
	out := []string{}
	for _, e := range entries {
		if !versionRE.MatchString(e.Name()) || !e.IsDir() {
			continue
		}
		if !validate || b.validInstalled(e.Name()) == nil {
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
	if err := realDirs(filepath.Join(dir, "libexec"), false); err != nil {
		return err
	}
	r, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer r.Close()
	if err := validatePayload(r, v, b.platform); err != nil {
		return err
	}
	return validateSupport(r)
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
		return "", fmt.Errorf("current points outside ATM spotatui versions; refusing mutation")
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
			return fmt.Errorf("native payload contains a symlink")
		case info.Mode().IsRegular():
			if info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
				return fmt.Errorf("special permission bits")
			}
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
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 8<<20 {
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
	if rec.Schema != 1 || rec.Plugin != "spotatui" || rec.Artifact.Version != v || len(rec.Files) == 0 {
		return fmt.Errorf("invalid spotatui ownership receipt")
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
