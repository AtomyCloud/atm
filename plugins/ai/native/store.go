//go:build linux

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

const maxBinary = int64(512 << 20)

type ownership struct {
	Schema           int
	Plugin, Provider string
}
type receipt struct {
	Schema       int
	Plugin       string
	Artifact     artifact
	BinarySHA256 string
	BinarySize   int64
}

func absolutePath(p string) error {
	if !filepath.IsAbs(p) || filepath.Clean(p) != p || p == "/" || len(p) > 2000 {
		return fmt.Errorf("dedicated clean absolute path required")
	}
	for _, r := range p {
		if r < 32 || r == 127 || r == ':' {
			return fmt.Errorf("unsafe path")
		}
	}
	return nil
}
func owner(i os.FileInfo) uint32 {
	s, ok := i.Sys().(*syscall.Stat_t)
	if !ok {
		return ^uint32(0)
	}
	return s.Uid
}
func dirs(path string, create, private bool) error {
	if err := absolutePath(path); err != nil {
		return err
	}
	p := "/"
	for _, part := range strings.Split(path, "/") {
		if part == "" {
			continue
		}
		p = filepath.Join(p, part)
		i, err := os.Lstat(p)
		if os.IsNotExist(err) && create {
			if err = os.Mkdir(p, 0700); err != nil {
				return err
			}
			i, err = os.Lstat(p)
		}
		if err != nil {
			return err
		}
		if !i.IsDir() || i.Mode()&os.ModeSymlink != 0 || (owner(i) != 0 && owner(i) != uint32(os.Geteuid())) {
			return fmt.Errorf("untrusted directory: %s", p)
		}
		if i.Mode().Perm()&0022 != 0 && !(owner(i) == 0 && i.Mode()&os.ModeSticky != 0 && p != path) {
			return fmt.Errorf("writable ancestor refused: %s", p)
		}
		if p == path && private && (owner(i) != uint32(os.Geteuid()) || i.Mode().Perm() != 0700) {
			return fmt.Errorf("private owned directory (0700) required: %s", p)
		}
	}
	return nil
}
func regular(path string, mode os.FileMode, limit int64) (*os.File, error) {
	if err := dirs(filepath.Dir(path), false, false); err != nil {
		return nil, err
	}
	i, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !i.Mode().IsRegular() || i.Mode().Perm() != mode || i.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 || owner(i) != uint32(os.Geteuid()) || i.Size() > limit || !singleLink(i) {
		return nil, fmt.Errorf("unsafe regular file: %s", path)
	}
	f, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	j, err := f.Stat()
	if err != nil || !os.SameFile(i, j) {
		f.Close()
		return nil, fmt.Errorf("file changed while opening")
	}
	return f, nil
}
func readJSON(path string, out any) error {
	f, err := regular(path, 0600, 64<<10)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (64<<10)+1))
	if err != nil {
		return err
	}
	if len(data) > 64<<10 {
		return fmt.Errorf("state size exceeded")
	}
	return decode(data, out, true)
}
func writeJSON(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(data) > 64<<10 {
		return fmt.Errorf("state size exceeded")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	if err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err != nil {
		return err
	}
	return ce
}
func entries(path string, max int) ([]os.DirEntry, error) {
	if err := dirs(path, false, true); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	es, err := f.ReadDir(max + 1)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if len(es) > max {
		return nil, fmt.Errorf("directory entry bound exceeded")
	}
	return es, nil
}
func (b *backend) current(id string) (string, error) {
	p := filepath.Join(b.root(id), "current")
	target, err := os.Readlink(p)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("foreign active entry preserved")
	}
	v := strings.TrimSuffix(strings.TrimPrefix(target, "versions/"), "/tool")
	if target != "versions/"+v+"/tool" || !versionRE.MatchString(v) {
		return "", fmt.Errorf("foreign active link preserved")
	}
	return v, nil
}
func (b *backend) inspect(ctx context.Context, id string, verify bool) ([]string, string, error) {
	root := b.root(id)
	if err := dirs(root, false, true); os.IsNotExist(err) {
		return nil, "", nil
	} else if err != nil {
		return nil, "", err
	}
	var own ownership
	if err := readJSON(filepath.Join(root, "owner.json"), &own); err != nil {
		return nil, "", err
	}
	if own != (ownership{1, "ai", id}) {
		return nil, "", fmt.Errorf("foreign preview root; no adoption")
	}
	es, err := entries(root, 5)
	if err != nil {
		return nil, "", err
	}
	for _, e := range es {
		if e.Name() != "owner.json" && e.Name() != "versions" && e.Name() != "current" && !(cliCommand(id) != "" && (e.Name() == "bin" || e.Name() == "path.json")) {
			return nil, "", fmt.Errorf("unknown preview entry preserved")
		}
	}
	es, err = entries(filepath.Join(root, "versions"), 1024)
	if err != nil {
		return nil, "", err
	}
	var vs []string
	for _, e := range es {
		if err = ctx.Err(); err != nil {
			return nil, "", err
		}
		if !e.IsDir() || !versionRE.MatchString(e.Name()) {
			return nil, "", fmt.Errorf("unknown version entry preserved")
		}
		if _, err = b.verifyVersion(ctx, id, e.Name(), verify); err != nil {
			return nil, "", err
		}
		vs = append(vs, e.Name())
	}
	sort.Strings(vs)
	current, err := b.current(id)
	if err != nil {
		return nil, "", err
	}
	if current != "" {
		found := false
		for _, v := range vs {
			found = found || v == current
		}
		if !found {
			return nil, "", fmt.Errorf("active version is missing")
		}
	}
	if _, err := b.inspectIntegration(id, current); err != nil {
		return nil, "", err
	}
	return vs, current, nil
}
func (b *backend) verifyVersion(ctx context.Context, id, v string, hash bool) (receipt, error) {
	return b.verifyVersionAt(ctx, id, v, filepath.Join(b.root(id), "versions", v), hash)
}
func (b *backend) verifyVersionAt(ctx context.Context, id, v, dir string, hash bool) (receipt, error) {
	var rec receipt
	if !versionRE.MatchString(v) {
		return rec, fmt.Errorf("invalid version")
	}
	es, err := entries(dir, 2)
	if err != nil {
		return rec, err
	}
	if len(es) != 2 {
		return rec, fmt.Errorf("version contains missing/unknown files")
	}
	for _, e := range es {
		if e.Name() != "receipt.json" && e.Name() != "tool" {
			return rec, fmt.Errorf("unknown version file preserved")
		}
	}
	if err = readJSON(filepath.Join(dir, "receipt.json"), &rec); err != nil {
		return rec, err
	}
	if rec.Schema != 1 || rec.Plugin != "ai" || rec.Artifact.Provider != id || rec.Artifact.Version != v || rec.Artifact.Arch != b.arch || rec.Artifact.validate() != nil || !validSHA(rec.BinarySHA256) || rec.BinarySize <= 0 || rec.BinarySize > maxBinary {
		return rec, fmt.Errorf("invalid AI receipt")
	}
	f, err := regular(filepath.Join(dir, "tool"), 0755, maxBinary)
	if err != nil {
		return rec, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return rec, err
	}
	if info.Size() != rec.BinarySize {
		return rec, fmt.Errorf("binary size changed; preserving")
	}
	if hash {
		h := sha256.New()
		n, e := io.Copy(h, io.LimitReader(contextReader{ctx, f}, rec.BinarySize+1))
		if e != nil {
			return rec, e
		}
		if n != rec.BinarySize || hex.EncodeToString(h.Sum(nil)) != rec.BinarySHA256 {
			return rec, fmt.Errorf("binary changed; preserving")
		}
	}
	return rec, nil
}
func (b *backend) locked(ctx context.Context, id string, fn func() error) error {
	if _, _, err := b.inspect(ctx, id, true); err != nil {
		return err
	}
	root := b.root(id)
	if b.req.Operation != "install" {
		if _, err := os.Lstat(root); err != nil {
			return fmt.Errorf("no owned preview installation; manual/legacy files preserved")
		}
	}
	if err := dirs(filepath.Dir(root), true, false); err != nil {
		return err
	}
	lock := root + ".lock"
	if err := os.Mkdir(lock, 0700); err != nil {
		return fmt.Errorf("AI preview locked/interrupted; inspect %s: %w", lock, err)
	}
	defer os.Remove(lock)
	if _, _, err := b.inspect(ctx, id, true); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn()
}
func (b *backend) initialize(id string) error {
	root := b.root(id)
	if _, err := os.Lstat(root); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Mkdir(root, 0700); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(root, "owner.json"), ownership{1, "ai", id}); err != nil {
		return err
	}
	return os.Mkdir(filepath.Join(root, "versions"), 0700)
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}
func validateELF(path, arch string, appimage bool) error {
	f, err := regular(path, 0755, maxBinary)
	if err != nil {
		return err
	}
	defer f.Close()
	ef, err := elf.NewFile(f)
	if err != nil {
		return fmt.Errorf("expected ELF executable: %w", err)
	}
	machine := elf.EM_X86_64
	if arch == "arm64" {
		machine = elf.EM_AARCH64
	}
	if ef.Class != elf.ELFCLASS64 || ef.Data != elf.ELFDATA2LSB || ef.Machine != machine || (ef.Type != elf.ET_EXEC && ef.Type != elf.ET_DYN) {
		return fmt.Errorf("ELF architecture/type mismatch")
	}
	if appimage {
		var magic [3]byte
		if _, err = f.ReadAt(magic[:], 8); err != nil || !bytes.Equal(magic[:], []byte{'A', 'I', 2}) {
			return fmt.Errorf("expected type-2 AppImage")
		}
	}
	return nil
}
