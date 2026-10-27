package install

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

type File struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Mode   uint32 `json:"mode"`
}

type extractor struct {
	ctx      context.Context
	root     *os.Root
	prefix   string
	limits   Limits
	entries  int
	expanded int64
	seen     map[string]string
	explicit map[string]bool
	files    map[string]File
}

func extract(ctx context.Context, store *os.Root, archive, dest string, s Spec, l Limits) (map[string]File, error) {
	root, err := store.OpenRoot(dest)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := store.Open(archive)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	x := extractor{ctx: ctx, root: root, prefix: s.StripPrefix, limits: l, seen: map[string]string{}, explicit: map[string]bool{}, files: map[string]File{}}
	if s.Format == "zip" {
		info, err := f.Stat()
		if err != nil {
			return nil, err
		}
		z, err := zip.NewReader(f, info.Size())
		if err != nil {
			return nil, err
		}
		if len(z.File) > l.Entries {
			return nil, fmt.Errorf("too many archive entries")
		}
		for _, entry := range z.File {
			if entry.UncompressedSize64 > uint64(l.FileBytes) {
				return nil, fmt.Errorf("archive file too large")
			}
			if !entry.Mode().IsRegular() && !entry.Mode().IsDir() {
				return nil, fmt.Errorf("archive links/special files refused")
			}
			r, err := entry.Open()
			if err != nil {
				return nil, err
			}
			err = x.entry(entry.Name, entry.Mode(), int64(entry.UncompressedSize64), r)
			closeErr := r.Close()
			if err != nil {
				return nil, err
			}
			if closeErr != nil {
				return nil, closeErr
			}
		}
	} else {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		// Bound headers/padding too, not just declared file sizes.
		budget := l.ExpandedBytes + int64(l.Entries)*4096 + 1024
		bounded := &io.LimitedReader{R: contextReader{ctx, gz}, N: budget + 1}
		tr := tar.NewReader(bounded)
		for {
			h, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
			if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA && h.Typeflag != tar.TypeDir {
				return nil, fmt.Errorf("archive links/special files refused")
			}
			if len(h.PAXRecords) != 0 {
				return nil, fmt.Errorf("PAX extensions are not enabled")
			}
			if err = x.entry(h.Name, h.FileInfo().Mode(), h.Size, tr); err != nil {
				return nil, err
			}
		}
		// Consume the trailer so truncated gzip and corrupt checksums cannot pass.
		if _, err = io.Copy(io.Discard, bounded); err != nil {
			return nil, err
		}
		if bounded.N <= 0 {
			return nil, fmt.Errorf("expanded archive exceeds limit")
		}
	}
	if len(x.files) == 0 {
		return nil, fmt.Errorf("archive contains no files")
	}
	return x.files, ctx.Err()
}

func (x *extractor) entry(name string, mode os.FileMode, size int64, src io.Reader) error {
	if err := x.ctx.Err(); err != nil {
		return err
	}
	x.entries++
	if x.entries > x.limits.Entries {
		return fmt.Errorf("too many archive entries")
	}
	if mode.IsDir() {
		name = strings.TrimSuffix(name, "/")
	}
	if !portablePath(name) || size < 0 || size > x.limits.FileBytes {
		return fmt.Errorf("unsafe or oversized archive entry: %q", name)
	}
	if x.prefix != "" {
		if name == x.prefix && mode.IsDir() {
			return nil
		}
		var ok bool
		name, ok = strings.CutPrefix(name, x.prefix+"/")
		if !ok || !portablePath(name) {
			return fmt.Errorf("entry outside expected archive prefix")
		}
	}
	if x.explicit[strings.ToLower(name)] {
		return fmt.Errorf("duplicate archive entry")
	}
	x.explicit[strings.ToLower(name)] = true
	parts := strings.Split(name, "/")
	for i := range parts {
		prefix := strings.Join(parts[:i+1], "/")
		key := strings.ToLower(prefix)
		kind := "d:"
		if i == len(parts)-1 && !mode.IsDir() {
			kind = "f:"
		}
		if previous, ok := x.seen[key]; ok && previous != kind+prefix {
			return fmt.Errorf("archive path/type/case collision")
		}
		x.seen[key] = kind + prefix
	}
	if mode.IsDir() {
		return x.root.MkdirAll(name, 0700)
	}
	if size > x.limits.ExpandedBytes-x.expanded {
		return fmt.Errorf("expanded archive exceeds limit")
	}
	x.expanded += size
	if err := x.root.MkdirAll(path.Dir(name), 0700); err != nil {
		return err
	}
	f, err := x.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(f, h), io.LimitReader(contextReader{x.ctx, src}, size+1))
	perm := os.FileMode(0600)
	if mode.Perm()&0111 != 0 {
		perm = 0700
	}
	if copyErr == nil {
		copyErr = f.Chmod(perm)
	}
	if copyErr == nil {
		copyErr = f.Sync()
	}
	info, statErr := f.Stat()
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	if statErr != nil {
		return statErr
	}
	if closeErr != nil {
		return closeErr
	}
	if n != size {
		return fmt.Errorf("archive size mismatch")
	}
	x.files[name] = File{SHA256: hex.EncodeToString(h.Sum(nil)), Size: n, Mode: uint32(info.Mode().Perm())}
	return nil
}
