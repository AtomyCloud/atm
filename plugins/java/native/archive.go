package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"debug/elf"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

const receiptName = ".atm-java-receipt.json"

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
func extractArchive(ctx context.Context, archive string, dest *os.Root) (string, error) {
	f, err := os.Open(archive)
	if err != nil {
		return "", err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer gz.Close()
	bounded := &io.LimitedReader{R: contextReader{ctx, gz}, N: limits.ExpandedBytes + int64(limits.Entries)*4096 + 1025}
	tr := tar.NewReader(bounded)
	prefix := ""
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
			return "", err
		}
		if err = ctx.Err(); err != nil {
			return "", err
		}
		count++
		if count > limits.Entries {
			return "", fmt.Errorf("archive entry limit exceeded")
		}
		name := strings.TrimSuffix(h.Name, "/")
		if !safeArchivePath(name) {
			return "", fmt.Errorf("unsafe archive path")
		}
		top, rest, hasSlash := strings.Cut(name, "/")
		if prefix == "" {
			prefix = top
		}
		if prefix != top {
			return "", fmt.Errorf("archive must have one JDK root")
		}
		if !hasSlash {
			if h.Typeflag != tar.TypeDir {
				return "", fmt.Errorf("JDK archive root must be a directory")
			}
			if seen["."] {
				return "", fmt.Errorf("duplicate archive root")
			}
			seen["."] = true
			continue
		}
		name = rest
		if !safeArchivePath(name) || name == receiptName || seen[name] {
			return "", fmt.Errorf("unsafe/reserved/duplicate archive entry")
		}
		seen[name] = true
		if h.Size < 0 || h.Size > limits.FileBytes || h.Size > limits.ExpandedBytes-total {
			return "", fmt.Errorf("expanded archive budget exceeded")
		}
		total += h.Size
		if h.Mode&07000 != 0 {
			return "", fmt.Errorf("special archive permissions refused")
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err = dest.MkdirAll(name, 0700); err != nil {
				return "", err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err = dest.MkdirAll(path.Dir(name), 0700); err != nil {
				return "", err
			}
			out, err := dest.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				return "", err
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
				return "", copyErr
			}
			if closeErr != nil {
				return "", closeErr
			}
		case tar.TypeSymlink, tar.TypeLink:
			target := h.Linkname
			if path.IsAbs(target) || len(target) > 1024 || strings.ContainsAny(target, "\\:\x00\r\n") {
				return "", fmt.Errorf("unsafe archive link")
			}
			if h.Typeflag == tar.TypeLink {
				var ok bool
				target, ok = strings.CutPrefix(target, prefix+"/")
				if !ok || !safeArchivePath(target) {
					return "", fmt.Errorf("hardlink outside JDK root")
				}
			} else {
				target = path.Clean(path.Join(path.Dir(name), target))
			}
			if !safeArchivePath(target) {
				return "", fmt.Errorf("archive link escapes root")
			}
			links[name] = archiveLink{target, h.Typeflag == tar.TypeLink}
		default:
			return "", fmt.Errorf("archive special file refused")
		}
	}
	if _, err = io.Copy(io.Discard, bounded); err != nil {
		return "", err
	}
	if bounded.N <= 0 {
		return "", fmt.Errorf("expanded stream budget exceeded")
	}
	resolved := map[string]string{}
	for name := range seen {
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if _, ok := links[parent]; ok {
				return "", fmt.Errorf("archive entry has a link ancestor")
			}
		}
	}
	for name, l := range links {
		final, err := resolveLink(l.target, links)
		if err != nil {
			return "", err
		}
		info, err := dest.Lstat(final)
		if err != nil || (!info.Mode().IsRegular() && !info.IsDir()) || (l.hard && !info.Mode().IsRegular()) {
			return "", fmt.Errorf("archive link target must exist inside payload")
		}
		resolved[name] = final
		// A link cannot also serve as an extraction parent for another entry.
		if _, err = dest.Lstat(name); !os.IsNotExist(err) {
			return "", fmt.Errorf("archive link conflicts with an extracted path")
		}
	}
	for name, l := range links {
		if err = ctx.Err(); err != nil {
			return "", err
		}
		if err = dest.MkdirAll(path.Dir(name), 0700); err != nil {
			return "", err
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
			return "", err
		}
	}
	return prefix, ctx.Err()
}

var releaseKeyRE = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

func parseRelease(data []byte) (string, error) {
	if len(data) > 64<<10 {
		return "", fmt.Errorf("release file exceeds 64 KiB")
	}
	fields := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || !releaseKeyRE.MatchString(key) || len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' {
			return "", fmt.Errorf("invalid literal JDK release field")
		}
		if _, exists := fields[key]; exists {
			return "", fmt.Errorf("duplicate JDK release field")
		}
		value = value[1 : len(value)-1]
		if strings.ContainsAny(value, "\x00\r\n\"") {
			return "", fmt.Errorf("invalid quoted release value")
		}
		fields[key] = value
	}
	v := fields["JAVA_VERSION"]
	if !actualVersionRE.MatchString(v) {
		return "", fmt.Errorf("release lacks a supported literal JAVA_VERSION")
	}
	return v, nil
}
func validatePayload(root *os.Root, platform string) (string, error) {
	info, err := root.Lstat("bin")
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("real JDK bin directory required")
	}
	for _, name := range []string{"bin/java", "bin/javac"} {
		info, err := root.Lstat(name)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 || info.Size() > limits.FileBytes {
			return "", fmt.Errorf("regular executable %s required", name)
		}
		f, err := root.Open(name)
		if err != nil {
			return "", err
		}
		binary, parseErr := elf.NewFile(f)
		f.Close()
		if parseErr != nil {
			return "", fmt.Errorf("invalid JDK ELF: %w", parseErr)
		}
		machine := elf.EM_X86_64
		if platform == "aarch64" {
			machine = elf.EM_AARCH64
		}
		if binary.Class != elf.ELFCLASS64 || binary.Data != elf.ELFDATA2LSB || binary.Machine != machine || (binary.Type != elf.ET_EXEC && binary.Type != elf.ET_DYN) {
			return "", fmt.Errorf("JDK executable architecture/type mismatch")
		}
	}
	info, err = root.Lstat("release")
	if err != nil || !info.Mode().IsRegular() || info.Size() > 64<<10 {
		return "", fmt.Errorf("regular bounded JDK release file required")
	}
	f, err := root.Open("release")
	if err != nil {
		return "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (64<<10)+1))
	if err != nil {
		return "", err
	}
	return parseRelease(data)
}
