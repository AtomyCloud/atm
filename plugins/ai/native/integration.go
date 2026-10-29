//go:build linux

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

var integrationProviders = []string{"picoclaw", "codex-cli", "ollama", hermesAgent}
var profileNames = []string{".profile", ".bashrc", ".zshrc"}

type pathProfile struct {
	Name      string
	Separator bool
}
type pathReceipt struct {
	Schema                       int
	Plugin, Provider, Root, Home string
	Profiles                     []pathProfile
}

func cliCommand(id string) string {
	switch id {
	case "picoclaw":
		return "picoclaw"
	case "codex-cli":
		return "codex"
	case "ollama":
		return "ollama"
	case hermesAgent:
		return "hermes"
	}
	return ""
}

func cliLink(id string) string {
	if id == "ollama" {
		return "../current/bin/ollama"
	}
	return "../current"
}

// Pin the provider's complete receipt and payload without inspecting PATH state.
// Ollama owns a full bin/lib tree, not the single executable of the other CLIs.
func (b *backend) integrationVersion(ctx context.Context, id, v string) (any, error) {
	if id == "ollama" {
		return b.ollamaVerify(ctx, v, b.ollamaVersion(v))
	}
	return b.verifyVersion(ctx, id, v, true)
}
func integrationHome() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if err = dirs(home, false, false); err != nil {
		return "", err
	}
	i, err := os.Lstat(home)
	if err != nil || owner(i) != uint32(os.Geteuid()) {
		return "", fmt.Errorf("owned default user HOME required")
	}
	return home, nil
}
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func (b *backend) pathBlock(id string) string {
	return "# >>> ATM native AI " + id + " PATH >>>\nexport PATH=" + shellQuote(filepath.Join(b.root(id), "bin")) + "${PATH:+\":$PATH\"}\n# <<< ATM native AI " + id + " PATH <<<\n"
}

// Exact per-profile enrollment, including an inserted line separator, prevents
// adoption and lets detach preserve unrelated bytes without normalizing files.
func (b *backend) profileBlock(id string, p pathProfile) string {
	if p.Separator {
		return "\n" + b.pathBlock(id)
	}
	return b.pathBlock(id)
}
func (b *backend) checkProfile(id string, s pathState, p *pathProfile) error {
	text := string(s.Data)
	needle := "ATM native AI " + id + " PATH"
	if p == nil {
		if strings.Contains(text, needle) {
			return fmt.Errorf("unowned/malformed AI PATH block preserved")
		}
		return nil
	}
	block := b.profileBlock(id, *p)
	pos := strings.Index(text, block)
	if !s.Exists || strings.Count(text, needle) != 2 || strings.Count(text, block) != 1 || pos < 0 || (!p.Separator && pos > 0 && text[pos-1] != '\n') {
		return fmt.Errorf("missing/edited AI PATH block preserved")
	}
	return nil
}
func (r *pathReceipt) profile(name string) *pathProfile {
	if r != nil {
		for i := range r.Profiles {
			if r.Profiles[i].Name == name {
				return &r.Profiles[i]
			}
		}
	}
	return nil
}

func (b *backend) inspectIntegration(id, current string) (*pathReceipt, error) {
	return b.inspectIntegrationContext(context.Background(), id, current)
}

