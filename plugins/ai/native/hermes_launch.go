//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"time"

	"github.com/AtomyCloud/atm/internal/protocol"
	"golang.org/x/sys/unix"
)

const hermesLaunchCommandLimit = 256 << 10

type hermesLaunchOptions struct {
	apps string
	dry  bool
	args []string
}

type hermesLaunchResult struct {
	Code      int
	Signal    syscall.Signal
	Uncertain bool
}

func parseHermesLaunch(args []string, environmentDry bool) (hermesLaunchOptions, error) {
	o := hermesLaunchOptions{dry: environmentDry}
	seen := map[string]bool{}
	consent, delimiter := false, false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			o.args = append([]string{}, args[i+1:]...)
			delimiter = true
			break
		}
		if seen[arg] {
			return o, errors.New(label("hermes_launch_usage"))
		}
		seen[arg] = true
		switch arg {
		case "--apps-dir":
			i++
			if i == len(args) {
				return o, errors.New(label("hermes_launch_usage"))
			}
			o.apps = args[i]
		case "--accept-runtime-execution":
			consent = true
		case "--dry-run":
			o.dry = true
		default:
			return o, errors.New(label("hermes_launch_usage"))
		}
	}
	if !consent || !delimiter || absolutePath(o.apps) != nil || len(o.args) > 128 {
		return o, errors.New(label("hermes_launch_usage"))
	}
	size := 0
	for _, arg := range o.args {
		size += len(arg)
		if strings.ContainsRune(arg, 0) || size > 64<<10 {
			return o, errors.New(label("hermes_launch_usage"))
		}
	}
	return o, nil
}

// Preparation's private paths remain fixed, but interactive terminal settings
// must not inherit CI/dumb/no-color. No caller credentials or runtime overrides
// are copied; users configure this version's private HOME explicitly.
func hermesLaunchEnvironment(dir string) []string {
	var env []string
	for _, entry := range hermesEnvironment(dir) {
		key, _, _ := strings.Cut(entry, "=")
		if key != "TERM" && key != "CI" && key != "NO_COLOR" {
			env = append(env, entry)
		}
	}
	term := os.Getenv("TERM")
	if len(term) > 128 || strings.IndexFunc(term, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_.+", r))
	}) >= 0 {
		term = ""
	}
	if term != "" {
		env = append(env, "TERM="+term)
	}
	return env
}

func hermesLaunchCommand(output, python string, tail []string) ([]string, error) {
	var values []any
	if len(output) > hermesLaunchCommandLimit || decode([]byte(output), &values, true) != nil || len(values) != 4+len(tail) {
		return nil, errors.New(label("hermes_launch_command"))
	}
	command := make([]string, len(values))
	for i, value := range values {
		var ok bool
		if command[i], ok = value.(string); !ok {
			return nil, errors.New(label("hermes_launch_command"))
		}
	}
	if command[0] != python || command[1] != "-I" || command[2] != "-c" || command[3] == "" || len(command[3]) > 128<<10 || !reflect.DeepEqual(command[4:], tail) {
		return nil, errors.New(label("hermes_launch_command"))
	}
	for _, arg := range command {
		if strings.ContainsRune(arg, 0) {
			return nil, errors.New(label("hermes_launch_command"))
		}
	}
	// -I ignores PYTHONDONTWRITEBYTECODE. Preserve the official bootstrap and
	// suffix verbatim, adding only the interpreter's bytecode suppression flag.
	return append([]string{command[0], "-I", "-B"}, command[2:]...), nil
}

