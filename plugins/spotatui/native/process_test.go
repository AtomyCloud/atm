//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/AtomyCloud/atm/internal/config"
	"github.com/AtomyCloud/atm/internal/plugin"
	"github.com/AtomyCloud/atm/internal/runner"
	"time"
)

func compiledFixtures(t *testing.T) (string, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "compiled fixtures with spaces")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin/go")
	build := func(out string, args ...string) {
		cmd := exec.Command(goBin, append([]string{"build", "-o", out}, args...)...)
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if data, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("fixture build: %v %s", err, data)
		}
	}
	native := filepath.Join(dir, "atm-spotatui")
	build(native, "github.com/AtomyCloud/atm/plugins/spotatui/native")
	data, err := os.ReadFile("process_fixture.txt")
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(dir, "main.go")
	write(t, source, data, 0600)
	helper := filepath.Join(dir, "synthetic-process")
	build(helper, source)
	return native, helper
}
func copied(t *testing.T, source, target string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		t.Fatal(err)
	}
	if err := copyExecutable(context.Background(), source, target); err != nil {
		t.Fatal(err)
	}
}
func TestCompiledLauncherStreamsArgumentsAndCustomLocalName(t *testing.T) {
	native, helper := compiledFixtures(t)
	b := newTestBackend(t)
	mockDeps(b)
	releasesFixture(t, b, "0.47.0")
	if err := action(b, "install", "--trust-dependencies"); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(b.root, "0.47.0")
	for _, name := range []string{"bin/spotatui", "shims/yt-dlp", "libexec/spotatui"} {
		target := filepath.Join(dir, name)
		if err := os.Remove(target); err != nil {
			t.Fatal(err)
		}
		source := native
		if name == "libexec/spotatui" {
			source = helper
		}
		copied(t, source, target)
	}
	external := filepath.Join(t.TempDir(), "external tools with spaces")
	for _, name := range []string{"yt-dlp", "node", "ffmpeg", "ffprobe"} {
		copied(t, helper, filepath.Join(external, name))
	}
	d := dependencies{YTDLP: filepath.Join(external, "yt-dlp"), FFmpeg: filepath.Join(external, "ffmpeg"), FFprobe: filepath.Join(external, "ffprobe"), Runtime: "node", RuntimePath: filepath.Join(external, "node")}
	data, _ := json.Marshal(d)
	write(t, filepath.Join(dir, dependencyFile), data, 0600)
	// A custom-named local symlink must dispatch by the executable's real layout.
	custom := filepath.Join(t.TempDir(), "custom command/music")
	if err := os.MkdirAll(filepath.Dir(custom), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(b.root, "current/bin/spotatui"), custom); err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(t.TempDir(), "local bin/spotatui")
	if err := os.MkdirAll(filepath.Dir(local), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(b.root, "current/bin/spotatui"), local); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", external)
	t.Setenv("ATM_TEST_MARKER", "keep user config and environment")
	t.Setenv("ATM_TEST_EXIT", "37")
	t.Setenv("ATM_TEST_MODE", "echo")
	args := []string{"--version", "a b", "$HOME", "; touch forbidden", "'quoted'", "", "--config-location", "user config"}
	type result struct {
		Args       []string `json:"args"`
		Stdin      string   `json:"stdin"`
		Path       string   `json:"path"`
		Marker     string   `json:"marker"`
		ShimActive string   `json:"shim_active"`
	}
	run := func(path, mode string, wantArgs []string, wantPath string) {
		t.Helper()
		cmd := exec.Command(path, args...)
		cmd.Env = setEnv(os.Environ(), "ATM_TEST_MODE", mode)
		cmd.Stdin = strings.NewReader("literal input\n")
		var out, stderr bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &stderr
		err := cmd.Run()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 37 {
			t.Fatalf("exit forwarding: %v; stderr=%s stdout=%s", err, stderr.String(), out.String())
		}
		var got result
		if err = json.Unmarshal(out.Bytes(), &got); err != nil {
			t.Fatal(err, out.String())
		}
		if !reflect.DeepEqual(got.Args, wantArgs) || got.Stdin != "literal input\n" || got.Path != wantPath || got.Marker != os.Getenv("ATM_TEST_MARKER") {
			t.Fatalf("forwarding: %+v", got)
		}
		if stderr.String() != "synthetic stderr\n" {
			t.Fatal(stderr.String())
		}
	}
	scopedPath := strings.Join([]string{filepath.Join(dir, "shims"), external, external, external}, ":")
	for _, path := range []string{filepath.Join(dir, "bin/spotatui"), filepath.Join(b.root, "current/bin/spotatui"), local, custom} {
		run(path, "echo", args, scopedPath)
	}
	shimArgs := append([]string{"--no-js-runtimes", "--js-runtimes", "node:" + d.RuntimePath}, args...)
	run(filepath.Join(dir, "shims/yt-dlp"), "echo", shimArgs, external)
	run(custom, "chain", shimArgs, scopedPath)
	if os.Getenv("PATH") != external {
		t.Fatal("parent PATH mutated")
	}
	t.Setenv("ATM_TEST_MODE", "recursion")
	t.Setenv("ATM_TEST_SHIM", filepath.Join(dir, "shims/yt-dlp"))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(dir, "shims/yt-dlp"))
	out, err := cmd.CombinedOutput()
	if err == nil || ctx.Err() != nil || !strings.Contains(string(out), "recursive") {
		t.Fatal("shim recursion not refused", err, string(out))
	}
	// Hard-linking the shim itself as the dependency is also rejected before exec.
	recursive := filepath.Join(external, "recursive")
	if err = os.Link(filepath.Join(dir, "shims/yt-dlp"), recursive); err != nil {
		t.Fatal(err)
	}
	d.YTDLP = recursive
	data, _ = json.Marshal(d)
	write(t, filepath.Join(dir, dependencyFile), data, 0600)
	cmd = exec.Command(filepath.Join(dir, "shims/yt-dlp"))
	out, err = cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "recursive") {
		t.Fatal(err, string(out))
	}
}