func (b *backend) inspectIntegrationContext(ctx context.Context, id, current string) (*pathReceipt, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cliCommand(id) == "" {
		return nil, nil
	}
	root := b.root(id)
	var rec *pathReceipt
	if _, err := os.Lstat(filepath.Join(root, "path.json")); err == nil {
		rec = &pathReceipt{}
		if err = readJSON(filepath.Join(root, "path.json"), rec); err != nil {
			return nil, err
		}
		home, e := integrationHome()
		if e != nil {
			return nil, e
		}
		if rec.Schema != 1 || rec.Plugin != "ai" || rec.Provider != id || rec.Root != root || rec.Home != home || current == "" || len(rec.Profiles) < 2 || len(rec.Profiles) > 3 {
			return nil, fmt.Errorf("invalid/stale AI PATH receipt")
		}
		for i, p := range rec.Profiles {
			if p.Name != profileNames[i] {
				return nil, fmt.Errorf("invalid AI PATH profile inventory")
			}
		}
		for _, name := range profileNames {
			s, e := pathSnapshot(filepath.Join(home, name), false)
			if e != nil {
				return nil, e
			}
			if e = b.checkProfile(id, s, rec.profile(name)); e != nil {
				return nil, e
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if id == hermesAgent {
		bridge, err := b.inspectHermesBridge(ctx)
		if err != nil {
			return nil, err
		}
		if rec != nil && bridge == nil {
			return nil, fmt.Errorf("enrolled Hermes native bridge missing")
		}
		return rec, nil
	}
	bin := filepath.Join(root, "bin")
	es, err := entries(bin, 1)
	if os.IsNotExist(err) && rec == nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if rec == nil {
		if len(es) != 0 {
			return nil, fmt.Errorf("unowned AI bin entry preserved")
		}
		return nil, nil
	}
	if len(es) != 1 || es[0].Name() != cliCommand(id) {
		return nil, fmt.Errorf("changed AI bin inventory")
	}
	s, err := pathSnapshot(filepath.Join(bin, cliCommand(id)), true)
	if err != nil {
		return nil, err
	}
	if !s.Exists || s.Link != cliLink(id) {
		return nil, fmt.Errorf("changed AI command link preserved")
	}
	return rec, nil
}

func (b *backend) integrationChanges(id, home string, rec *pathReceipt, remove bool) ([]pathChange, error) {
	next := pathReceipt{Schema: 1, Plugin: "ai", Provider: id, Root: b.root(id), Home: home}
	var profiles []pathChange
	for _, name := range profileNames {
		path := filepath.Join(home, name)
		before, err := pathSnapshot(path, false)
		if err != nil {
			return nil, err
		}
		p := rec.profile(name)
		if err = b.checkProfile(id, before, p); err != nil {
			return nil, err
		}
		if !before.Exists && (remove || name == ".zshrc") {
			continue
		}
		after := before
		if remove {
			if p != nil {
				text, block := string(before.Data), b.profileBlock(id, *p)
				pos := strings.Index(text, block)
				prefix, suffix := text[:pos], text[pos+len(block):]
				// Another provider or user content may now depend on this newline.
				if p.Separator && len(prefix) > 0 && !strings.HasSuffix(prefix, "\n") && len(suffix) > 0 {
					prefix += "\n"
				}
				after.Data = []byte(prefix + suffix)
			}
		} else {
			entry := pathProfile{Name: name}
			if p != nil {
				entry = *p
			} else {
				entry.Separator = len(before.Data) > 0 && before.Data[len(before.Data)-1] != '\n'
				after = pathRegular(append(append([]byte{}, before.Data...), []byte(b.profileBlock(id, entry))...))
				if before.Exists {
					after.Mode = before.Mode
				}
			}
			next.Profiles = append(next.Profiles, entry)
		}
		if len(after.Data) > maxProfile {
			return nil, fmt.Errorf("AI PATH would exceed profile size bound")
		}
		profiles = append(profiles, pathChange{Path: path, Before: before, After: after})
	}
	root := b.root(id)
	before, err := pathSnapshot(filepath.Join(root, "path.json"), false)
	if err != nil {
		return nil, err
	}
	after := pathState{}
	if !remove {
		data, e := json.Marshal(next)
		if e != nil {
			return nil, e
		}
		after = pathRegular(append(data, '\n'))
	}
	record := pathChange{Path: filepath.Join(root, "path.json"), Before: before, After: after}
	// Hermes retains its independently receipt-owned native bridge on detach.
	// Only small profile/receipt data enters the profile rollback journal.
	if id == hermesAgent {
		if remove {
			return append(profiles, record), nil
		}
		return append([]pathChange{record}, profiles...), nil
	}
	linkPath := filepath.Join(root, "bin", cliCommand(id))
	linkBefore := pathState{}
	if _, e := os.Lstat(filepath.Dir(linkPath)); e == nil {
		linkBefore, err = pathSnapshot(linkPath, true)
	} else if !os.IsNotExist(e) {
		err = e
	}
	if err != nil {
		return nil, err
	}
	linkAfter := pathState{}
	if !remove {
		linkAfter = pathState{Exists: true, Mode: os.ModeSymlink | 0777, Link: cliLink(id)}
	}
	link := pathChange{Path: linkPath, Before: linkBefore, After: linkAfter, AllowLink: true}
	if remove {
		return append(profiles, link, record), nil
	}
	return append([]pathChange{link, record}, profiles...), nil
}

func (b *backend) integrate(ctx context.Context, o options) error {
	if o.refreshLauncher {
		return b.refreshHermesLauncher(ctx)
	}
	ids := integrationProviders
	if o.provider != "" {
		ids = []string{o.provider}
	}
	var completed []string
	for _, id := range ids {
		_, current, err := b.inspect(ctx, id, true)
		if err == nil && o.provider == "" && current == "" {
			continue
		}
		if err == nil {
			err = b.locked(ctx, id, func() error { return b.changeIntegration(ctx, id, o.removePath) })
		}
		if err != nil {
			return fmt.Errorf("AI PATH %s failed; previously completed providers=%v (not rolled back): %w", id, completed, err)
		}
		completed = append(completed, id)
	}
	if len(completed) == 0 {
		b.log(label("path_none"))
	}
	return ctx.Err()
}

// Uninstall already holds the provider lock. All locks fail fast, including the
// HOME-wide lock shared by providers and distinct configured Apps roots.
func (b *backend) detachIntegration(ctx context.Context, id string) error {
	current, err := b.current(id)
	if err != nil {
		return err
	}
	rec, err := b.inspectIntegration(id, current)
	if err != nil {
		return err
	}
	if rec == nil {
		home, e := os.UserHomeDir()
		if e != nil {
			return e
		}
		if _, e = os.Lstat(home); os.IsNotExist(e) {
			return nil
		}
	}
	return b.changeIntegration(ctx, id, true)
}

func (b *backend) changeIntegration(ctx context.Context, id string, remove bool) (result error) {
	if id == hermesAgent {
		return b.changeHermesIntegration(ctx, remove)
	}
	home, err := integrationHome()
	if err != nil {
		return err
	}
	lock := filepath.Join(home, ".atm-ai-path.lock")
	if err = os.Mkdir(lock, 0700); err != nil {
		return fmt.Errorf("AI profiles locked/interrupted; inspect %s: %w", lock, err)
	}
	retain := false
	defer func() {
		if !retain {
			// No recursive deletion and no paths read back from editable metadata.
			if e := os.Remove(lock); e != nil {
				result = errors.Join(result, fmt.Errorf("AI profile lock cleanup incomplete; inspect %s: %w", lock, e))
			}
		}
	}()
	_, current, err := b.inspect(ctx, id, true)
	if err != nil {
		return err
	}
	rec, err := b.inspectIntegration(id, current)
	if err != nil {
		return err
	}
	if !remove && current == "" {
		return fmt.Errorf("no selected owned CLI preview")
	}
	changes, err := b.integrationChanges(id, home, rec, remove)
	if err != nil {
		return err
	}
	if err = b.boundary("path-preflight"); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	// Pin the selected receipt and payload, then recheck after staging and the
	// fault boundary immediately before publication. No provider is executed.
	var selected any
	if !remove {
		selected, err = b.integrationVersion(ctx, id, current)
		if err != nil {
			return err
		}
		if err = dirs(filepath.Join(b.root(id), "bin"), true, true); err != nil {
			return err
		}
	}
	check := func() error {
		_, now, e := b.inspect(ctx, id, true)
		if e != nil {
			return e
		}
		if now != current {
			return fmt.Errorf("selection changed before PATH publication")
		}
		if !remove {
			r, e := b.integrationVersion(ctx, id, current)
			if e != nil {
				return e
			}
			if !reflect.DeepEqual(r, selected) {
				return fmt.Errorf("selected receipt changed before PATH publication")
			}
		}
		return ctx.Err()
	}
	before := func() error {
		if err := b.boundary("path-before-publish"); err != nil {
			return err
		}
		return check()
	}
	retain, err = b.applyPathChanges(ctx, lock, changes, before, check)
	if err == nil {
		if remove {
			b.log(label("path_removed"))
		} else {
			b.log(label("path_enabled"))
		}
	}
	return err
}
