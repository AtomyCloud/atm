package main

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func safePath(s string) bool {
	if !fs.ValidPath(s) || s == "." || len(s) > 1024 || strings.Count(s, "/") > 48 || strings.ContainsAny(s, "\\:\x00\r\n") {
		return false
	}
	for _, r := range s {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}
func resolveLink(name string, links map[string]string) (string, error) {
	for depth := 0; depth < 32; depth++ {
		changed := false
		parts := strings.Split(name, "/")
		for i := range parts {
			prefix := strings.Join(parts[:i+1], "/")
			if link, ok := links[prefix]; ok {
				name = path.Join(link, strings.Join(parts[i+1:], "/"))
				if !safePath(name) {
					return "", fmt.Errorf("link escapes package")
				}
				changed = true
				break
			}
		}
		if !changed {
			return name, nil
		}
	}
	return "", fmt.Errorf("cyclic/excessive archive links")
}
func extractZIP(ctx context.Context, archive string, dest *os.Root) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size() > maxArchive {
		return fmt.Errorf("archive limit")
	}
	z, err := zip.NewReader(f, info.Size())
	if err != nil {
		return err
	}
	if len(z.File) == 0 || len(z.File) > maxEntries {
		return fmt.Errorf("archive entry limit")
	}
	// Google BasicInstaller layouts contain source.properties either at the ZIP
	// root or below exactly one wrapping directory. Reject ambiguous layouts.
	prefix := ""
	found := false
	for _, entry := range z.File {
		if entry.Name == "source.properties" || (strings.Count(entry.Name, "/") == 1 && path.Base(entry.Name) == "source.properties") {
			if found {
				return fmt.Errorf("ambiguous SDK archive layout")
			}
			found = true
			if entry.Name != "source.properties" {
				prefix = path.Dir(entry.Name) + "/"
			}
		}
	}
	if !found {
		return fmt.Errorf("SDK archive has no root source.properties")
	}
	seen := map[string]bool{}
	links := map[string]string{}
	var total int64
	for _, entry := range z.File {
		if err = ctx.Err(); err != nil {
			return err
		}
		raw := strings.TrimSuffix(entry.Name, "/")
		if !safePath(raw) {
			return fmt.Errorf("unsafe ZIP path")
		}
		if prefix != "" && raw == strings.TrimSuffix(prefix, "/") && entry.Mode().IsDir() {
			continue
		}
		name, ok := strings.CutPrefix(raw, prefix)
		if !ok || !safePath(name) || name == receiptName || strings.HasPrefix(name, receiptName+"/") || seen[name] {
			return fmt.Errorf("outside/duplicate/reserved ZIP path")
		}
		seen[name] = true
		if entry.UncompressedSize64 > uint64(maxFile) || entry.UncompressedSize64 > uint64(maxExpanded-total) {
			return fmt.Errorf("ZIP expanded size limit")
		}
		size := int64(entry.UncompressedSize64)
		total += size
		mode := entry.Mode()
		if mode&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
			return fmt.Errorf("special ZIP permissions refused")
		}
		if mode.IsDir() {
			if err = dest.MkdirAll(name, 0700); err != nil {
				return err
			}
			continue
		}
		if !mode.IsRegular() && mode&os.ModeSymlink == 0 {
			return fmt.Errorf("special ZIP entry refused")
		}
		in, err := entry.Open()
		if err != nil {
			return err
		}
		if mode&os.ModeSymlink != 0 {
			if size > 1024 {
				in.Close()
				return fmt.Errorf("oversized ZIP link")
			}
			data, e := io.ReadAll(io.LimitReader(contextReader{ctx, in}, 1025))
			in.Close()
			if e != nil {
				return e
			}
			target := string(data)
			if path.IsAbs(target) || strings.ContainsAny(target, "\\:\x00\r\n") {
				return fmt.Errorf("unsafe ZIP link")
			}
			target = path.Clean(path.Join(path.Dir(name), target))
			if !safePath(target) {
				return fmt.Errorf("ZIP link escapes package")
			}
			links[name] = target
			continue
		}
		if err = dest.MkdirAll(path.Dir(name), 0700); err != nil {
			in.Close()
			return err
		}
		out, err := dest.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			in.Close()
			return err
		}
		n, copyErr := io.Copy(out, io.LimitReader(contextReader{ctx, in}, size+1))
		in.Close()
		if copyErr == nil && n != size {
			copyErr = fmt.Errorf("ZIP entry size mismatch")
		}
		perm := os.FileMode(0600)
		if mode.Perm()&0111 != 0 {
			perm = 0700
		}
		if copyErr == nil {
			copyErr = out.Chmod(perm)
		}
		if copyErr == nil {
			copyErr = out.Sync()
		}
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	for name := range seen {
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if _, ok := links[parent]; ok {
				return fmt.Errorf("ZIP entry has link ancestor")
			}
		}
	}
	resolved := map[string]string{}
	for name, target := range links {
		final, e := resolveLink(target, links)
		if e != nil {
			return e
		}
		i, e := dest.Lstat(final)
		if e != nil || (!i.Mode().IsRegular() && !i.IsDir()) {
			return fmt.Errorf("ZIP link target must exist in package")
		}
		if _, e = dest.Lstat(name); !os.IsNotExist(e) {
			return fmt.Errorf("ZIP link conflicts with extracted path")
		}
		resolved[name] = final
	}
	for name, target := range resolved {
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = dest.MkdirAll(path.Dir(name), 0700); err != nil {
			return err
		}
		relative, e := filepath.Rel(path.Dir(name), target)
		if e != nil {
			return e
		}
		if err = dest.Symlink(relative, name); err != nil {
			return err
		}
	}
	return ctx.Err()
}
