//go:build linux

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/AtomyCloud/atm/internal/config"
	"github.com/AtomyCloud/atm/internal/plugin"
	"github.com/AtomyCloud/atm/internal/protocol"
	"github.com/AtomyCloud/atm/internal/runner"
)

var fixtureBinary string

func TestMain(m *testing.M) {
	if os.Getenv("ATM_OS_PACKAGES_TEST_HELPER") == "1" {
		err := serveWith(context.Background(), os.Stdin, os.Stdout, func(req protocol.Request, log func(string)) (*backend, error) {
			b, e := newBackend(req, log)
			if e != nil {
				return nil, e
			}
			b.system = func() (systemInfo, error) { return detectSystem([]byte("ID=ubuntu\nVERSION_CODENAME=noble\n"), false) }
			b.uid = func() int { return 1000 }
			b.resolve = func(name string) (string, error) {
				switch name {
				case "sudo", "apt-get":
					return filepath.Join(os.Getenv("ATM_OS_PACKAGES_TEST_TOOLS"), name), nil
				}
				return "", fmt.Errorf("invalid fixture tool")
			}
			return b, nil
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	dir, e := os.MkdirTemp("", "atm-os-packages-tools-")
	if e != nil {
		panic(e)
	}
	source, e := os.ReadFile("process_fixture.txt")
	if e != nil {
		panic(e)
	}
	file := filepath.Join(dir, "main.go")
	if e = os.WriteFile(file, source, 0600); e != nil {
		panic(e)
	}
	fixtureBinary = filepath.Join(dir, "fixture")
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin/go"), "build", "-o", fixtureBinary, file)
	if out, e := cmd.CombinedOutput(); e != nil {
		os.RemoveAll(dir)
		fmt.Fprintln(os.Stderr, string(out))
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
func copyExecutable(t *testing.T, source, dest string) {
	t.Helper()
	data, e := os.ReadFile(source)
	if e != nil {
		t.Fatal(e)
	}
	writeFixture(t, dest, data)
	if e = os.Chmod(dest, 0700); e != nil {
		t.Fatal(e)
	}
}
func processStopped(pid int) bool {
	data, e := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	return os.IsNotExist(e) || strings.Contains(string(data), ") Z ")
}
func TestBoundedOutputCleanEnvironmentLiteralArguments(t *testing.T) {
	t.Setenv("APT_CONFIG", "/private/config")
	t.Setenv("PYTHONPATH", "/private/modules")
	t.Setenv("ATM_PRIVATE_TOKEN", "private")
	output, e := runCommand(context.Background(), fixtureBinary, []string{"flood"})
	if e != nil || len(output) > (33<<10) || !strings.Contains(output, "truncated") {
		t.Fatal(len(output), e)
	}
	output, e = runCommand(context.Background(), fixtureBinary, []string{"inspect", "$(touch forbidden)", "space argument", "--literal"})
	if e != nil {
		t.Fatal(e)
	}
	var got struct {
		Args []string
		Env  []string
	}
	if e = json.Unmarshal([]byte(output), &got); e != nil {
		t.Fatal(e)
	}
	if len(got.Args) != 3 || got.Args[0] != "$(touch forbidden)" {
		t.Fatal(got)
	}
	for _, s := range got.Env {
		if strings.Contains(s, "private") {
			t.Fatal("inherited private environment", s)
		}
	}
}
func TestCompiledCORECancellationLeavesUnresolvedAttempt(t *testing.T) {
	b, _ := testBackend(t)
	tools := filepath.Join(t.TempDir(), "tools")
	for _, name := range []string{"sudo", "apt-get"} {
		copyExecutable(t, fixtureBinary, filepath.Join(tools, name))
	}
	t.Setenv("ATM_OS_PACKAGES_TEST_HELPER", "1")
	t.Setenv("ATM_OS_PACKAGES_TEST_TOOLS", tools)
	self, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	dir := filepath.Join(b.req.Home, "plugins/os_packages")
	manifest, e := os.ReadFile("../plugin.json")
	if e != nil {
		t.Fatal(e)
	}
	writeFixture(t, filepath.Join(dir, "plugin.json"), manifest)
	copyExecutable(t, self, filepath.Join(dir, "bin/linux-"+runtime.GOARCH+"/atm-os-packages"))
	cfg := config.Config{Home: b.req.Home, PluginsDir: filepath.Join(b.req.Home, "plugins"), AppsDir: b.req.AppsDir, Language: "en-us"}
	plugins, e := plugin.Discover(cfg, "linux")
	if e != nil || len(plugins) != 1 {
		t.Fatal(plugins, e)
	}
	plan, e := runner.Plugin(cfg, plugins[0], "install", []string{"--accept-system-packages"})
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan runner.Outcome, 1)
	go func() { done <- runner.RunNative(ctx, cfg, plan, runner.NativeOptions{Timeout: 10 * time.Second}) }()
	received := false
	var pids []int
	defer func() {
		cancel()
		if !received {
			select {
			case <-done:
			case <-time.After(12 * time.Second):
			}
		}
		for _, pid := range pids {
			if !processStopped(pid) {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	}()
	for _, name := range []string{"parent.pid", "child.pid"} {
		pid := 0
		for i := 0; i < 500; i++ {
			data, _ := os.ReadFile(filepath.Join(tools, name))
			pid, _ = strconv.Atoi(string(data))
			if pid > 0 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if pid == 0 {
			t.Fatal("fixture did not become ready", name)
		}
		pids = append(pids, pid)
	}
	cancel()
	select {
	case out := <-done:
		received = true
		if out.Status != "cancelled" || !strings.Contains(out.Message, "Privileged") {
			t.Fatal(out)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("CORE cancellation timed out")
	}
	for _, pid := range pids {
		confirmed := false
		for i := 0; i < 100; i++ {
			if processStopped(pid) {
				confirmed = true
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !confirmed {
			t.Fatal("synthetic descendant survived", pid)
		}
	}
	r, _, e := readRecord(b.state)
	if e != nil || r == nil || r.Status != "running" {
		t.Fatal(r, e)
	}
	lock := filepath.Join(filepath.Dir(b.state), ".native-lock")
	if _, e = os.Stat(lock); e != nil {
		t.Fatal(e)
	}
	if e = os.Remove(lock); e != nil {
		t.Fatal(e)
	}
	if _, release, e := openJournal(b.state); e == nil {
		release()
		t.Fatal("interrupted attempt silently retried")
	}
}
