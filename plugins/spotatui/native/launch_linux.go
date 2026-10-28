//go:build linux

package main

import "syscall"

func execNative(path string, args, env []string) error { return syscall.Exec(path, args, env) }
