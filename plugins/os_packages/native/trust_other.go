//go:build !linux

package main

import (
	"fmt"
	"os"
)

func systemCommand(string) (string, error) {
	return "", fmt.Errorf("OS Packages native backend is Linux-only")
}

func ownedByCurrentUser(os.FileInfo) bool { return false }

func journalDirectory(string, bool) error {
	return fmt.Errorf("OS Packages native backend is Linux-only")
}
