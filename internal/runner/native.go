package runner

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/AtomyCloud/atm/internal/config"
	"github.com/AtomyCloud/atm/internal/coreupdate"
	"github.com/AtomyCloud/atm/internal/data"
	"github.com/AtomyCloud/atm/internal/plugin"
	"github.com/AtomyCloud/atm/internal/protocol"
)

const DefaultTimeout = 15 * time.Minute

type NativeOptions struct {
	Timeout time.Duration
	// Callbacks run on output-reading goroutines and must return promptly.
	Event      func(protocol.Event) error
	Diagnostic func(string) error
}

type Outcome struct {
	RequestID string
	Status    string
	Code      string
	Message   string
	ExitCode  int
}

type NativeError struct{ Outcome Outcome }

func (e *NativeError) Error() string {
	return e.Outcome.Code + ": " + protocol.SafeText(e.Outcome.Message)
}
func (o Outcome) Err() error {
	if o.Status == "success" {
		return nil
	}
	return &NativeError{o}
}
func (o Outcome) Event() protocol.Event {
	return protocol.Event{Protocol: 1, RequestID: o.RequestID, Type: "result", Status: o.Status, Code: o.Code, Message: o.Message}
}

func RunNative(parent context.Context, c config.Config, plan Plan, opts NativeOptions) Outcome {
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return Outcome{Status: "error", Code: "identity", Message: err.Error(), ExitCode: 1}
	}
	requestID := hex.EncodeToString(id)
	failure := func(code string, err error) Outcome { return Outcome{requestID, "error", code, err.Error(), 1} }
	if c.DryRun || plan.DryRun {
		return failure("dry_run", fmt.Errorf("dry-run cannot start native processes"))
	}
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(parent, opts.Timeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return interrupted(requestID, err)
	}
	exe, err := validateNativePlan(c, plan)
	if err != nil {
		return failure("invalid_plan", err)
	}
	req := protocol.Request{Protocol: 1, RequestID: requestID, PluginID: plan.PluginID, Operation: plan.Operation, Arguments: append([]string{}, plan.Arguments...), Home: c.Home, AppsDir: c.AppsDir, Language: c.Language}
	encoded, err := json.Marshal(req)
	if err != nil || len(encoded)+1 > protocol.MaxFrame {
		return failure("request_size", fmt.Errorf("native request exceeds limit"))
	}
	fatal := make(chan error, 1)
	report := func(err error) {
		select {
		case fatal <- err:
		default:
		}
	}
	stdout := &eventWriter{stream: protocol.Stream{RequestID: requestID}, event: opts.Event, fail: report}
	stderr := &diagnosticWriter{emit: opts.Diagnostic, fail: report}
	cmd := exec.Command(exe)
	cmd.Dir = plan.Directory
	cmd.Stdin = bytes.NewReader(append(encoded, '\n'))
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.WaitDelay = time.Second
	tree, err := prepareProcess(cmd)
	if err != nil {
		return failure("process_setup", err)
	}
	defer tree.close()
	if err = ctx.Err(); err != nil {
		return interrupted(requestID, err)
	}
	if err = cmd.Start(); err != nil {
		return failure("start", err)
	}
	if err = tree.attach(cmd); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return failure("process_setup", err)
	}
	wait := make(chan error, 1)
	go func() { wait <- cmd.Wait() }()
	var waitErr, abort error
	select {
	case waitErr = <-wait:
	case abort = <-fatal:
		tree.kill()
		waitErr = <-wait
	case <-ctx.Done():
		tree.kill()
		waitErr = <-wait
		abort = ctx.Err()
	}
	// Also remove descendants on normal root exit. Native operations cannot leave daemons.
	tree.kill()
	if errors.Is(abort, context.Canceled) || errors.Is(abort, context.DeadlineExceeded) {
		return interrupted(requestID, abort)
	}
	if abort != nil {
		return failure("protocol", abort)
	}
	if stdout.err != nil {
		return failure("protocol", stdout.err)
	}
	if stderr.err != nil {
		return failure("diagnostic", stderr.err)
	}
	if len(stdout.pending) != 0 {
		return failure("protocol", fmt.Errorf("unterminated JSONL frame"))
	}
	if stdout.stream.Result == nil {
		return failure("protocol", fmt.Errorf("process exited without a terminal result"))
	}
	result := stdout.stream.Result
	if result.Status == "success" && waitErr != nil {
		return failure("exit_mismatch", waitErr)
	}
	if errors.Is(waitErr, exec.ErrWaitDelay) {
		return failure("pipe_timeout", waitErr)
	}
	code := ExitCode(waitErr)
	if result.Status == "error" && code == 0 {
		code = 1
	}
	if result.Status == "cancelled" {
		code = 130
	}
	return Outcome{requestID, result.Status, result.Code, result.Message, code}
}

