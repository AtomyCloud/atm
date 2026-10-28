package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
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
	req        protocol.Request
	home, root string
	client     *http.Client
	log        func(string)
	cmakePath  string
}

func newBackend(req protocol.Request, log func(string)) (*backend, error) {
	if req.PluginID != "android_sdk" {
		return nil, fmt.Errorf("expected android_sdk plugin")
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return nil, fmt.Errorf("native Android SDK supports Linux amd64 only")
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
	return &backend{req: req, home: filepath.Clean(home), root: filepath.Join(req.AppsDir, "AndroidStudio/SDK"), client: &http.Client{Timeout: 30 * time.Minute}, log: log}, nil
}
func serve(ctx context.Context, in io.Reader, out io.Writer) error {
	req, err := protocol.ReadRequest(in)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(out)
	var emitErr error
	emit := func(kind, message, status string, choices []choice) error {
		message = protocol.SafeText(message)
		if len(message) > protocol.MaxMessage {
			return fmt.Errorf("event exceeds message limit; refusing truncation")
		}
		return enc.Encode(protocol.Event{Protocol: 1, RequestID: req.RequestID, Type: kind, Message: message, Status: status, Choices: choices})
	}
	if err = emit("started", "Android SDK", "", nil); err != nil {
		return err
	}
	b, err := newBackend(req, func(s string) {
		if emitErr == nil {
			emitErr = emit("log", s, "", nil)
		}
	})
	var choices []choice
	if err == nil {
		choices, err = b.execute(ctx)
	}
	if emitErr != nil {
		return emitErr
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
	args := b.req.Arguments
	switch b.req.Operation {
	case "menu":
		return b.menu(ctx, args)
	case "status":
		if len(args) != 0 {
			return nil, fmt.Errorf("status takes no arguments")
		}
		return nil, b.status()
	case "install", "update":
		opts, err := parseOptions(args, b.req.Operation == "update")
		if err != nil {
			return nil, err
		}
		if !sha256RE.MatchString(opts.Accept) {
			return nil, fmt.Errorf("review licenses first: atm plugin android_sdk menu review (or review-update); --accept-licenses requires its exact 64-character digest")
		}
		if _, err = b.ownedRoot(false); err != nil {
			return nil, err
		}
		plan, err := b.plan(ctx, opts, b.req.Operation)
		if err != nil {
			return nil, err
		}
		if _, err = reviewOutput(plan); err != nil {
			return nil, err
		}
		if plan.digest() != opts.Accept {
			return nil, fmt.Errorf("reviewed plan/licenses changed; review again before acceptance")
		}
		return nil, b.withLock(func() error { return b.install(ctx, plan, b.req.Operation == "update") })
	case "remove":
		if len(args) != 1 {
			return nil, fmt.Errorf("remove requires one package ID")
		}
		if _, err := packageDir(args[0]); err != nil {
			return nil, err
		}
		if args[0] == toolsID {
			return nil, fmt.Errorf("remove command-line tools with uninstall, not alone")
		}
		if _, err := b.ownedRoot(true); err != nil {
			return nil, err
		}
		return nil, b.withLock(func() error { return b.removePackage(ctx, args[0]) })
	case "uninstall":
		if len(args) != 0 {
			return nil, fmt.Errorf("uninstall takes no arguments")
		}
		if _, err := b.ownedRoot(true); err != nil {
			return nil, err
		}
		return nil, b.withLock(func() error { return b.uninstall(ctx) })
	case "integrate":
		if len(args) > 1 || (len(args) == 1 && args[0] != "path") {
			return nil, fmt.Errorf("integrate accepts only optional path")
		}
		if _, err := b.ownedRoot(true); err != nil {
			return nil, err
		}
		return nil, b.withLock(func() error { return b.integrate(ctx, false) })
	default:
		return nil, fmt.Errorf("unsupported Android SDK operation")
	}
}
