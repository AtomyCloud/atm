package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func isLauncher() bool {
	self, err := os.Executable()
	return err == nil && filepath.Base(filepath.Dir(self)) == "bin" && filepath.Base(self) == "rea"
}
func copyLauncher(ctx context.Context, dest string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	in, err := os.Open(self)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		return err
	}
	n, err := io.Copy(out, io.LimitReader(contextReader{ctx, in}, maxFile+1))
	if err == nil && n > maxFile {
		err = fmt.Errorf("launcher too large")
	}
	if err == nil {
		err = out.Sync()
	}
	e := out.Close()
	if err == nil {
		err = e
	}
	return err
}
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
	if err = realDirs(dir, false); err != nil {
		return err
	}
	var d dependencies
	if err = privateJSON(filepath.Join(dir, dependencyFile), &d); err != nil {
		return err
	}
	if err = d.validate(); err != nil {
		return err
	}
	node, err := nativeExecutable(d.Node)
	if err != nil {
		return err
	}
	if node != d.Node {
		return fmt.Errorf("pinned runtime path changed")
	}
	sum, err := hashFile(context.Background(), node)
	if err != nil {
		return err
	}
	if sum != d.NodeSHA256 {
		return fmt.Errorf("pinned Node changed; refresh through ATM install/update")
	}
	script := filepath.Join(dir, "lib/node_modules/rea-agents/scripts/rea.mjs")
	if _, err = readRegular(script, 16<<20); err != nil {
		return err
	}
	if within(node, dir) {
		return fmt.Errorf("recursive Node launcher refused")
	}
	path := cleanSearchPath(strings.Join([]string{filepath.Dir(node), filepath.Dir(d.PS), os.Getenv("PATH")}, ":"))
	env := []string{}
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "PATH=") {
			env = append(env, e)
		}
	}
	env = append(env, "PATH="+path)
	return execNative(node, append([]string{node, script}, os.Args[1:]...), env)
}
