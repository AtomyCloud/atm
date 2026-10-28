package main

import (
	"bytes"
	"context"
	"debug/elf"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func readRegular(root *os.Root, name string, max int64) ([]byte, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > max {
		return nil, fmt.Errorf("invalid/oversized regular file: %s", name)
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if int64(len(data)) > max {
		return nil, fmt.Errorf("file grew beyond limit")
	}
	return data, err
}
func validatePayload(root *os.Root, v, platform, commit string) error {
	for _, p := range []string{"bin", "resources", "resources/app", "resources/app/out"} {
		i, err := root.Lstat(p)
		if err != nil || !i.IsDir() {
			return fmt.Errorf("required real directory missing: %s", p)
		}
	}
	for _, p := range []string{"bin/code", "code", "resources/app/out/cli.js", "resources/app/resources/linux/code.png"} {
		i, err := root.Lstat(p)
		if err != nil || !i.Mode().IsRegular() || i.Size() > limits.FileBytes {
			return fmt.Errorf("required regular payload file missing: %s", p)
		}
		if (p == "code" || p == "bin/code") && i.Mode().Perm()&0111 == 0 {
			return fmt.Errorf("launcher is not executable")
		}
	}
	data, err := readRegular(root, "resources/app/package.json", 2<<20)
	if err != nil {
		return err
	}
	var pkg struct{ Version, Name string }
	if err = json.Unmarshal(data, &pkg); err != nil {
		return err
	}
	if pkg.Version != v || pkg.Name != "Code" {
		return fmt.Errorf("VS Code package identity mismatch")
	}
	data, err = readRegular(root, "resources/app/product.json", 2<<20)
	if err != nil {
		return err
	}
	var product struct{ Commit, Quality, ApplicationName string }
	if err = json.Unmarshal(data, &product); err != nil {
		return err
	}
	if product.ApplicationName != "code" || product.Quality != "stable" || !commitRE.MatchString(product.Commit) || (commit != "" && product.Commit != commit) {
		return fmt.Errorf("VS Code product/build identity mismatch")
	}
	f, err := root.Open("code")
	if err != nil {
		return err
	}
	defer f.Close()
	binary, err := elf.NewFile(f)
	if err != nil {
		return fmt.Errorf("invalid VS Code ELF: %w", err)
	}
	machine := elf.EM_X86_64
	if platform == "linux-arm64" {
		machine = elf.EM_AARCH64
	}
	if binary.Class != elf.ELFCLASS64 || binary.Data != elf.ELFDATA2LSB || binary.Machine != machine || (binary.Type != elf.ET_EXEC && binary.Type != elf.ET_DYN) {
		return fmt.Errorf("VS Code executable architecture/type mismatch")
	}
	return nil
}
func (b *backend) readReceipt(v string) (receipt, error) {
	var rec receipt
	if !slotRE.MatchString(v) {
		return rec, fmt.Errorf("invalid version slot")
	}
	if err := realDirs(filepath.Join(b.root, v), false); err != nil {
		return rec, err
	}
	root, err := os.OpenRoot(filepath.Join(b.root, v))
	if err != nil {
		return rec, err
	}
	defer root.Close()
	data, err := readRegular(root, receiptName, 32<<20)
	if err != nil {
		return rec, fmt.Errorf("missing/unsafe native receipt; preserving legacy/user installation: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&rec); err != nil {
		return rec, err
	}
	a := rec.Artifact
	if dec.Decode(new(any)) != io.EOF || rec.Schema != 1 || rec.Plugin != "vscode" || "code_"+a.Version != v || a.Platform != b.platform || !commitRE.MatchString(a.Commit) || !hashRE.MatchString(a.SHA256) || !approvedURL(a.URL, true) || a.Size <= 0 || a.Size > limits.ArchiveBytes || len(rec.Files) == 0 || len(rec.Files) > limits.Entries*2 {
		return rec, fmt.Errorf("invalid VS Code ownership receipt")
	}
	return rec, nil
}
func (b *backend) validInstalled(v string) error {
	if !slotRE.MatchString(v) {
		return fmt.Errorf("invalid version slot")
	}
	dir := filepath.Join(b.root, v)
	if err := realDirs(dir, false); err != nil {
		return err
	}
	commit := ""
	if _, err := os.Lstat(filepath.Join(dir, receiptName)); err == nil {
		rec, err := b.readReceipt(v)
		if err != nil {
			return err
		}
		commit = rec.Artifact.Commit
	} else if !os.IsNotExist(err) {
		return err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	return validatePayload(root, strings.TrimPrefix(v, "code_"), b.platform, commit)
}
func (b *backend) status() error {
	v, err := b.current()
	if err != nil {
		return err
	}
	if v == "" {
		b.log("No portable VS Code selected")
	} else {
		b.log("Current: " + v)
		if err = b.validInstalled(v); err != nil {
			return err
		}
	}
	vs, err := b.installed()
	if err != nil {
		return err
	}
	for _, s := range vs {
		ownership := "legacy/unowned"
		if b.hasReceipt(s) {
			ownership = "native receipt (not a full integrity audit)"
		}
		b.log(s + " - " + ownership)
	}
	return nil
}

// Executable files are deleted only after exact receipt verification. Personal
// portable data is moved intact to a retained directory, never recursively deleted.
func (b *backend) removeOwned(ctx context.Context, v string) error {
	if err := b.verifyOwned(ctx, v); err != nil {
		return err
	}
	rec, err := b.readReceipt(v)
	if err != nil {
		return err
	}
	source := filepath.Join(b.root, v, "data")
	if _, err := os.Lstat(source); err == nil {
		if err = realDirs(source, false); err != nil {
			return err
		}
		backup, err := b.dataBackupDir(v)
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = os.Rename(source, filepath.Join(backup, "data")); err != nil {
			return err
		}
		b.log("Portable data preserved: " + filepath.Join(backup, "data"))
	} else if !os.IsNotExist(err) {
		return err
	}
	// Do not remove data that reappears after it was preserved (e.g. a running editor).
	if _, err := os.Lstat(source); !os.IsNotExist(err) {
		return fmt.Errorf("data reappeared; close VS Code before removal")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := os.OpenRoot(filepath.Join(b.root, v))
	if err != nil {
		return err
	}
	defer root.Close()
	names := make([]string, 0, len(rec.Files))
	for name := range rec.Files {
		names = append(names, name)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	// Never recursively remove the version: newly created user data/unknown files
	// make directory removal fail instead of being swept up by RemoveAll.
	for _, name := range names {
		if err = ctx.Err(); err != nil {
			return err
		}
		if !safeArchivePath(name) || name == "data" || strings.HasPrefix(name, "data/") {
			return fmt.Errorf("unsafe receipt deletion path")
		}
		if err = root.Remove(name); err != nil {
			return err
		}
	}
	if err = root.Remove(receiptName); err != nil {
		return err
	}
	return os.Remove(filepath.Join(b.root, v))
}