func interrupted(id string, err error) Outcome {
	if errors.Is(err, context.DeadlineExceeded) {
		return Outcome{id, "error", "timeout", "Native operation timed out; process tree terminated", 124}
	}
	return Outcome{id, "cancelled", "cancelled", "Native operation cancelled; process tree terminated", 130}
}

func validateNativePlan(c config.Config, p Plan) (string, error) {
	if err := coreupdate.StartupGuard(c.Home); err != nil {
		return "", err
	}
	if _, err := os.Lstat(filepath.Join(c.PluginsDir, ".atm-update-pending.json")); !os.IsNotExist(err) {
		return "", fmt.Errorf("plugin recovery pending; run updates recover plugins")
	}
	if p.Schema != 2 || p.Executor != "native" || p.Protocol != "jsonl-v1" || !plugin.IDPattern.MatchString(p.PluginID) {
		return "", fmt.Errorf("invalid native plan")
	}
	if len(p.Arguments) > 128 {
		return "", fmt.Errorf("too many arguments")
	}
	if _, err := os.Lstat(filepath.Join(c.Home, ".atm-core-update-pending")); !os.IsNotExist(err) {
		return "", fmt.Errorf("CORE recovery pending")
	}
	dir, err := filepath.Abs(filepath.Join(c.PluginsDir, p.PluginID))
	if err != nil {
		return "", err
	}
	planDir, err := filepath.Abs(p.Directory)
	if err != nil || planDir != dir {
		return "", fmt.Errorf("plugin directory mismatch")
	}
	var manifest plugin.Manifest
	manifestPath := filepath.Join(dir, "plugin.json")
	info, err := os.Lstat(manifestPath)
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("native manifest is not a regular file")
	}
	if err = data.JSON(manifestPath, &manifest); err != nil {
		return "", err
	}
	if err = manifest.Validate(p.PluginID); err != nil {
		return "", err
	}
	if manifest.Schema != 2 || !slices.Contains(manifest.Operations, p.Operation) {
		return "", fmt.Errorf("native operation no longer supported")
	}
	key := "ATM_PLUGIN_" + strings.ToUpper(p.PluginID) + "_ENABLED"
	enabled := !c.Disabled[key]
	if value := os.Getenv(key); value != "" {
		enabled = value != "0"
	}
	if !enabled {
		return "", fmt.Errorf("plugin disabled")
	}
	target := runtime.GOOS + "/" + runtime.GOARCH
	pl := plugin.Plugin{Manifest: manifest, Directory: dir}
	exe, err := pl.NativeExecutable(target)
	if err != nil {
		return "", err
	}
	if len(p.Command) != 1 || p.Command[0] != exe {
		return "", fmt.Errorf("native executable changed since planning")
	}
	if err = plugin.CheckBinary(exe, target); err != nil {
		return "", err
	}
	return exe, nil
}

type eventWriter struct {
	pending []byte
	total   int
	stream  protocol.Stream
	event   func(protocol.Event) error
	fail    func(error)
	err     error
}

func (w *eventWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	w.total += len(p)
	if w.total > 16*1024*1024 {
		w.err = fmt.Errorf("protocol output exceeds 16 MiB")
	}
	n := len(p)
	for len(p) > 0 && w.err == nil {
		i := bytes.IndexByte(p, '\n')
		length := len(p)
		if i >= 0 {
			length = i
		}
		if len(w.pending)+length > protocol.MaxFrame {
			w.err = fmt.Errorf("protocol frame exceeds 64 KiB")
			break
		}
		w.pending = append(w.pending, p[:length]...)
		if i < 0 {
			break
		}
		var e protocol.Event
		e, w.err = w.stream.Accept(w.pending)
		w.pending = w.pending[:0]
		if w.err == nil && e.Type != "result" && w.event != nil {
			w.err = w.event(e)
		}
		p = p[i+1:]
	}
	if w.err != nil {
		w.fail(w.err)
		return 0, w.err
	}
	return n, nil
}

type diagnosticWriter struct {
	total, emitted int
	emit           func(string) error
	fail           func(error)
	err            error
}

func (w *diagnosticWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	w.total += len(p)
	if w.total > 1024*1024 {
		w.err = fmt.Errorf("diagnostic output exceeds 1 MiB")
	} else if w.emitted < 32768 {
		n := min(len(p), 32768-w.emitted)
		w.emitted += n
		if w.emit != nil {
			w.err = w.emit(protocol.SafeText(string(p[:n])))
		}
	}
	if w.err != nil {
		w.fail(w.err)
		return 0, w.err
	}
	return len(p), nil
}
