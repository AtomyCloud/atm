package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"time"
)

const dataBytes int64 = 16 << 30
const dataFileBytes int64 = 4 << 30
const dataEntries = 200000

type dataEntry struct {
	Mode     fs.FileMode
	Size     int64
	Hash     string
	Modified time.Time
}

// Closed editors only. Refuse links (including SingletonLock), sockets/devices
// and oversized trees instead of following paths outside the selected profile.
func dataSnapshot(ctx context.Context, dir string) (map[string]dataEntry, error) {
	if err := realDirs(dir, false); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	result := map[string]dataEntry{}
	var total int64
	err = fs.WalkDir(root.FS(), ".", func(name string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if name != "." && !safeArchivePath(name) {
			return fmt.Errorf("unsafe portable data path")
		}
		if len(result) >= dataEntries {
			return fmt.Errorf("portable data entry limit exceeded")
		}
		i, err := d.Info()
		if err != nil {
			return err
		}
		if i.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
			return fmt.Errorf("portable data contains special permissions: %s", name)
		}
		e := dataEntry{Mode: i.Mode(), Modified: i.ModTime()}
		if !i.IsDir() {
			if !i.Mode().IsRegular() {
				return fmt.Errorf("portable data contains links/special entries; close VS Code and inspect %s", name)
			}
			e.Size = i.Size()
			if e.Size > dataFileBytes || e.Size > dataBytes-total {
				return fmt.Errorf("portable data exceeds copy limit")
			}
			total += e.Size
			f, err := root.Open(name)
			if err != nil {
				return err
			}
			h := sha256.New()
			n, err := io.Copy(h, io.LimitReader(contextReader{ctx, f}, e.Size+1))
			f.Close()
			if err != nil {
				return err
			}
			if n != e.Size {
				return fmt.Errorf("portable data changed during snapshot")
			}
			e.Hash = hex.EncodeToString(h.Sum(nil))
		}
		result[name] = e
		return nil
	})
	return result, err
}
func copyData(ctx context.Context, src, dst string, snapshot map[string]dataEntry) error {
	in, err := os.OpenRoot(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenRoot(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	names := make([]string, 0, len(snapshot))
	for name := range snapshot {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err = ctx.Err(); err != nil {
			return err
		}
		e := snapshot[name]
		if name == "." {
			continue
		}
		info, err := in.Lstat(name)
		if err != nil {
			return err
		}
		if info.Mode() != e.Mode || !info.ModTime().Equal(e.Modified) {
			return fmt.Errorf("portable source changed during copy")
		}
		if e.Mode.IsDir() {
			if err = out.MkdirAll(name, 0700); err != nil {
				return err
			}
			continue
		}
		if err = out.MkdirAll(path.Dir(name), 0700); err != nil {
			return err
		}
		f, err := in.Open(name)
		if err != nil {
			return err
		}
		g, err := out.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			f.Close()
			return err
		}
		h := sha256.New()
		n, err := io.Copy(io.MultiWriter(g, h), io.LimitReader(contextReader{ctx, f}, e.Size+1))
		f.Close()
		if err == nil && (n != e.Size || hex.EncodeToString(h.Sum(nil)) != e.Hash) {
			err = fmt.Errorf("portable source changed during copy")
		}
		if err == nil {
			err = g.Chmod(e.Mode.Perm())
		}
		if err == nil {
			err = g.Sync()
		}
		closeErr := g.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if err = out.Chtimes(name, e.Modified, e.Modified); err != nil {
			return err
		}
	}
	// Restore directory permissions/timestamps last, after populating children.
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	for _, name := range names {
		e := snapshot[name]
		if e.Mode.IsDir() {
			if err = out.Chmod(name, e.Mode.Perm()); err != nil {
				return err
			}
			if err = out.Chtimes(name, e.Modified, e.Modified); err != nil {
				return err
			}
		}
	}
	return ctx.Err()
}
func (b *backend) dataBackupDir(v string) (string, error) {
	directory := filepath.Join(b.root, "preserved-data")
	if err := realDirs(directory, true); err != nil {
		return "", err
	}
	return os.MkdirTemp(directory, v+"-")
}
func (b *backend) migrateData(ctx context.Context, src, dst string) error {
	if src == dst {
		return fmt.Errorf("source and destination must differ")
	}
	if err := b.validInstalled(src); err != nil {
		return err
	}
	if err := b.validInstalled(dst); err != nil {
		return err
	}
	source := filepath.Join(b.root, src, "data")
	target := filepath.Join(b.root, dst, "data")
	snapshot, err := dataSnapshot(ctx, source)
	if err != nil {
		return err
	}
	var before map[string]dataEntry
	if _, err = os.Lstat(target); err == nil {
		before, err = dataSnapshot(ctx, target)
		if err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	stage, err := os.MkdirTemp(b.root, ".atm-data-stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	payload := filepath.Join(stage, "data")
	if err = os.Mkdir(payload, 0700); err != nil {
		return err
	}
	if err = copyData(ctx, source, payload, snapshot); err != nil {
		return err
	}
	copied, err := dataSnapshot(ctx, payload)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(snapshot, copied) {
		return fmt.Errorf("staged portable data differs from source")
	}
	now, err := dataSnapshot(ctx, source)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(snapshot, now) {
		return fmt.Errorf("source data changed before publication; close VS Code")
	}
	if before != nil {
		now, err = dataSnapshot(ctx, target)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(before, now) {
			return fmt.Errorf("destination data changed before publication; close VS Code")
		}
	} else {
		if _, err = os.Lstat(target); !os.IsNotExist(err) {
			return fmt.Errorf("destination appeared before publication")
		}
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	backup := ""
	if before != nil {
		backupDir, err := b.dataBackupDir(dst)
		if err != nil {
			return err
		}
		backup = filepath.Join(backupDir, "data")
		b.log("Previous destination data will be preserved at " + backup)
		if err = os.Rename(target, backup); err != nil {
			return err
		}
	}
	rollback := func(cause error) error {
		if backup != "" {
			if _, e := os.Lstat(target); !os.IsNotExist(e) {
				return fmt.Errorf("%v; destination appeared, retained backup at %s", cause, backup)
			}
			if e := os.Rename(backup, target); e != nil {
				return fmt.Errorf("%v; rollback failed; data preserved at %s: %w", cause, backup, e)
			}
		}
		return cause
	}
	if err = ctx.Err(); err != nil {
		return rollback(err)
	}
	// Rename only after the destination was verified absent. A crash between the
	// two renames leaves the old data in the logged retained backup, not deleted.
	if _, err = os.Lstat(target); !os.IsNotExist(err) {
		return rollback(fmt.Errorf("migration destination appeared"))
	}
	if err = os.Rename(payload, target); err != nil {
		return rollback(err)
	}
	b.log("Copied portable data from " + src + " to " + dst + "; source and previous destination preserved")
	return nil
}
