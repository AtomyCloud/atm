//go:build linux

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func hermesHelperArgs(mode string, args ...string) []string {
	return append([]string{"-test.run=^TestHermesProcessHelper$", "--", mode}, args...)
}

// All subprocesses are this Go test binary, selected by argv rather than an
// environment marker so the empty-environment contract can also be exercised.
func TestHermesProcessHelper(t *testing.T) {
	if len(os.Args) < 4 || os.Args[2] != "--" {
		return
	}
	args := os.Args[4:]
	fail := func(err error) {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(91)
		}
	}
	switch os.Args[3] {
	case "inspect":
		dir, err := os.Getwd()
		fail(err)
		stdin, err := io.ReadAll(io.LimitReader(os.Stdin, 1))
		fail(err)
		fail(json.NewEncoder(os.Stdout).Encode(struct {
			Args, Env  []string
			Dir, Stdin string
		}{args, os.Environ(), dir, string(stdin)}))
		fmt.Fprint(os.Stderr, "separate stderr")
	case "output":
		n, err := strconv.Atoi(args[1])
		fail(err)
		if args[0] == "stdout" || args[0] == "both" {
			_, err = io.WriteString(os.Stdout, strings.Repeat("o", n))
			fail(err)
		}
		if args[0] == "stderr" || args[0] == "both" {
			_, err = io.WriteString(os.Stderr, strings.Repeat("e", n))
			fail(err)
		}
		if len(args) > 2 {
			for {
				time.Sleep(time.Second)
			}
		}
	case "fail":
		fmt.Fprint(os.Stdout, "secret-stdout")
		fmt.Fprint(os.Stderr, "secret-stderr")
		os.Exit(23)
	case "tree", "orphan":
		fail(os.WriteFile(filepath.Join(args[0], "parent.pid"), []byte(strconv.Itoa(os.Getpid())), 0600))
		self, err := os.Executable()
		fail(err)
		child := exec.Command(self, hermesHelperArgs("child", args[0])...)
		child.Env = []string{}
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		fail(child.Start())
		if os.Args[3] == "orphan" {
			os.Exit(0)
		}
		fail(child.Wait())
	case "child":
		fail(os.WriteFile(filepath.Join(args[0], "child.pid"), []byte(strconv.Itoa(os.Getpid())), 0600))
		for {
			time.Sleep(time.Second)
		}
	default:
		os.Exit(92)
	}
	os.Exit(0)
}

func hermesTestExecutable(t *testing.T) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return self
}

func TestHermesCommandExactInvocation(t *testing.T) {
	t.Setenv("ATM_HERMES_PRIVATE", "must-not-inherit")
	t.Setenv("PWD", "/incorrect-parent-directory")
	dir := t.TempDir()
	args := []string{"$(touch forbidden)", "; touch forbidden", "space argument", "", "--literal", "a=b", "line\nbreak"}
	for _, env := range [][]string{nil, {}, {"ONLY=allowed", "EMPTY=", "VALUE=a=b", "PWD=/explicit-value"}} {
		out, err := runHermesCommand(context.Background(), hermesTestExecutable(t), hermesHelperArgs("inspect", args...), dir, env)
		if err != nil {
			t.Fatal(err)
		}
		var got struct {
			Args, Env  []string
			Dir, Stdin string
		}
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got.Args, args) || !slices.Equal(got.Env, env) || got.Dir != dir || got.Stdin != "" {
			t.Fatalf("invocation mismatch: %+v", got)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "forbidden")); !os.IsNotExist(err) {
		t.Fatalf("shell syntax was executed: %v", err)
	}
}

func TestHermesCommandRejectsInvalidInvocation(t *testing.T) {
	self, dir := hermesTestExecutable(t), t.TempDir()
	for _, tc := range []struct {
		name, executable, dir string
		env                   []string
	}{
		{"relative executable", "program", dir, nil},
		{"relative directory", self, ".", nil},
		{"empty executable", "", dir, nil},
		{"empty directory", self, "", nil},
		{"missing executable", filepath.Join(dir, "missing"), dir, nil},
		{"missing directory", self, filepath.Join(dir, "missing"), nil},
		{"missing equals", self, dir, []string{"secret-entry"}},
		{"empty key", self, dir, []string{"=secret-value"}},
		{"duplicate key", self, dir, []string{"KEY=secret-first", "KEY=secret-second"}},
		{"nul entry", self, dir, []string{"KEY=secret-\x00value"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := runHermesCommand(context.Background(), tc.executable, hermesHelperArgs("inspect"), tc.dir, tc.env)
			if err == nil || out != "" || strings.Contains(err.Error(), "secret-") {
				t.Fatalf("expected redacted refusal, got %q, %v", out, err)
			}
		})
	}
}

