//go:build !linux && !darwin && !windows

package coreupdate

import "os"

func lockFile(*os.File) error                { return ErrUnsupported }
func singleLink(*os.File, os.FileInfo) error { return ErrUnsupported }
