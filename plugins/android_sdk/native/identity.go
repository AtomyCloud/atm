package main

import (
	"archive/zip"
	"bytes"
	"debug/elf"
	"encoding/xml"
	"fmt"
	"os"
	"strings"
)

func properties(data []byte) (map[string]string, error) {
	out := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "Pkg.Revision" || key == "Pkg.Path" || key == "AndroidVersion.ApiLevel" {
			if _, exists := out[key]; exists || strings.Contains(value, "\\") {
				return nil, fmt.Errorf("ambiguous SDK identity properties")
			}
			out[key] = value
		}
	}
	return out, nil
}
func validatePayload(root *os.Root, s packageSpec) error {
	data, err := readRegular(root, "source.properties", 1<<20)
	if err != nil {
		return err
	}
	p, err := properties(data)
	if err != nil {
		return err
	}
	rev, err := parseRevision(p["Pkg.Revision"])
	if err != nil || compare(rev, s.Revision) != 0 {
		return fmt.Errorf("SDK package revision mismatch")
	}
	if p["Pkg.Path"] != "" && p["Pkg.Path"] != s.ID {
		return fmt.Errorf("SDK package path identity mismatch")
	}
	if strings.HasPrefix(s.ID, "platforms;") && p["AndroidVersion.ApiLevel"] != strings.TrimPrefix(s.ID, "platforms;android-") {
		return fmt.Errorf("SDK platform API mismatch")
	}
	executable := ""
	switch {
	case s.ID == toolsID:
		if _, err = readRegular(root, "bin/sdkmanager", 1<<20); err != nil {
			return err
		}
		data, err = readRegular(root, "lib/sdkmanager-classpath.jar", 8<<20)
		if err != nil {
			return err
		}
		jar, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return fmt.Errorf("invalid sdkmanager classpath JAR")
		}
		found := false
		for _, e := range jar.File {
			if e.Name == "META-INF/MANIFEST.MF" {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("sdkmanager classpath JAR has no manifest")
		}
	case s.ID == "platform-tools":
		executable = "adb"
	case s.ID == "emulator":
		executable = "emulator"
	case strings.HasPrefix(s.ID, "build-tools;"):
		executable = "aapt2"
	case strings.HasPrefix(s.ID, "cmake;"):
		executable = "bin/cmake"
	case strings.HasPrefix(s.ID, "ndk;"):
		executable = "toolchains/llvm/prebuilt/linux-x86_64/bin/clang"
	case strings.HasPrefix(s.ID, "platforms;"):
		i, e := root.Lstat("android.jar")
		if e != nil || !i.Mode().IsRegular() || i.Size() > maxFile {
			return fmt.Errorf("platform android.jar missing/unsafe")
		}
	}
	if executable != "" {
		i, err := root.Stat(executable)
		if err != nil || !i.Mode().IsRegular() || i.Mode().Perm()&0111 == 0 {
			return fmt.Errorf("SDK executable missing: %s", executable)
		}
		f, err := root.Open(executable)
		if err != nil {
			return err
		}
		defer f.Close()
		binary, err := elf.NewFile(f)
		if err != nil {
			return err
		}
		if binary.Class != elf.ELFCLASS64 || binary.Data != elf.ELFDATA2LSB || binary.Machine != elf.EM_X86_64 || (binary.Type != elf.ET_EXEC && binary.Type != elf.ET_DYN) {
			return fmt.Errorf("SDK executable architecture mismatch")
		}
	}
	return nil
}

type localDetails struct {
	Type    string `xml:"xsi:type,attr"`
	Content string `xml:",innerxml"`
}
type localDependencies struct {
	Items []dependency `xml:"dependency"`
}
type localPackage struct {
	XMLName      xml.Name           `xml:"localPackage"`
	Path         string             `xml:"path,attr"`
	Details      localDetails       `xml:"type-details"`
	Revision     revision           `xml:"revision"`
	Name         string             `xml:"display-name"`
	License      reference          `xml:"uses-license"`
	Dependencies *localDependencies `xml:"dependencies,omitempty"`
}

func localMetadata(s packageSpec) ([]byte, error) {
	// The repository schema declares unqualified children in a namespaced root.
	// Preserve the platform type-details, including API/extension/layoutlib fields.
	lic, err := xml.Marshal(s.License)
	if err != nil {
		return nil, err
	}
	var dependencies *localDependencies
	if len(s.Dependencies) > 0 {
		dependencies = &localDependencies{s.Dependencies}
	}
	lp, err := xml.Marshal(localPackage{Path: s.ID, Details: localDetails{s.Type, s.TypeContent}, Revision: s.Revision, Name: s.Name, License: reference{s.License.ID}, Dependencies: dependencies})
	if err != nil {
		return nil, err
	}
	header := `<?xml version="1.0" encoding="UTF-8"?><repo:repository xmlns:repo="http://schemas.android.com/repository/android/common/02" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xmlns:generic="http://schemas.android.com/repository/android/generic/02" xmlns:sdk="http://schemas.android.com/sdk/android/repo/repository2/03">`
	return []byte(header + string(lic) + string(lp) + "</repo:repository>"), nil
}
