package main

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
	"time"
)

type tailOutput struct {
	mu        sync.Mutex
	data      []byte
	truncated bool
}

func (w *tailOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	const limit = 32 << 10
	if len(p) >= limit {
		w.data = append(w.data[:0], p[len(p)-limit:]...)
		w.truncated = true
	} else {
		if extra := len(w.data) + len(p) - limit; extra > 0 {
			w.data = w.data[extra:]
			w.truncated = true
		}
		w.data = append(w.data, p...)
	}
	return n, nil
}
func commandEnvironment() []string {
	return []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "DEBIAN_FRONTEND=noninteractive", "TERM=dumb"}
}
func runCommand(ctx context.Context, path string, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = commandEnvironment()
	cmd.Dir = "/"
	cmd.Stdin = nil
	cmd.WaitDelay = time.Second
	// Do not detach from CORE's group. Privileged manager cleanup still depends on sudo/OS policy.
	output := &tailOutput{}
	cmd.Stdout = output
	cmd.Stderr = output
	err := cmd.Run()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	s := string(output.data)
	if output.truncated {
		s = "[earlier output truncated]\n" + s
	}
	if err != nil {
		return s, fmt.Errorf("%s: %w", path, err)
	}
	return s, nil
}