func TestCompiledDependencyProbesAndProcessGroupCleanup(t *testing.T) {
	_, helper := compiledFixtures(t)
	b := newTestBackend(t)
	dir := filepath.Join(t.TempDir(), "probe tools")
	for _, name := range []string{"yt-dlp", "node", "ffmpeg", "ffprobe"} {
		copied(t, helper, filepath.Join(dir, name))
	}
	t.Setenv("PATH", dir)
	t.Setenv("ATM_TEST_MODE", "probe")
	b.look = findExecutable
	b.run = runProbe
	d, err := b.dependencies(context.Background())
	if err != nil || d.Runtime != "node" {
		t.Fatal(d, err)
	}
	t.Setenv("ATM_TEST_MODE", "overflow")
	if _, err = runProbe(context.Background(), helper, nil); err == nil || !strings.Contains(err.Error(), "64 KiB") {
		t.Fatal("unbounded output", err)
	}

}
func stopped(pid int) bool {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	return os.IsNotExist(err) || strings.Contains(string(data), ") Z ")
}

func TestCompiledCoreRunnerCancelsProbeTreeAndCleansFailure(t *testing.T) {
	native, helper := compiledFixtures(t)
	for _, mode := range []string{"parent", "runtime-error"} {
		t.Run(mode, func(t *testing.T) {
			b := newTestBackend(t)
			runtimeDir := t.TempDir()
			pluginDir := filepath.Join(runtimeDir, "plugins/spotatui")
			data, err := os.ReadFile("../plugin.json")
			if err != nil {
				t.Fatal(err)
			}
			write(t, filepath.Join(pluginDir, "plugin.json"), data, 0600)
			copied(t, native, filepath.Join(pluginDir, "bin/linux-"+runtime.GOARCH+"/atm-spotatui"))
			toolsDir := filepath.Join(t.TempDir(), "external probes")
			for _, name := range []string{"yt-dlp", "deno", "node", "ffmpeg", "ffprobe"} {
				copied(t, helper, filepath.Join(toolsDir, name))
			}
			t.Setenv("PATH", toolsDir)
			t.Setenv("ATM_TEST_MODE", mode)
			evidence := t.TempDir()
			childFile := filepath.Join(evidence, "child.pid")
			parentFile := filepath.Join(evidence, "parent.pid")
			calls := filepath.Join(evidence, "calls")
			t.Setenv("ATM_TEST_PID", childFile)
			t.Setenv("ATM_TEST_PARENT_PID", parentFile)
			t.Setenv("ATM_TEST_CALLS", calls)
			c := config.Config{Home: runtimeDir, PluginsDir: filepath.Join(runtimeDir, "plugins"), AppsDir: b.req.AppsDir, Language: "en-us"}
			ps, err := plugin.Discover(c, "linux")
			if err != nil || len(ps) != 1 {
				t.Fatal(ps, err)
			}
			plan, err := runner.Plugin(c, ps[0], "install", []string{"--trust-dependencies"})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan runner.Outcome, 1)
			go func() { done <- runner.RunNative(ctx, c, plan, runner.NativeOptions{Timeout: 8 * time.Second}) }()
			received := false
			// Cleanup is registered before any assertion; a regression cannot leak fixtures.
			defer func() {
				cancel()
				if !received {
					select {
					case <-done:
					case <-time.After(10 * time.Second):
					}
				}
				for _, file := range []string{parentFile, childFile} {
					data, _ := os.ReadFile(file)
					pid, _ := strconv.Atoi(string(data))
					if pid > 0 && !stopped(pid) {
						_ = syscall.Kill(pid, syscall.SIGKILL)
					}
				}
			}()
			waitPID := func(file string) int {
				for i := 0; i < 500; i++ {
					data, err := os.ReadFile(file)
					if err == nil {
						pid, _ := strconv.Atoi(string(data))
						if pid > 0 {
							return pid
						}
					}
					time.Sleep(10 * time.Millisecond)
				}
				t.Fatal("fixture readiness timed out", file)
				return 0
			}
			child, parent := waitPID(childFile), waitPID(parentFile)
			if mode == "parent" {
				cancel()
			}
			select {
			case out := <-done:
				received = true
				if mode == "parent" && out.Status != "cancelled" {
					t.Fatal(out)
				}
				if mode == "runtime-error" && out.Status != "error" {
					t.Fatal(out)
				}
			case <-time.After(4 * time.Second):
				t.Fatal("CORE did not promptly finish probe failure/cancellation")
			}
			for _, pid := range []int{child, parent} {
				for i := 0; i < 100 && !stopped(pid); i++ {
					time.Sleep(10 * time.Millisecond)
				}
				if !stopped(pid) {
					t.Fatal("CORE left fixture process running", pid)
				}
			}
			if mode == "runtime-error" {
				data, _ := os.ReadFile(calls)
				if strings.Contains(string(data), "node\n") {
					t.Fatal("runtime execution failure fell back to Node", string(data))
				}
			}
			absent(t, b.root)
		})
	}
}

