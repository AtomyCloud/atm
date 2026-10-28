package main

import (
	"debug/elf"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

func readRegular(root *os.Root, name string, max int64) ([]byte, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > max {
		return nil, fmt.Errorf("regular bounded %s required", name)
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if len(data) > int(max) {
		return nil, fmt.Errorf("%s exceeds size limit", name)
	}
	return data, err
}
func validatePayload(root *os.Root, version, arch string) error {
	return validateSDK(root, version, arch, false)
}

func validateSDK(root *os.Root, version, arch string, allowMissingVersion bool) error {
	for _, name := range []string{"bin", "bin/cache", "bin/cache/dart-sdk", "bin/cache/dart-sdk/bin", ".git"} {
		info, err := root.Lstat(name)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("real Flutter %s directory required", name)
		}
	}
	info, err := root.Lstat("bin/flutter")
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return fmt.Errorf("regular executable Flutter launcher required")
	}
	launcher, err := readRegular(root, "bin/flutter", 1<<20)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(string(launcher), "#!") {
		return fmt.Errorf("invalid Flutter launcher")
	}
	data, err := readRegular(root, "bin/cache/flutter.version.json", 64<<10)
	if os.IsNotExist(err) {
		data, err = readRegular(root, "version", 4096)
		if os.IsNotExist(err) && allowMissingVersion {
			// Flutter bootstrap invalidates these generated files. The caller
			// still verifies native receipt, Git revision and launcher identity.
		} else if err != nil {
			return err
		} else if strings.TrimSpace(string(data)) != version {
			return fmt.Errorf("Flutter version does not match directory")
		}
	} else {
		if err != nil {
			return err
		}
		var meta struct {
			Version string `json:"frameworkVersion"`
		}
		if err = json.Unmarshal(data, &meta); err != nil {
			return err
		}
		if meta.Version != version {
			return fmt.Errorf("Flutter SDK version changed; directory label no longer matches")
		}
	}
	name := "bin/cache/dart-sdk/bin/dart"
	info, err = root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 || info.Size() > limits.FileBytes {
		return fmt.Errorf("regular executable Dart required")
	}
	f, err := root.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	binary, err := elf.NewFile(f)
	if err != nil {
		return fmt.Errorf("invalid Dart ELF: %w", err)
	}
	machine := elf.EM_X86_64
	if arch == "arm64" {
		machine = elf.EM_AARCH64
	}
	if binary.Class != elf.ELFCLASS64 || binary.Data != elf.ELFDATA2LSB || binary.Machine != machine || (binary.Type != elf.ET_EXEC && binary.Type != elf.ET_DYN) {
		return fmt.Errorf("Dart architecture/type mismatch")
	}
	return nil
}

// Read only the current Git ref; never launch git or trust Git configuration.
func sdkRevision(root *os.Root) (string, error) {
	head, err := readRegular(root, ".git/HEAD", 4096)
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(head))
	if ref, ok := strings.CutPrefix(value, "ref: "); ok {
		if !strings.HasPrefix(ref, "refs/") || !safeArchivePath(ref) {
			return "", fmt.Errorf("unsafe Git HEAD ref")
		}
		data, e := readRegular(root, ".git/"+ref, 4096)
		if e == nil {
			value = strings.TrimSpace(string(data))
		} else if os.IsNotExist(e) {
			packed, e := readRegular(root, ".git/packed-refs", 8<<20)
			if e != nil {
				return "", e
			}
			value = ""
			for _, line := range strings.Split(string(packed), "\n") {
				parts := strings.Fields(line)
				if len(parts) == 2 && parts[1] == ref {
					if value != "" {
						return "", fmt.Errorf("duplicate packed ref")
					}
					value = parts[0]
				}
			}
		} else {
			return "", e
		}
	}
	if len(value) != 40 || strings.Trim(value, "0123456789abcdef") != "" {
		return "", fmt.Errorf("unsupported SDK Git revision")
	}
	return value, nil
}