func TestHermesCommandOutputBounds(t *testing.T) {
	self, dir := hermesTestExecutable(t), t.TempDir()
	for _, stream := range []string{"stdout", "stderr", "both"} {
		t.Run(stream+" exact limit", func(t *testing.T) {
			out, err := runHermesCommand(context.Background(), self, hermesHelperArgs("output", stream, strconv.Itoa(hermesCommandOutputLimit)), dir, nil)
			want := 0
			if stream != "stderr" {
				want = hermesCommandOutputLimit
			}
			if err != nil || len(out) != want || strings.Trim(out, "o") != "" {
				t.Fatalf("length=%d want=%d error=%v", len(out), want, err)
			}
		})
		for _, tail := range [][]string{nil, {"wait"}} {
			t.Run(fmt.Sprintf("%s overflow %v", stream, tail), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				args := append([]string{stream, strconv.Itoa(hermesCommandOutputLimit + 1)}, tail...)
				out, err := runHermesCommand(ctx, self, hermesHelperArgs("output", args...), dir, nil)
				if !errors.Is(err, errHermesCommandOutput) || out != "" || ctx.Err() != nil {
					t.Fatalf("overflow did not fail closed promptly: length=%d error=%v context=%v", len(out), err, ctx.Err())
				}
			})
		}
	}
}

func TestHermesCommandOutputCannotBypassWrite(t *testing.T) {
	cancelled := false
	w := &hermesCommandOutput{cancel: func() { cancelled = true }}
	if _, ok := any(w).(io.ReaderFrom); ok {
		t.Fatal("io.ReaderFrom can bypass the bounded Write method")
	}
	// Hide WriterTo to exercise io.Copy's destination ReaderFrom optimization.
	src := struct{ io.Reader }{strings.NewReader(strings.Repeat("x", 2*hermesCommandOutputLimit))}
	n, err := io.Copy(w, src)
	if err != nil || n != 2*hermesCommandOutputLimit || !w.overflow || !cancelled || w.buffer.Len() != hermesCommandOutputLimit {
		t.Fatalf("copy bypassed bound: n=%d retained=%d overflow=%v cancelled=%v err=%v", n, w.buffer.Len(), w.overflow, cancelled, err)
	}
	if _, err := w.Write([]byte("more")); err != nil || w.buffer.Len() != hermesCommandOutputLimit {
		t.Fatal("subsequent write exceeded bound", err)
	}
}

func TestHermesCommandExitFailure(t *testing.T) {
	out, err := runHermesCommand(context.Background(), hermesTestExecutable(t), hermesHelperArgs("fail", "secret-argument"), t.TempDir(), []string{"TOKEN=secret-env"})
	var exitErr *exec.ExitError
	if out != "" || !errors.As(err, &exitErr) || exitErr.ExitCode() != 23 || strings.Contains(err.Error(), "secret-") {
		t.Fatalf("exit failure was lost or leaked invocation/output: %q, %v", out, err)
	}
}

func TestHermesCommandAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out, err := runHermesCommand(ctx, hermesTestExecutable(t), hermesHelperArgs("inspect"), t.TempDir(), nil)
	if out != "" || !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled command was not refused", err)
	}
}

func hermesProcessStopped(pid int) bool {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	// An orphan zombie may await the host's reaper, but cannot execute or hold pipes.
	return os.IsNotExist(err) || err == nil && strings.Contains(string(data), ") Z ")
}

func TestHermesCommandDescendantCleanup(t *testing.T) {
	for _, mode := range []string{"cancel", "deadline", "orphan"} {
		t.Run(mode, func(t *testing.T) {
			dir, self := t.TempDir(), hermesTestExecutable(t)
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			if mode == "deadline" {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
			}
			fixtureMode := "tree"
			if mode == "orphan" {
				fixtureMode = "orphan"
			}
			done := make(chan error, 1)
			go func() {
				_, err := runHermesCommand(ctx, self, hermesHelperArgs(fixtureMode, dir), dir, nil)
				done <- err
			}()
			finished := false
			var pids []int
			t.Cleanup(func() {
				cancel()
				if !finished {
					select {
					case <-done:
					case <-time.After(5 * time.Second):
						t.Error("fixture cleanup timed out")
					}
				}
				for _, pid := range pids {
					if !hermesProcessStopped(pid) {
						_ = syscall.Kill(pid, syscall.SIGKILL)
					}
				}
			})
			for _, name := range []string{"parent.pid", "child.pid"} {
				until := time.Now().Add(4 * time.Second)
				pid := 0
				for time.Now().Before(until) {
					data, _ := os.ReadFile(filepath.Join(dir, name))
					pid, _ = strconv.Atoi(string(data))
					if pid > 0 {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				if pid <= 0 {
					t.Fatal("fixture did not become ready", name)
				}
				pids = append(pids, pid)
			}
			if pgid, err := syscall.Getpgid(pids[1]); err != nil || pgid != pids[0] {
				t.Fatalf("descendant not in isolated command group: pgid=%d parent=%d err=%v", pgid, pids[0], err)
			}
			if mode == "cancel" {
				cancel()
			}
			select {
			case err := <-done:
				finished = true
				want := error(context.Canceled)
				if mode == "deadline" {
					want = context.DeadlineExceeded
				} else if mode == "orphan" {
					want = exec.ErrWaitDelay
				}
				if !errors.Is(err, want) {
					t.Fatalf("got %v, want %v", err, want)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("command did not finish within cancellation/pipe bound")
			}
			for _, pid := range pids {
				until := time.Now().Add(time.Second)
				for !hermesProcessStopped(pid) && time.Now().Before(until) {
					time.Sleep(10 * time.Millisecond)
				}
				if !hermesProcessStopped(pid) {
					t.Fatal("command process survived", pid)
				}
			}
		})
	}
}
