//go:build !linux && !darwin && !windows

package install

import (
	"fmt"
	"os"
)

func lockFile(*os.File) error { return fmt.Errorf("installation locking unsupported on this platform") }
