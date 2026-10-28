package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
)

const receiptName = ".atm-node-receipt.json"

func safeArchivePath(name string) bool {
	if !fs.ValidPath(name) || name == "." || len(name) > 1024 || strings.Count(name, "/") > 32 || strings.ContainsAny(name, "\\:\x00\r\n") {
		return false
	}
	for _, r := range name {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}

// Links are created only after all regular entries, and must target regular files.
// os.Root confines every write even if a path is replaced during extraction.
func extractArchive(ctx context.Context, archive string, dest *os.Root, prefix string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	bounded := &io.LimitedReader{R: contextReader{ctx, gz}, N: limits.ExpandedBytes + int64(limits.Entries)*4096 + 1025}
	tr := tar.NewReader(bounded)
	seen := map[string]bool{}
	var total int64
	count := 0
	type link struct {
		name, target string
		hard         bool
	}
	links := []link{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		count++
		if count > limits.Entries {
			return fmt.Errorf("archive entry limit exceeded")
		}
		name := strings.TrimSuffix(h.Name, "/")
		if !safeArchivePath(name) {
			return fmt.Errorf("unsafe archive path")
		}
		if name == prefix && h.Typeflag == tar.TypeDir {
			continue
		}
		name, ok := strings.CutPrefix(name, prefix+"/")
		if !ok || !safeArchivePath(name) || name == receiptName {
			return fmt.Errorf("archive outside expected root or reserved name")
		}
		if seen[name] {
			return fmt.Errorf("duplicate archive path: %s", name)
		}
		seen[name] = true
		if h.Size < 0 || h.Size > limits.FileBytes || h.Size > limits.ExpandedBytes-total {
			return fmt.Errorf("archive expanded size exceeded")
		}
		total += h.Size
		if h.Mode&07000 != 0 {
			return fmt.Errorf("special archive permissions refused")
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err = dest.MkdirAll(name, 0700); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err = dest.MkdirAll(path.Dir(name), 0700); err != nil {
				return err
			}
			out, err := dest.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				return err
			}
			n, copyErr := io.Copy(out, contextReader{ctx, tr})
			if copyErr == nil && n != h.Size {
				copyErr = fmt.Errorf("archive size mismatch")
			}
			mode := os.FileMode(0600)
			if h.Mode&0111 != 0 {
				mode = 0700
			}
			if copyErr == nil {
				copyErr = out.Chmod(mode)
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
		case tar.TypeSymlink, tar.TypeLink:
			target := h.Linkname
			if h.Typeflag == tar.TypeLink {
				var ok bool
				target, ok = strings.CutPrefix(target, prefix+"/")
				if !ok || !safeArchivePath(target) {
					return fmt.Errorf("unsafe archive hardlink")
				}
			} else {
				if path.IsAbs(target) || strings.ContainsAny(target, "\\:\x00\r\n") || len(target) > 1024 {
					return fmt.Errorf("unsafe archive symlink")
				}
				target = path.Clean(path.Join(path.Dir(name), target))
				if !safeArchivePath(target) {
					return fmt.Errorf("escaping archive symlink")
				}
			}
			links = append(links, link{name, target, h.Typeflag == tar.TypeLink})
		default:
			return fmt.Errorf("archive special entry refused")
		}
	}
	// Read through gzip trailers, including concatenated members, under the total budget.
	if _, err = io.Copy(io.Discard, bounded); err != nil {
		return err
	}
	if bounded.N <= 0 {
		return fmt.Errorf("expanded stream limit exceeded")
	}
	for _, l := range links {
		if err = ctx.Err(); err != nil {
			return err
		}
		info, err := dest.Lstat(l.target)
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("archive link target must be an existing regular file")
		}
		if err = dest.MkdirAll(path.Dir(l.name), 0700); err != nil {
			return err
		}
		if l.hard {
			err = dest.Link(l.target, l.name)
		} else {
			relative, relErr := relativeLink(path.Dir(l.name), l.target)
			if relErr != nil {
				return relErr
			}
			err = dest.Symlink(relative, l.name)
		}
		if err != nil {
			return err
		}
	}
	info, err := dest.Lstat("bin/node")
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return fmt.Errorf("archive lacks executable bin/node")
	}
	for _, name := range []string{"bin/npm", "bin/npx"} {
		info, err := dest.Stat(name)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
			return fmt.Errorf("archive lacks executable %s", name)
		}
	}
	return ctx.Err()
}