func (b *backend) launchHermes(ctx context.Context, args []string) (result hermesLaunchResult, err error) {
	if err = b.hermesPrivilegePolicy(); err != nil {
		return result, err
	}
	root := b.root(hermesAgent)
	if err = dirs(root, false, true); err != nil {
		return result, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return result, err
	}
	cwd, err = filepath.EvalSymlinks(cwd)
	if err != nil {
		return result, err
	}
	if cwd == root || strings.HasPrefix(cwd, root+string(os.PathSeparator)) {
		return result, errors.New(label("hermes_launch_cwd"))
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	lock := root + ".lock"
	if err = os.Mkdir(lock, 0700); err != nil {
		return result, fmt.Errorf("%s: %w", label("hermes_launch_locked"), err)
	}
	lockInfo, err := os.Lstat(lock)
	if err != nil {
		return result, err
	}
	retain := false
	var journal pathState
	journalPath := filepath.Join(lock, "launch.json")
	defer func() {
		now, e := os.Lstat(lock)
		if e != nil || !os.SameFile(lockInfo, now) {
			retain = true
		}
		if !retain && journal.Exists {
			now, e := pathSnapshot(journalPath, false)
			if e != nil || !pathUnchanged(now, journal) {
				retain = true
			} else if e = os.Remove(journalPath); e != nil {
				retain = true
			}
		}
		if !retain {
			if e := os.Remove(lock); e != nil {
				retain = true
			}
		}
		if retain {
			err = errors.Join(err, fmt.Errorf("%s: %s", label("hermes_launch_retained"), lock))
		}
	}()
	selectedPath := filepath.Join(root, "current.json")
	selected, err := pathSnapshot(selectedPath, false)
	if err != nil {
		return result, err
	}
	pinned, err := hermesSelectionSnapshot(ctx, root)
	if err != nil {
		return result, err
	}
	verify := func() error {
		if err := pinned.verify(ctx, root); err != nil {
			return err
		}
		versions, active, err := b.hermesInspect(ctx)
		if err != nil {
			return err
		}
		if active != hermesPMVersion || !reflect.DeepEqual(versions, []string{hermesPMVersion}) {
			return errors.New(label("hermes_launch_selected"))
		}
		now, err := pathSnapshot(selectedPath, false)
		if err != nil {
			return err
		}
		if !pathUnchanged(now, selected) {
			return errors.New(label("hermes_launch_changed"))
		}
		return pinned.verify(ctx, root)
	}
	if err = verify(); err != nil {
		return result, err
	}
	dir := filepath.Join(root, "versions", hermesPMVersion)
	var complete hermesCompletion
	if err = readJSON(filepath.Join(dir, "complete.json"), &complete); err != nil {
		return result, err
	}
	source, python := filepath.Join(dir, "source"), complete.Evidence.StorePython
	env := hermesLaunchEnvironment(dir)
	tail := append([]string{"--cli"}, args...)
	helperArgs := append([]string{"-I", "-B", filepath.Join(source, "hermes_cli", "_launchers.py"), "--print-runtime-command", "--"}, tail...)
	if err = b.boundary("hermes-before-launch-helper"); err != nil {
		return result, err
	}
	if err = verify(); err != nil {
		return result, err
	}
	// This journal contains no argv, environment or credentials. Any helper
	// failure can have side effects, so retain evidence until verified completion.
	retain = true
	if err = writeJSON(journalPath, struct {
		Schema, PID int
		Version     string
	}{1, os.Getpid(), hermesPMVersion}); err != nil {
		return result, err
	}
	journal, err = pathSnapshot(journalPath, false)
	if err != nil {
		return result, err
	}
	helperCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)
	helperCtx, cancel := context.WithTimeout(helperCtx, 30*time.Second)
	output, helperErr := b.hermesRun(helperCtx, python, helperArgs, source, env)
	cancel()
	stop()
	if helperErr != nil {
		return result, helperErr
	}
	command, err := hermesLaunchCommand(output, python, tail)
	if err != nil {
		return result, err
	}
	if err = b.boundary("hermes-after-launch-helper"); err != nil {
		return result, err
	}
	if err = verify(); err != nil {
		return result, err
	}
	guard, err := hermesHostCommand("setpriv")
	if err != nil {
		return result, err
	}
	run := runHermesInteractive
	if b.hermes != nil && b.hermes.interactive != nil {
		run = b.hermes.interactive
	}
	result, err = run(ctx, guard, append([]string{"--no-new-privs", "--"}, command...), cwd, env)
	if err != nil || result.Uncertain || result.Signal != 0 {
		return result, err
	}
	if err = verify(); err != nil {
		return result, err
	}
	retain = false
	return result, nil
}

