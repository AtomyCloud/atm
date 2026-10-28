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
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/AtomyCloud/atm/internal/config"
	"github.com/AtomyCloud/atm/internal/protocol"
)

const registry = "https://registry.npmjs.org"

var stable = regexp.MustCompile(`^(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})$`)

type backend struct {
	req                                  protocol.Request
	home, base, root, state, defaultBase string
	client                               *http.Client
	log                                  func(string)
	run                                  commandRunner
}
type options struct {
	action, version, path, sub string
	trust                      bool
}
type choice = protocol.Choice

func env(key, fallback string) string {
	if s := os.Getenv(key); s != "" {
		return s
	}
	return fallback
}
func newBackend(req protocol.Request, log func(string)) (*backend, error) {
	if req.PluginID != "rea" || runtime.GOOS != "linux" || (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64") {
		return nil, fmt.Errorf("REA native management requires Linux amd64/arm64")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	b := &backend{req: req, home: home, log: log, run: runCommand, client: &http.Client{Timeout: 5 * time.Minute}}
	b.defaultBase = env("ATM_REA_INSTALL_ROOT", filepath.Join(req.AppsDir, "rea"))
	cfg, err := config.Load(req.Home, false)
	if err != nil {
		return nil, err
	}
	b.state = filepath.Join(cfg.StateDir, "rea/native-install.json")
	for _, p := range []string{home, req.Home, req.AppsDir, b.defaultBase, b.state} {
		if err = cleanPath(p); err != nil {
			return nil, err
		}
	}
	b.setBase(b.defaultBase)
	return b, nil
}
func (b *backend) setBase(base string) { b.base = base; b.root = filepath.Join(base, "native-v1") }
func parseOptions(op string, args []string) (options, error) {
	o := options{action: op, version: "latest"}
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if op == "menu" && !strings.HasPrefix(a, "--") && o.sub == "" {
			o.sub = a
			continue
		}
		key, val, eq := strings.Cut(a, "=")
		if seen[key] {
			return o, fmt.Errorf("duplicate option %s", key)
		}
		seen[key] = true
		switch key {
		case "--trust-dependencies":
			if eq {
				return o, fmt.Errorf("trust flag takes no value")
			}
			o.trust = true
		case "--update", "--dependencies", "--status":
			if op != "install" || o.action != "install" || eq {
				return o, fmt.Errorf("conflicting action")
			}
			o.action = strings.TrimPrefix(key, "--")
		case "--path", "--version":
			if !eq {
				i++
				if i == len(args) {
					return o, fmt.Errorf("missing %s value", key)
				}
				val = args[i]
			}
			if val == "" {
				return o, fmt.Errorf("empty option")
			}
			if key == "--path" {
				o.path = val
			} else {
				o.version = val
			}
		default:
			return o, fmt.Errorf("unknown argument %s", a)
		}
	}
	if o.version != "latest" && !stable.MatchString(o.version) {
		return o, fmt.Errorf("exact stable X.Y.Z or latest required")
	}
	if o.path != "" {
		if err := cleanPath(o.path); err != nil {
			return o, err
		}
	}
	if o.action != "install" && o.action != "update" && !(op == "menu" && (o.sub == "install" || o.sub == "version")) && (seen["--path"] || seen["--version"]) {
		return o, fmt.Errorf("path/version only apply to installation")
	}
	if o.trust && o.action != "install" && o.action != "update" && o.action != "dependencies" && !(op == "menu" && o.sub == "dependencies") {
		return o, fmt.Errorf("dependency trust is not applicable")
	}
	if op == "status" && len(args) > 0 {
		return o, fmt.Errorf("status takes no arguments")
	}
	return o, nil
}
func (b *backend) execute(ctx context.Context) ([]choice, error) {
	o, err := parseOptions(b.req.Operation, b.req.Arguments)
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if err = b.remembered(); err != nil {
		return nil, err
	}
	if o.action == "status" {
		return nil, b.status()
	}
	if o.action == "menu" {
		return b.menu(ctx, o)
	}
	if o.action == "dependencies" {
		return nil, b.diagnostics(ctx, o.trust)
	}
	if o.path != "" && o.path != b.base {
		if _, e := os.Lstat(b.state); e == nil {
			return nil, fmt.Errorf("remove the remembered native installation before changing destination")
		}
		b.setBase(o.path)
	}
	if err = b.validateBase(); err != nil {
		return nil, err
	}
	if err = b.preflight(ctx); err != nil {
		return nil, err
	}
	switch o.action {
	case "install", "update":
		if !o.trust {
			return nil, fmt.Errorf("Node/npm/ps execution requires --trust-dependencies; inspect menu dependencies first")
		}
		v, e := b.current()
		if e != nil {
			return nil, e
		}
		if o.action == "update" && v == "" {
			return nil, fmt.Errorf("no native installation to update")
		}
		return nil, b.locked(func() error { return b.install(ctx, o) })
	case "remove", "uninstall", "integrate":
		if _, e := os.Lstat(b.state); e != nil {
			return nil, fmt.Errorf("no remembered native installation; external/legacy installations preserved")
		}
		return nil, b.locked(func() error {
			if o.action == "integrate" {
				return b.integrate(ctx)
			}
			return b.uninstall(ctx)
		})
	default:
		return nil, fmt.Errorf("unsupported REA operation %s", o.action)
	}
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
	if os.Getenv("ATM_DRY_RUN") == "1" {
		return emit("result", "Dry-run: no filesystem, network or subprocess activity", "success", nil)
	}
	b, err := newBackend(req, func(s string) { _ = emit("log", s, "", nil) })
	var choices []choice
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
