package cli

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/AtomyCloud/atm/internal/config"
	"github.com/AtomyCloud/atm/internal/coreupdate"
	"github.com/charmbracelet/x/term"
)

// This entrypoint deliberately does not resolve an ATM home, load configuration
// or discover plugins: the installed directory may be absent after interruption.
// Run it from a separately trusted ATM executable outside the installation.
func recoverCore(args []string, in *os.File, out, stderr io.Writer, dry, yes, asJSON bool, timeout time.Duration) error {
	flags := flag.NewFlagSet("recover-core", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", "", "absolute interrupted ATM installation root")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || !filepath.IsAbs(*root) || filepath.Clean(*root) != *root || filepath.Dir(*root) == *root {
		return fmt.Errorf("usage: atm [--yes|--dry-run] recover-core --root ABSOLUTE_PATH")
	}
	if strings.ContainsAny(*root, "\x00\r\n") {
		return fmt.Errorf("invalid recovery root")
	}
	preview := map[string]any{"executor": "native-core", "operation": "recover-core", "root": *root, "dry_run": dry}
	if dry {
		return outputJSON(out, preview)
	}
	if !yes {
		if asJSON || in == nil || !term.IsTerminal(in.Fd()) {
			return fmt.Errorf("CORE recovery requires --yes or interactive confirmation")
		}
		if err := outputJSON(stderr, preview); err != nil {
			return err
		}
		fmt.Fprint(stderr, "Restore the verified previous ATM package? [y/N]: ")
		answer, err := bufio.NewReader(in).ReadString('\n')
		if err != nil {
			return err
		}
		if strings.ToLower(strings.TrimSpace(answer)) != "y" {
			return fmt.Errorf("recovery cancelled")
		}
	}
	key, err := coreupdate.OfficialPublicKey()
	if err != nil {
		return err
	}
	ctx, stop := coreContext(config.Config{NativeTimeout: timeout})
	defer stop()
	return executeRecovery(ctx, coreupdate.BundlePolicy{Root: *root, PublicKey: key, OS: runtime.GOOS, Arch: runtime.GOARCH}, out)
}

func executeRecovery(ctx context.Context, p coreupdate.BundlePolicy, out io.Writer) error {
	if !coreupdate.ProductionEnabled {
		return coreupdate.ErrProductionDisabled
	}
	result, err := coreupdate.RecoverBundle(ctx, p, true)
	if writeErr := outputJSON(out, result); writeErr != nil {
		return writeErr
	}
	return err
}
