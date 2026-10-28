package main

import (
	"archive/tar"
	"context"
	"fmt"
	"github.com/ulikunitz/xz"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const receiptName = ".atm-flutter-receipt.json"

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

type archiveLink struct {
	target string
	hard   bool
}

// Resolve link chains before creating any link. Expansion is bounded, and the
// final target must already exist in the rooted regular-file/directory tree.
func resolveLink(name string, links map[string]archiveLink) (string, error) {
	for depth := 0; depth < 32; depth++ {
		changed := false
		parts := strings.Split(name, "/")
		for i := range parts {
			prefix := strings.Join(parts[:i+1], "/")
			if link, ok := links[prefix]; ok {
				name = path.Join(link.target, strings.Join(parts[i+1:], "/"))
				if !safeArchivePath(name) {
					return "", fmt.Errorf("link escapes payload")
				}
				changed = true
				break
			}
		}
		if !changed {
			return name, nil
		}
	}
	return "", fmt.Errorf("archive link cycle or excessive chain")
}
func extractArchive(ctx context.Context, archive string, dest *os.Root, expectedPrefix string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = preflightXZ(ctx, f); err != nil {
		return err
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	gz, err := (xz.ReaderConfig{DictCap: 8 << 20, SingleStream: true}).NewReader(contextReader{ctx, f})
	if err != nil {
		return err
	}
	bounded := &io.LimitedReader{R: contextReader{ctx, gz}, N: limits.ExpandedBytes + int64(limits.Entries)*4096 + 1025}
	tr := tar.NewReader(bounded)
	prefix := expectedPrefix
	seen := map[string]bool{}
	links := map[string]archiveLink{}
	var total int64
	count := 0
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
		top, rest, hasSlash := strings.Cut(name, "/")
		if prefix != top {
			return fmt.Errorf("archive must have one Flutter root")
		}
		if !hasSlash {
			if h.Typeflag != tar.TypeDir {
				return fmt.Errorf("Flutter archive root must be a directory")
			}
			if seen["."] {
				return fmt.Errorf("duplicate archive root")
			}
			seen["."] = true
			continue
		}
		name = rest
		if !safeArchivePath(name) || name == receiptName || seen[name] {
			return fmt.Errorf("unsafe/reserved/duplicate archive entry")
		}
		seen[name] = true
		if h.Size < 0 || h.Size > limits.FileBytes || h.Size > limits.ExpandedBytes-total {
			return fmt.Errorf("expanded archive budget exceeded")
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
			if path.IsAbs(target) || len(target) > 1024 || strings.ContainsAny(target, "\\:\x00\r\n") {
				return fmt.Errorf("unsafe archive link")
			}
			if h.Typeflag == tar.TypeLink {
				var ok bool
				target, ok = strings.CutPrefix(target, prefix+"/")
				if !ok || !safeArchivePath(target) {
					return fmt.Errorf("hardlink outside Flutter root")
				}
			} else {
				target = path.Clean(path.Join(path.Dir(name), target))
			}
			if !safeArchivePath(target) {
				return fmt.Errorf("archive link escapes root")
			}
			links[name] = archiveLink{target, h.Typeflag == tar.TypeLink}
		default:
			return fmt.Errorf("archive special file refused")
		}
	}
	if _, err = io.Copy(io.Discard, bounded); err != nil {
		return err
	}
	if bounded.N <= 0 {
		return fmt.Errorf("expanded stream budget exceeded")
	}
	resolved := map[string]string{}
	for name := range seen {
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if _, ok := links[parent]; ok {
				return fmt.Errorf("archive entry has a link ancestor")
			}
		}
	}
	for name, l := range links {
		final, err := resolveLink(l.target, links)
		if err != nil {
			return err
		}
		info, err := dest.Lstat(final)
		if err != nil || (!info.Mode().IsRegular() && !info.IsDir()) || (l.hard && !info.Mode().IsRegular()) {
			return fmt.Errorf("archive link target must exist inside payload")
		}
		resolved[name] = final
		// A link cannot also serve as an extraction parent for another entry.
		if _, err = dest.Lstat(name); !os.IsNotExist(err) {
			return fmt.Errorf("archive link conflicts with an extracted path")
		}
	}
	for name, l := range links {
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = dest.MkdirAll(path.Dir(name), 0700); err != nil {
			return err
		}
		if l.hard {
			err = dest.Link(resolved[name], name)
		} else {
			var relative string
			relative, err = filepath.Rel(path.Dir(name), resolved[name])
			if err == nil {
				err = dest.Symlink(relative, name)
			}
		}
		if err != nil {
			return err
		}
	}
	return ctx.Err()
}
