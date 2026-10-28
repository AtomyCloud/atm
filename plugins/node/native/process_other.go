//go:build !linux

package main

import (
	"context"
	"fmt"
	"time"
)

func runCommand(context.Context, string, []string, time.Duration) (string, error) {
	return "", fmt.Errorf("native Node process execution supports Linux only")
}
