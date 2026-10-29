//go:build linux

// AI native preview. The production manifest remains legacy until provider parity.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/AtomyCloud/atm/internal/protocol"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

type choice = protocol.Choice
type backend struct {
	req    protocol.Request
	arch   string
	client *http.Client
	log    func(string)
	// Unit tests inject transaction faults, never through the process environment.
	checkpoint          func(string) error
	system              *systemTools
	codexDesktop        *codexDesktopTools
	codexRPM            *systemTools
	discovery           *discoveryConfig
	hermes              *hermesTools
	hermesAlias         string
	ollamaService       *ollamaServiceTools
	ollamaLifecycleLock os.FileInfo
}
type options struct {
	provider, version  string
	trust              bool
	acceptSystem       bool
	acceptDependencies bool
	removePath         bool
	refreshLauncher    bool
	keepPreparation    bool
	acceptPM           bool
	rocm               bool
	serviceAction      string
	acceptService      bool
}

func newBackend(req protocol.Request, log func(string)) (*backend, error) {
	if req.PluginID != "ai" || runtime.GOOS != "linux" || (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64") {
		return nil, fmt.Errorf("AI preview requires Linux amd64/arm64")
	}
	if err := absolutePath(req.AppsDir); err != nil {
		return nil, err
	}
	return &backend{req: req, arch: runtime.GOARCH, client: &http.Client{Timeout: 5 * time.Minute}, log: log}, nil
}
func parseOptions(op string, args []string) (options, error) {
	o := options{version: "latest"}
	if op == "status" {
		if len(args) != 0 {
			return o, fmt.Errorf("status takes no arguments")
		}
		return o, nil
	}
	if op == "menu" {
		if len(args) > 1 {
			return o, fmt.Errorf("menu expects at most one provider")
		}
		if len(args) == 1 {
			o.provider = args[0]
			if _, ok := providers[o.provider]; !ok {
				return o, fmt.Errorf("unknown provider")
			}
		}
		return o, nil
	}
	switch op {
	case "install", "update", "use", "remove", "uninstall", "integrate":
	default:
		return o, fmt.Errorf("unsupported AI operation")
	}
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		key, value, eq := strings.Cut(args[i], "=")
		if seen[key] {
			return o, fmt.Errorf("duplicate option %s", key)
		}
		seen[key] = true
		switch key {
		case "--accept-service-execution":
			if eq || op != "integrate" {
				return o, fmt.Errorf("service consent is a bare integrate-only flag")
			}
			o.acceptService = true
		case "--service-action":
			if op != "integrate" {
				return o, fmt.Errorf("service actions require integrate")
			}
			if !eq {
				i++
				if i == len(args) {
					return o, fmt.Errorf("missing service action")
				}
				value = args[i]
			}
			o.serviceAction = value
		case "--rocm":
			if eq || (op != "install" && op != "update") {
				return o, fmt.Errorf("--rocm is a bare opt-in for Ollama install/update only")
			}
			o.rocm = true
		case "--keep-preparation":
			if eq || op != "uninstall" {
				return o, fmt.Errorf("--keep-preparation applies only to Hermes deactivation via uninstall")
			}
			o.keepPreparation = true
		case "--refresh-launcher":
			if eq || op != "integrate" {
				return o, fmt.Errorf("launcher refresh applies only to integrate")
			}
			o.refreshLauncher = true
		case "--accept-dependency-execution":
			if eq || (op != "install" && op != "update") {
				return o, fmt.Errorf("dependency execution consent applies only to install/update")
			}
			o.acceptPM = true
		case "--remove-path":
			if eq || op != "integrate" {
				return o, fmt.Errorf("PATH removal is only applicable to integrate")
			}
			o.removePath = true
		case "--trust-download":
			if eq || (op != "install" && op != "update") {
				return o, fmt.Errorf("download trust is not applicable")
			}
			o.trust = true
		case "--accept-system-packages":
			if eq || (op != "install" && op != "update" && op != "uninstall") {
				return o, fmt.Errorf("system package consent is only applicable to supported install/update/uninstall operations")
			}
			o.acceptSystem = true
		case "--accept-dependencies":
			if eq || op != "install" {
				return o, fmt.Errorf("dependency package consent is a bare initial-install-only flag")
			}
			o.acceptDependencies = true
		case "--provider", "--version":
			if !eq {
				i++
				if i == len(args) {
					return o, fmt.Errorf("missing option value")
				}
				value = args[i]
			}
			if key == "--provider" {
				o.provider = value
			} else {
				o.version = value
			}
		default:
			return o, fmt.Errorf("unknown argument %s", key)
		}
	}
	if op == "integrate" {
		if seen["--service-action"] || o.acceptService {
			if o.provider != "ollama" || !seen["--service-action"] {
				return o, fmt.Errorf("service action requires explicit --provider ollama")
			}
			for key := range seen {
				if key != "--provider" && key != "--service-action" && key != "--accept-service-execution" {
					return o, fmt.Errorf("service and PATH/version/download options cannot be combined")
				}
			}
			switch o.serviceAction {
			case "status":
				if o.acceptService {
					return o, fmt.Errorf("readonly service status accepts no consent")
				}
			case "register", "start", "stop", "remove":
				if !o.acceptService {
					return o, fmt.Errorf("service mutation requires --accept-service-execution and CORE confirmation")
				}
			default:
				return o, fmt.Errorf("unsupported service action; autostart is deferred")
			}
			return o, nil
		}
		if o.refreshLauncher && (o.provider != hermesAgent || o.removePath) {
			return o, fmt.Errorf("--refresh-launcher requires explicit --provider hermes-agent and excludes --remove-path")
		}
		if seen["--version"] || (seen["--provider"] && cliCommand(o.provider) == "") {
			return o, fmt.Errorf("integrate accepts only --provider picoclaw|codex-cli|ollama|hermes-agent and --remove-path")
		}
		return o, nil
	}
	if _, ok := providers[o.provider]; !ok {
		return o, fmt.Errorf("explicit supported --provider required")
	}
	if o.rocm && o.provider != "ollama" {
		return o, fmt.Errorf("--rocm requires explicit --provider ollama")
	}
	if o.acceptDependencies && o.provider != codexDesktopRPM {
		return o, fmt.Errorf("dependency package consent applies only to initial Codex RPM installation")
	}
	if o.acceptPM && o.provider != hermesAgent {
		return o, fmt.Errorf("dependency execution consent applies only to Hermes Agent")
	}
	if o.keepPreparation && o.provider != hermesAgent {
		return o, fmt.Errorf("--keep-preparation requires explicit --provider hermes-agent")
	}
	if o.provider == hermesAgent && op == "uninstall" && !o.keepPreparation {
		return o, fmt.Errorf("Hermes deactivation requires --keep-preparation; physical removal is outside this retention policy")
	}
	if o.provider == hermesAgent && op != "install" && op != "update" && op != "use" && op != "uninstall" {
		return o, fmt.Errorf("Hermes Agent preview supports install/update, exact completed selection, explicit PATH and deactivation with --keep-preparation; physical removal is outside this retention policy")
	}
	if o.provider == hermesAgent && op == "use" && (!seen["--version"] || o.version != hermesPMVersion) {
		return o, fmt.Errorf("Hermes selection requires reviewed exact --version %s", hermesPMVersion)
	}
	if o.provider == codexDesktopSystem {
		if (op != "install" && op != "update" && op != "uninstall") || o.version != "latest" {
			return o, fmt.Errorf("ChatGPT Desktop with Codex supports latest-only system DEB install/update and explicit nonpurge uninstall; DNF and Arch remain Pending")
		}
		if op == "uninstall" && !o.acceptSystem {
			return o, fmt.Errorf("ChatGPT Desktop uninstall requires --accept-system-packages")
		}
	} else if o.provider == codexDesktopRPM {
		if op == "uninstall" {
			if !o.acceptSystem || o.trust || seen["--version"] {
				return o, fmt.Errorf("RPM uninstall requires --accept-system-packages, no download trust or version")
			}
		} else if (op != "install" && op != "update") || o.version != "latest" || !o.trust || !o.acceptSystem {
			return o, fmt.Errorf("RPM install/update requires latest, --trust-download and --accept-system-packages")
		}
		for key := range seen {
			if key != "--provider" && key != "--version" && key != "--trust-download" && key != "--accept-system-packages" && key != "--accept-dependencies" {
				return o, fmt.Errorf("inapplicable RPM option")
			}
		}
	} else if o.provider == hermesSystem {
		if op != "install" {
			return o, fmt.Errorf("Hermes Desktop system preview supports install only; use the system package manager for administration")
		}
	} else if o.acceptSystem {
		return o, fmt.Errorf("system package consent is not applicable to this provider")
	}
	if o.version != "latest" && !versionRE.MatchString(o.version) {
		return o, fmt.Errorf("expected stable X.Y.Z or latest")
	}
	if (op == "use" || op == "remove") && o.version == "latest" {
		return o, fmt.Errorf("exact --version required")
	}
	if op == "uninstall" && seen["--version"] {
		return o, fmt.Errorf("uninstall takes no version")
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
	if o.rocm && b.arch != "amd64" {
		return nil, fmt.Errorf("Ollama ROCm companion requires Linux amd64; hardware compatibility is not checked")
	}
	switch b.req.Operation {
	case "menu":
		return b.menu(ctx, o.provider)
	case "status":
		return nil, b.status(ctx)
	case "integrate":
		if o.serviceAction != "" {
			return nil, b.ollamaServiceExecute(ctx, o)
		}
		return nil, b.integrate(ctx, o)
	case "install", "update":
		if !o.trust {
			return nil, fmt.Errorf("official executable download requires --trust-download; review provider menu")
		}
	}
	if o.provider == codexDesktopSystem {
		if b.req.Operation == "uninstall" {
			return nil, b.removeCodexDesktop(ctx, o)
		}
		return nil, b.installCodexDesktop(ctx, o)
	}
	if o.provider == codexDesktopRPM {
		return nil, b.manageCodexRPM(ctx, o)
	}
	if o.provider == hermesSystem {
		if !o.acceptSystem {
			return nil, fmt.Errorf("system installation requires --trust-download and --accept-system-packages; review provider menu")
		}
		return nil, b.installHermesSystem(ctx, o)
	}
	if o.provider == hermesAgent {
		if b.req.Operation == "uninstall" {
			return nil, b.deactivateHermesAgent(ctx)
		}
		if b.req.Operation == "use" {
			return nil, b.useHermesAgent(ctx, o.version)
		}
		if !o.acceptPM {
			return nil, fmt.Errorf("Hermes Agent requires --trust-download and --accept-dependency-execution; review its menu first")
		}
		return nil, b.installHermesAgent(ctx, o)
	}
	err = b.locked(ctx, o.provider, func() error {
		if o.provider == "ollama" {
			return b.ollamaExecute(ctx, o)
		}
		switch b.req.Operation {
		case "install", "update":
			return b.install(ctx, o)
		case "use":
			return b.activate(ctx, o.provider, o.version)
		case "remove":
			return b.remove(ctx, o.provider, o.version)
		case "uninstall":
			return b.uninstall(ctx, o.provider)
		}
		return fmt.Errorf("unsupported operation")
	})
	return nil, err
}
func (b *backend) boundary(name string) error {
	if b.checkpoint != nil {
		return b.checkpoint(name)
	}
	return nil
}
func (b *backend) root(id string) string {
	return filepath.Join(b.req.AppsDir, providers[id].path, "native-v1")
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
		status, message, choices = "error", err.Error(), nil
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			status = "cancelled"
		}
	}
	return emit("result", message, status, choices)
}
func main() {
	if len(os.Args) > 1 && (os.Args[1] == codexGuardFlag || os.Args[1] == codexGuardEarlyFlag) {
		if err := codexAPTGuardMain(os.Args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if executable, err := os.Executable(); err == nil && filepath.Base(executable) == "hermes" {
		hermesAliasMain(executable, os.Args[1:])
		return
	}
	if len(os.Args) > 1 {
		if os.Args[1] == "--hermes-cli" {
			hermesCLIMain(os.Args[2:])
			return
		}
		fmt.Fprintln(os.Stderr, label("hermes_launch_usage"))
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := serve(ctx, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
