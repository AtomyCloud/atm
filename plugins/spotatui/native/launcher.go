package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const dependencyFile = ".atm-dependencies.json"

func isLauncher() bool {
	self, err := os.Executable()
	if err != nil {
		return false
	}
	name, dir := filepath.Base(self), filepath.Base(filepath.Dir(self))
	return (name == "spotatui" && dir == "bin") || (name == "yt-dlp" && dir == "shims")
}
func readDependencies(dir string) (dependencies, error) {
	var d dependencies
	p := filepath.Join(dir, dependencyFile)
	i, err := os.Lstat(p)
	if err != nil {
		return d, err
	}
	if !i.Mode().IsRegular() || i.Mode().Perm() != 0600 || i.Size() > 16384 {
		return d, fmt.Errorf("unsafe dependency record")
	}
	f, err := os.Open(p)
	if err != nil {
		return d, err
	}
	defer f.Close()
	dec := json.NewDecoder(io.LimitReader(f, 16385))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&d); err != nil {
		return d, err
	}
	if dec.Decode(new(any)) != io.EOF {
		return d, fmt.Errorf("trailing dependency data")
	}
	return d, d.validate()
}
func validateSupport(r *os.Root) error {
	for _, name := range []string{"bin/spotatui", "shims/yt-dlp", dependencyFile} {
		i, err := r.Lstat(name)
		if err != nil || !i.Mode().IsRegular() || i.Size() <= 0 || i.Size() > limits.FileBytes {
			return fmt.Errorf("missing/unsafe native support file %s", name)
		}
		if name != dependencyFile && i.Mode().Perm()&0111 == 0 {
			return fmt.Errorf("support launcher not executable")
		}
	}
	return nil
}
func copyExecutable(ctx context.Context, source, dest string) error {
	i, err := os.Stat(source)
	if err != nil {
		return err
	}
	if !i.Mode().IsRegular() || i.Size() <= 0 || i.Size() > limits.FileBytes {
		return fmt.Errorf("unsafe native launcher source")
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		return err
	}
	n, err := io.Copy(out, io.LimitReader(contextReader{ctx, in}, i.Size()+1))
	if err == nil && n != i.Size() {
		err = fmt.Errorf("launcher source changed")
	}
	if err == nil {
		err = out.Sync()
	}
	closeErr := out.Close()
	if err != nil {
		return err
	}
	return closeErr
}
func writeSupport(ctx context.Context, payload string, d dependencies) error {
	if err := d.validate(); err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	for _, name := range []string{"bin/spotatui", "shims/yt-dlp"} {
		if err = os.Mkdir(filepath.Dir(filepath.Join(payload, name)), 0700); err != nil {
			return err
		}
		if err = copyExecutable(ctx, self, filepath.Join(payload, name)); err != nil {
			return err
		}
	}
	data, err := json.Marshal(d)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(payload, dependencyFile), data, 0600)
}

// The launcher resolves its actual executable, not argv[0] or the working
// directory. Both current and user-local symlinks therefore share one layout.
func launch() error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	self, err = filepath.EvalSymlinks(self)
	if err != nil {
		return err
	}
	dir := filepath.Dir(filepath.Dir(self))
	kind := filepath.Base(filepath.Dir(self))
	if (kind != "bin" || filepath.Base(self) != "spotatui") && (kind != "shims" || filepath.Base(self) != "yt-dlp") {
		return fmt.Errorf("invalid native launcher location")
	}
	if err = realDirs(dir, false); err != nil {
		return err
	}
	d, err := readDependencies(dir)
	if err != nil {
		return err
	}
	env := os.Environ()
	target := filepath.Join(dir, "libexec/spotatui")
	args := append([]string{}, os.Args[1:]...)
	if kind == "shims" {
		if os.Getenv("ATM_SPOTATUI_SHIM_ACTIVE") == "1" {
			return fmt.Errorf("recursive yt-dlp shim refused")
		}
		target = d.YTDLP
		args = append([]string{"--no-js-runtimes", "--js-runtimes", d.Runtime + ":" + d.RuntimePath}, args...)
		env = setEnv(env, "ATM_SPOTATUI_SHIM_ACTIVE", "1")
	} else {
		if os.Getenv("ATM_SPOTATUI_LAUNCH_ACTIVE") == "1" {
			return fmt.Errorf("recursive player launcher refused")
		}
		path := strings.Join([]string{filepath.Join(dir, "shims"), filepath.Dir(d.FFmpeg), filepath.Dir(d.FFprobe), os.Getenv("PATH")}, string(os.PathListSeparator))
		env = setEnv(env, "PATH", path)
		env = setEnv(env, "ATM_SPOTATUI_LAUNCH_ACTIVE", "1")
	}
	targetInfo, err := os.Stat(target)
	if err != nil {
		return err
	}
	selfInfo, err := os.Stat(self)
	if err != nil {
		return err
	}
	if !targetInfo.Mode().IsRegular() || targetInfo.Mode().Perm()&0111 == 0 || os.SameFile(selfInfo, targetInfo) {
		return fmt.Errorf("unsafe/recursive launch target")
	}
	return execNative(target, append([]string{target}, args...), env)
}