// Keep the foreground process group and real file descriptors. A new group
// without terminal handoff would stop on stdin (SIGTTIN). Only the immediate
// child is supervised; detached descendants are not a sandboxed process tree.
func runHermesInteractive(ctx context.Context, executable string, args []string, cwd string, env []string) (hermesLaunchResult, error) {
	var result hermesLaunchResult
	if err := ctx.Err(); err != nil {
		return result, err
	}
	cmd := exec.Command(executable, args...)
	cmd.Dir, cmd.Env = cwd, append([]string{}, env...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	signals := make(chan os.Signal, 8)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)
	defer signal.Stop(signals)
	if err := cmd.Start(); err != nil {
		return result, errors.New(label("hermes_launch_start"))
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var kill <-chan time.Time
	var timer *time.Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	cancelled := ctx.Done()
	terminate := func() {
		if timer == nil {
			timer = time.NewTimer(time.Second)
			kill = timer.C
		}
	}
	for {
		select {
		case sig := <-signals:
			result.Uncertain = true
			// os/signal cannot identify the sender. With a shared foreground
			// TTY, INT/QUIT already reach the child: never double-deliver them.
			// A targeted supervisor-only INT in this case is not forwarded.
			terminalInterrupt := (sig == os.Interrupt || sig == syscall.SIGQUIT) && hermesForegroundTerminal()
			if !terminalInterrupt {
				_ = cmd.Process.Signal(sig)
			}
			if sig == syscall.SIGTERM || sig == syscall.SIGHUP {
				terminate()
			}
		case <-cancelled:
			result.Uncertain = true
			cancelled = nil
			_ = cmd.Process.Signal(syscall.SIGTERM)
			terminate()
		case <-kill:
			_ = cmd.Process.Kill()
			kill = nil
		case err := <-done:
			if cmd.ProcessState == nil {
				result.Uncertain = true
				return result, errors.New(label("hermes_launch_wait"))
			}
			if status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok {
				if status.Signaled() {
					result.Signal = status.Signal()
					result.Uncertain = true
				} else {
					result.Code = status.ExitStatus()
				}
			}
			var exited *exec.ExitError
			if err != nil && !errors.As(err, &exited) {
				result.Uncertain = true
				return result, errors.New(label("hermes_launch_wait"))
			}
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			return result, nil
		}
	}
}

func hermesForegroundTerminal() bool {
	// fd 0 may be redirected while the process still has a controlling TTY.
	// Verify the fixed Linux controlling-terminal device, never a caller path.
	fd, err := unix.Open("/dev/tty", unix.O_RDONLY|unix.O_NOCTTY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return false
	}
	defer unix.Close(fd)
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Uid != 0 || stat.Mode&unix.S_IFMT != unix.S_IFCHR || unix.Major(uint64(stat.Rdev)) != 5 || unix.Minor(uint64(stat.Rdev)) != 0 {
		return false
	}
	foreground, err := unix.IoctlGetInt(fd, unix.TIOCGPGRP)
	return err == nil && foreground == syscall.Getpgrp()
}

func hermesLaunchExitStatus(result hermesLaunchResult, err error) int {
	// Conventional shell status, not self-signalling: Go gives signals
	// such as SIGQUIT runtime-specific stack-dump/exit semantics.
	if result.Signal != 0 {
		return 128 + int(result.Signal)
	}
	if err != nil && result.Code == 0 {
		return 1
	}
	return result.Code
}

func hermesCLIMain(args []string) {
	o, err := parseHermesLaunch(args, os.Getenv("ATM_DRY_RUN") == "1")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if o.dry {
		fmt.Fprintln(os.Stdout, label("hermes_launch_dry"))
		return
	}
	b, err := newBackend(protocol.Request{PluginID: "ai", AppsDir: o.apps}, func(string) {})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, label("hermes_launch_notice"))
	result, err := b.launchHermes(context.Background(), o.args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	os.Exit(hermesLaunchExitStatus(result, err))
}
