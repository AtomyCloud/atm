//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

const maxProfile = 1 << 20

type pathState struct {
	Exists bool
	Mode   os.FileMode
	Data   []byte
	Link   string
	info   os.FileInfo
}
type pathChange struct {
	Path          string
	Before, After pathState
	AllowLink     bool
}

func singleLink(i os.FileInfo) bool {
	s, ok := i.Sys().(*syscall.Stat_t)
	return ok && s.Nlink == 1
}
func pathRegular(data []byte) pathState {
	return pathState{Exists: true, Mode: 0600, Data: data}
}
func pathSnapshot(path string, allowLink bool) (pathState, error) {
	s := pathState{}
	if err := dirs(filepath.Dir(path), false, false); err != nil {
		return s, err
	}
	i, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if owner(i) != uint32(os.Geteuid()) || !singleLink(i) {
		return s, fmt.Errorf("foreign/hardlinked PATH file preserved: %s", path)
	}
	s.Exists, s.Mode, s.info = true, i.Mode(), i
	if allowLink && i.Mode()&os.ModeSymlink != 0 {
		s.Link, err = os.Readlink(path)
		return s, err
	}
	if !i.Mode().IsRegular() || i.Size() > maxProfile || i.Mode().Perm()&0022 != 0 || i.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return s, fmt.Errorf("unsafe/oversized PATH file preserved: %s", path)
	}
	f, err := regular(path, i.Mode().Perm(), maxProfile)
	if err != nil {
		return s, err
	}
	defer f.Close()
	j, err := f.Stat()
	if err != nil || !os.SameFile(i, j) {
		return s, fmt.Errorf("PATH file changed while opening")
	}
	s.Data, err = io.ReadAll(io.LimitReader(f, maxProfile+1))
	if err != nil {
		return s, err
	}
	if len(s.Data) > maxProfile {
		return s, fmt.Errorf("PATH file grew beyond limit")
	}
	j, err = f.Stat()
	if err != nil || !singleLink(j) || j.Mode() != i.Mode() || j.Size() != i.Size() || !j.ModTime().Equal(i.ModTime()) {
		return s, fmt.Errorf("PATH file changed while reading")
	}
	return s, nil
}
func samePathState(a, b pathState) bool {
	return a.Exists == b.Exists && a.Mode == b.Mode && a.Link == b.Link && bytes.Equal(a.Data, b.Data)
}
func pathUnchanged(now, before pathState) bool {
	return samePathState(now, before) && (before.info == nil || (now.info != nil && os.SameFile(now.info, before.info)))
}

// Adapted from the REA change helper: exact modes, no-follow/hardlink guards,
// no-replace creation, and a synced before/after journal for uncertain restore.
// Directory entries are not fsynced; this is not a power-loss durability claim.
func replacePath(c pathChange) error {
	now, err := pathSnapshot(c.Path, c.AllowLink)
	if err != nil {
		return err
	}
	if !pathUnchanged(now, c.Before) {
		return fmt.Errorf("PATH entry changed before mutation: %s", c.Path)
	}
	if !c.After.Exists {
		if now.Exists {
			return os.Remove(c.Path)
		}
		return nil
	}
	stage, err := os.MkdirTemp(filepath.Dir(c.Path), ".atm-ai-path-write-")
	if err != nil {
		return err
	}
	entry := filepath.Join(stage, "entry")
	defer os.Remove(stage)
	defer os.Remove(entry)
	if c.After.Link != "" {
		err = os.Symlink(c.After.Link, entry)
	} else {
		f, e := os.OpenFile(entry, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		_, err = f.Write(c.After.Data)
		if err == nil {
			err = f.Chmod(c.After.Mode.Perm())
		}
		if err == nil {
			err = f.Sync()
		}
		e = f.Close()
		if err == nil {
			err = e
		}
	}
	if err != nil {
		return err
	}
	now, err = pathSnapshot(c.Path, c.AllowLink)
	if err != nil {
		return err
	}
	if !pathUnchanged(now, c.Before) {
		return fmt.Errorf("PATH entry changed before publication: %s", c.Path)
	}
	if !now.Exists {
		return unix.Renameat2(unix.AT_FDCWD, entry, unix.AT_FDCWD, c.Path, unix.RENAME_NOREPLACE)
	}
	return os.Rename(entry, c.Path)
}

func (b *backend) applyPathChanges(ctx context.Context, lock string, changes []pathChange, before, after func() error) (retain bool, err error) {
	var pending []pathChange
	for _, c := range changes {
		if !samePathState(c.Before, c.After) {
			pending = append(pending, c)
		}
	}
	if len(pending) == 0 {
		return false, before()
	}
	journal := filepath.Join(lock, "journal.json")
	f, err := os.OpenFile(journal, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return true, err
	}
	err = json.NewEncoder(f).Encode(struct {
		Schema  int
		Changes []pathChange
	}{1, pending})
	if err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err == nil {
		err = ce
	}
	if err != nil {
		return true, fmt.Errorf("PATH journal incomplete; retained at %s: %w", lock, err)
	}
	var applied []pathChange
	fail := func(cause error) (bool, error) {
		var restore error
		for i := len(applied) - 1; i >= 0; i-- {
			c := applied[i]
			if e := replacePath(pathChange{Path: c.Path, Before: c.After, After: c.Before, AllowLink: c.AllowLink}); e != nil && restore == nil {
				restore = e
			}
		}
		if restore != nil {
			return true, fmt.Errorf("%w; restoration incomplete: %v; backups/journal retained at %s", cause, restore, lock)
		}
		if e := os.Remove(journal); e != nil {
			return true, fmt.Errorf("%w; restored writes but journal cleanup failed at %s: %v", cause, lock, e)
		}
		return false, cause
	}
	if err = before(); err != nil {
		return fail(err)
	}
	// Preflight all destinations again before the first write, not one at a time.
	for _, c := range pending {
		now, e := pathSnapshot(c.Path, c.AllowLink)
		if e != nil {
			return fail(e)
		}
		if !pathUnchanged(now, c.Before) {
			return fail(fmt.Errorf("PATH preflight changed: %s", c.Path))
		}
	}
	for _, c := range pending {
		if err = ctx.Err(); err != nil {
			return fail(err)
		}
		if err = replacePath(c); err != nil {
			return fail(err)
		}
		applied = append(applied, c)
		now, e := pathSnapshot(c.Path, c.AllowLink)
		if e != nil || !samePathState(now, c.After) {
			return fail(fmt.Errorf("published PATH entry changed: %s: %v", c.Path, e))
		}
		applied[len(applied)-1].After = now
		if err = b.boundary("path-published-" + filepath.Base(c.Path)); err != nil {
			return fail(err)
		}
	}
	if err = ctx.Err(); err != nil {
		return fail(err)
	}
	if err = after(); err != nil {
		return fail(err)
	}
	// Revalidate the whole published write set, not just the last file. Earlier
	// writes may have changed while later profiles were being published.
	for _, c := range applied {
		now, e := pathSnapshot(c.Path, c.AllowLink)
		if e != nil {
			return fail(e)
		}
		if !pathUnchanged(now, c.After) {
			return fail(fmt.Errorf("published PATH state changed before completion: %s", c.Path))
		}
	}
	if err = os.Remove(journal); err != nil {
		return true, fmt.Errorf("PATH writes published; journal cleanup failed, inspect %s: %w", lock, err)
	}
	return false, nil
}
