//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	hermesCommandOutputLimit = 1 << 20 // Per stream.
	hermesCommandTimeout     = 30 * time.Minute
	hermesCommandWaitDelay   = time.Second
)

var errHermesCommandOutput = errors.New("Hermes command output exceeded bound")

type hermesCommandOutput struct {
	// Do not embed bytes.Buffer: its promoted ReadFrom bypasses Write in io.Copy.
	buffer   bytes.Buffer
	overflow bool
	cancel   context.CancelFunc
}

func (w *hermesCommandOutput) Write(p []byte) (int, error) {
	n := len(p)
	left := hermesCommandOutputLimit - w.buffer.Len()
	if n > left {
		p = p[:left]
		if !w.overflow {
			w.overflow = true
			w.cancel()
		}
	}
	_, _ = w.buffer.Write(p)
	return n, nil
}

// runHermesCommand bounds an explicitly selected command, not arbitrary upstream
// code. It is not a sandbox: descendants can escape a process group. The caller
// owns executable trust, consent, environment policy, routing and lifecycle.
// Only successful stdout is returned; stderr, arguments and environment values
// are never logged or included in errors. Stdout is upstream data, not safe log content.
func runHermesCommand(ctx context.Context, executable string, args []string, dir string, env []string) (string, error) {
	if !filepath.IsAbs(executable) || !filepath.IsAbs(dir) {
		return "", errors.New("Hermes command requires absolute executable and working directory")
	}
	// exec.Cmd otherwise silently deduplicates keys. Reject inputs that cannot be
	// passed through exactly, and keep nil env distinct from inheriting os.Environ.
	seen := make(map[string]bool, len(env))
	for _, entry := range env {
		key, _, ok := strings.Cut(entry, "=")
		if !ok || key == "" || strings.ContainsRune(entry, 0) || seen[key] {
			return "", errors.New("invalid or duplicate Hermes command environment entry")
		}
		seen[key] = true
	}
	ctx, cancel := context.WithTimeout(ctx, hermesCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Dir = dir
	cmd.Env = append([]string{}, env...)
	cmd.Stdin = nil
	cmd.WaitDelay = hermesCommandWaitDelay
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	killGroup := func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.Cancel = killGroup
	stdout := &hermesCommandOutput{cancel: cancel}
	stderr := &hermesCommandOutput{cancel: cancel}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err := cmd.Run()
	if err != nil || ctx.Err() != nil || stdout.overflow || stderr.overflow {
		// WaitDelay can expire after the leader exits while descendants retain its
		// pipes. exec's fallback kills only the leader, so also clean up its group.
		_ = killGroup()
		if stdout.overflow || stderr.overflow {
			return "", errHermesCommandOutput
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("Hermes command failed: %w", err)
	}
	return stdout.buffer.String(), nil
}
