//go:build !linux

package main

import "fmt"

func execNative(path string, args, env []string) error {
	return fmt.Errorf("spotatui launcher requires Linux")
}
