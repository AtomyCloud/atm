package main

import (
	"context"
	"fmt"
)

func (b *backend) menu(ctx context.Context, args []string) ([]choice, error) {
	add := func(id, key, op string, args ...string) choice {
		return choice{ID: id, Label: label(key), Operation: op, Arguments: args}
	}
	back := add("back", "back", "menu")
	custom := choice{ID: "custom", Label: label("custom"), Operation: "install", Arguments: []string{"--version"}, Prompt: label("version_prompt")}
	if len(args) == 1 && args[0] == "--latest" {
		releases, err := b.releases(ctx, "stable")
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			b.log(label("offline") + ": " + err.Error())
			return []choice{custom, back}, nil
		}
		out := []choice{}
		for _, a := range releases {
			if a.Ambiguous {
				continue
			}
			out = append(out, choice{ID: a.Version, Label: label("install_version", a.Version), Operation: "install", Arguments: []string{"--version", a.Version}})
			if len(out) == 5 {
				break
			}
		}
		return append(out, custom, back), nil
	}
	if len(args) == 2 && args[0] == "--installed" && (args[1] == "use" || args[1] == "remove") {
		vs, err := b.installed()
		if err != nil {
			return nil, err
		}
		current, err := b.current()
		if err != nil {
			return nil, err
		}
		out := []choice{}
		for _, v := range vs {
			if args[1] == "remove" && (!b.hasReceipt(v) || v == current) {
				continue
			}
			if len(out) == 99 {
				return nil, fmt.Errorf("too many versions; use CLI exact version")
			}
			text := label(args[1]+"_version", v)
			if v == current {
				text += " " + label("current")
			}
			out = append(out, choice{ID: v, Label: text, Operation: args[1], Arguments: []string{v}})
		}
		if len(out) == 0 {
			b.log(label("empty"))
		}
		return append(out, back), nil
	}
	if len(args) != 0 {
		return nil, fmt.Errorf("invalid menu page")
	}
	return []choice{add("latest", "latest", "menu", "--latest"), custom, add("update", "update", "update"), add("status", "status", "status"), add("use", "use", "menu", "--installed", "use"), add("remove", "remove", "menu", "--installed", "remove"), add("uninstall", "uninstall", "uninstall"), add("integrate", "integrate", "integrate")}, nil
}
