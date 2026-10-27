//go:build !windows

package setup

import (
	"context"
	"os"
)

func integrateWindows(_ context.Context, _ *os.Root, _ Options, result Result) (Result, error) {
	return result, ErrUnavailable
}
