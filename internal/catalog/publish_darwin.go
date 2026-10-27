package catalog

import (
	"os"

	"golang.org/x/sys/unix"
)

func publishNoReplace(from, to *os.File, old, name string) error {
	return unix.RenameatxNp(int(from.Fd()), old, int(to.Fd()), name, unix.RENAME_EXCL)
}
