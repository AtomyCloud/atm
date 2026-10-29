//go:build linux

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func launchArgs() []string {
	return []string{"--apps-dir", "/not/probed/apps", "--accept-runtime-execution", "--"}
}

func TestHermesLaunchOptions(t *testing.T) {
	for _, env := range []bool{false, true} {
		for _, bad := range [][]string{
			nil, {"--apps-dir", "/apps", "--"}, {"--accept-runtime-execution", "--"},
			{"--apps-dir", "relative", "--accept-runtime-execution", "--"},
			{"--apps-dir", "/apps", "--accept-runtime-execution"},
			{"--apps-dir", "/apps", "--apps-dir", "/other", "--accept-runtime-execution", "--"},
			{"--apps-dir", "/apps", "--accept-runtime-execution", "--dry-run", "--dry-run", "--"},
			{"--apps-dir", "/apps", "--accept-runtime-execution", "--unknown", "--"},
			append(launchArgs(), "nul\x00"), append(launchArgs(), strings.Repeat("x", (64<<10)+1)),
			append(launchArgs(), make([]string, 129)...),
		} {
			if _, err := parseHermesLaunch(bad, env); err == nil {
				t.Fatalf("accepted malformed options: %q", bad)
			}
		}
		got, err := parseHermesLaunch(append(launchArgs(), "--dry-run", "", "spaces ' \" $()"), env)
		if err != nil || got.dry != env || !reflect.DeepEqual(got.args, []string{"--dry-run", "", "spaces ' \" $()"}) {
			t.Fatal(got, err)
		}
	}
}

func TestHermesLaunchMachineCommand(t *testing.T) {
	tail := []string{"--cli", "", "a 'quoted' argument", "--run-module", "not-a-launcher-option"}
	base := append([]string{"/private/python3", "-I", "-c", "trusted upstream bootstrap"}, tail...)
	encoded, _ := json.Marshal(base)
	got, err := hermesLaunchCommand(string(encoded), base[0], tail)
	if err != nil || !reflect.DeepEqual(got, append([]string{base[0], "-I", "-B"}, base[2:]...)) {
		t.Fatal(got, err)
	}
	bad := []string{"null", "[]", "{}", string(encoded) + " []", strings.Repeat(" ", hermesLaunchCommandLimit+1)}
	for i, replacement := range map[int]string{0: "/foreign/python", 1: "-E", 2: "-m", 3: "", 4: "--print-runtime-command", 5: "\x00", 6: "changed"} {
		c := append([]string{}, base...)
		c[i] = replacement
		data, _ := json.Marshal(c)
		bad = append(bad, string(data))
	}
	var nullable []any
	json.Unmarshal(encoded, &nullable)
	nullable[5] = nil // null must not stand in for a legitimately empty user arg.
	data, _ := json.Marshal(nullable)
	bad = append(bad, string(data))
	for _, data := range bad {
		if _, err := hermesLaunchCommand(data, base[0], tail); err == nil {
			t.Fatal("accepted invalid command")
		}
	}
}

func TestHermesLaunchEnvironment(t *testing.T) {
	for _, key := range []string{"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "PYTHONPATH", "PYTHONHOME", "VIRTUAL_ENV", "HERMES_HOME", "HERMES_RUNTIME_DIR", "HOME", "CI", "NO_COLOR", "LD_PRELOAD"} {
		t.Setenv(key, "must-not-leak")
	}
	t.Setenv("TERM", "xterm-256color")
	env := hermesLaunchEnvironment("/private/version")
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "must-not-leak") || strings.Contains(joined, "CI=") || strings.Contains(joined, "NO_COLOR=") || !strings.Contains(joined, "TERM=xterm-256color") || !strings.Contains(joined, "HOME=/private/version/home") || !strings.Contains(joined, "HERMES_HOME=/private/version/home/.hermes") {
		t.Fatal(env)
	}
	t.Setenv("TERM", "unsafe\nvalue")
	if strings.Contains(strings.Join(hermesLaunchEnvironment("/private/version"), "\n"), "TERM=") {
		t.Fatal("unsafe terminal forwarded")
	}
}

