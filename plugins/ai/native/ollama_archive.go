//go:build linux

package main

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"

	"github.com/klauspost/compress/zstd"
)

const (
	ollamaMaxDownload = int64(4 << 30)
	ollamaMaxExpanded = int64(12 << 30)
	ollamaMaxEntries  = 10000
	ollamaMaxReceipt  = int64(16 << 20)
)

func ollamaPath(name string) bool {
	if !fs.ValidPath(name) || name == "." || len(name) > 1024 || strings.Count(name, "/") > 32 || strings.ContainsAny(name, "\\:") {
		return false
	}
	for _, c := range name {
		if c < 32 || c == 127 {
			return false
		}
	}
	return name == "bin" || name == "bin/ollama" || name == "lib" || name == "lib/ollama" || strings.HasPrefix(name, "lib/ollama/")
}

func ollamaLinkText(s string) bool {
	if s == "" || len(s) > 1024 || path.IsAbs(s) || strings.ContainsAny(s, "\\:") {
		return false
	}
	for _, c := range s {
		if c < 32 || c == 127 {
			return false
		}
	}
	return true
}

// Resolve components without cleaning away a symlink followed by '..'. This
// matches filesystem traversal, while rejecting escapes and bounded link loops.
func ollamaResolve(name string, files map[string]ollamaEntry) (string, error) {
	pending, resolved := strings.Split(name, "/"), []string{}
	links, steps := 0, 0
	for len(pending) > 0 {
		steps++
		if steps > 2048 {
			return "", fmt.Errorf("Ollama link resolution bound exceeded")
		}
		part := pending[0]
		pending = pending[1:]
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			if len(resolved) == 0 {
				return "", fmt.Errorf("Ollama link escapes tree")
			}
			resolved = resolved[:len(resolved)-1]
			continue
		}
		key := strings.Join(append(append([]string{}, resolved...), part), "/")
		e, ok := files[key]
		if !ok {
			return "", fmt.Errorf("dangling Ollama link")
		}
		mode := os.FileMode(e.Mode)
		if mode&os.ModeSymlink != 0 {
			links++
			if links > 40 || !ollamaLinkText(e.Link) {
				return "", fmt.Errorf("cyclic or unsafe Ollama link")
			}
			pending = append(strings.Split(e.Link, "/"), pending...)
			continue
		}
		if len(pending) != 0 && !mode.IsDir() {
			return "", fmt.Errorf("Ollama link traverses non-directory")
		}
		resolved = append(resolved, part)
	}
	result := strings.Join(resolved, "/")
	if !ollamaPath(result) {
		return "", fmt.Errorf("Ollama link escapes bundle")
	}
	return result, nil
}

func ollamaCheckLinks(files map[string]ollamaEntry) error {
	children := map[string][]string{}
	for name, e := range files {
		if !ollamaPath(name) {
			return fmt.Errorf("invalid Ollama inventory path")
		}
		parent := path.Dir(name)
		if parent != "." && !os.FileMode(files[parent].Mode).IsDir() {
			return fmt.Errorf("Ollama entry has missing or linked ancestor")
		}
		children[parent] = append(children[parent], name)
		if os.FileMode(e.Mode)&os.ModeSymlink != 0 {
			if !strings.HasPrefix(name, "lib/ollama/") || !ollamaLinkText(e.Link) {
				return fmt.Errorf("unsafe Ollama library link")
			}
			target, err := ollamaResolve(name, files)
			if err != nil {
				return err
			}
			if !strings.HasPrefix(target, "lib/ollama/") || strings.HasPrefix(name, target+"/") {
				return fmt.Errorf("Ollama ancestor or non-library link")
			}
			children[name] = append(children[name], target)
		}
	}
	// Directory links can form cycles even when each individual symlink resolves.
	state := map[string]byte{}
	var visit func(string, int) error
	visit = func(name string, depth int) error {
		if state[name] == 1 || depth > 128 {
			return fmt.Errorf("cyclic or excessive Ollama directory links")
		}
		if state[name] == 2 {
			return nil
		}
		state[name] = 1
		for _, child := range children[name] {
			if err := visit(child, depth+1); err != nil {
				return err
			}
		}
		state[name] = 2
		return nil
	}
	return visit(".", 0)
}

func ollamaExtract(ctx context.Context, archive, tree string) error {
	return ollamaExtractBundles(ctx, tree, archive)
}

