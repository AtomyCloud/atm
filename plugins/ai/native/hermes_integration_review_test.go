//go:build linux

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/AtomyCloud/atm/internal/protocol"
)

// Exercise a real multi-MiB native copy and its entrypoint, not a fixture ELF or
// the Go test executable's main. Provider state is synthetic and never executed.
func TestHermesPATHCompiledLifecycle(t *testing.T) {
	buildDir := t.TempDir()
	if err := os.Chmod(buildDir, 0700); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(buildDir, "atm-ai")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	build := exec.CommandContext(ctx, filepath.Join(runtime.GOROOT(), "bin", "go"), "build", "-o", binary, ".")
	build.Env = append(os.Environ(), "GOOS=linux", "GOARCH="+runtime.GOARCH, "CGO_ENABLED=0")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatal("build preview", err, string(output))
	}
	if err := os.Chmod(binary, 0755); err != nil {
		t.Fatal(err)
	}
	tracer := ""
	if os.Getenv("ATM_TEST_HERMES_PATH_TRACE") == "1" {
		var err error
		tracer, err = exec.LookPath("strace")
		if err != nil {
			t.Fatal("explicit trace check requires strace", err)
		}
	}
	f := newHermesSelectionFixture(t, runtime.GOARCH)
	root := f.b.root(hermesAgent)
	home := os.Getenv("HOME")
	if home != filepath.Join(filepath.Dir(f.b.req.AppsDir), "user") || home != f.b.discovery.home {
		t.Fatal("compiled PATH test requires the fixture's temporary HOME")
	}
	originals := map[string][]byte{}
	for _, name := range profileNames {
		originals[name] = []byte("# preserve user content without newline: " + name)
		writeFixture(t, filepath.Join(home, name), originals[name], 0640)
	}
	external := filepath.Join(home, ".local", "bin", "hermes")
	writeFixture(t, external, []byte("foreign executable; never execute\n"), 0700)
	before := integrationRead(t, external)
	env := []string{"HOME=" + home, "PATH=", "ATM_DRY_RUN=0", "LANG=C.UTF-8"}
	invoke := func(executable string, args []string, input []byte) ([]byte, error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		command, commandArgs := executable, args
		trace := filepath.Join(t.TempDir(), "trace")
		if tracer != "" {
			command = tracer
			commandArgs = append([]string{"-f", "-qq", "-s", "4096", "-e", "trace=execve,execveat,connect", "-o", trace, executable}, args...)
		}
		cmd := exec.CommandContext(ctx, command, commandArgs...)
		cmd.Env = env
		cmd.Stdin = bytes.NewReader(input)
		output, err := cmd.CombinedOutput()
		if tracer != "" {
			data, readErr := os.ReadFile(trace)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if bytes.Contains(data, []byte("connect(")) || bytes.Contains(data, []byte("execveat(")) {
				t.Fatal("unexpected network or descriptor execution", string(data))
			}
			pattern := regexp.MustCompile(`execve\(("(?:[^"\\]|\\.)*")`)
			matches := pattern.FindAllSubmatch(data, -1)
			if len(matches) != 1 {
				t.Fatal("unexpected child execution", string(data))
			}
			var executed string
			if json.Unmarshal(matches[0][1], &executed) != nil || executed != executable {
				t.Fatal("unexpected executable", string(data))
			}
		}
		return output, err
	}
	wire := func(operation string, args ...string) protocol.Event {
		t.Helper()
		req := f.b.req
		req.Protocol, req.RequestID = 1, strings.Repeat("a", 32)
		req.Operation, req.Arguments = operation, args
		data, err := json.Marshal(req)
		if err != nil {
			t.Fatal(err)
		}
		output, err := invoke(binary, nil, data)
		if err != nil {
			t.Fatal("backend process", err, string(output))
		}
		stream := protocol.Stream{RequestID: req.RequestID}
		scanner := bufio.NewScanner(bytes.NewReader(output))
		for scanner.Scan() {
			if _, err := stream.Accept(scanner.Bytes()); err != nil {
				t.Fatal(err, string(output))
			}
		}
		if scanner.Err() != nil || stream.Result == nil || stream.Result.Status != "success" {
			t.Fatal("unsuccessful native operation", scanner.Err(), string(output))
		}
		return *stream.Result
	}
	wire("integrate", "--provider", hermesAgent)
	bridge := filepath.Join(root, "bin", "hermes")
	i, err := os.Lstat(bridge)
	if err != nil || !i.Mode().IsRegular() || i.Size() <= maxProfile {
		t.Fatal("expected real native bridge larger than profile journal bound", i, err)
	}
	for _, name := range profileNames {
		data := integrationRead(t, filepath.Join(home, name))
		if !bytes.Contains(data, []byte(f.b.pathBlock(hermesAgent))) {
			t.Fatal("profile missing owned block", name)
		}
	}
	wire("integrate", "--provider", hermesAgent)
	j, err := os.Lstat(bridge)
	if err != nil || !os.SameFile(i, j) || !i.ModTime().Equal(j.ModTime()) {
		t.Fatal("repeat enrollment replaced owned native bridge", err)
	}
	for _, args := range [][]string{
		{"--accept-runtime-execution", "--dry-run", "--", "--help"},
		{"--accept-runtime-execution", "--dry-run", "--", "chat", "spaces and 'quotes'"},
	} {
		if output, err := invoke(bridge, args, nil); err != nil || !bytes.Contains(output, []byte("Dry-run")) {
			t.Fatal("native alias did not dispatch dry-run", err, string(output))
		}
	}
	for _, args := range [][]string{
		{"--dry-run", "--"},
		{"--accept-runtime-execution", "--dry-run", "--apps-dir", "/foreign", "--"},
		{"--hermes-cli", "--apps-dir", f.b.req.AppsDir, "--accept-runtime-execution", "--"},
	} {
		if output, err := invoke(bridge, args, nil); err == nil {
			t.Fatal("alias accepted missing consent or root override", args, string(output))
		}
	}
	wire("use", "--provider", hermesAgent, "--version", hermesPMVersion)
	wire("status")
	wire("integrate", "--provider", hermesAgent, "--remove-path")
	if output, err := invoke(bridge, []string{"--accept-runtime-execution", "--", "--help"}, nil); err == nil || !bytes.Contains(output, []byte("requires valid explicit PATH enrollment")) {
		t.Fatal("detached alias did not refuse before provider execution", err, string(output))
	}
	for _, name := range profileNames {
		path := filepath.Join(home, name)
		data := integrationRead(t, path)
		info, err := os.Lstat(path)
		if err != nil || !bytes.Equal(data, originals[name]) || info.Mode().Perm() != 0640 {
			t.Fatal("detach changed original profile", name, err)
		}
	}
	if !bytes.Equal(integrationRead(t, external), before) {
		t.Fatal("foreign Hermes command was modified")
	}
	wire("integrate", "--provider", hermesAgent)
	wire("integrate", "--provider", hermesAgent, "--remove-path")
	integrationMissing(t, root+".lock")
	integrationMissing(t, filepath.Join(home, ".atm-ai-path.lock"))
	t.Log(fmt.Sprintf("real native bridge %d bytes; provider never executed; trace=%t", i.Size(), tracer != ""))
}

