package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"text/tabwriter"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"

	"github.com/AtomyCloud/atm/internal/config"
	"github.com/AtomyCloud/atm/internal/core"
	"github.com/AtomyCloud/atm/internal/coreupdate"
	"github.com/AtomyCloud/atm/internal/i18n"
	"github.com/AtomyCloud/atm/internal/plugin"
	"github.com/AtomyCloud/atm/internal/protocol"
	"github.com/AtomyCloud/atm/internal/runner"
	"github.com/AtomyCloud/atm/internal/tui"
	"github.com/AtomyCloud/atm/internal/version"
)

const Help = `ATM 0.0.2 - Go migration

Usage: atm [global options] <command>
  tui | menu                    Open the terminal interface
  doctor                        Inspect migration capabilities (no probes)
  plugins list [--json]          Read local plugins without executing them
  plugins sort [az|za|default]   Read or save native display order
  plugin <id> [menu|status]      Open a plugin or inspect its installation
  install <id> [arguments...]   Install through the Linux backend
  update <id> [arguments...]    Update through a native plugin
  use <id> <version>            Change an active version
  remove <id> <version>         Remove one version
  uninstall <id>                Uninstall through the Linux backend
  lang list|current|set <locale>
  plugins search [text]         Search the official catalog natively (read-only)
  plugins download <id>         Download verified official plugin code natively
  updates check|plugin <id>|all|changes|backups
  updates restore <backup-id>   Restore a native plugin code backup
  updates recover plugins      Recover an interrupted plugin transaction
  updates verify <id> <commit>  Explicitly verify an official plugin baseline
  path show|apply               Inspect/apply native user PATH integration
  setup portable --manifest-sha256 <digest>
                               Install an explicitly approved local package
  install                       Full Setup, orchestrated natively

Global options (before the command):
  --home PATH   ATM distribution directory; never persisted as Apps path
  --dry-run     Print planned operations; no processes, network or writes
  --json        Structured output for list, doctor and execution plans
  --yes         Confirm native mutating operations (CLI only)
  --timeout 15m Native operation deadline
  --no-mouse    Disable terminal mouse capture
  --no-animation  Disable the animated banner
  --demo        Simulate TUI operations without executing them
  --version     Show migration version and source commit

Native JSONL executors support Linux, macOS and Windows; application ports are pending.
Linux application operations still require the existing Bash dependencies.
Compiled CORE replacement awaits an approved Ed25519 key; bin/atm remains legacy.
`

func Run(args []string, in *os.File, out, stderr io.Writer) int {
	if err := run(args, in, out, stderr); err != nil {
		fmt.Fprintln(stderr, "ATM:", err)
		return runner.ExitCode(err)
	}
	return 0
}

