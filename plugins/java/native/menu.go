package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
)

func menuChoice(id, key, op string, args ...string) choice {
	return choice{ID: id, Label: label(key), Operation: op, Arguments: args}
}
func (b *backend) menu(ctx context.Context, args []string) ([]choice, error) {
	back := menuChoice("back", "back", "menu")
	if len(args) > 0 && args[0] == "--custom" {
		if len(args) < 2 || len(args) > 3 || !safeLabel(args[1]) {
			return nil, fmt.Errorf("custom JDK needs a safe exact version label")
		}
		next := choice{ID: "custom-url", Label: label("custom_url"), Operation: "menu", Arguments: []string{"--custom", args[1]}, Prompt: label("url_prompt")}
		if len(args) == 3 {
			if _, err := httpsURL(args[2]); err != nil {
				return nil, err
			}
			next = choice{ID: "custom-hash", Label: label("custom_hash"), Operation: "install", Arguments: []string{"--vendor", "custom", "--version", args[1], "--url", args[2], "--sha256"}, Prompt: label("hash_prompt")}
		}
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
			if args[1] == "remove" && (v == current || !b.hasReceipt(v)) {
				continue
			}
			if len(out) == 99 {
				return nil, fmt.Errorf("too many versions; use CLI vendor/version")
			}
			text := label(args[1]+"_version", v)
			if v == current {
				text += " " + label("current")
			}
			out = append(out, choice{ID: "v-" + strconv.Itoa(len(out)), Label: text, Operation: args[1], Arguments: []string{v}})
		}
		if len(out) == 0 {
			b.log(label("empty"))
		}
		return append(out, back), nil
	}
	if len(args) != 0 {
		return nil, fmt.Errorf("invalid Java menu page")
	}
	out := []choice{}
	for _, v := range vendors[:5] {
		version := "26"
		if v == "openjdk" {
			version = "26.0.1"
		}
		if v == "microsoft" || v == "graalvm" {
			version = "25"
		}
		out = append(out, menuChoice(v, v, "install", "--vendor", v, "--version", version))
	}
	return append(out, choice{ID: "custom", Label: label("custom"), Operation: "menu", Arguments: []string{"--custom"}, Prompt: label("label_prompt")}, menuChoice("status", "status", "status"), menuChoice("use", "use", "menu", "--installed", "use"), menuChoice("update", "update", "update"), menuChoice("remove", "remove", "menu", "--installed", "remove"), menuChoice("uninstall", "uninstall", "uninstall"), menuChoice("integrate", "integrate", "integrate")), nil
}
func (b *backend) status(ctx context.Context) error {
	vs, err := b.installed()
	if err != nil {
		return err
	}
	current, err := b.current()
	if err != nil {
		return err
	}
	if current == "" {
		b.log("ATM Java: not selected")
	} else {
		b.log("ATM Java current: " + current + " (" + filepath.Join(b.root, "current") + ")")
	}
	for _, v := range vs {
		if err = ctx.Err(); err != nil {
			return err
		}
		actual, err := b.actualVersion(v)
		if err != nil {
			return err
		}
		origin := "legacy/unowned (preserved)"
		if b.hasReceipt(v) {
			origin = "native receipt (not verified by status)"
		}
		b.log(v + " - JAVA_VERSION=" + actual + " - " + origin)
	}
	return ctx.Err()
}
