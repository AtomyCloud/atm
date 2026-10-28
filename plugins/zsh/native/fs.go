//go:build linux

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

func digest(v []byte) string { h := sha256.Sum256(v); return hex.EncodeToString(h[:]) }
func validHex(s string, n int) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == n && s == strings.ToLower(s)
}
func absolutePath(p string) error {
	if !filepath.IsAbs(p) || filepath.Clean(p) != p || p == "/" || len(p) > 2000 {
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
func owner(i os.FileInfo) uint32 {
	s, ok := i.Sys().(*syscall.Stat_t)
	if !ok {
		return ^uint32(0)
	}
	return s.Uid
}
func owned(i os.FileInfo) bool { return owner(i) == uint32(os.Geteuid()) }
func dirs(path string, create, private bool) error {
	if e := absolutePath(path); e != nil {
		return e
	}
	p := "/"
	for _, part := range strings.Split(path, "/") {
		if part == "" {
			continue
		}
		p = filepath.Join(p, part)
		i, e := os.Lstat(p)
		if os.IsNotExist(e) && create {
			if e = os.Mkdir(p, 0700); e != nil {
				return e
			}
			i, e = os.Lstat(p)
		}
		if e != nil {
			return e
		}
		if !i.IsDir() || i.Mode()&os.ModeSymlink != 0 || (owner(i) != 0 && !owned(i)) {
			return fmt.Errorf("untrusted directory: %s", p)
		}
		if i.Mode().Perm()&0022 != 0 && !(owner(i) == 0 && i.Mode()&os.ModeSticky != 0 && p != path) {
			return fmt.Errorf("writable ancestor refused: %s", p)
		}
		if p == path && private && (!owned(i) || i.Mode().Perm() != 0700) {
			return fmt.Errorf("private owned directory (0700) required: %s", p)
		}
	}
	return nil
}
func readFile(path string, limit int64, private bool) ([]byte, error) {
	if e := absolutePath(path); e != nil {
		return nil, e
	}
	if e := dirs(filepath.Dir(path), false, false); e != nil {
		return nil, e
	}
	r, e := os.OpenRoot(filepath.Dir(path))
	if e != nil {
		return nil, e
	}
	defer r.Close()
	i, e := r.Lstat(filepath.Base(path))
	if e != nil {
		return nil, e
	}
	if !i.Mode().IsRegular() || i.Size() > limit || i.Mode()&(os.ModeSetuid|os.ModeSetgid) != 0 {
		return nil, fmt.Errorf("bounded regular file required: %s", path)
	}
	if private && (!owned(i) || i.Mode().Perm() != 0600) {
		return nil, fmt.Errorf("private owned file (0600) required: %s", path)
	}
	f, e := r.OpenFile(filepath.Base(path), os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	j, e := f.Stat()
	if e != nil || !os.SameFile(i, j) {
		return nil, fmt.Errorf("file changed during open")
	}
	v, e := io.ReadAll(io.LimitReader(f, limit+1))
	if int64(len(v)) > limit {
		return nil, fmt.Errorf("file exceeds bound")
	}
	return v, e
}
func writeNew(path string, v []byte, mode os.FileMode) error {
	if e := dirs(filepath.Dir(path), false, false); e != nil {
		return e
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY|unix.O_NOFOLLOW, mode)
	if e != nil {
		return e
	}
	if _, e = f.Write(v); e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	return ce
}
func renameAbsent(a, b string) error {
	return unix.Renameat2(unix.AT_FDCWD, a, unix.AT_FDCWD, b, unix.RENAME_NOREPLACE)
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

func trustedFile(path string, executable bool) error {
	if e := absolutePath(path); e != nil {
		return e
	}
	for p := path; ; p = filepath.Dir(p) {
		i, e := os.Lstat(p)
		if e != nil {
			return e
		}
		if owner(i) != 0 || i.Mode().Perm()&0022 != 0 || i.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("untrusted system path: %s", p)
		}
		if p == path {
			if !i.Mode().IsRegular() || (executable && i.Mode().Perm()&0111 == 0) {
				return fmt.Errorf("trusted regular system file required")
			}
		} else if !i.IsDir() {
			return fmt.Errorf("invalid system ancestor")
		}
		if p == "/" {
			return nil
		}
	}
}
func systemCommand(name string) (string, error) {
	switch name {
	case "apt-get", "sudo", "chsh":
	default:
		return "", fmt.Errorf("unapproved executable")
	}
	for _, dir := range []string{"/usr/bin", "/usr/sbin", "/bin", "/sbin"} {
		p := filepath.Join(dir, name)
		if _, e := os.Lstat(p); os.IsNotExist(e) {
			continue
		}
		p, e := filepath.EvalSymlinks(p)
		if e != nil {
			return "", e
		}
		if e = trustedFile(p, true); e != nil {
			return "", e
		}
		return p, nil
	}
	return "", fmt.Errorf("trusted %s unavailable", name)
}
