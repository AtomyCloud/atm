package main

import (
	"context"
	"fmt"
	"path/filepath"
)

func menuChoice(id, key, op string, args ...string) choice {
	return choice{ID: id, Label: label(key), Operation: op, Arguments: args}
}
func methodChoices(op, version string) []choice {
	return []choice{menuChoice("binary", "binary", op, "--method", "binary", "--version", version), menuChoice("docker", "docker", op, "--method", "docker", "--version", version), menuChoice("back", "back", "menu")}
}
func (b *backend) menu(ctx context.Context, args []string) ([]choice, error) {
	back := menuChoice("back", "back", "menu")
	if len(args) == 1 && args[0] == "--usage" {
		state, err := b.readDockerState()
		if err != nil {
			return nil, err
		}
		b.usage(state.Selected)
		return []choice{back}, nil
	}
	if len(args) == 1 && args[0] == "--docker-remove" {
		state, err := b.readDockerState()
		if err != nil {
			return nil, err
		}
		out := []choice{}
		for _, v := range sortedDockerVersions(state) {
			if len(out) == 99 {
				return nil, fmt.Errorf("too many Docker versions; use CLI exact version")
			}
			out = append(out, choice{ID: v, Label: label("remove_docker_version", v), Operation: "remove", Arguments: []string{"--method", "docker", "--version", v}})
		}
		if len(out) == 0 {
			b.log(label("empty"))
		}
		return append(out, back), nil
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
			if args[1] == "remove" && (!b.hasReceipt(v) || current == v) {
				continue
			}
			if len(out) == 99 {
				return nil, fmt.Errorf("too many versions; use CLI exact version")
			}
			text := label(args[1]+"_version", v)
			if current == v {
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
	return []choice{menuChoice("binary", "binary_latest", "install", "--method", "binary"), menuChoice("docker", "docker_latest", "install", "--method", "docker"), {ID: "custom", Label: label("custom"), Operation: "install", Arguments: []string{"--version"}, Prompt: label("version_prompt")}, menuChoice("update", "update", "update"), menuChoice("use", "use", "menu", "--installed", "use"), menuChoice("status", "status", "status"), menuChoice("remove", "remove", "menu", "--installed", "remove"), menuChoice("remove-docker", "remove_docker", "menu", "--docker-remove"), menuChoice("uninstall", "uninstall", "uninstall"), menuChoice("integrate", "integrate", "integrate"), menuChoice("usage", "usage", "menu", "--usage")}, nil
}
func (b *backend) usage(v string) {
	if v == "" {
		v = "VERSION"
	}
	b.log(label("docs"))
	b.log("trivy --version")
	b.log("trivy image alpine:latest")
	b.log("trivy fs .")
	b.log("docker run --rm aquasec/trivy:" + v + " --version")
	b.log("docker run --rm aquasec/trivy:" + v + " image alpine:latest")
	b.log("docker run --rm -v \"$PWD:/work:ro\" aquasec/trivy:" + v + " fs /work")
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
		b.log("ATM Trivy binary: not selected")
	} else {
		b.log("ATM Trivy current: " + current + " (" + filepath.Join(b.root, "current") + ")")
	}
	for _, v := range vs {
		if err = ctx.Err(); err != nil {
			return err
		}
		origin := "legacy/unowned (preserved)"
		if b.hasReceipt(v) {
			origin = "native receipt (not verified by status)"
		}
		b.log("Binary " + v + ": " + origin)
	}
	state, err := b.readDockerState()
	if err != nil {
		return err
	}
	for _, v := range sortedDockerVersions(state) {
		rec := state.Images[v]
		b.log("Recorded Docker tag: " + rec.Tag + " (" + rec.ImageID + "); daemon not queried")
	}
	b.log("Legacy Docker state and external images are preserved, not adopted. Scan caches/data are never removed.")
	return ctx.Err()
}
