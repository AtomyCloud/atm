// Native VS Code policy. CORE owns confirmation and dry-run orchestration.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/AtomyCloud/atm/internal/protocol"
)

type choice = protocol.Choice
type backend struct {
	req                  protocol.Request
	home, root, platform string
	client               *http.Client
	log                  func(string)
	run                  func(context.Context, string, []string, []string) error
}

func newBackend(req protocol.Request, log func(string)) (*backend, error) {
	if req.PluginID != "vscode" {
		return nil, fmt.Errorf("expected vscode plugin")
	}
	if runtime.GOOS != "linux" || (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64") {
		return nil, fmt.Errorf("native VS Code supports Linux amd64/arm64 only")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	for _, p := range []string{home, req.Home, req.AppsDir} {
		if !filepath.IsAbs(p) || filepath.Clean(p) == "/" || strings.ContainsAny(p, "\x00\r\n:") {
			return nil, fmt.Errorf("absolute safe HOME, runtime and AppsDir required")
		}
	}
	arch := "x64"
	if runtime.GOARCH == "arm64" {
		arch = "arm64"
	}
	b := &backend{req: req, home: filepath.Clean(home), root: filepath.Join(req.AppsDir, "vscode"), platform: "linux-" + arch, client: &http.Client{Timeout: 15 * time.Minute}, log: log}
	b.run = func(ctx context.Context, executable string, args, env []string) error {
		cmd := exec.CommandContext(ctx, executable, args...)
		cmd.Env = env
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		cmd.WaitDelay = 3 * time.Second
		return cmd.Run()
	}
	return b, nil
}

func serve(ctx context.Context, in io.Reader, out io.Writer) error {
	req, err := protocol.ReadRequest(in)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(out)
	emit := func(kind, message, status string, choices []choice) error {
		message = protocol.SafeText(message)
		if len(message) > protocol.MaxMessage {
			message = message[:protocol.MaxMessage]
		}
		return enc.Encode(protocol.Event{Protocol: 1, RequestID: req.RequestID, Type: kind, Message: message, Status: status, Choices: choices})
	}
	if err = emit("started", "VS Code", "", nil); err != nil {
		return err
	}
	b, err := newBackend(req, func(s string) { _ = emit("log", s, "", nil) })
	var choices []choice
	if err == nil {
		choices, err = b.execute(ctx)
	}
	if err == nil {
		err = protocol.ValidateChoices(choices)
	}
	status, message := "success", label("complete")
	if req.Operation == "menu" {
		message = label("title")
	}
	if err != nil {
		status = "error"
		message = err.Error()
		choices = nil
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			status = "cancelled"
		}
	}
	return emit("result", message, status, choices)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := serve(ctx, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func (b *backend) execute(ctx context.Context) ([]choice, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if os.Getenv("ATM_DRY_RUN") == "1" {
		b.log("Dry-run: no network, commands or writes")
		return nil, nil
	}
	a := b.req.Arguments
	switch b.req.Operation {
	case "menu":
		return b.menu(ctx, a)
	case "status":
		if len(a) != 0 {
			return nil, fmt.Errorf("status takes no arguments")
		}
		return nil, b.status()
	case "install", "update":
		v, err := parseVersionOptions(a)
		if err != nil {
			return nil, err
		}
		return nil, b.withLock(func() error { return b.install(ctx, v) })
	case "use", "remove":
		if len(a) != 1 {
			return nil, fmt.Errorf("%s requires one version", b.req.Operation)
		}
		v, err := slot(a[0])
		if err != nil {
			return nil, err
		}
		return nil, b.withLock(func() error {
			if b.req.Operation == "use" {
				return b.activate(ctx, v)
			}
			return b.remove(ctx, v)
		})
	case "uninstall":
		if len(a) != 0 {
			return nil, fmt.Errorf("uninstall takes no arguments")
		}
		return nil, b.withLock(func() error { return b.uninstall(ctx) })
	case "integrate":
		if len(a) == 0 || (len(a) == 1 && (a[0] == "path" || a[0] == "cli" || a[0] == "desktop" || a[0] == "extensions")) {
			return nil, b.withLock(func() error { return b.integrate(ctx, a) })
		}
		if len(a) == 4 && a[0] == "migrate-data" && a[3] == "--closed" {
			src, err := slot(a[1])
			if err != nil {
				return nil, err
			}
			dst, err := slot(a[2])
			if err != nil {
				return nil, err
			}
			return nil, b.withLock(func() error { return b.migrateData(ctx, src, dst) })
		}
		return nil, fmt.Errorf("integrate accepts path, cli, desktop, extensions or migrate-data SOURCE TARGET --closed (close both editors first)")
	default:
		return nil, fmt.Errorf("unsupported VS Code operation: %s", b.req.Operation)
	}
}
