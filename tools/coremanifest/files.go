package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const maxReleaseBytes = 2 << 20
const maxScanBytes = (1 << 30) + 20000*4096 + 1024

// Only locate the exact release.json here. InspectPackage owns full validation.
func readRelease(ctx context.Context, f *os.File, size int64, root string, windows bool) ([]byte, error) {
	want := root + "/release.json"
	var release []byte
	read := func(name string, regular bool, size int64, r io.Reader) error {
		if name != want {
			return nil
		}
		if release != nil || !regular || size <= 0 || size > maxReleaseBytes {
			return fmt.Errorf("duplicate, unsafe or oversized release.json")
		}
		b, err := io.ReadAll(io.LimitReader(contextReader{ctx, r}, maxReleaseBytes+1))
		if err != nil {
			return err
		}
		if int64(len(b)) != size {
			return fmt.Errorf("release.json size mismatch")
		}
		release = b
		return nil
	}
	section := io.NewSectionReader(f, 0, size)
	if windows {
		z, err := zip.NewReader(section, size)
		if err != nil {
			return nil, err
		}
		if len(z.File) > 20000 {
			return nil, fmt.Errorf("too many archive entries")
		}
		for _, e := range z.File {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if e.Name != want {
				continue
			}
			if e.UncompressedSize64 > maxReleaseBytes {
				return nil, fmt.Errorf("oversized release.json")
			}
			r, err := e.Open()
			if err != nil {
				return nil, err
			}
			err = read(e.Name, e.Mode().IsRegular(), int64(e.UncompressedSize64), r)
			closeErr := r.Close()
			if err != nil || closeErr != nil {
				return nil, errors.Join(err, closeErr)
			}
		}
	} else {
		gz, err := gzip.NewReader(section)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		bounded := &io.LimitedReader{R: contextReader{ctx, gz}, N: maxScanBytes + 1}
		tr := tar.NewReader(bounded)
		for entries := 0; ; entries++ {
			h, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
			if entries >= 20000 {
				return nil, fmt.Errorf("too many archive entries")
			}
			if err = read(h.Name, h.Typeflag == tar.TypeReg || h.Typeflag == tar.TypeRegA, h.Size, tr); err != nil {
				return nil, err
			}
		}
		if _, err = io.Copy(io.Discard, bounded); err != nil {
			return nil, err
		}
		if bounded.N <= 0 {
			return nil, fmt.Errorf("expanded archive exceeds limit")
		}
	}
	if release == nil {
		return nil, fmt.Errorf("missing %s", want)
	}
	return release, ctx.Err()
}

func realDirectory(path string) error {
	for p := path; ; p = filepath.Dir(p) {
		i, err := os.Lstat(p)
		if err != nil {
			return err
		}
		if !i.IsDir() || i.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink or non-directory parent refused: %s", p)
		}
		if filepath.Dir(p) == p {
			return nil
		}
	}
}

func openRegular(path string, limit int64) (*os.File, os.FileInfo, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, nil, err
	}
	if err = realDirectory(filepath.Dir(path)); err != nil {
		return nil, nil, err
	}
	i, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if !i.Mode().IsRegular() || i.Size() <= 0 || i.Size() > limit || i.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return nil, nil, fmt.Errorf("archive must be a bounded regular file, without symlinks or special modes")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	after, err := f.Stat()
	if err == nil && (!os.SameFile(i, after) || after.Mode() != i.Mode() || after.Size() != i.Size()) {
		err = fmt.Errorf("archive changed while opening")
	}
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	return f, after, nil
}

type artifact struct {
	name string
	data []byte
}

func writeOutputs(ctx context.Context, out string, artifacts []artifact) (err error) {
	out, err = filepath.Abs(out)
	if err != nil {
		return err
	}
	if err = realDirectory(filepath.Dir(out)); err != nil {
		return err
	}
	parent, err := os.OpenRoot(filepath.Dir(out))
	if err != nil {
		return err
	}
	defer parent.Close()
	name := filepath.Base(out)
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = parent.Mkdir(name, 0700); err != nil {
		return fmt.Errorf("output must not already exist: %w", err)
	}
	dirInfo, err := parent.Lstat(name)
	if err != nil {
		return err
	}
	r, err := parent.OpenRoot(name)
	if err != nil {
		return errors.Join(err, parent.Remove(name))
	}
	defer r.Close()
	owned := map[string]os.FileInfo{}
	defer func() {
		if err == nil {
			return
		}
		// Never recursively delete output: preserve unrelated/replaced entries.
		for name, info := range owned {
			current, e := r.Lstat(name)
			if e == nil && os.SameFile(info, current) && current.Mode().IsRegular() {
				err = errors.Join(err, r.Remove(name))
			}
		}
		current, e := parent.Lstat(name)
		if e == nil && os.SameFile(dirInfo, current) && current.IsDir() {
			err = errors.Join(err, parent.Remove(name))
		}
	}()
	for _, a := range artifacts {
		if err = ctx.Err(); err != nil {
			return err
		}
		if a.name != "manifest.json" && a.name != "manifest.signing-input" {
			return fmt.Errorf("unexpected output name")
		}
		f, e := r.OpenFile(a.name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		i, e := f.Stat()
		if e != nil {
			f.Close()
			return e
		}
		owned[a.name] = i
		_, e = io.Copy(f, contextReader{ctx, bytes.NewReader(a.data)})
		if e == nil {
			e = f.Sync()
		}
		if e = errors.Join(e, f.Close()); e != nil {
			return e
		}
	}
	return ctx.Err()
}
