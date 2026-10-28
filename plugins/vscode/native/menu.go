package main

import (
	"context"
	"fmt"
	"strings"
)

func option(id, operation string, args ...string) choice {
	return choice{ID: id, Label: label(id), Operation: operation, Arguments: args}
}
func (b *backend) menu(ctx context.Context, args []string) ([]choice, error) {
	back := option("back", "menu")
	if len(args) == 0 {
		return []choice{option("install", "menu", "releases"), {ID: "custom", Label: label("custom"), Operation: "install", Prompt: label("version_prompt")}, option("update", "update"), option("use", "menu", "use"), option("remove", "menu", "remove"), option("uninstall", "uninstall"), option("integrate", "menu", "integrate"), option("extensions", "integrate", "extensions"), option("migrate", "menu", "migrate"), option("status", "status")}, nil
	}
	if len(args) == 1 && args[0] == "releases" {
		vs, err := b.releases(ctx)
		if err != nil {
			return nil, err
		}
		if len(vs) > 7 {
			vs = vs[:7]
		}
		out := []choice{}
		for _, v := range vs {
			out = append(out, choice{ID: v, Label: fmt.Sprintf(label("install_version"), v), Operation: "install", Arguments: []string{v}})
		}
		out = append(out, choice{ID: "custom", Label: label("custom"), Operation: "install", Prompt: label("version_prompt")}, back)
		return out, nil
	}
	if len(args) == 1 && args[0] == "integrate" {
		return []choice{{ID: "all", Label: label("integration_all"), Operation: "integrate"}, {ID: "path", Label: label("integration_path"), Operation: "integrate", Arguments: []string{"path"}}, {ID: "cli", Label: label("integration_cli"), Operation: "integrate", Arguments: []string{"cli"}}, {ID: "desktop", Label: label("integration_desktop"), Operation: "integrate", Arguments: []string{"desktop"}}, back}, nil
	}
	if (len(args) == 1 && (args[0] == "use" || args[0] == "remove" || args[0] == "migrate")) || (len(args) == 2 && args[0] == "migrate") {
		vs, err := b.installed()
		if err != nil {
			return nil, err
		}
		if len(vs) > 90 {
			return nil, fmt.Errorf("too many versions; use explicit CLI version")
		}
		out := []choice{}
		if len(args) == 2 {
			if !slotRE.MatchString(args[1]) {
				return nil, fmt.Errorf("invalid migration source")
			}
		}
		for _, v := range vs {
			c := choice{ID: v, Label: fmt.Sprintf(label("version"), strings.TrimPrefix(v, "code_")), Operation: args[0], Arguments: []string{v}}
			if args[0] == "migrate" {
				if len(args) == 1 {
					c.Label = fmt.Sprintf(label("copy_source"), v)
					c.Operation = "menu"
					c.Arguments = []string{"migrate", v}
				} else {
					if v == args[1] {
						continue
					}
					c.Label = fmt.Sprintf(label("copy_target"), v)
					c.Operation = "integrate"
					c.Arguments = []string{"migrate-data", args[1], v, "--closed"}
				}
			}
			out = append(out, c)
		}
		return append(out, back), nil
	}
	return nil, fmt.Errorf("unknown VS Code submenu")
}