func TestDependencyFailuresAndProbeArguments(t *testing.T) {
	for _, kind := range []string{"missing-ffmpeg", "old-yt", "bad-date", "old-runtime", "missing-ejs", "missing-runtime-diagnostic", "wrong-exit", "probe-failure"} {
		t.Run(kind, func(t *testing.T) {
			b := newTestBackend(t)
			mockDeps(b)
			baseLook, baseRun := b.look, b.run
			b.look = func(name string) (string, error) {
				if kind == "missing-ffmpeg" && name == "ffmpeg" {
					return "", os.ErrNotExist
				}
				return baseLook(name)
			}
			b.run = func(ctx context.Context, path string, args []string) (probeResult, error) {
				out, err := baseRun(ctx, path, args)
				if filepath.Base(path) == "node" {
					if kind == "old-runtime" {
						out.stdout = "v20.0.0\n"
					}
					return out, err
				}
				joined := strings.Join(args, " ")
				for _, flag := range []string{"--ignore-config", "--no-plugin-dirs", "--no-cache-dir", "--no-remote-components"} {
					if !strings.Contains(joined, flag) {
						t.Fatal("missing diagnostic isolation", args)
					}
				}
				if args[len(args)-1] == "--version" {
					if kind == "old-yt" {
						out.stdout = "2025.11.11\n"
					}
					if kind == "bad-date" {
						out.stdout = "2025.19.99\n"
					}
					return out, err
				}
				if !strings.Contains(joined, "--no-js-runtimes --js-runtimes node:") {
					t.Fatal("default runtime was not cleared", args)
				}
				switch kind {
				case "missing-ejs":
					out.stderr = "[debug] JS runtimes: node-22.0.0\n"
				case "missing-runtime-diagnostic":
					out.stderr = "yt_dlp_ejs-0.3.1\n"
				case "wrong-exit":
					out.code = 0
				case "probe-failure":
					return out, context.DeadlineExceeded
				}
				return out, err
			}
			if err := action(b, "install", "--trust-dependencies"); err == nil {
				t.Fatal("bad dependency accepted")
			}
			absent(t, b.root)
		})
	}
}
