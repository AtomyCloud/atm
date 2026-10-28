//go:build !linux

package main

import (
	"context"
	"fmt"
	"os"
)

func ownedByCurrentUser(os.FileInfo) bool             { return false }
func rootOwned(os.FileInfo) bool                      { return false }
func openNoFollow(*os.Root, string) (*os.File, error) { return nil, fmt.Errorf("Linux required") }
func renameNoReplace(string, string) error            { return fmt.Errorf("Linux required") }
func systemCommand(string) (string, error)            { return "", fmt.Errorf("Linux required") }
func trustedSystemFile(string, bool) error            { return fmt.Errorf("Linux required") }
func browserClosed(context.Context, string) error     { return fmt.Errorf("Linux required") }