func run(args []string, in *os.File, out, stderr io.Writer) error {
	flags := flag.NewFlagSet("atm", flag.ContinueOnError)
	flags.SetOutput(stderr)
	homeArg := flags.String("home", "", "ATM home")
	dry := flags.Bool("dry-run", false, "no execution or writes")
	jsonOutput := flags.Bool("json", false, "structured output")
	showVersion := flags.Bool("version", false, "version")
	shortVersion := flags.Bool("v", false, "version")
	noMouse := flags.Bool("no-mouse", false, "disable mouse")
	noAnimation := flags.Bool("no-animation", false, "disable animation")
	demo := flags.Bool("demo", false, "simulate TUI operations")
	yes := flags.Bool("yes", false, "confirm native operations")
	timeout := flags.Duration("timeout", runner.DefaultTimeout, "native operation deadline")
	flags.Usage = func() { fmt.Fprint(out, Help) }
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if *showVersion || *shortVersion {
		fmt.Fprintf(out, "atm %s (%s)\n", version.Version, version.Commit)
		return nil
	}
	args = flags.Args()
	if len(args) == 0 {
		args = []string{"tui"}
	}
	if args[0] == "help" {
		fmt.Fprint(out, Help)
		return nil
	}
	home, err := config.ResolveHome(*homeArg)
	if err != nil {
		return err
	}
	c, err := config.Load(home, *dry || *demo)
	if err != nil {
		return err
	}
	c.NoMouse = *noMouse
	c.NoAnimation = *noAnimation
	if *timeout <= 0 {
		return fmt.Errorf("--timeout must be positive")
	}
	c.NativeTimeout, c.AssumeYes = *timeout, *yes
	if *demo && args[0] != "tui" && args[0] != "menu" {
		return fmt.Errorf("--demo is available only with tui/menu")
	}
	if args[0] == "lang" || args[0] == "language" {
		return language(c, args[1:], out)
	}
	if args[0] == "plugins" && len(args) >= 2 && args[1] == "sort" {
		if len(args) == 2 {
			fmt.Fprintln(out, c.Sort)
			return nil
		}
		if len(args) != 3 {
			return fmt.Errorf("usage: plugins sort [az|za|default]")
		}
		p := c.Preferences
		p.Sort = args[2]
		if err := c.Save(p); err != nil {
			return err
		}
		if c.DryRun {
			fmt.Fprint(out, "DRY-RUN: ")
		}
		fmt.Fprintln(out, "sort", p.Sort)
		return nil
	}
	if core.IsCommand(args) {
		var items []plugin.Plugin
		if args[0] == "install" {
			items, err = plugin.Discover(c, runtime.GOOS)
			if err != nil {
				return err
			}
		}
		return executeCore(c, args, items, in, out, stderr, *jsonOutput)
	}
	if args[0] != "doctor" {
		if err = coreupdate.StartupGuard(c.Home); err != nil {
			return err
		}
	}
	plugins, err := plugin.Discover(c, runtime.GOOS)
	if err != nil {
		return err
	}
	switch args[0] {
	case "tui", "menu":
		if len(args) != 1 {
			return fmt.Errorf("tui/menu takes no arguments")
		}
		if *jsonOutput {
			return fmt.Errorf("TUI cannot emit JSON; use plugins list or doctor")
		}
		output, ok := out.(*os.File)
		if !ok || !term.IsTerminal(in.Fd()) || !term.IsTerminal(output.Fd()) {
			return fmt.Errorf("TUI requires an interactive terminal; use plugins list or --help")
		}
		words, err := i18n.Load(home, c.Language)
		if err != nil {
			return err
		}
		model := tui.New(c, plugins, words, *demo)
		defer model.Close()
		_, err = tea.NewProgram(model, tea.WithInput(in), tea.WithOutput(out)).Run()
		return err
	case "doctor":
		if len(args) != 1 {
			return fmt.Errorf("doctor takes no arguments")
		}
		_, bashErr := exec.LookPath("bash")
		_, pendingErr := os.Lstat(filepath.Join(c.Home, ".atm-core-update-pending"))
		report := map[string]any{"version": version.Version, "commit": version.Commit, "platform": c.Platform(),
			"home": c.Home, "apps_dir": c.AppsDir, "plugins_dir": c.PluginsDir, "preferences": c.PreferenceFile(),
			"plugin_count": len(plugins), "legacy_backend": runtime.GOOS == "linux" && bashErr == nil,
			"legacy_recovery_pending": !os.IsNotExist(pendingErr), "native_application_backends": "pending",
			"compiled_core_updater": "blocked: approved Ed25519 public key required", "discovery_executes_plugins": false,
			"native_catalog_search": true, "native_catalog_download": true,
			"native_component_updates": true, "native_setup": true, "native_full_setup": true,
			"plugin_desktop_integration": "pending native contract"}
		report["compiled_recovery_pending"] = coreupdate.StartupGuard(c.Home) != nil
		report["native_executor"] = "jsonl-v1"
		report["state_dir"] = c.StateDir
		return outputJSON(out, report)
	case "plugins":
		if len(args) >= 2 && args[1] == "download" {
			if len(args) != 3 {
				return fmt.Errorf("usage: plugins download <id>")
			}
			return downloadCatalog(c, args[2], in, out, stderr, *jsonOutput)
		}
		if len(args) >= 2 && args[1] == "search" {
			return searchCatalog(c, plugins, strings.Join(args[2:], " "), out, *jsonOutput)
		}
		if len(args) == 1 || args[1] == "list" {
			if len(args) > 3 || (len(args) == 3 && args[2] != "--json") {
				return fmt.Errorf("usage: plugins list [--json]")
			}
			if *jsonOutput || len(args) == 3 {
				return outputJSON(out, plugins)
			}
			w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tVERSION\tNAME\tBACKEND")
			for _, p := range plugins {
				state := p.Executor
				if !p.Available {
					state = p.Reason
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", p.ID, p.Version, p.Name, state)
			}
			return w.Flush()
		}
	case "plugin", "install", "update", "remove", "uninstall", "use", "status":
		if len(args) < 2 {
			return fmt.Errorf("%s requires a plugin ID; Full Setup is available in the TUI", args[0])
		}
		p, err := plugin.Find(plugins, args[1])
		if err != nil {
			return err
		}
		action, rest := args[0], args[2:]
		if action == "plugin" {
			action = "menu"
			if p.Executor == "native" {
				action = p.Operations[0]
				for _, op := range p.Operations {
					if op == "status" {
						action = op
						break
					}
				}
			}
			if len(rest) > 0 {
				action, rest = rest[0], rest[1:]
			}
		}
		plan, err := runner.Plugin(c, p, action, rest)
		if err != nil {
			return err
		}
		return execute(c, plan, in, out, stderr, *jsonOutput)
	}
	return fmt.Errorf("unknown command: %s", strings.Join(args, " "))
}

func execute(c config.Config, plan runner.Plan, in *os.File, out, stderr io.Writer, asJSON bool) error {
	if c.DryRun {
		if asJSON {
			return outputJSON(out, plan)
		}
		fmt.Fprint(out, "DRY-RUN:")
		if plan.Executor == "native" {
			fmt.Fprintf(out, " native %q %q", plan.PluginID, plan.Operation)
			for _, arg := range plan.Arguments {
				fmt.Fprintf(out, " %q", arg)
			}
			fmt.Fprintln(out)
			return nil
		}
		for _, arg := range plan.Command {
			fmt.Fprintf(out, " %q", arg)
		}
		fmt.Fprintln(out)
		return nil
	}
	if plan.Executor == "native" {
		if plan.Operation != "status" && !c.AssumeYes {
			if asJSON || !term.IsTerminal(in.Fd()) {
				return fmt.Errorf("native changes require --yes or interactive confirmation")
			}
			fmt.Fprintf(stderr, "Run trusted native plugin %s (%s)? [y/N]: ", plan.PluginID, plan.Operation)
			answer, err := bufio.NewReader(in).ReadString('\n')
			if err != nil {
				return err
			}
			if strings.ToLower(strings.TrimSpace(answer)) != "y" {
				return fmt.Errorf("operation cancelled")
			}
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		emit := func(e protocol.Event) error {
			if asJSON {
				return json.NewEncoder(out).Encode(e)
			}
			_, err := fmt.Fprintf(out, "%s: %s\n", e.Type, protocol.SafeText(e.Message))
			return err
		}
		result := runner.RunNative(ctx, c, plan, runner.NativeOptions{Timeout: c.NativeTimeout, Event: emit, Diagnostic: func(s string) error { _, err := fmt.Fprintln(stderr, s); return err }})
		if err := emit(result.Event()); err != nil {
			return err
		}
		return result.Err()
	}
	if asJSON {
		return fmt.Errorf("legacy interactive operations do not emit JSON; use --dry-run --json for plans")
	}
	cmd, err := runner.Command(c, plan)
	if err != nil {
		return err
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = in, out, stderr
	return cmd.Run()
}

func language(c config.Config, args []string, out io.Writer) error {
	if len(args) == 0 || (len(args) == 1 && args[0] == "current") {
		fmt.Fprintln(out, c.Language)
		return nil
	}
	if len(args) == 1 && args[0] == "list" {
		locales, err := i18n.Locales(c.Home)
		if err != nil {
			return err
		}
		fmt.Fprintln(out, strings.Join(locales, "\n"))
		return nil
	}
	if len(args) == 2 && args[0] == "set" {
		if _, err := i18n.Load(c.Home, args[1]); err != nil {
			return err
		}
		p := c.Preferences
		p.Language = args[1]
		if err := c.Save(p); err != nil {
			return err
		}
		if c.DryRun {
			fmt.Fprint(out, "DRY-RUN: ")
		}
		fmt.Fprintln(out, "language", p.Language)
		return nil
	}
	return fmt.Errorf("usage: lang list|current|set <locale>; interactive selection is in the TUI")
}

func outputJSON(out io.Writer, value any) error {
	e := json.NewEncoder(out)
	e.SetIndent("", "  ")
	return e.Encode(value)
}
