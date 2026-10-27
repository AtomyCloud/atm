//go:build !linux && !darwin && !windows

package catalog

import (
	"fmt"
	"os"
)

func publishNoReplace(from, to *os.File, old, name string) error {
	return fmt.Errorf("atomic non-replacing publication is unsupported on this platform")
}
