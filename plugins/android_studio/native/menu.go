package main

import (
	"context"
	"fmt"
	"strconv"
)

func menuChoice(id, key, op string, args ...string) choice {
	return choice{ID: id, Label: label(key), Operation: op, Arguments: args}
}
func (b *backend) menu(ctx context.Context, args []string) ([]choice, error) {
	back := menuChoice("back", "back", "menu")
	if len(args) > 0 && args[0] == "--custom" {
		if len(args) < 2 || len(args) > 5 {
			return nil, fmt.Errorf("invalid custom install step")
		}
		name := args[1]
		if !safeText(name, 120) || slug(name) == "" {
			return nil, fmt.Errorf("valid display name required")
		}
		keys := []string{"version_prompt", "package_prompt", "url_prompt", "hash_prompt"}
		if len(args) >= 3 && !versionRE.MatchString(args[2]) {
			return nil, fmt.Errorf("four-component distribution version required")
		}
		if len(args) >= 4 && (!safeArchivePath(args[3]) || !packageName(args[3])) {
			return nil, fmt.Errorf("safe tar.gz package required")
		}
		if len(args) == 5 {
			if _, err := httpsURL(args[4]); err != nil {
				return nil, err
			}
			return []choice{{ID: "custom-hash", Label: label("custom_hash"), Operation: "install", Arguments: []string{"--id", "custom", "--name", name, "--version", args[2], "--package", args[3], "--url", args[4], "--sha256"}, Prompt: label("hash_prompt")}, back}, nil
		}
		next := choice{ID: "custom-next", Label: label("custom_next"), Operation: "menu", Arguments: append([]string(nil), args...), Prompt: label(keys[len(args)-2])}
		return []choice{next, back}, nil
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
			if args[1] == "remove" && (current == v || !b.hasReceipt(v)) {
				continue
			}
			a, err := b.spec(v)
			if err != nil {
				return nil, err
			}
			if len(out) >= 99 {
				return nil, fmt.Errorf("too many versions; use exact CLI target")
			}
			out = append(out, choice{ID: "v-" + strconv.Itoa(len(out)), Label: label(args[1]+"_version", a.target()), Operation: args[1], Arguments: []string{a.target()}})
		}
		if len(out) == 0 {
			b.log(label("empty"))
		}
		return append(out, back), nil
	}
	if len(args) != 0 {
		return nil, fmt.Errorf("invalid Android Studio menu page")
	}
	out := []choice{}
	for _, a := range catalog {
		out = append(out, choice{ID: a.ID, Label: label("install_version", a.Name, a.Version), Operation: "install", Arguments: []string{"--id", a.ID}})
	}
	return append(out, choice{ID: "custom", Label: label("custom"), Operation: "menu", Arguments: []string{"--custom"}, Prompt: label("name_prompt")}, menuChoice("status", "status", "status"), menuChoice("use", "use", "menu", "--installed", "use"), menuChoice("remove", "remove", "menu", "--installed", "remove"), menuChoice("uninstall", "uninstall", "uninstall"), menuChoice("integrate", "integrate", "integrate")), nil
}
func (b *backend) status(ctx context.Context) error {
	vs, err := b.versionDirs(false)
	if err != nil {
		return err
	}
	current, err := b.current()
	if err != nil {
		return err
	}
	if current == "" {
		b.log("ATM Android Studio: not selected")
	} else {
		b.log("ATM Android Studio current directory: " + current)
	}
	for _, v := range vs {
		if err := ctx.Err(); err != nil {
			return err
		}
		a, err := b.spec(v)
		if err != nil {
			b.log(v + ": unrecognized/changed metadata; preserved")
			continue
		}
		text := a.target() + " - " + a.Name
		if err = b.validInstalled(v); err != nil {
			text += "; identity unavailable/changed: " + err.Error()
		} else if b.hasReceipt(v) {
			rec, err := b.readReceipt(v)
			if err != nil {
				return err
			}
			text += "; IDE " + rec.Identity.ProductVersion + " build " + rec.Identity.Build + " (native receipt; full tree not verified)"
		} else {
			text += " (legacy/unowned; preserved)"
		}
		b.log(text)
	}
	b.log("No IDE or Java process was executed; this is not a runtime health check.")
	return ctx.Err()
}
