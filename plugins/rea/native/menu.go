package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func (b *backend) menu(ctx context.Context, o options) ([]choice, error) {
	switch o.sub {
	case "":
		if err := b.status(); err != nil {
			return nil, err
		}
		return []choice{{ID: "install", Label: label("install"), Operation: "menu", Arguments: []string{"install"}},
			{ID: "update", Label: label("update"), Operation: "update", Arguments: []string{"--trust-dependencies"}},
			{ID: "remove", Label: label("remove"), Operation: "uninstall"},
			{ID: "dependencies", Label: label("dependencies"), Operation: "menu", Arguments: []string{"dependencies"}},
			{ID: "details", Label: label("details"), Operation: "menu", Arguments: []string{"details"}},
			{ID: "integrate", Label: label("integrate"), Operation: "integrate"}}, nil
	case "install":
		return []choice{{ID: "default", Label: label("default"), Operation: "menu", Arguments: []string{"version", "--path", b.base}},
			{ID: "custom", Label: label("custom"), Operation: "menu", Arguments: []string{"version", "--path"}, Prompt: "Absolute dedicated base (native-v1 will be appended)"}}, nil
	case "version":
		base := o.path
		if base == "" {
			base = b.base
		}
		return []choice{{ID: "latest", Label: label("latest"), Operation: "install", Arguments: []string{"--trust-dependencies", "--path", base}},
			{ID: "exact", Label: label("exact"), Operation: "install", Arguments: []string{"--trust-dependencies", "--path", base, "--version"}, Prompt: "Stable X.Y.Z"}}, nil
	case "dependencies":
		if err := b.diagnostics(ctx, o.trust); err != nil {
			return nil, err
		}
		if !o.trust {
			return []choice{{ID: "trust", Label: label("trust"), Operation: "menu", Arguments: []string{"dependencies", "--trust-dependencies"}}}, nil
		}
		return nil, nil
	case "details":
		if err := b.status(); err != nil {
			return nil, err
		}
		b.log("Agent setup/doctor/uninstall are separate REA commands: ATM never runs them, installs skills or edits MCP registrations. Use ATM update for native prefixes. PATH integration is explicit.")
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown REA submenu")
	}
}
func executable(path string) bool {
	i, e := os.Stat(path)
	return e == nil && i.Mode().IsRegular() && i.Mode().Perm()&0111 != 0
}
func externalVersion(path string) string {
	p, e := filepath.EvalSymlinks(path)
	if e != nil || filepath.Base(p) != "rea.mjs" || filepath.Base(filepath.Dir(p)) != "scripts" {
		return "unavailable"
	}
	var v struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	data, e := readRegular(filepath.Join(filepath.Dir(filepath.Dir(p)), "package.json"), 2<<20)
	if e != nil {
		return "unavailable"
	}
	if e = decodeJSON(data, &v, false); e != nil || v.Name != "rea-agents" || !stable.MatchString(v.Version) {
		return "unavailable"
	}
	return v.Version
}
func (b *backend) status() error {
	found := false
	v, err := b.current()
	if err != nil {
		return err
	}
	if v != "" {
		data, e := readRegular(filepath.Join(b.root, marker), 128)
		if e != nil || string(data) != magic {
			return fmt.Errorf("native REA root ownership marker is missing/invalid")
		}
		target, e := os.Readlink(filepath.Join(b.root, "bin/rea"))
		if e != nil || target != "../current/bin/rea" {
			return fmt.Errorf("native REA launcher link is missing/foreign")
		}
		if _, err = b.readReceipt(v); err != nil {
			return err
		}
		launcher := filepath.Join(b.root, "versions", v, "bin/rea")
		if err = realDirs(filepath.Dir(launcher), false); err != nil {
			return err
		}
		i, e := os.Lstat(launcher)
		if e != nil || !i.Mode().IsRegular() || i.Mode().Perm()&0111 == 0 {
			return fmt.Errorf("native REA selection is incomplete: executable launcher missing")
		}
		b.log("REA " + v + " (native managed; " + filepath.Join(b.root, "bin/rea") + ")")
		found = true
	}
	path, _ := exec.LookPath("rea")
	if !filepath.IsAbs(path) {
		path = ""
	}
	seen := map[string]bool{}
	legacyEntry := ""
	legacyState := env("ATM_REA_STATE_FILE", filepath.Join(filepath.Dir(b.state), "install.json"))
	if cleanPath(legacyState) == nil {
		data, e := readRegular(legacyState, 16384)
		if e == nil {
			var legacy struct {
				Schema int    `json:"schema"`
				Root   string `json:"root"`
			}
			if decodeJSON(data, &legacy, true) == nil && legacy.Schema == 1 && cleanPath(legacy.Root) == nil {
				legacyEntry = filepath.Join(legacy.Root, "bin/rea")
			}
		}
	}
	for _, p := range []string{filepath.Join(b.base, "bin/rea"), legacyEntry, filepath.Join(b.req.AppsDir, "rea/bin/rea"), filepath.Join(b.home, "Apps/rea/bin/rea"), filepath.Join(b.home, ".local/bin/rea"), path} {
		if p == "" || seen[p] || !executable(p) {
			continue
		}
		seen[p] = true
		actual, _ := filepath.EvalSymlinks(p)
		if v != "" && actual == filepath.Join(b.root, "versions", v, "bin/rea") {
			continue
		}
		b.log("REA " + externalVersion(p) + " (manual/external; " + p + ")")
		found = true
	}
	if !found {
		b.log("REA not detected")
	}
	if path == "" {
		b.log("REA is not on PATH; detected absolute commands remain usable")
	}
	return nil
}
