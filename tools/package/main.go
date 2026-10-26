// Build local migration packages. No installation, upload or publication.
package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/AtomyCloud/atm/internal/version"
)

type payload struct {
	name string
	data []byte
	mode fs.FileMode
}

var stamp = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)

func main() {
	targetOS := flag.String("os", "linux", "linux or windows")
	arch := flag.String("arch", "amd64", "amd64 or arm64")
	out := flag.String("out", "", "local output directory (required)")
	flag.Parse()
	if err := build(*targetOS, *arch, *out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func build(targetOS, arch, out string) error {
	if out == "" {
		return fmt.Errorf("--out is required")
	}
	if !(targetOS == "linux" && (arch == "amd64" || arch == "arm64") || targetOS == "windows" && arch == "amd64") {
		return fmt.Errorf("unsupported migration build target %s/%s", targetOS, arch)
	}
	if _, err := os.Stat("go.mod"); err != nil {
		return fmt.Errorf("run from the ATM repository root")
	}
	commit := "development"
	if b, err := exec.Command("git", "rev-parse", "HEAD").Output(); err == nil {
		commit = strings.TrimSpace(string(b))
		if status, err := exec.Command("git", "status", "--porcelain").Output(); err != nil || len(status) > 0 {
			commit += "-dirty"
		}
	}
	temp, err := os.MkdirTemp("", "atm-build-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	binaryName := "atm"
	if targetOS == "windows" {
		binaryName += ".exe"
	}
	binary := filepath.Join(temp, binaryName)
	cmd := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-ldflags", "-s -w -buildid= -X github.com/AtomyCloud/atm/internal/version.Commit="+commit, "-o", binary, "./cmd/atm")
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if key != "GOOS" && key != "GOARCH" && key != "CGO_ENABLED" {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "GOOS="+targetOS, "GOARCH="+arch, "CGO_ENABLED=0")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	b, err := os.ReadFile(binary)
	if err != nil {
		return err
	}
	files := []payload{{binaryName, b, 0755}}
	for _, root := range []string{"bin", "lib", "libexec", "plugins", "lang", "core-api.json", "LICENSE"} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if strings.HasPrefix(d.Name(), ".") || d.Name() == "tests" || d.Name() == "__pycache__" {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("non-regular runtime file refused: %s", path)
			}
			if strings.HasSuffix(path, ".pyc") || strings.HasSuffix(path, ".log") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			mode := fs.FileMode(0644)
			if info.Mode()&0111 != 0 || strings.HasPrefix(filepath.ToSlash(path), "bin/") {
				mode = 0755
			}
			files = append(files, payload{filepath.ToSlash(path), data, mode})
			return nil
		})
		if err != nil {
			return err
		}
	}
	guide, err := os.ReadFile("docs/en-us/MIGRATION-GO-0.0.2.md")
	if err != nil {
		return err
	}
	files = append(files, payload{"README.md", guide, 0644})
	hashes := map[string]string{}
	for _, f := range files {
		hashes[f.name] = fmt.Sprintf("%x", sha256.Sum256(f.data))
	}
	manifest, err := json.MarshalIndent(map[string]any{"schema": 1, "version": version.Version, "commit": commit, "os": targetOS, "arch": arch, "migration": true, "files": hashes}, "", "  ")
	if err != nil {
		return err
	}
	files = append(files, payload{"release.json", append(manifest, '\n'), 0644})
	sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })
	if err := os.MkdirAll(out, 0755); err != nil {
		return err
	}
	name := "atm-" + version.Version + "-" + targetOS + "-" + arch
	ext := ".tar.gz"
	if targetOS == "windows" {
		ext = ".zip"
	}
	path := filepath.Join(out, name+ext)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	good := false
	defer func() {
		f.Close()
		if !good {
			os.Remove(path)
		}
	}()
	if err := archive(f, name, files, targetOS == "windows"); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	archiveFile, err := os.Open(path)
	if err != nil {
		return err
	}
	defer archiveFile.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, archiveFile); err != nil {
		return err
	}
	checksum, err := os.OpenFile(path+".sha256", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	_, writeErr := fmt.Fprintf(checksum, "%x  %s\n", hash.Sum(nil), filepath.Base(path))
	closeErr := checksum.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	good = true
	fmt.Println(path)
	return nil
}

func archive(out io.Writer, root string, files []payload, windows bool) error {
	if windows {
		w := zip.NewWriter(out)
		for _, f := range files {
			h := &zip.FileHeader{Name: root + "/" + f.name, Method: zip.Deflate, Modified: stamp}
			h.SetMode(f.mode)
			entry, err := w.CreateHeader(h)
			if err != nil {
				return err
			}
			if _, err = entry.Write(f.data); err != nil {
				return err
			}
		}
		return w.Close()
	}
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	for _, f := range files {
		if err := tw.WriteHeader(&tar.Header{Name: root + "/" + f.name, Mode: int64(f.mode), Size: int64(len(f.data)), ModTime: stamp, Typeflag: tar.TypeReg}); err != nil {
			return err
		}
		if _, err := tw.Write(f.data); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}
