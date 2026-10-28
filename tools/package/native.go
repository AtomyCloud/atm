package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/AtomyCloud/atm/internal/plugin"
)

func compile(targetOS, arch, output, source, ldflags string) error {
	cmd := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-ldflags", strings.TrimSpace("-s -w -buildid= "+ldflags), "-o", output, source)
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		switch key {
		case "GOOS", "GOARCH", "CGO_ENABLED", "GOAMD64", "GOARM64", "GOWORK":
		default:
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "GOOS="+targetOS, "GOARCH="+arch, "CGO_ENABLED=0", "GOAMD64=v1", "GOARM64=v8.0", "GOWORK=off")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

func nativeManifest(id string) (plugin.Manifest, error) {
	var m plugin.Manifest
	path := filepath.Join("plugins", id, "plugin.json")
	info, err := os.Lstat(path)
	if err != nil {
		return m, err
	}
	if !info.Mode().IsRegular() {
		return m, fmt.Errorf("non-regular manifest refused: %s", path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return m, err
	}
	if err := m.Validate(id); err != nil {
		return m, err
	}
	return m, nil
}

func discoverNative() ([]plugin.Manifest, error) {
	info, err := os.Lstat("plugins")
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("plugins root must be a regular directory")
	}
	entries, err := os.ReadDir("plugins")
	if err != nil {
		return nil, err
	}
	var manifests []plugin.Manifest
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("symlink plugin refused: %s", entry.Name())
		}
		if !entry.IsDir() {
			continue
		}
		m, err := nativeManifest(entry.Name())
		if os.IsNotExist(err) {
			continue // Metadata-only legacy plugins remain runtime payloads.
		}
		if err != nil {
			return nil, err
		}
		if m.Schema != 2 || m.Executor != "native" {
			continue
		}
		if err := validateNativeSource(m); err != nil {
			return nil, err
		}
		manifests = append(manifests, m)
	}
	return manifests, nil
}

func validateNativeSource(m plugin.Manifest) error {
	if err := m.Validate(m.ID); err != nil {
		return err
	}
	if m.Schema != 2 || m.Executor != "native" {
		return fmt.Errorf("%s: native manifest required", m.ID)
	}
	seen := map[string]bool{}
	for _, rel := range m.Executables {
		// Generated files must never replace manifests, legacy scripts or source.
		if !strings.HasPrefix(rel, "bin/") || seen[rel] {
			return fmt.Errorf("%s: native outputs require distinct bin/ paths", m.ID)
		}
		seen[rel] = true
		for _, part := range strings.Split(rel, "/") {
			if strings.HasPrefix(part, ".") {
				return fmt.Errorf("%s: hidden native output path refused", m.ID)
			}
		}
		if strings.HasSuffix(rel, ".sha256") {
			return fmt.Errorf("%s: native output uses reserved checksum suffix", m.ID)
		}
	}
	source := filepath.Join("plugins", m.ID, "native")
	info, err := os.Lstat(source)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("%s: missing or non-directory local native source: %s", m.ID, source)
	}
	found := false
	err = filepath.WalkDir(source, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("non-regular native source refused: %s", path)
		}
		if filepath.Dir(path) == source && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			found = true
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%s: local native Go source missing", m.ID)
	}
	return nil
}

func buildNative(manifests []plugin.Manifest, targetOS, arch, stage string) ([]payload, error) {
	var files []payload
	for _, m := range manifests {
		built, err := buildNativePlugin(m, targetOS, arch, stage)
		if err != nil {
			return nil, err
		}
		files = append(files, built...)
	}
	return files, nil
}

