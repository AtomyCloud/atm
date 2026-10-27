package core

import (
	"context"
	"fmt"
	"io"

	"github.com/AtomyCloud/atm/internal/config"
	"github.com/AtomyCloud/atm/internal/protocol"
	"github.com/AtomyCloud/atm/internal/runner"
)

// FullSetup orchestrates plugins in Go. Only explicit legacy app operations
// enter the compatibility bridge; no legacy CORE command is used.
func FullSetup(ctx context.Context, c config.Config, p Plan, in io.Reader, out, stderr io.Writer) (BatchResult, error) {
	if c.DryRun {
		return BatchResult{}, fmt.Errorf("dry-run cannot execute Full Setup")
	}
	return RunBatch(ctx, p, func(ctx context.Context, plan runner.Plan) error {
		fmt.Fprintf(stderr, "Plugin: %s (%s)\n", plan.PluginID, plan.Executor)
		if plan.Executor == "native" {
			outcome := runner.RunNative(ctx, c, plan, runner.NativeOptions{Timeout: c.NativeTimeout,
				Event: func(e protocol.Event) error {
					if e.Message != "" {
						fmt.Fprintln(stderr, protocol.SafeText(e.Message))
					}
					return nil
				},
				Diagnostic: func(text string) error { fmt.Fprintln(stderr, protocol.SafeText(text)); return nil },
			})
			return outcome.Err()
		}
		cmd, err := runner.Command(c, plan)
		if err != nil {
			return err
		}
		// Legacy installers retain their interactive terminal and existing process
		// behavior. They are not advertised as native/cancellable transactions.
		cmd.Stdin, cmd.Stdout, cmd.Stderr = in, out, stderr
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = cmd.Run(); err != nil {
			return err
		}
		return ctx.Err()
	})
}
