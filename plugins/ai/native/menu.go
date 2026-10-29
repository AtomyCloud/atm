//go:build linux

package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"path/filepath"
)

//go:embed en-us.json
var englishJSON []byte
var english = func() map[string]string {
	var v map[string]string
	if err := json.Unmarshal(englishJSON, &v); err != nil {
		panic(err)
	}
	return v
}()

func label(key string) string {
	if v, ok := english[key]; ok {
		return v
	}
	panic("missing AI label: " + key)
}

var providerOrder = []string{"picoclaw", "hermes-desktop", "codex-cli"}

func (b *backend) menu(ctx context.Context, id string) ([]choice, error) {
	b.log(label("preview"))
	if id == "" {
		out := []choice{}
		for _, p := range providerOrder {
			out = append(out, choice{ID: p, Label: providers[p].name, Operation: "menu", Arguments: []string{p}})
		}
		out = append(out, choice{ID: hermesSystem, Label: providers[hermesSystem].name, Operation: "menu", Arguments: []string{hermesSystem}})
		out = append(out, choice{ID: codexDesktopSystem, Label: providers[codexDesktopSystem].name, Operation: "menu", Arguments: []string{codexDesktopSystem}})
		out = append(out, choice{ID: "ollama", Label: providers["ollama"].name, Operation: "menu", Arguments: []string{"ollama"}})
		out = append(out, choice{ID: hermesAgent, Label: providers[hermesAgent].name, Operation: "menu", Arguments: []string{hermesAgent}})
		return append(out, choice{ID: "status", Label: label("status"), Operation: "status"}), nil
	}
	if id == codexDesktopSystem {
		return b.codexDesktopMenu(ctx)
	}
	if err := b.logDiscovery(ctx, id); err != nil {
		return nil, err
	}
	if id == hermesSystem {
		return b.systemMenu(ctx)
	}
	if id == hermesAgent {
		return b.hermesMenu(ctx)
	}
	b.log(providers[id].docs)
	b.log(label("trust"))
	b.log("Private install root: " + b.root(id))
	if id == "hermes-desktop" {
		b.log(label("appimage"))
	}
	if id == "ollama" {
		b.ollamaGuidance()
	}
	vs, current, err := b.inspect(ctx, id, false)
	if err != nil {
		return nil, err
	}
	base := []string{"--provider", id}
	out := []choice{
		{ID: "install", Label: label("install"), Operation: "install", Arguments: append(append([]string{}, base...), "--trust-download")},
		{ID: "custom", Label: label("custom"), Operation: "install", Arguments: append(append([]string{}, base...), "--trust-download", "--version"), Prompt: label("version_prompt")},
		{ID: "update", Label: label("update"), Operation: "update", Arguments: append(append([]string{}, base...), "--trust-download")},
	}
	if id == "ollama" && b.arch == "amd64" {
		out = append(out,
			choice{ID: "install-rocm", Label: label("ollama_install_rocm"), Operation: "install", Arguments: []string{"--provider", id, "--trust-download", "--rocm"}},
			choice{ID: "update-rocm", Label: label("ollama_update_rocm"), Operation: "update", Arguments: []string{"--provider", id, "--trust-download", "--rocm"}},
		)
	}
	if len(vs) > 45 {
		return nil, fmt.Errorf("too many installed versions for menu; use explicit CLI version")
	}
	for _, v := range vs {
		args := []string{"--provider", id, "--version", v}
		if v != current {
			out = append(out, choice{ID: "use-" + v, Label: label("use") + " " + v, Operation: "use", Arguments: args})
			out = append(out, choice{ID: "remove-" + v, Label: label("remove") + " " + v, Operation: "remove", Arguments: args})
		}
	}
	if cliCommand(id) != "" && current != "" {
		b.log(label("path_scope"))
		out = append(out, choice{ID: "integrate", Label: label("path_integrate"), Operation: "integrate", Arguments: base})
		rec, err := b.inspectIntegration(id, current)
		if err != nil {
			return nil, err
		}
		if rec != nil {
			out = append(out, choice{ID: "remove-path", Label: label("path_detach"), Operation: "integrate", Arguments: []string{"--provider", id, "--remove-path"}})
		}
	}
	out = append(out, choice{ID: "uninstall", Label: label("uninstall"), Operation: "uninstall", Arguments: base}, choice{ID: "back", Label: label("back"), Operation: "menu"})
	return out, nil
}
func (b *backend) status(ctx context.Context) error {
	b.log(label("preview"))
	if err := b.logDiscovery(ctx, ""); err != nil {
		return err
	}
	for _, id := range providerOrder {
		vs, current, err := b.inspect(ctx, id, false)
		if err != nil {
			return err
		}
		b.log(fmt.Sprintf("%s: recorded versions=%d; active=%s (not executed)", providers[id].name, len(vs), current))
		if current != "" {
			b.log("Run explicitly: " + filepath.Join(b.root(id), "current"))
		}
		if cliCommand(id) != "" && current != "" {
			rec, err := b.inspectIntegration(id, current)
			if err != nil {
				return err
			}
			if rec != nil {
				b.log(providers[id].name + ": " + label("path_recorded"))
			} else {
				b.log(providers[id].name + ": " + label("path_absent"))
			}
		}
	}
	if err := b.systemStatus(); err != nil {
		return err
	}
	b.codexDesktopStatus()
	if err := b.ollamaStatus(ctx); err != nil {
		return err
	}
	if err := b.hermesStatus(ctx); err != nil {
		return err
	}
	b.log(label("preserved"))
	return ctx.Err()
}