func ollamaROCmPath(name string) bool {
	if !ollamaPath(name) || !strings.HasPrefix(name, "lib/ollama/rocm_v") {
		return false
	}
	version, _, _ := strings.Cut(strings.TrimPrefix(name, "lib/ollama/rocm_v"), "/")
	if len(version) == 0 || len(version) > 6 {
		return false
	}
	for _, c := range version {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func ollamaExtractBundles(ctx context.Context, tree string, archives ...string) error {
	if len(archives) < 1 || len(archives) > 2 {
		return fmt.Errorf("Ollama requires a base bundle and at most one companion")
	}
	files := map[string]ollamaEntry{}
	remaining := ollamaMaxExpanded + 1
	for i, archive := range archives {
		if err := ollamaExtractPart(ctx, archive, tree, files, &remaining, i > 0); err != nil {
			return err
		}
		if files["bin/ollama"].Mode != 0755 || !os.FileMode(files["lib/ollama"].Mode).IsDir() {
			return fmt.Errorf("incomplete Ollama bin/lib base bundle")
		}
	}
	if err := ollamaCheckLinks(files); err != nil {
		return err
	}
	r, err := os.OpenRoot(tree)
	if err != nil {
		return err
	}
	defer r.Close()
	// Links are created only after both archives pass the combined graph checks.
	for name, e := range files {
		if err = ctx.Err(); err != nil {
			return err
		}
		if e.Link != "" {
			if err = r.Symlink(e.Link, name); err != nil {
				return err
			}
		}
	}
	return ctx.Err()
}

func ollamaExtractPart(ctx context.Context, archive, tree string, files map[string]ollamaEntry, remaining *int64, rocm bool) error {
	f, err := regular(archive, 0600, ollamaMaxDownload)
	if err != nil {
		return err
	}
	defer f.Close()
	zr, err := zstd.NewReader(contextReader{ctx, f}, zstd.WithDecoderConcurrency(1), zstd.WithDecoderLowmem(true), zstd.WithDecoderMaxWindow(128<<20), zstd.WithDecoderMaxMemory(128<<20))
	if err != nil {
		return err
	}
	defer zr.Close()
	limited := &io.LimitedReader{R: contextReader{ctx, zr}, N: *remaining}
	defer func() { *remaining = limited.N }()
	tr := tar.NewReader(limited)
	r, err := os.OpenRoot(tree)
	if err != nil {
		return err
	}
	defer r.Close()
	seen := map[string]bool{}
	var total int64
	var payload bool
	addParents := func(name string) error {
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if e, ok := files[parent]; ok && !os.FileMode(e.Mode).IsDir() {
				return fmt.Errorf("Ollama archive link or file ancestor")
			}
			files[parent] = ollamaEntry{Mode: uint32(os.ModeDir | 0700)}
		}
		if len(files) > ollamaMaxEntries {
			return fmt.Errorf("Ollama inventory entry limit")
		}
		return r.MkdirAll(path.Dir(name), 0700)
	}
	for count := 0; ; count++ {
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
		if count >= ollamaMaxEntries {
			return fmt.Errorf("Ollama archive entry limit")
		}
		name := strings.TrimPrefix(h.Name, "./")
		if h.Typeflag == tar.TypeDir {
			name = strings.TrimSuffix(name, "/")
		}
		if name == "" || name == "." {
			if h.Typeflag != tar.TypeDir || seen["."] || h.Size != 0 || h.Mode&07000 != 0 {
				return fmt.Errorf("invalid Ollama archive root")
			}
			seen["."] = true
			continue
		}
		if !ollamaPath(name) || seen[name] || h.Mode&07000 != 0 {
			return fmt.Errorf("unsafe or duplicate Ollama archive path/mode")
		}
		if rocm && !ollamaROCmPath(name) && !(h.Typeflag == tar.TypeDir && (name == "lib" || name == "lib/ollama")) {
			return fmt.Errorf("unsupported Ollama ROCm layout; expected lib/ollama/rocm_v<digits>/")
		}
		if rocm && ollamaROCmPath(name) && strings.Count(name, "/") == 2 && h.Typeflag != tar.TypeDir {
			return fmt.Errorf("Ollama ROCm version root must be a directory")
		}
		seen[name] = true
		if h.Size < 0 || h.Size > ollamaMaxExpanded-total {
			return fmt.Errorf("Ollama expanded payload bound exceeded")
		}
		total += h.Size
		for key := range h.PAXRecords {
			if strings.Contains(strings.ToLower(key), "sparse") {
				return fmt.Errorf("sparse Ollama archive entry refused")
			}
		}
		if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA && h.Size != 0 {
			return fmt.Errorf("non-file Ollama archive data")
		}
		if old, ok := files[name]; ok && (h.Typeflag != tar.TypeDir || !os.FileMode(old.Mode).IsDir()) {
			return fmt.Errorf("conflicting Ollama archive entry")
		}
		if err = addParents(name); err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if name == "bin/ollama" {
				return fmt.Errorf("Ollama binary must be regular")
			}
			if err = r.MkdirAll(name, 0700); err != nil {
				return err
			}
			files[name] = ollamaEntry{Mode: uint32(os.ModeDir | 0700)}
		case tar.TypeReg, tar.TypeRegA:
			if name == "bin" || name == "lib" || name == "lib/ollama" {
				return fmt.Errorf("Ollama layout requires directories")
			}
			out, err := r.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				return err
			}
			n, copyErr := io.Copy(out, contextReader{ctx, tr})
			mode := os.FileMode(0600)
			if h.Mode&0111 != 0 {
				mode = 0755
			}
			if copyErr == nil && n != h.Size {
				copyErr = fmt.Errorf("Ollama tar size mismatch")
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
			files[name] = ollamaEntry{Mode: uint32(mode)}
			payload = payload || h.Size > 0
		case tar.TypeSymlink:
			if !strings.HasPrefix(name, "lib/ollama/") || !ollamaLinkText(h.Linkname) {
				return fmt.Errorf("unsafe Ollama library symlink")
			}
			files[name] = ollamaEntry{Mode: uint32(os.ModeSymlink | 0777), Link: h.Linkname}
		default:
			return fmt.Errorf("Ollama hardlink or special archive entry refused")
		}
		if len(files) > ollamaMaxEntries {
			return fmt.Errorf("Ollama inventory entry limit")
		}
	}
	// Consume padding and the zstd checksum under the same expanded-byte bound.
	buf := make([]byte, 32<<10)
	for {
		n, err := limited.Read(buf)
		for _, v := range buf[:n] {
			if v != 0 {
				return fmt.Errorf("nonzero data after Ollama tar end")
			}
		}
		if limited.N <= 0 {
			return fmt.Errorf("Ollama expanded stream bound exceeded")
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	if rocm && !payload {
		return fmt.Errorf("empty Ollama ROCm companion")
	}
	return ctx.Err()
}