func newHermesLaunchFixture(t *testing.T) (*hermesAgentFixture, *int) {
	t.Helper()
	f := newHermesSelectionFixture(t, "amd64")
	root := f.b.root(hermesAgent)
	dir := filepath.Join(root, "versions", hermesPMVersion)
	var complete hermesCompletion
	if err := readJSON(filepath.Join(dir, "complete.json"), &complete); err != nil {
		t.Fatal(err)
	}
	calls := new(int)
	f.b.hermes.run = func(ctx context.Context, exe string, args []string, cwd string, env []string) (string, error) {
		*calls++
		if exe != complete.Evidence.StorePython || cwd != filepath.Join(dir, "source") || !reflect.DeepEqual(env, hermesLaunchEnvironment(dir)) {
			t.Fatal("wrong helper runtime/environment")
		}
		prefix := []string{"-I", "-B", filepath.Join(dir, "source", "hermes_cli", "_launchers.py"), "--print-runtime-command", "--", "--cli"}
		if len(args) < len(prefix) || !reflect.DeepEqual(args[:len(prefix)], prefix) {
			t.Fatal(args)
		}
		out, _ := json.Marshal(append([]string{exe, "-I", "-c", "trusted fixture bootstrap"}, args[5:]...))
		return string(out), nil
	}
	f.b.hermes.interactive = func(ctx context.Context, exe string, args []string, cwd string, env []string) (hermesLaunchResult, error) {
		*calls++
		guard, err := hermesHostCommand("setpriv")
		want := []string{"--no-new-privs", "--", complete.Evidence.StorePython, "-I", "-B", "-c", "trusted fixture bootstrap", "--cli"}
		if err != nil || exe != guard || len(args) < len(want) || !reflect.DeepEqual(args[:len(want)], want) || !reflect.DeepEqual(env, hermesLaunchEnvironment(dir)) {
			t.Fatal(exe, args, err)
		}
		actualCWD, _ := os.Getwd()
		actualCWD, _ = filepath.EvalSymlinks(actualCWD)
		if cwd != actualCWD {
			t.Fatal("runtime changed working directory", cwd)
		}
		if _, err := os.Stat(filepath.Join(root+".lock", "launch.json")); err != nil {
			t.Fatal("lock not held through runtime", err)
		}
		if err := f.b.useHermesAgent(ctx, hermesPMVersion); err == nil {
			t.Fatal("concurrent use acquired launch lock")
		}
		return hermesLaunchResult{Code: 23}, nil
	}
	return f, calls
}

func TestHermesLaunchSelectedAndLocked(t *testing.T) {
	f, calls := newHermesLaunchFixture(t)
	result, err := f.b.launchHermes(context.Background(), []string{"argument", ""})
	if err != nil || result.Code != 23 || *calls != 2 {
		t.Fatal(result, *calls, err)
	}
	integrationMissing(t, f.b.root(hermesAgent)+".lock")
	integrationMissing(t, filepath.Join(f.b.root(hermesAgent), "path.json"))
}

