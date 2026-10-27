//go:build !linux && !darwin && !windows

package updates

import (
	"fmt"
	"os"
)

func lockFile(*os.File) error {
	return fmt.Errorf("component update locking unsupported on this platform")
}