func TestHermesAliasAuthorityRefusals(t *testing.T) {
	for _, kind := range []string{"running-inode", "not-enrolled", "home-lock", "profile-edited"} {
		t.Run(kind, func(t *testing.T) {
			f := hermesIntegrationFixture(t, runtime.GOARCH)
			mustRun(t, f.fixture, "integrate", "--provider", hermesAgent)
			root, home := f.b.root(hermesAgent), os.Getenv("HOME")
			want := "running executable identity mismatch"
			switch kind {
			case "not-enrolled":
				mustRun(t, f.fixture, "integrate", "--provider", hermesAgent, "--remove-path")
				want = "requires valid explicit PATH enrollment"
			case "home-lock":
				if err := os.Mkdir(filepath.Join(home, ".atm-ai-path.lock"), 0700); err != nil {
					t.Fatal(err)
				}
				want = "PATH operation locked/interrupted"
			case "profile-edited":
				writeFixture(t, filepath.Join(home, ".profile"), []byte("# changed externally\n"), 0640)
				want = "missing/edited AI PATH block preserved"
			}
			// Direct authority validation cannot start a subprocess. The test
			// executable must never be accepted as the separately owned alias.
			if _, _, err := f.b.verifyHermesAlias(context.Background(), filepath.Join(root, "bin", "hermes")); err == nil || !strings.Contains(err.Error(), want) {
				t.Fatal("alias authority accepted or unexpected refusal", kind, err)
			}
		})
	}
}
