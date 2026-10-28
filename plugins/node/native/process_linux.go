package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"
)

type boundedOutput struct {
	mu       sync.Mutex
	data     []byte
	overflow bool
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	remain := 8192 - len(b.data)
	if n > remain {
		b.overflow = true
		p = p[:remain]
	}
	b.data = append(b.data, p...)
	return n, nil
}
func runCommand(ctx context.Context, name string, args []string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = nil
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	// Inherit CORE's process group so its cancellation also reaches our children.
	// CommandContext kills the direct child on a local deadline; privileged APT
	// descendants may require administrator recovery and cannot be rolled back.
	cmd.WaitDelay = time.Second
	out := &boundedOutput{}
	cmd.Stdout = out
	cmd.Stderr = out
	err := cmd.Run()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		return "", err
	}
	if out.overflow && len(args) == 1 && args[0] == "--version" {
		return "", fmt.Errorf("command output exceeds 8 KiB")
	}
	return string(out.data), nil
}