func TestHermesLaunchRefusalAndRetention(t *testing.T) {
	for _, mode := range []string{"absent-selection", "unreviewed", "incomplete", "locked", "root", "cancelled", "source-before", "source-after", "selection-after", "receipt-after", "helper-error", "malformed", "signal", "runtime-change", "lock-replaced"} {
		t.Run(mode, func(t *testing.T) {
			f, calls := newHermesLaunchFixture(t)
			root := f.b.root(hermesAgent)
			dir := filepath.Join(root, "versions", hermesPMVersion)
			ctx := context.Background()
			if mode == "cancelled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			switch mode {
			case "absent-selection":
				os.Remove(filepath.Join(root, "current.json"))
			case "unreviewed":
				writeFixture(t, filepath.Join(root, "current.json"), []byte(`{"Version":"0.22.6"}`), 0600)
			case "incomplete":
				os.Remove(filepath.Join(dir, "complete.json"))
			case "locked":
				os.Mkdir(root+".lock", 0700)
			case "root":
				f.b.hermes.uid = func() int { return 0 }
			}
			f.b.checkpoint = func(name string) error {
				if mode == "source-before" && name == "hermes-before-launch-helper" || mode == "source-after" && name == "hermes-after-launch-helper" {
					writeFixture(t, filepath.Join(dir, "source", "pm", "cli.py"), []byte("changed"), 0644)
				}
				if name == "hermes-after-launch-helper" {
					if mode == "selection-after" {
						writeFixture(t, filepath.Join(root, "current.json"), []byte("{\"Version\":\"0.22.5\"}\n \n"), 0600)
					}
					if mode == "receipt-after" {
						path := filepath.Join(dir, "complete.json")
						data, _ := os.ReadFile(path)
						writeFixture(t, path, append(data, ' '), 0600)
					}
				}
				return nil
			}
			if mode == "helper-error" || mode == "malformed" {
				f.b.hermes.run = func(context.Context, string, []string, string, []string) (string, error) {
					*calls++
					if mode == "helper-error" {
						return "", errors.New("fixture failure")
					}
					return "null", nil
				}
			}
			original := f.b.hermes.interactive
			f.b.hermes.interactive = func(ctx context.Context, exe string, args []string, cwd string, env []string) (hermesLaunchResult, error) {
				r, err := original(ctx, exe, args, cwd, env)
				if mode == "signal" {
					r.Signal = syscall.SIGTERM
					r.Uncertain = true
				}
				if mode == "runtime-change" {
					writeFixture(t, filepath.Join(dir, "source", "pm", "cli.py"), []byte("changed"), 0644)
				}
				if mode == "lock-replaced" {
					os.Rename(root+".lock", root+".lock-original")
					os.Mkdir(root+".lock", 0700)
					writeFixture(t, filepath.Join(root+".lock", "foreign"), []byte("keep"), 0600)
				}
				return r, err
			}
			_, err := f.b.launchHermes(ctx, nil)
			if err == nil {
				t.Fatal("failure accepted")
			}
			if *calls > 0 {
				if _, err := os.Lstat(root + ".lock"); err != nil {
					t.Fatal("execution evidence lost", err)
				}
			} else if mode != "locked" {
				integrationMissing(t, root+".lock")
			}
		})
	}
}

