//go:build linux

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
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

// Only the compiled test executable admits a fixture TLS registry. The shipping
// backend has no registry override or helper entrypoint.
func serveTestHelper() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	req, e := protocol.ReadRequest(os.Stdin)
	if e != nil {
		return 1
	}
	enc := json.NewEncoder(os.Stdout)
	emit := func(kind, message, status string) {
		enc.Encode(protocol.Event{Protocol: 1, RequestID: req.RequestID, Type: kind, Message: protocol.SafeText(message), Status: status})
	}
	emit("started", "REA synthetic TLS backend", "")
	b, e := newBackend(req, func(s string) { emit("log", s, "") })
	if e == nil {
		u, err := url.Parse(os.Getenv("ATM_REA_TEST_URL"))
		if err != nil {
			e = err
		} else {
			cert, err := os.ReadFile(os.Getenv("ATM_REA_TEST_CERT"))
			if err != nil {
				e = err
			} else {
				pool := x509.NewCertPool()
				if !pool.AppendCertsFromPEM(cert) {
					e = fmt.Errorf("invalid fixture certificate")
				} else {
					b.client = &http.Client{Transport: rewriteTransport{&http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}, u}, Timeout: 10 * time.Second}
					_, e = b.execute(ctx)
				}
			}
		}
	}
	if e != nil {
		emit("result", e.Error(), "error")
	} else {
		emit("result", "done", "success")
	}
	return 0
}

func processStopped(pid int) bool {
	data, e := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	return os.IsNotExist(e) || errors.Is(e, syscall.ESRCH) || strings.Contains(string(data), ") Z ")
}
func TestCompiledCORECancelsDependencyTreeAndCleansFailure(t *testing.T) {
	for _, mode := range []string{"hang", "probe-error", "deadline", "npm-hang"} {
		t.Run(mode, func(t *testing.T) {
			b := testBackend(t)
			tools := toolchain(t, b)
			selectedMode := mode
			if mode == "deadline" {
				selectedMode = "hang"
			}
			control(t, tools, selectedMode, "")
			fallback := filepath.Join(b.req.AppsDir, "node/current/bin")
			copyFixture(t, compiledTool, filepath.Join(fallback, "node"))
			write(t, filepath.Join(fallback, "npm-cli.js"), []byte("fixture"), 0600)
			os.Symlink("npm-cli.js", filepath.Join(fallback, "npm"))
			binary := compiledNative
			if mode == "npm-hang" {
				fixture := fixtureRegistry(t, b, "6.8.0")
				cert := filepath.Join(t.TempDir(), "cert.pem")
				write(t, cert, fixture.Certificate, 0600)
				t.Setenv("ATM_REA_TEST_HELPER", "1")
				t.Setenv("ATM_REA_TEST_URL", fixture.URL)
				t.Setenv("ATM_REA_TEST_CERT", cert)
				binary = filepath.Join(t.TempDir(), "rea.test")
				cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin/go"), "test", "-c", "-o", binary, ".")
				if out, e := cmd.CombinedOutput(); e != nil {
					t.Fatal(e, string(out))
				}
			}
			root := t.TempDir()
			pluginDir := filepath.Join(root, "plugins/rea")
			data, e := os.ReadFile("../plugin.json")
			if e != nil {
				t.Fatal(e)
			}
			write(t, filepath.Join(pluginDir, "plugin.json"), data, 0600)
			copyFixture(t, binary, filepath.Join(pluginDir, "bin/linux-"+runtime.GOARCH+"/atm-rea"))
			c := config.Config{Home: root, PluginsDir: filepath.Join(root, "plugins"), AppsDir: b.req.AppsDir, Language: "en-us"}
			plugins, e := plugin.Discover(c, "linux")
			if e != nil || len(plugins) != 1 {
				t.Fatal(plugins, e)
			}
			plan, e := runner.Plugin(c, plugins[0], "install", []string{"--trust-dependencies"})
			if e != nil {
				t.Fatal(e)
			}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan runner.Outcome, 1)
			go func() { done <- runner.RunNative(ctx, c, plan, runner.NativeOptions{Timeout: 8 * time.Second}) }()
			received := false
			files := []string{filepath.Join(tools, "parent.pid"), filepath.Join(tools, "child.pid")}
			defer func() {
				cancel()
				if !received {
					select {
					case <-done:
					case <-time.After(10 * time.Second):
					}
				}
				for _, file := range files {
					data, _ := os.ReadFile(file)
					pid, _ := strconv.Atoi(string(data))
					if pid > 0 && !processStopped(pid) {
						syscall.Kill(pid, syscall.SIGKILL)
					}
				}
			}()
			var pids []int
			for _, file := range files {
				pid := 0
				for i := 0; i < 500; i++ {
					data, _ := os.ReadFile(file)
					pid, _ = strconv.Atoi(string(data))
					if pid > 0 {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				if pid == 0 {
					t.Fatal("fixture readiness timeout", file)
				}
				pids = append(pids, pid)
			}
			if mode == "hang" || mode == "npm-hang" {
				cancel()
			}
			select {
			case out := <-done:
				received = true
				if (mode == "hang" || mode == "npm-hang" || mode == "deadline") && out.Status != "cancelled" || mode == "probe-error" && out.Status != "error" {
					t.Fatal(out)
				}
			case <-time.After(7 * time.Second):
				t.Fatal("CORE did not finish cancellation/failure")
			}
			for _, pid := range pids {
				stopped := false
				for i := 0; i < 100; i++ {
					if processStopped(pid) {
						stopped = true
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				if !stopped {
					t.Fatal("descendant survived", pid)
				}
			}
			if mode == "npm-hang" {
				absent(t, filepath.Join(b.root, "current"))
				absent(t, b.state)
				absent(t, filepath.Join(b.root, "bin/rea"))
			} else {
				absent(t, b.root)
			}
			absent(t, filepath.Join(fallback, "calls.jsonl"))
			if mode != "npm-hang" {
				calls, _ := os.ReadFile(filepath.Join(tools, "calls.jsonl"))
				for _, line := range strings.Split(strings.TrimSpace(string(calls)), "\n") {
					var call struct{ Args []string }
					json.Unmarshal([]byte(line), &call)
					if len(call.Args) > 1 && filepath.Base(call.Args[0]) == "npm-cli.js" {
						t.Fatal("continued after process failure")
					}
				}
			}
		})
	}
}
