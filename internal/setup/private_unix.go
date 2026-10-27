//go:build !windows

package setup

import "os"

func secureDirectory(root *os.Root, name string) error { return root.Chmod(name, 0700) }
