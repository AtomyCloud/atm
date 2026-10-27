package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"text/tabwriter"

	"github.com/AtomyCloud/atm/internal/catalog"
	"github.com/AtomyCloud/atm/internal/config"
	"github.com/AtomyCloud/atm/internal/plugin"
	"github.com/AtomyCloud/atm/internal/protocol"
)

func searchCatalog(c config.Config, plugins []plugin.Plugin, query string, out io.Writer, asJSON bool) error {
	if c.DryRun {
		if asJSON {
			return outputJSON(out, map[string]any{"executor": "native-core", "operation": "plugins search",
				"source": catalog.Source, "query": query, "dry_run": true})
		}
		_, err := fmt.Fprintf(out, "DRY-RUN: search %s for %q (no network or writes)\n", catalog.Source, query)
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, c.NativeTimeout)
	defer cancel()
	snapshot, err := catalog.New(os.Getenv("GITHUB_TOKEN")).Search(ctx, query)
	if err != nil {
		return err
	}
	snapshot.MarkLocal(plugins)
	snapshot.Entries = snapshot.Filter("", c.Sort)
	return printCatalog(out, snapshot, asJSON)
}

func printCatalog(out io.Writer, snapshot catalog.Snapshot, asJSON bool) error {
	if asJSON {
		return outputJSON(out, snapshot)
	}
	if _, err := fmt.Fprintf(out, "%s\nCommit: %s\n", snapshot.Source, snapshot.Commit); err != nil {
		return err
	}
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tVERSION\tNAME\tLOCAL\tBACKEND\tDESCRIPTION")
	for _, e := range snapshot.Entries {
		fmt.Fprintf(w, "%s\t%s\t%s\t%t\t%s\t%s\n", e.ID, protocol.SafeText(e.Version),
			protocol.SafeText(e.Name), e.Local, e.Executor, protocol.SafeText(e.Description))
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if len(snapshot.Entries) == 0 {
		_, err := fmt.Fprintln(out, "No plugins found.")
		return err
	}
	return nil
}