// Only the Go test executable has these harmless subprocess entrypoints.
func TestHermesLaunchProcessHelper(t *testing.T) {
	role := os.Getenv("ATM_TEST_LAUNCH_ROLE")
	if role == "" {
		return
	}
	mode := os.Getenv("ATM_TEST_LAUNCH_MODE")
	if role == "exit-quit" {
		os.Exit(hermesLaunchExitStatus(hermesLaunchResult{Signal: syscall.SIGQUIT}, nil))
	}
	if role == "supervisor" {
		self, _ := os.Executable()
		ctx := context.Background()
		if mode == "cancel" {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, 200*time.Millisecond)
			defer cancel()
		}
		r, err := runHermesInteractive(ctx, self, []string{"-test.run=^TestHermesLaunchProcessHelper$"}, os.TempDir(), []string{"ATM_TEST_LAUNCH_ROLE=child", "ATM_TEST_LAUNCH_MODE=" + mode})
		fmt.Printf("RESULT code=%d signal=%d uncertain=%t error=%t\n", r.Code, r.Signal, r.Uncertain, err != nil)
		os.Exit(0)
	}
	if role != "child" {
		os.Exit(99)
	}
	interrupts := make(chan os.Signal, 8)
	if mode == "interrupt" || mode == "interrupt-redirected" {
		signal.Notify(interrupts, os.Interrupt)
	}
	if mode == "resize" {
		signal.Notify(interrupts, syscall.SIGWINCH)
	}
	if mode == "ignore-term" || mode == "cancel" {
		signal.Ignore(syscall.SIGTERM)
	}
	if mode == "pty" || mode == "interrupt" || mode == "resize" {
		pg, err := unix.IoctlGetInt(int(os.Stdin.Fd()), unix.TIOCGPGRP)
		if err != nil || pg != syscall.Getpgrp() {
			fmt.Println("NOT-FOREGROUND")
			os.Exit(98)
		}
	}
	if mode == "interrupt-redirected" {
		_, err := unix.IoctlGetInt(int(os.Stdin.Fd()), unix.TIOCGPGRP)
		if err == nil || !hermesForegroundTerminal() {
			fmt.Println("NOT-FOREGROUND redirected fixture")
			os.Exit(98)
		}
	}
	fmt.Println("READY")
	if mode == "resize" {
		<-interrupts
		size, err := unix.IoctlGetWinsize(int(os.Stdin.Fd()), unix.TIOCGWINSZ)
		if err != nil || size.Row != 47 || size.Col != 132 {
			os.Exit(95)
		}
		fmt.Println("RESIZED 47x132")
	}
	if mode == "ignore-term" || mode == "cancel" || mode == "term" {
		for {
			time.Sleep(time.Second)
		}
	}
	if mode == "interrupt" || mode == "interrupt-redirected" {
		<-interrupts
		time.Sleep(150 * time.Millisecond)
		select {
		case <-interrupts:
			fmt.Println("DOUBLE-INTERRUPT")
			os.Exit(97)
		default:
		}
		fmt.Println("ONE-INTERRUPT")
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		os.Exit(96)
	}
	fmt.Fprint(os.Stdout, "STDOUT "+line)
	fmt.Fprint(os.Stderr, "STDERR "+line)
	os.Exit(23)
}

func launchProcessTest(t *testing.T, mode string, tty bool) (*exec.Cmd, io.Writer, <-chan string, *os.File) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(self, "-test.run=^TestHermesLaunchProcessHelper$")
	cmd.Env = []string{"ATM_TEST_LAUNCH_ROLE=supervisor", "ATM_TEST_LAUNCH_MODE=" + mode}
	var reader io.ReadCloser
	var writer io.WriteCloser
	var slave *os.File
	var terminal *os.File
	if tty {
		master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		if err = unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
			master.Close()
			t.Fatal(err)
		}
		n, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
		if err != nil {
			master.Close()
			t.Fatal(err)
		}
		slave, err = os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|syscall.O_NOCTTY, 0600)
		if err != nil {
			master.Close()
			t.Fatal(err)
		}
		cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
		reader, writer = master, master
		terminal = master
		if mode == "interrupt-redirected" {
			cmd.Stdin = nil
			writer, err = cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			cmd.SysProcAttr.Ctty = 1
		}
	} else {
		writer, err = cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		reader, err = cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		cmd.Stderr = cmd.Stdout
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if slave != nil {
		slave.Close()
	}
	t.Cleanup(func() { syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); writer.Close(); reader.Close() })
	lines := make(chan string, 32)
	go func() {
		defer close(lines)
		s := bufio.NewScanner(reader)
		for s.Scan() {
			lines <- s.Text()
		}
	}()
	return cmd, writer, lines, terminal
}

func awaitLaunchLine(t *testing.T, lines <-chan string, wanted string) {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatal("output closed before", wanted)
			}
			if strings.Contains(line, wanted) {
				return
			}
			if strings.Contains(line, "DOUBLE-") || strings.Contains(line, "NOT-FOREGROUND") {
				t.Fatal(line)
			}
		case <-timer.C:
			t.Fatal("timeout waiting for", wanted)
		}
	}
}

