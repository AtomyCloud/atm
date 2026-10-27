package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/AtomyCloud/atm/internal/config"
	"github.com/AtomyCloud/atm/internal/plugin"
	"github.com/AtomyCloud/atm/internal/protocol"
)

var fixtureBinary []byte

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "atm-native-build-")
	if err != nil {
		panic(err)
	}
	path := filepath.Join(dir, "fixture")
	goName := "go"
	if runtime.GOOS == "windows" {
		path += ".exe"
		goName += ".exe"
	}
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", goName), "build", "-o", path, "../../tools/native-fixture")
	if output, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintln(os.Stderr, string(output), err)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	fixtureBinary, err = os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func nativePlan(t *testing.T, args ...string) (config.Config, Plan) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "ATM space \u00e7")
	dir := filepath.Join(root, "plugins", "native_fixture")
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0700); err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile("../../tests/fixtures/native_fixture/plugin.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "plugin.json"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	name := "fixture"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err = os.WriteFile(filepath.Join(dir, "bin", name), fixtureBinary, 0700); err != nil {
		t.Fatal(err)
	}
	c := config.Config{Home: root, PluginsDir: filepath.Join(root, "plugins"), AppsDir: filepath.Join(root, "Apps"), Language: "en-us"}
	plugins, err := plugin.Discover(c, runtime.GOOS)
	if err != nil {
		t.Fatal(err)
	}
	if len(plugins) != 1 {
		t.Fatal(plugins)
	}
	plan, err := Plugin(c, plugins[0], "install", args)
	if err != nil {
		t.Fatal(err)
	}
	return c, plan
}

func TestNativeProtocolOutcomes(t *testing.T) {
	for _, tc := range []struct {
		mode, status, code string
		exit               int
	}{
		{"success", "success", "", 0}, {"error", "error", "fixture_error", 7}, {"error-zero", "error", "", 1},
		{"cancelled", "cancelled", "", 130}, {"missing", "error", "protocol", 1}, {"malformed", "error", "protocol", 1},
		{"oversized", "error", "protocol", 1}, {"truncated", "error", "protocol", 1}, {"wrong-id", "error", "protocol", 1},
		{"duplicate", "error", "protocol", 1}, {"late-event", "error", "protocol", 1}, {"success-nonzero", "error", "exit_mismatch", 1},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			c, p := nativePlan(t, tc.mode)
			var events []protocol.Event
			o := RunNative(context.Background(), c, p, NativeOptions{Timeout: 10 * time.Second, Event: func(e protocol.Event) error { events = append(events, e); return nil }})
			if o.Status != tc.status || o.Code != tc.code || o.ExitCode != tc.exit || ExitCode(o.Err()) != tc.exit {
				t.Fatalf("outcome: %+v", o)
			}
			for _, e := range events {
				if e.Type == "result" {
					t.Fatal("unvalidated terminal event escaped")
				}
			}
		})
	}
}

func TestNativeLiteralArgumentsAndNoShell(t *testing.T) {
	c, p := nativePlan(t, "echo", "with spaces", "$(touch sentinel)", "a;b", "quote\"", "\u65e5\u672c")
	t.Setenv("PATH", "")
	var echoed protocol.Request
	o := RunNative(context.Background(), c, p, NativeOptions{Event: func(e protocol.Event) error {
		if e.Type == "log" {
			return json.Unmarshal([]byte(e.Message), &echoed)
		}
		return nil
	}})
	if o.Err() != nil {
		t.Fatal(o.Err())
	}
	if fmt.Sprint(echoed.Arguments) != fmt.Sprint(p.Arguments) || echoed.AppsDir != c.AppsDir || echoed.Home != c.Home {
		t.Fatalf("changed request: %+v", echoed)
	}
	if _, err := os.Stat(filepath.Join(p.Directory, "sentinel")); !os.IsNotExist(err) {
		t.Fatal("shell interpreted arguments")
	}
}

func TestNativeCancellationTimeoutAndDescendants(t *testing.T) {
	for _, mode := range []string{"cancel", "timeout", "tree", "tree-exit"} {
		t.Run(mode, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "heartbeat")
			args := []string{"hang"}
			if strings.HasPrefix(mode, "tree") {
				args = []string{mode, marker}
			}
			c, p := nativePlan(t, args...)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			timeout := 10 * time.Second
			if mode == "timeout" {
				timeout = 200 * time.Millisecond
			}
			start := time.Now()
			o := RunNative(ctx, c, p, NativeOptions{Timeout: timeout, Event: func(e protocol.Event) error {
				if (mode == "cancel" && e.Type == "started") || (mode == "tree" && e.Message == "Child ready") {
					cancel()
				}
				return nil
			}})
			if time.Since(start) > 5*time.Second {
				t.Fatal("cleanup exceeded deadline")
			}
			if mode == "timeout" {
				if o.ExitCode != 124 {
					t.Fatal(o)
				}
			} else if mode != "tree-exit" {
				if o.ExitCode != 130 {
					t.Fatal(o)
				}
			} else if o.Err() == nil {
				t.Fatal("inherited pipes should not survive root exit")
			}
			if strings.HasPrefix(mode, "tree") {
				time.Sleep(150 * time.Millisecond)
				before, err := os.ReadFile(marker)
				if err != nil {
					t.Fatal(err)
				}
				time.Sleep(250 * time.Millisecond)
				after, err := os.ReadFile(marker)
				if err != nil || string(before) != string(after) {
					t.Fatal("descendant still running", err)
				}
			}
		})
	}
}

func TestNativeGuardsAndBoundedDiagnostics(t *testing.T) {
	for _, mode := range []string{"dry-run", "pending", "tampered-command", "disabled", "manifest-change", "script", "stderr-flood", "callback-error", "cancel-before-start"} {
		t.Run(mode, func(t *testing.T) {
			c, p := nativePlan(t, mode)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			opts := NativeOptions{Timeout: 5 * time.Second}
			var diagnostics int
			opts.Diagnostic = func(s string) error { diagnostics += len(s); return nil }
			switch mode {
			case "dry-run":
				c.DryRun = true
			case "pending":
				if err := os.WriteFile(filepath.Join(c.Home, ".atm-core-update-pending"), []byte("pending"), 0600); err != nil {
					t.Fatal(err)
				}
			case "tampered-command":
				p.Command = []string{"sh", "-c", "true"}
			case "disabled":
				t.Setenv("ATM_PLUGIN_NATIVE_FIXTURE_ENABLED", "0")
			case "manifest-change":
				if err := os.WriteFile(filepath.Join(p.Directory, "plugin.json"), []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
			case "script":
				if err := os.WriteFile(p.Command[0], []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
					t.Fatal(err)
				}
			case "callback-error":
				opts.Event = func(protocol.Event) error { return fmt.Errorf("consumer failed") }
			case "cancel-before-start":
				cancel()
			}
			o := RunNative(ctx, c, p, opts)
			if o.Err() == nil {
				t.Fatal("guard accepted", mode)
			}
			if diagnostics > 32768 {
				t.Fatal("unbounded diagnostics", diagnostics)
			}
		})
	}
}
