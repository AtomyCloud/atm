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
	"strings"
)

const receiptName = ".atm-trivy-receipt.json"

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

// Validate every archive entry but publish only the root executable. All links
// are refused even when they would not be extracted.
func extractArchive(ctx context.Context, archive string, dest *os.Root) error {
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
	found := false
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
		if !safeArchivePath(name) || seen[name] || h.Mode&07000 != 0 {
			return fmt.Errorf("unsafe or duplicate archive entry")
		}
		seen[name] = true
		if h.Size < 0 || h.Size > limits.FileBytes || h.Size > limits.ExpandedBytes-total {
			return fmt.Errorf("expanded archive size exceeded")
		}
		total += h.Size
		if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA && h.Typeflag != tar.TypeDir {
			return fmt.Errorf("archive links/special files refused")
		}
		if name != "trivy" {
			continue
		}
		if h.Typeflag == tar.TypeDir || h.Size == 0 || h.Mode&0111 == 0 {
			return fmt.Errorf("root Trivy executable required")
		}
		if err = dest.Mkdir("bin", 0700); err != nil {
			return err
		}
		out, err := dest.OpenFile("bin/trivy", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
		if err != nil {
			return err
		}
		n, copyErr := io.Copy(out, contextReader{ctx, tr})
		if copyErr == nil && n != h.Size {
			copyErr = fmt.Errorf("archive size mismatch")
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
		found = true
	}
	if _, err = io.Copy(io.Discard, bounded); err != nil {
		return err
	}
	if bounded.N <= 0 {
		return fmt.Errorf("expanded archive stream exceeded")
	}
	if !found {
		return fmt.Errorf("archive lacks root trivy executable")
	}
	return ctx.Err()
}
func validatePayload(root *os.Root, version, platform string) error {
	info, err := root.Lstat("bin/trivy")
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 || info.Size() > limits.FileBytes {
		return fmt.Errorf("regular executable bin/trivy required")
	}
	f, err := root.Open("bin/trivy")
	if err != nil {
		return err
	}
	defer f.Close()
	executable, err := elf.NewFile(f)
	if err != nil {
		return fmt.Errorf("invalid Trivy ELF executable: %w", err)
	}
	machine := elf.EM_X86_64
	if platform == "Linux-ARM64" {
		machine = elf.EM_AARCH64
	}
	if executable.Class != elf.ELFCLASS64 || executable.Data != elf.ELFDATA2LSB || executable.Machine != machine || (executable.Type != elf.ET_EXEC && executable.Type != elf.ET_DYN) {
		return fmt.Errorf("Trivy executable architecture/type mismatch")
	}
	return nil
}
