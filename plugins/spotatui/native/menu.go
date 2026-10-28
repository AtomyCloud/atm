package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

func (b *backend) status() error {
	saved := b.location
	defer b.selectLocation(saved)
	for _, loc := range []string{"atm", "local"} {
		b.selectLocation(loc)
		v, err := b.current()
		state := "none"
		if err != nil {
			state = "unknown/changed (not executed)"
		} else if v != "" {
			state = v
		}
		b.log(fmt.Sprintf("%s native: %s; root: %s", loc, state, b.root))
		base := b.atmRoot
		if loc == "local" {
			base = b.localRoot
		}
		if _, err := os.Lstat(filepath.Join(base, "current")); err == nil {
			b.log(loc + " legacy current present; external/unmanaged, version not probed")
		}
	}
	if _, err := os.Lstat(b.localEntry); err == nil {
		b.log("User-local entry present: " + b.localEntry + " (not executed)")
	}
	if path, err := b.look("spotatui"); err == nil {
		b.log("PATH command: " + path + " (not executed)")
	}
	return nil
}
func (b *backend) menu(ctx context.Context, o options) ([]choice, error) {
	switch o.sub {
	case "":
		_ = b.status()
		return []choice{{ID: "atm", Label: label("atm"), Operation: "menu", Arguments: []string{"actions", "--location", "atm"}}, {ID: "local", Label: label("local"), Operation: "menu", Arguments: []string{"actions", "--location", "local"}}, {ID: "versions", Label: label("versions"), Operation: "menu", Arguments: []string{"versions"}}, {ID: "dependencies", Label: label("dependencies"), Operation: "menu", Arguments: []string{"dependencies"}}, {ID: "usage", Label: label("usage"), Operation: "menu", Arguments: []string{"usage"}}}, nil
	case "actions":
		args := []string{"--location", o.location}
		choices := []choice{}
		for _, op := range []string{"install", "update", "use", "remove", "uninstall", "integrate"} {
			a := append([]string{}, args...)
			if op == "install" || op == "update" || op == "use" {
				a = append(a, "--trust-dependencies")
			}
			c := choice{ID: op, Label: label(op), Operation: op, Arguments: a}
			if op == "use" || op == "remove" {
				c.Prompt = label("version_prompt")
			}
			choices = append(choices, c)
		}
		choices = append(choices, choice{ID: "custom", Label: label("custom"), Operation: "install", Arguments: []string{"--location", o.location, "--trust-dependencies", "--version"}, Prompt: label("version_prompt")})
		return choices, nil
	case "versions":
		_ = b.status()
		saved := b.location
		defer b.selectLocation(saved)
		for _, loc := range []string{"atm", "local"} {
			b.selectLocation(loc)
			vs, err := b.versionDirs(false)
			if err != nil {
				b.log(loc + ": " + err.Error())
				continue
			}
			if len(vs) > 50 {
				return nil, fmt.Errorf("too many versions for bounded menu output")
			}
			for _, v := range vs {
				state := "native-owned"
				if err := b.verifyOwned(ctx, v); err != nil {
					state = "unowned/changed; preserved"
				}
				b.log(loc + " " + v + " " + state)
			}
		}
		return nil, nil
	case "dependencies":
		if o.trust {
			_, err := b.dependencies(ctx)
			return nil, err
		}
		for _, name := range []string{"ffmpeg", "ffprobe", "yt-dlp", "deno", "node"} {
			path, err := b.look(name)
			if err != nil {
				path = "not found"
			}
			b.log(name + ": " + path + " (not executed)")
		}
		b.log("Probes execute external tools and their subprocesses. No URL or remote EJS fetch is requested; diagnostics are not a playback/compatibility guarantee.")
		return []choice{{ID: "probe", Label: label("probe"), Operation: "menu", Arguments: []string{"dependencies", "--trust-dependencies"}}}, nil
	case "usage":
		b.log(filepath.Join(b.root, "current/bin/spotatui"))
		b.log("Integration is explicit. User settings/data and external installs are retained. https://spotatui.com/")
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown menu route %s", o.sub)
	}
}
