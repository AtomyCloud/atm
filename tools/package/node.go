package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

func nodeManifest() (plugin.Manifest, error) {
	var m plugin.Manifest
	b, err := os.ReadFile("plugins/node/plugin.json")
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return m, err
	}
	if err := m.Validate("node"); err != nil {
		return m, err
	}
	if m.Executor != "native" {
		return m, fmt.Errorf("Node packaging requires its native manifest")
	}
	return m, nil
}

// Build from source into private staging, never reuse a checkout executable.
func buildNode(targetOS, arch, stage string) ([]payload, error) {
	m, err := nodeManifest()
	if err != nil {
		return nil, err
	}
	target := targetOS + "/" + arch
	rel, supported := m.Executables[target]
	if !supported {
		if targetOS == "linux" {
			return nil, fmt.Errorf("Node manifest has no executable for %s", target)
		}
		return nil, nil
	}
	name := "plugins/node/" + rel
	path := filepath.Join(stage, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	if err := compile(targetOS, arch, path, "./plugins/node/native", ""); err != nil {
		return nil, fmt.Errorf("build Node %s: %w", target, err)
	}
	file, err := nodePayload(m, target, stage)
	if err != nil {
		return nil, err
	}
	return []payload{file}, nil
}

func nodePayload(m plugin.Manifest, target, stage string) (payload, error) {
	p := plugin.Plugin{Manifest: m, Directory: filepath.Join(stage, "plugins", "node")}
	path, err := p.NativeExecutable(target)
	if err != nil {
		return payload{}, err
	}
	if err := plugin.CheckBinary(path, target); err != nil {
		return payload{}, fmt.Errorf("Node executable: %w", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return payload{}, err
	}
	return payload{"plugins/node/" + m.Executables[target], b, 0755}, nil
}

func buildNodeOnly(targetOS, arch, out string) error {
	if out == "" {
		return fmt.Errorf("--out is required")
	}
	if targetOS != "linux" || (arch != "amd64" && arch != "arm64") {
		return fmt.Errorf("native Node backend pending for %s/%s", targetOS, arch)
	}
	stage, err := os.MkdirTemp("", "atm-node-build-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	files, err := buildNode(targetOS, arch, stage)
	if err != nil {
		return err
	}
	if len(files) != 1 {
		return fmt.Errorf("Node manifest has no executable for %s/%s", targetOS, arch)
	}
	return publishNode(out, files[0])
}

// Source-checkout builds may replace an older generated binary, never a link.
func publishNode(out string, file payload) error {
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
	f, err := os.CreateTemp(parent, ".atm-node-*")
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

func excludedRuntimePath(path string) bool {
	path = filepath.ToSlash(path)
	for _, part := range strings.Split(path, "/") {
		if strings.HasPrefix(part, ".") || part == "tests" || part == "__pycache__" || part == "internal-docs" {
			return true
		}
	}
	if path == "plugins/node/native" || strings.HasPrefix(path, "plugins/node/native/") ||
		path == "plugins/node/bin" || strings.HasPrefix(path, "plugins/node/bin/") {
		return true
	}
	return strings.HasSuffix(path, ".go") || strings.HasSuffix(path, ".pyc") || strings.HasSuffix(path, ".log")
}
