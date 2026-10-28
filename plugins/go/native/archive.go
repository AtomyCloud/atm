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

const receiptName = ".atm-go-receipt.json"

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

// Official Go archives need no links. Reject all links and special entries;
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
	return ctx.Err()
}
