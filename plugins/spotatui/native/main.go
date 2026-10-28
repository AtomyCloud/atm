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
	req                                                            protocol.Request
	home, root, atmRoot, localRoot, localEntry, location, platform string
	client                                                         *http.Client
	log                                                            func(string)
	run                                                            probeRunner
	look                                                           func(string) (string, error)
}

func newBackend(req protocol.Request, log func(string)) (*backend, error) {
	if req.PluginID != "spotatui" || runtime.GOOS != "linux" || (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64") {
		return nil, fmt.Errorf("spotatui requires Linux amd64/arm64")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	envPath := func(key, fallback string) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		return fallback
	}
	b := &backend{req: req, home: home, log: log, client: &http.Client{Timeout: 5 * time.Minute}, run: runProbe, look: findExecutable}
	b.atmRoot = envPath("ATM_SPOTATUI_INSTALL_ROOT", filepath.Join(req.AppsDir, "spotatui"))
	b.localRoot = envPath("ATM_SPOTATUI_LOCAL_ROOT", filepath.Join(home, ".local/share/atm/spotatui"))
	b.localEntry = envPath("ATM_SPOTATUI_LOCAL_ENTRY", filepath.Join(home, ".local/bin/spotatui"))
	for _, p := range []string{home, req.AppsDir, b.atmRoot, b.localRoot, b.localEntry} {
		if !filepath.IsAbs(p) || filepath.Clean(p) != p || p == "/" || strings.ContainsAny(p, "\x00\r\n:") {
			return nil, fmt.Errorf("clean absolute HOME/application paths without PATH separators required")
		}
	}
	inside := func(a, c string) bool { return a == c || strings.HasPrefix(a, c+string(filepath.Separator)) }
	if inside(b.atmRoot, b.localRoot) || inside(b.localRoot, b.atmRoot) || inside(b.localEntry, b.atmRoot) || inside(b.localEntry, b.localRoot) {
		return nil, fmt.Errorf("ATM/local roots and local entry must not overlap")
	}
	b.platform = "linux-x86_64"
	if runtime.GOARCH == "arm64" {
		b.platform = "linux-aarch64"
	}
	b.selectLocation("atm")
	return b, nil
}
func (b *backend) selectLocation(location string) {
	b.location = location
	base := b.atmRoot
	if location == "local" {
		base = b.localRoot
	}
	b.root = filepath.Join(base, "native-v1")
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
	if err = emit("started", label("title"), "", nil); err != nil {
		return err
	}
	var choices []choice
	// Protocol dry-run does not even inspect the filesystem or dependency PATH.
	if os.Getenv("ATM_DRY_RUN") == "1" {
		return emit("result", "Dry-run: plan only; no filesystem, network or subprocess activity", "success", nil)
	}
	b, err := newBackend(req, func(s string) { _ = emit("log", s, "", nil) })
	if err == nil {
		choices, err = b.execute(ctx)
	}
	status, message := "success", label("complete")
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
	if isLauncher() {
		if err := launch(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := serve(ctx, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type options struct {
	location, version, sub string
	trust                  bool
}

func parseOptions(op string, args []string) (options, error) {
	o := options{location: "atm", version: "latest"}
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") {
			if op == "menu" && o.sub == "" {
				o.sub = arg
				continue
			}
			if (op == "use" || op == "remove") && !seen["version"] {
				v, err := normalizeVersion(arg)
				if err != nil {
					return o, err
				}
				o.version = v
				seen["version"] = true
				continue
			}
			return o, fmt.Errorf("unexpected argument %s", arg)
		}
		key, val, eq := strings.Cut(arg, "=")
		if seen[key] {
			return o, fmt.Errorf("duplicate option %s", key)
		}
		seen[key] = true
		if key == "--trust-dependencies" {
			if eq {
				return o, fmt.Errorf("trust flag takes no value")
			}
			o.trust = true
			continue
		}
		if key != "--location" && key != "--version" {
			return o, fmt.Errorf("unknown option %s", key)
		}
		if !eq {
			i++
			if i >= len(args) {
				return o, fmt.Errorf("missing option value")
			}
			val = args[i]
		}
		if key == "--location" {
			if val != "atm" && val != "local" {
				return o, fmt.Errorf("location must be atm or local")
			}
			o.location = val
		} else {
			if op != "install" && op != "update" {
				return o, fmt.Errorf("--version is only for install/update")
			}
			if val != "latest" {
				var err error
				val, err = normalizeVersion(val)
				if err != nil {
					return o, err
				}
			}
			o.version = val
		}
	}
	if (op == "use" || op == "remove") && !seen["version"] {
		return o, fmt.Errorf("one installed version required")
	}
	if o.trust && op != "install" && op != "update" && op != "use" && !(op == "menu" && o.sub == "dependencies") {
		return o, fmt.Errorf("dependency trust is not applicable")
	}
	if op == "status" && len(args) != 0 {
		return o, fmt.Errorf("status takes no arguments")
	}
	return o, nil
}
func (b *backend) execute(ctx context.Context) ([]choice, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if os.Getenv("ATM_DRY_RUN") == "1" {
		b.log("Dry-run: no filesystem, network or subprocess activity")
		return nil, nil
	}
	o, err := parseOptions(b.req.Operation, b.req.Arguments)
	if err != nil {
		return nil, err
	}
	b.selectLocation(o.location)
	switch b.req.Operation {
	case "menu":
		return b.menu(ctx, o)
	case "status":
		return nil, b.status()
	case "install", "update", "use":
		if !o.trust {
			return nil, fmt.Errorf("external dependency execution requires --trust-dependencies; inspect menu dependencies first")
		}
		if err = b.preflight(ctx); err != nil {
			return nil, err
		}
		if b.req.Operation == "use" {
			if err = b.verifyOwned(ctx, o.version); err != nil {
				return nil, err
			}
		}
		deps, err := b.dependencies(ctx)
		if err != nil {
			return nil, err
		}
		return nil, b.withLock(func() error {
			if err := b.preflight(ctx); err != nil {
				return err
			}
			if b.req.Operation == "use" {
				if err := b.refresh(ctx, o.version, deps); err != nil {
					return err
				}
				return b.activate(ctx, o.version)
			}
			return b.install(ctx, o, deps)
		})
	case "remove", "uninstall", "integrate":
		if err = b.preflight(ctx); err != nil {
			return nil, err
		}
		// Refusals against absent/unowned versions must not create a native root.
		if b.req.Operation == "remove" {
			if err = b.verifyOwned(ctx, o.version); err != nil {
				return nil, err
			}
		}
		if b.req.Operation == "integrate" {
			v, e := b.current()
			if e != nil {
				return nil, e
			}
			if v == "" {
				return nil, fmt.Errorf("no native version selected")
			}
		}
		if _, err = os.Lstat(b.root); os.IsNotExist(err) {
			return nil, fmt.Errorf("no native installation")
		}
		return nil, b.withLock(func() error {
			switch b.req.Operation {
			case "remove":
				return b.remove(ctx, o.version)
			case "uninstall":
				return b.uninstall(ctx)
			default:
				return b.integrate(ctx)
			}
		})
	default:
		return nil, fmt.Errorf("unsupported spotatui operation %s", b.req.Operation)
	}
}
