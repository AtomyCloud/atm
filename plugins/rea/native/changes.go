package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
)

type fileState struct {
	Exists bool
	Mode   os.FileMode
	Data   []byte
	Link   string
}
type change struct {
	path          string
	before, after fileState
}

func snapshot(path string) (fileState, error) {
	s := fileState{}
	if err := realDirs(filepath.Dir(path), false); err != nil {
		return s, err
	}
	i, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	s.Exists = true
	s.Mode = i.Mode()
	if i.Mode()&os.ModeSymlink != 0 {
		s.Link, err = os.Readlink(path)
		return s, err
	}
	s.Data, err = readRegular(path, 32<<20)
	return s, err
}
func regularState(data []byte) fileState { return fileState{Exists: true, Mode: 0600, Data: data} }
func linkState(target string) fileState {
	return fileState{Exists: true, Mode: os.ModeSymlink | 0777, Link: target}
}
func sameState(a, b fileState) bool {
	return a.Exists == b.Exists && a.Mode == b.Mode && a.Link == b.Link && bytes.Equal(a.Data, b.Data)
}
func planned(path string, after fileState) (change, error) {
	before, err := snapshot(path)
	return change{path, before, after}, err
}
func replace(c change) error {
	now, err := snapshot(c.path)
	if err != nil {
		return err
	}
	if !sameState(now, c.before) {
		return fmt.Errorf("entry changed before mutation: %s", c.path)
	}
	if !c.after.Exists {
		if !now.Exists {
			return nil
		}
		return os.Remove(c.path)
	}
	temp, err := os.MkdirTemp(filepath.Dir(c.path), ".atm-rea-write-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	p := filepath.Join(temp, "entry")
	if c.after.Link != "" {
		err = os.Symlink(c.after.Link, p)
	} else {
		f, e := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, c.after.Mode.Perm())
		if e != nil {
			return e
		}
		_, err = f.Write(c.after.Data)
		if err == nil {
			err = f.Chmod(c.after.Mode.Perm())
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
	now, err = snapshot(c.path)
	if err != nil {
		return err
	}
	if !sameState(now, c.before) {
		return fmt.Errorf("entry changed before publication")
	}
	if !now.Exists {
		if c.after.Link != "" {
			return os.Symlink(c.after.Link, c.path)
		}
		return os.Link(p, c.path)
	}
	return os.Rename(p, c.path)
}
func applyChanges(ctx context.Context, changes []change) (func() error, error) {
	var applied []change
	undo := func() error {
		for i := len(applied) - 1; i >= 0; i-- {
			c := applied[i]
			if err := replace(change{c.path, c.after, c.before}); err != nil {
				return err
			}
		}
		return nil
	}
	fail := func(err error) (func() error, error) {
		if e := undo(); e != nil {
			return nil, fmt.Errorf("%v; rollback failed: %w", err, e)
		}
		return nil, err
	}
	for _, c := range changes {
		if reflect.DeepEqual(c.before, c.after) {
			continue
		}
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		if err := replace(c); err != nil {
			return fail(err)
		}
		applied = append(applied, c)
	}
	return undo, nil
}
