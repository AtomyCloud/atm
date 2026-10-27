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
	"text/tabwriter"

	"github.com/charmbracelet/x/term"

	"github.com/AtomyCloud/atm/internal/catalog"
	"github.com/AtomyCloud/atm/internal/config"
	"github.com/AtomyCloud/atm/internal/plugin"
	"github.com/AtomyCloud/atm/internal/protocol"
)

func downloadCatalog(c config.Config, id string, in *os.File, out, stderr io.Writer, asJSON bool) error {
	if err := catalog.CheckDestination(c.PluginsDir, id); err != nil {
		return err
	}
	if c.DryRun {
		if asJSON {
			return outputJSON(out, map[string]any{"executor": "native-core", "operation": "plugins download", "id": id, "source": catalog.Source, "directory": c.PluginsDir, "dry_run": true})
		}
		_, err := fmt.Fprintf(out, "DRY-RUN: download plugin %q from %s into %q (no network or writes)\n", id, catalog.Source, c.PluginsDir)
		return err
	}
	if !c.AssumeYes && (asJSON || !term.IsTerminal(in.Fd())) {
		return fmt.Errorf("plugin download requires --yes or interactive confirmation")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	ctx, cancel := context.WithTimeout(ctx, c.NativeTimeout)
	client := catalog.New(os.Getenv("GITHUB_TOKEN"))
	plan, err := client.Prepare(ctx, id, nil)
	// No staging exists yet; restore default Ctrl+C handling while asking consent.
	cancel()
	stop()
	if err != nil {
		return err
	}
	info := plan.Info()
	if !c.AssumeYes {
		fmt.Fprintf(stderr, "Source: %s\nPlugin: %s\nCommit: %s\nFiles: %d (%d bytes)\nDestination: %s\nPlugins are trusted code and can run with your permissions after restart.\nDownload this plugin code? [y/N]: ", info.Source, info.ID, info.Commit, info.Files, info.Bytes, c.PluginsDir)
		answer, err := bufio.NewReader(in).ReadString('\n')
		if err != nil {
			return err
		}
		if strings.ToLower(strings.TrimSpace(answer)) != "y" {
			return fmt.Errorf("download cancelled")
		}
	}
	ctx, stop = signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel = context.WithTimeout(ctx, c.NativeTimeout)
	defer cancel()
	result, err := client.Download(ctx, plan, c.PluginsDir)
	if err != nil {
		return err
	}
	if asJSON {
		return outputJSON(out, result)
	}
	_, err = fmt.Fprintf(out, "Downloaded %s at %s\nCommit: %s\nRestart ATM to load this plugin. No application was installed.\n", result.ID, result.Directory, result.Commit)
	for _, warning := range result.Warnings {
		fmt.Fprintln(stderr, warning)
	}
	return err
}

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
