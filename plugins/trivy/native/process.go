package main

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
	"time"
)

type commandRunner func(context.Context, string, []string, time.Duration) (string, error)
type boundedOutput struct {
	mu       sync.Mutex
	data     []byte
	overflow bool
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	remaining := 8192 - len(b.data)
	if n > remaining {
		b.overflow = true
		p = p[:remaining]
	}
	b.data = append(b.data, p...)
	return n, nil
}
func runCommand(ctx context.Context, name string, args []string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = nil
	cmd.WaitDelay = time.Second
	// Inherit CORE's process group: its cancellation also terminates Docker's
	// descendants. The local command deadline terminates the direct Docker CLI.
	stdout, stderr := &boundedOutput{}, &boundedOutput{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		return "", fmt.Errorf("Docker command failed: %w (%s)", err, string(stderr.data))
	}
	if len(args) > 0 && (args[0] == "image" || args[0] == "info") && stdout.overflow {
		return "", fmt.Errorf("Docker image output exceeds 8 KiB")
	}
	return string(stdout.data), nil
}