// Build from source into private staging, never reuse a checkout executable.
func buildNativePlugin(m plugin.Manifest, targetOS, arch, stage string) ([]payload, error) {
	if err := validateNativeSource(m); err != nil {
		return nil, err
	}
	target := targetOS + "/" + arch
	rel, supported := m.Executables[target]
	if !supported {
		fmt.Fprintf(os.Stderr, "%s: unavailable for %s (no declared executable)\n", m.ID, target)
		return nil, nil
	}
	name := "plugins/" + m.ID + "/" + rel
	path := filepath.Join(stage, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	if err := compile(targetOS, arch, path, "./plugins/"+m.ID+"/native", ""); err != nil {
		return nil, fmt.Errorf("build %s %s: %w", m.ID, target, err)
	}
	file, err := nativePayload(m, target, stage)
	if err != nil {
		return nil, err
	}
	return []payload{file}, nil
}

func nativePayload(m plugin.Manifest, target, stage string) (payload, error) {
	if err := m.Validate(m.ID); err != nil {
		return payload{}, err
	}
	p := plugin.Plugin{Manifest: m, Directory: filepath.Join(stage, "plugins", m.ID)}
	path, err := p.NativeExecutable(target)
	if err != nil {
		return payload{}, err
	}
	if err := plugin.CheckBinary(path, target); err != nil {
		return payload{}, fmt.Errorf("%s executable: %w", m.ID, err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return payload{}, err
	}
	return payload{"plugins/" + m.ID + "/" + m.Executables[target], b, 0755}, nil
}

func buildPluginsOnly(targetOS, arch, out string) error {
	if out == "" {
		return fmt.Errorf("--out is required")
	}
	if !slices.Contains(plugin.NativeTargets, targetOS+"/"+arch) {
		return fmt.Errorf("unsupported native target %s/%s", targetOS, arch)
	}
	manifests, err := discoverNative()
	if err != nil {
		return err
	}
	return buildNativeOnly(manifests, targetOS, arch, out)
}

func buildNativeOnly(manifests []plugin.Manifest, targetOS, arch, out string) error {
	stage, err := os.MkdirTemp("", "atm-native-build-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	files, err := buildNative(manifests, targetOS, arch, stage)
	if err != nil {
		return err
	}
	for _, file := range files {
		if err := publishNative(out, file); err != nil {
			return err
		}
		digest := fmt.Sprintf("%x  %s\n", sha256.Sum256(file.data), filepath.Base(file.name))
		if err := publishNative(out, payload{file.name + ".sha256", []byte(digest), 0644}); err != nil {
			return err
		}
	}
	return nil
}

// Source-checkout builds may replace an older generated binary, never a link.
func publishNative(out string, file payload) error {
	parts := strings.Split(file.name, "/")
	if !fs.ValidPath(file.name) || strings.ContainsAny(file.name, "\\:") || len(parts) < 4 || parts[0] != "plugins" || !plugin.IDPattern.MatchString(parts[1]) || parts[2] != "bin" {
		return fmt.Errorf("unsafe generated output path: %s", file.name)
	}
	root, err := filepath.Abs(out)
	if err != nil {
		return err
	}
	destination := filepath.Join(root, filepath.FromSlash(file.name))
	parent := filepath.Dir(destination)
	for path := parent; ; path = filepath.Dir(path) {
		info, err := os.Lstat(path)
		if err == nil && !info.IsDir() {
			return fmt.Errorf("native output parent is not a regular directory: %s", path)
		}
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if path == root {
			break
		}
	}
	if info, err := os.Lstat(destination); err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("native output is not a regular file: %s", destination)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(parent, 0755); err != nil {
		return err
	}
	// The repository's root ignore file may be local-only. Generated outputs
	// protect themselves, while preserving any existing developer ignore rules.
	ignore, err := os.OpenFile(filepath.Join(parent, ".gitignore"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err == nil {
		_, writeErr := ignore.WriteString("*\n")
		closeErr := ignore.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
	} else if !os.IsExist(err) {
		return err
	}
	ignorePath := filepath.Join(parent, ".gitignore")
	info, err := os.Lstat(ignorePath)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("native output ignore file must be regular: %s", ignorePath)
	}
	rules, err := os.ReadFile(ignorePath)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(rules)) != "*" {
		return fmt.Errorf("native output directory must ignore all generated files: %s", parent)
	}
	f, err := os.CreateTemp(parent, ".atm-native-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(file.data); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(file.mode); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), destination); err != nil {
		return err
	}
	fmt.Println(destination)
	return nil
}

func excludedRuntimePath(path string, nativeIDs map[string]bool) bool {
	path = filepath.ToSlash(path)
	for _, part := range strings.Split(path, "/") {
		if strings.HasPrefix(part, ".") || part == "tests" || part == "__pycache__" || part == "internal-docs" {
			return true
		}
	}
	parts := strings.Split(path, "/")
	if len(parts) >= 3 && parts[0] == "plugins" && nativeIDs[parts[1]] && (parts[2] == "native" || parts[2] == "bin") {
		return true
	}
	return strings.HasSuffix(path, ".go") || strings.HasSuffix(path, ".pyc") || strings.HasSuffix(path, ".log")
}
