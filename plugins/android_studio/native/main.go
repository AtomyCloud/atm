// Native Android Studio backend. CORE owns consent and dry-run; this process owns Android Studio policy.
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

type backend struct {
	ctx                  context.Context
	req                  protocol.Request
	home, root, platform string
	client               *http.Client

	log func(string)
}

func newBackend(req protocol.Request, log func(string)) (*backend, error) {
	if req.PluginID != "android_studio" {
		return nil, fmt.Errorf("expected android_studio plugin")
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return nil, fmt.Errorf("native Android Studio supports Linux amd64 only")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(home) || !filepath.IsAbs(req.AppsDir) || filepath.Clean(req.AppsDir) == "/" {
		return nil, fmt.Errorf("absolute user HOME and AppsDir required")
	}
	for _, p := range []string{home, req.AppsDir} {
		if strings.ContainsAny(p, "\x00\r\n:") {
			return nil, fmt.Errorf("unsafe PATH directory")
		}
	}
	arch := "x64"
	if runtime.GOARCH == "arm64" {
		arch = "arm64"
	}
	return &backend{req: req, home: home, root: filepath.Join(req.AppsDir, "AndroidStudio"), platform: arch, client: &http.Client{Timeout: 30 * time.Minute}, log: log}, nil
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
	if err = emit("started", "Android Studio", "", nil); err != nil {
		return err
	}
	b, err := newBackend(req, func(s string) {
		if len(s) > protocol.MaxMessage {
			s = s[:protocol.MaxMessage]
		}
		_ = emit("log", s, "", nil)
	})
	var choices []choice
	if err == nil {
		choices, err = b.execute(ctx)
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
	b.ctx = ctx
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
		return nil, b.status(ctx)
	case "install":
		o, err := parseOptions(args)
		if err != nil {
			return nil, err
		}
		return nil, b.withLock(func() error {
			return b.install(ctx, o)
		})
	case "use", "remove":
		if len(args) != 1 {
			return nil, fmt.Errorf("%s requires one installed version", b.req.Operation)
		}
		v, err := b.resolveTarget(args[0])
		if err != nil {
			return nil, err
		}
		return nil, b.withLock(func() error {
			if b.req.Operation == "use" {
				return b.activate(ctx, v)
			}
			return b.remove(ctx, v)
		})
	case "uninstall", "integrate":
		if len(args) != 0 {
			return nil, fmt.Errorf("%s takes no arguments", b.req.Operation)
		}
		return nil, b.withLock(func() error {
			if b.req.Operation == "uninstall" {
				return b.uninstall(ctx)
			}
			v, err := b.current()
			if err != nil {
				return err
			}
			if v == "" {
				return fmt.Errorf("no portable Android Studio selected")
			}
			if err = b.validInstalled(v); err != nil {
				return err
			}
			return b.integrate(ctx)
		})
	default:
		return nil, fmt.Errorf("unsupported Android Studio operation: %s", b.req.Operation)
	}
}
