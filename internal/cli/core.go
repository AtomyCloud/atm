package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/AtomyCloud/atm/internal/config"
	"github.com/AtomyCloud/atm/internal/core"
	"github.com/AtomyCloud/atm/internal/plugin"
	"github.com/AtomyCloud/atm/internal/runner"
	"github.com/charmbracelet/x/term"
)

func coreContext(c config.Config) (context.Context, func()) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	timeout := c.NativeTimeout
	if timeout <= 0 {
		timeout = runner.DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	return ctx, func() { cancel(); stop() }
}

func executeCore(c config.Config, args []string, plugins []plugin.Plugin, in *os.File, out, stderr io.Writer, asJSON bool) error {
	p, err := core.Prepare(c, args, plugins)
	if err != nil {
		return err
	}
	if c.DryRun {
		return outputJSON(out, p)
	}
	if p.Mutating && !c.AssumeYes && (asJSON || in == nil || !term.IsTerminal(in.Fd())) {
		return fmt.Errorf("CORE changes require --yes or interactive confirmation")
	}
	ctx, stop := coreContext(c)
	e, err := core.Resolve(ctx, c, p)
	stop()
	if err != nil {
		return err
	}
	if p.Mutating && !c.AssumeYes {
		if err := outputJSON(stderr, e.Preview); err != nil {
			return err
		}
		fmt.Fprint(stderr, "Apply this native CORE operation? [y/N]: ")
		answer, err := bufio.NewReader(in).ReadString('\n')
		if err != nil {
			return err
		}
		if strings.ToLower(strings.TrimSpace(answer)) != "y" {
			return fmt.Errorf("operation cancelled")
		}
	}
	ctx, stop = coreContext(c)
	defer stop()
	if p.Arguments[0] == "install" {
		if asJSON {
			for _, item := range p.Plugins {
				if item.Executor == "legacy-bash" {
					return fmt.Errorf("legacy Full Setup requires terminal output; omit --json")
				}
			}
		}
		result, err := core.FullSetup(ctx, c, p, in, out, stderr)
		if writeErr := outputJSON(out, result); writeErr != nil {
			return writeErr
		}
		return err
	}
	result, err := core.Execute(ctx, c, e, true)
	if writeErr := outputJSON(out, result); writeErr != nil {
		return writeErr
	}
	return err
}