func TestHermesLaunchRealInteractiveProcesses(t *testing.T) {
	for _, mode := range []string{"stdio", "pty", "interrupt", "interrupt-redirected", "resize", "term", "ignore-term", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			cmd, input, lines, terminal := launchProcessTest(t, mode, mode == "pty" || strings.HasPrefix(mode, "interrupt") || mode == "resize")
			awaitLaunchLine(t, lines, "READY")
			if mode == "resize" {
				master := input.(*os.File)
				if err := unix.IoctlSetWinsize(int(master.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: 47, Col: 132}); err != nil {
					t.Fatal(err)
				}
				awaitLaunchLine(t, lines, "RESIZED 47x132")
			}
			switch mode {
			case "term", "ignore-term":
				if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
					t.Fatal(err)
				}
			case "interrupt", "interrupt-redirected":
				if _, err := terminal.Write([]byte{3}); err != nil {
					t.Fatal(err)
				}
				awaitLaunchLine(t, lines, "ONE-INTERRUPT")
				fallthrough
			case "stdio", "pty", "resize":
				if _, err := io.WriteString(input, "hello terminal\n"); err != nil {
					t.Fatal(err)
				}
				awaitLaunchLine(t, lines, "STDOUT hello terminal")
				awaitLaunchLine(t, lines, "STDERR hello terminal")
			}
			want := "RESULT code=23 signal=0 uncertain=false error=false"
			switch mode {
			case "interrupt", "interrupt-redirected":
				want = "RESULT code=23 signal=0 uncertain=true error=false"
			case "term":
				want = "RESULT code=0 signal=15 uncertain=true error=false"
			case "ignore-term":
				want = "RESULT code=0 signal=9 uncertain=true error=false"
			case "cancel":
				want = "RESULT code=0 signal=9 uncertain=true error=true"
			}
			awaitLaunchLine(t, lines, want)
			if err := cmd.Wait(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestHermesLaunchExitStatus(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGHUP, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGKILL} {
		if got := hermesLaunchExitStatus(hermesLaunchResult{Signal: sig}, errors.New("retained")); got != 128+int(sig) {
			t.Fatal(got, sig)
		}
	}
	if hermesLaunchExitStatus(hermesLaunchResult{Code: 23}, nil) != 23 || hermesLaunchExitStatus(hermesLaunchResult{}, errors.New("failed")) != 1 {
		t.Fatal("incorrect ordinary exit mapping")
	}
	self, _ := os.Executable()
	cmd := exec.Command(self, "-test.run=^TestHermesLaunchProcessHelper$")
	cmd.Env = []string{"ATM_TEST_LAUNCH_ROLE=exit-quit"}
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 131 || len(out) != 0 {
		t.Fatalf("SIGQUIT mapping: %q %v", out, err)
	}
}

func TestHermesLaunchManagedCWD(t *testing.T) {
	f, calls := newHermesLaunchFixture(t)
	t.Chdir(filepath.Join(f.b.root(hermesAgent), "versions", hermesPMVersion, "source"))
	_, err := f.b.launchHermes(context.Background(), nil)
	if err == nil || *calls != 0 {
		t.Fatal("managed cwd accepted", err, *calls)
	}
	integrationMissing(t, f.b.root(hermesAgent)+".lock")
}

func TestHermesLaunchDryNoState(t *testing.T) {
	// Parsing uses only supplied strings, including a syntactically valid root
	// that does not exist. The compiled CLI no-I/O cases are owned by the parent.
	o, err := parseHermesLaunch([]string{"--apps-dir", "/definitely-absent/hermes", "--accept-runtime-execution", "--dry-run", "--"}, false)
	if err != nil || !o.dry || !bytes.Equal([]byte(o.apps), []byte("/definitely-absent/hermes")) {
		t.Fatal(o, err)
	}
}
