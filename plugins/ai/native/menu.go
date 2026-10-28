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
		return append(out, choice{ID: "status", Label: label("status"), Operation: "status"}), nil
	}
	if err := b.logDiscovery(ctx, id); err != nil {
		return nil, err
	}
	if id == hermesSystem {
		return b.systemMenu(ctx)
	}
	b.log(providers[id].docs)
	b.log(label("trust"))
	b.log("Private install root: " + b.root(id))
	if id == "hermes-desktop" {
		b.log(label("appimage"))
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
	}
	if err := b.systemStatus(); err != nil {
		return err
	}
	b.log(label("preserved"))
	return ctx.Err()
}
