package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func absolutePath(p string) error {
	if !filepath.IsAbs(p) || filepath.Clean(p) != p || p == "/" || len(p) > 3000 {
		return fmt.Errorf("clean bounded absolute path required")
	}
	for _, r := range p {
		if r < 32 || r == 127 {
			return fmt.Errorf("control character in path")
		}
	}
	return nil
}
func overlaps(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

// Refuse replaceable ancestry. Sticky, root-owned /tmp is allowed only above the leaf.
func realDirs(path string, create, private bool) error {
	if err := absolutePath(path); err != nil {
		return err
	}
	current := "/"
	for _, part := range strings.Split(path, "/") {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		i, e := os.Lstat(current)
		if os.IsNotExist(e) && create {
			if e = os.Mkdir(current, 0700); e != nil {
				return e
			}
			i, e = os.Lstat(current)
		}
		if e != nil {
			return e
		}
		if !i.IsDir() || i.Mode()&os.ModeSymlink != 0 || (!ownedByCurrentUser(i) && !rootOwned(i)) {
			return fmt.Errorf("untrusted directory: %s", current)
		}
		if i.Mode().Perm()&0022 != 0 && !(rootOwned(i) && i.Mode()&os.ModeSticky != 0 && current != path) {
			return fmt.Errorf("writable directory refused: %s", current)
		}
		if current == path && private && (!ownedByCurrentUser(i) || i.Mode().Perm() != 0700) {
			return fmt.Errorf("private current-user directory required: %s", current)
		}
	}
	return nil
}
func openDir(path string) (*os.Root, error) {
	if err := realDirs(path, false, false); err != nil {
		return nil, err
	}
	return os.OpenRoot(path)
}
func regular(r *os.Root, name string, limit int64) (*os.File, error) {
	i, e := r.Lstat(name)
	if e != nil {
		return nil, e
	}
	if !i.Mode().IsRegular() || i.Size() < 0 || i.Size() > limit || i.Mode()&(os.ModeSetuid|os.ModeSetgid) != 0 {
		return nil, fmt.Errorf("bounded regular file required: %s", name)
	}
	f, e := openNoFollow(r, name)
	if e != nil {
		return nil, e
	}
	after, e := f.Stat()
	if e != nil || !os.SameFile(i, after) || !after.Mode().IsRegular() {
		f.Close()
		return nil, fmt.Errorf("file changed while opening: %s", name)
	}
	return f, nil
}
func readRegular(path string, limit int64) ([]byte, error) {
	if e := absolutePath(path); e != nil {
		return nil, e
	}
	r, e := openDir(filepath.Dir(path))
	if e != nil {
		return nil, e
	}
	defer r.Close()
	f, e := regular(r, filepath.Base(path), limit)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, limit+1))
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("file limit exceeded")
	}
	return b, e
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if e := r.ctx.Err(); e != nil {
		return 0, e
	}
	return r.r.Read(p)
}
func syncDir(path string) error {
	r, e := openDir(path)
	if e != nil {
		return e
	}
	defer r.Close()
	f, e := r.Open(".")
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}
func renameAbsent(old, new string) error { return renameNoReplace(old, new) }
