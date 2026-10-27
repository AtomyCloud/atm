package plugin

import (
	"debug/macho"
	"encoding/binary"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

var NativeActions = []string{"status", "install", "update", "remove", "uninstall", "use", "integrate"}
var NativeTargets = []string{"linux/amd64", "linux/arm64", "windows/amd64", "darwin/amd64", "darwin/arm64"}

func (m Manifest) validateNative() error {
	if m.Executor != "native" || m.Protocol != "jsonl-v1" {
		return fmt.Errorf("invalid native executor/protocol: %s", m.ID)
	}
	if len(m.Platforms) == 0 || len(m.Executables) == 0 || len(m.Operations) == 0 {
		return fmt.Errorf("incomplete native manifest: %s", m.ID)
	}
	seen := map[string]bool{}
	for _, platform := range m.Platforms {
		if (platform != "linux" && platform != "windows" && platform != "darwin") || seen[platform] {
			return fmt.Errorf("invalid/duplicate native platform: %s", platform)
		}
		seen[platform] = true
	}
	covered := map[string]bool{}
	for target, path := range m.Executables {
		platform, _, _ := strings.Cut(target, "/")
		if !slices.Contains(NativeTargets, target) || !seen[platform] || !safeExecutablePath(path) {
			return fmt.Errorf("invalid native target/path: %s", target)
		}
		if platform == "windows" && !strings.EqualFold(filepath.Ext(path), ".exe") {
			return fmt.Errorf("Windows native entrypoints must be .exe files")
		}
		covered[platform] = true
	}
	if len(covered) != len(seen) {
		return fmt.Errorf("native platform has no executable")
	}
	seen = map[string]bool{}
	for _, op := range m.Operations {
		if !slices.Contains(NativeActions, op) || seen[op] {
			return fmt.Errorf("invalid/duplicate native operation: %s", op)
		}
		seen[op] = true
	}
	return nil
}

func safeExecutablePath(path string) bool {
	if !fs.ValidPath(path) || path == "." || strings.ContainsAny(path, "\\:<>\"|?*\x00") {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
			return false
		}
		for _, r := range part {
			if r < 32 || r == 127 {
				return false
			}
		}
		base, _, _ := strings.Cut(strings.ToUpper(part), ".")
		if slices.Contains([]string{"CON", "PRN", "AUX", "NUL"}, base) {
			return false
		}
		if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9' {
			return false
		}
	}
	return true
}

// Recheck at launch as well as discovery. Plugins are trusted code, not sandboxed.
func (p Plugin) NativeExecutable(target string) (string, error) {
	rel, ok := p.Executables[target]
	if !ok {
		return "", fmt.Errorf("no native executable for %s", target)
	}
	if !safeExecutablePath(rel) {
		return "", fmt.Errorf("unsafe native executable path")
	}
	root, err := filepath.Abs(p.Directory)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("native plugin root is not a regular directory")
	}
	path := root
	parts := strings.Split(rel, "/")
	for i, part := range parts {
		path = filepath.Join(path, part)
		info, err = os.Lstat(path)
		if err != nil {
			return "", fmt.Errorf("native executable unavailable: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("native entrypoint symlink refused")
		}
		if i < len(parts)-1 && !info.IsDir() {
			return "", fmt.Errorf("native entrypoint parent is not a directory")
		}
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("native entrypoint is not a regular file")
	}
	if !strings.HasPrefix(target, "windows/") && info.Mode().Perm()&0111 == 0 {
		return "", fmt.Errorf("native entrypoint is not executable")
	}
	return path, nil
}

func CheckBinary(path, target string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return CheckBinaryReader(f, target)
}

// CheckBinaryReader allows package inspection without extracting executables.
func CheckBinaryReader(reader io.ReaderAt, target string) error {
	if target == "darwin/amd64" || target == "darwin/arm64" {
		f, err := macho.NewFile(reader)
		if err != nil {
			return err
		}
		defer f.Close()
		cpu := macho.CpuAmd64
		if target == "darwin/arm64" {
			cpu = macho.CpuArm64
		}
		if f.Cpu != cpu || f.Type != macho.TypeExec {
			return fmt.Errorf("native binary does not match %s", target)
		}
		return nil
	}
	h := make([]byte, 64)
	if _, err := reader.ReadAt(h, 0); err != nil {
		return fmt.Errorf("invalid native binary: %w", err)
	}
	if strings.HasPrefix(target, "linux/") {
		machine := uint16(62)
		if target == "linux/arm64" {
			machine = 183
		}
		if string(h[:4]) != "\x7fELF" || h[4] != 2 || h[5] != 1 || binary.LittleEndian.Uint16(h[18:20]) != machine {
			return fmt.Errorf("native binary does not match %s", target)
		}
		return nil
	}
	if target == "windows/amd64" && string(h[:2]) == "MZ" {
		pe := make([]byte, 6)
		if _, err := reader.ReadAt(pe, int64(binary.LittleEndian.Uint32(h[60:64]))); err == nil && string(pe[:4]) == "PE\x00\x00" && binary.LittleEndian.Uint16(pe[4:]) == 0x8664 {
			return nil
		}
	}
	return fmt.Errorf("native binary does not match %s", target)
}
