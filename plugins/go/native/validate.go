package main

import (
	"debug/elf"
	"fmt"
	"io"
	"os"
	"strings"
)

func validatePayload(root *os.Root, version, platform string) error {
	for _, name := range []string{"bin", "bin/go", "VERSION"} {
		info, err := root.Lstat(name)
		if err != nil {
			return err
		}
		if name == "bin" {
			if !info.IsDir() {
				return fmt.Errorf("Go bin must be a real directory")
			}
			continue
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("Go %s must be a regular file", name)
		}
		if name == "bin/go" && (info.Mode().Perm()&0111 == 0 || info.Size() > limits.FileBytes) {
			return fmt.Errorf("invalid Go executable")
		}
		if name == "VERSION" && info.Size() > 4096 {
			return fmt.Errorf("Go VERSION exceeds limit")
		}
	}
	f, err := root.Open("VERSION")
	if err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(f, 4097))
	f.Close()
	if err != nil {
		return err
	}
	first, _, _ := strings.Cut(string(data), "\n")
	if len(data) > 4096 || first != "go"+version {
		return fmt.Errorf("Go VERSION does not match selected release")
	}
	f, err = root.Open("bin/go")
	if err != nil {
		return err
	}
	defer f.Close()
	binary, err := elf.NewFile(f)
	if err != nil {
		return fmt.Errorf("invalid Go ELF executable: %w", err)
	}
	machine := elf.EM_X86_64
	if platform == "linux-arm64" {
		machine = elf.EM_AARCH64
	}
	if binary.Class != elf.ELFCLASS64 || binary.Data != elf.ELFDATA2LSB || binary.Machine != machine || (binary.Type != elf.ET_EXEC && binary.Type != elf.ET_DYN) {
		return fmt.Errorf("Go executable architecture/type mismatch")
	}
	return nil
}
