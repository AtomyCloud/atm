package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/AtomyCloud/atm/internal/catalog"
)

func (b *backend) initialize() error {
	if err := realDirs(b.root, true); err != nil {
		return err
	}
	file := filepath.Join(b.root, marker)
	if _, err := os.Lstat(file); os.IsNotExist(err) {
		if err = os.WriteFile(file, []byte(magic), 0600); err != nil {
			return err
		}
	}
	for _, name := range []string{"versions", "bin"} {
		if err := realDirs(filepath.Join(b.root, name), true); err != nil {
			return err
		}
	}
	return nil
}
func (b *backend) install(ctx context.Context, o options) error {
	if err := b.preflight(ctx); err != nil {
		return err
	}
	// Preflight precedes every dependency execution and network operation.
	work, err := os.MkdirTemp("", "atm-rea-dependencies-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	d, err := b.dependencies(ctx, work)
	if err != nil {
		return err
	}
	a, err := b.resolve(ctx, o.version)
	if err != nil {
		return err
	}
	current, err := b.current()
	if err != nil {
		return err
	}
	if o.action == "update" && current == "" {
		return fmt.Errorf("no native version to update")
	}
	if err = b.initialize(); err != nil {
		return err
	}
	baseline := map[string]fileState{}
	for _, p := range []string{b.state, filepath.Join(b.root, "current"), filepath.Join(b.root, "bin/rea")} {
		s, e := snapshot(p)
		if e != nil {
			return e
		}
		baseline[p] = s
	}
	lock := filepath.Join(b.root, ".atm-native-lock")
	if err = os.Mkdir(lock, 0700); err != nil {
		return err
	}
	defer os.Remove(lock)
	// Holding both state and root locks serializes destination changes and users of the same prefix.
	dest := filepath.Join(b.root, "versions", a.Version)
	existing := false
	if _, err = os.Lstat(dest); err == nil {
		if err = b.verifyOwned(ctx, a.Version); err != nil {
			return err
		}
		r, err := b.readReceipt(a.Version)
		if err != nil {
			return err
		}
		if r.Artifact != a {
			return fmt.Errorf("same-version upstream metadata changed; preserving existing bytes")
		}
		existing = true
	} else if !os.IsNotExist(err) {
		return err
	}
	stage, err := os.MkdirTemp(b.root, ".atm-stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	archive := filepath.Join(stage, "rea.tgz")
	if err = b.download(ctx, a, archive); err != nil {
		return err
	}
	payload := filepath.Join(stage, "payload")
	home := filepath.Join(stage, "npm-home")
	for _, p := range []string{payload, home} {
		if err = os.Mkdir(p, 0700); err != nil {
			return err
		}
	}
	userConfig, globalConfig := filepath.Join(home, "user.npmrc"), filepath.Join(home, "global.npmrc")
	for _, p := range []string{userConfig, globalConfig} {
		if err = os.WriteFile(p, nil, 0600); err != nil {
			return err
		}
	}
	args := []string{d.NPM, "install", "--global", "--prefix", payload, "--registry=" + registry, "--engine-strict=true", "--force=false", "--ignore-scripts=true", "--audit=false", "--fund=false", "--update-notifier=false", "--userconfig=" + userConfig, "--globalconfig=" + globalConfig, "--cache=" + filepath.Join(home, "cache"), "--fetch-retries=0", archive}
	b.log("Installing verified exact REA archive through trusted npm; lifecycle scripts disabled, engine-strict enabled, force disabled. Optional native providers are not guaranteed ready.")
	if _, err = b.run(ctx, d.Node, args, processEnv(home, d.Node, d.PS), home, 10*time.Minute); err != nil {
		return err
	}
	if err = validatePackage(payload, a.Version); err != nil {
		return err
	}
	// Reinstall into staging even for a retained version: npm must check its
	// actual engines against the newly selected Node before runtime refresh.
	if existing {
		return b.activate(ctx, a.Version, &d, baseline)
	}
	if err = realDirs(filepath.Join(payload, "bin"), false); err != nil {
		return err
	}
	for _, name := range []string{"rea", "rea-agents"} {
		p := filepath.Join(payload, "bin", name)
		target, e := os.Readlink(p)
		if e != nil {
			return fmt.Errorf("missing/foreign npm launcher %s", name)
		}
		if filepath.Clean(filepath.Join(filepath.Dir(p), target)) != filepath.Join(payload, "lib/node_modules/rea-agents/scripts/rea.mjs") || filepath.IsAbs(target) {
			return fmt.Errorf("unexpected npm entrypoint link")
		}
		if err = os.Remove(p); err != nil {
			return err
		}
	}
	if err = copyLauncher(ctx, filepath.Join(payload, "bin/rea")); err != nil {
		return err
	}
	if err = os.Symlink("rea", filepath.Join(payload, "bin/rea-agents")); err != nil {
		return err
	}
	runtimeData, _ := json.Marshal(d)
	if err = os.WriteFile(filepath.Join(payload, dependencyFile), runtimeData, 0600); err != nil {
		return err
	}
	files, err := inventory(ctx, payload)
	if err != nil {
		return err
	}
	data, _ := json.Marshal(receipt{1, "rea", a, files})
	if err = os.WriteFile(filepath.Join(payload, receiptName), data, 0600); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	from, err := os.Open(stage)
	if err != nil {
		return err
	}
	defer from.Close()
	to, err := os.Open(filepath.Dir(dest))
	if err != nil {
		return err
	}
	defer to.Close()
	if err = catalog.PublishDirectory(from, to, "payload", a.Version); err != nil {
		return err
	}
	// An inactive verified prefix can remain after activation failure. Never replace it blindly.
	return b.activate(ctx, a.Version, nil, baseline)
}
func (b *backend) activate(ctx context.Context, v string, d *dependencies, baseline map[string]fileState) error {
	if err := b.verifyOwned(ctx, v); err != nil {
		return err
	}
	var changes []change
	add := func(path string, after fileState) error {
		c, e := planned(path, after)
		if e != nil {
			return e
		}
		if before, ok := baseline[path]; ok {
			if !sameState(before, c.before) {
				return fmt.Errorf("activation entry changed during installation: %s", path)
			}
			c.before = before
		}
		changes = append(changes, c)
		return nil
	}
	if d != nil {
		r, err := b.readReceipt(v)
		if err != nil {
			return err
		}
		data, _ := json.Marshal(d)
		p := filepath.Join(b.root, "versions", v, dependencyFile)
		if err = add(p, regularState(data)); err != nil {
			return err
		}
		r.Files[dependencyFile] = contentEntry(data, 0600)
		data, _ = json.Marshal(r)
		if err = add(filepath.Join(b.root, "versions", v, receiptName), regularState(data)); err != nil {
			return err
		}
	}
	current, err := b.current()
	if err != nil {
		return err
	}
	state, _ := json.Marshal(destination{1, "rea", b.base})
	if err = add(b.state, regularState(state)); err != nil {
		return err
	}
	entryPath := filepath.Join(b.root, "bin/rea")
	c, err := planned(entryPath, linkState("../current/bin/rea"))
	if err != nil {
		return err
	}
	if c.before.Exists && !sameState(c.before, c.after) {
		return fmt.Errorf("foreign launcher entry preserved")
	}
	if before := baseline[entryPath]; !sameState(before, c.before) {
		return fmt.Errorf("launcher changed during installation")
	}
	changes = append(changes, c)
	if current != "" {
		if err = b.verifyOwned(ctx, current); err != nil {
			return err
		}
	}
	if err = add(filepath.Join(b.root, "current"), linkState("versions/"+v)); err != nil {
		return err
	}
	undo, err := applyChanges(ctx, changes)
	if err != nil {
		return err
	}
	if err = b.verifyOwned(ctx, v); err != nil {
		if e := undo(); e != nil {
			return fmt.Errorf("%v; activation rollback: %w", err, e)
		}
		return err
	}
	b.log("Selected REA " + v + ": " + entryPath + ". User PATH/agent integration was not enabled automatically.")
	return nil
}
func (b *backend) uninstall(ctx context.Context) error {
	if err := b.preflight(ctx); err != nil {
		return err
	}
	vs, err := b.versions()
	if err != nil {
		return err
	}
	if len(vs) == 0 {
		return fmt.Errorf("no native REA versions")
	}
	lock := filepath.Join(b.root, ".atm-native-lock")
	if err = os.Mkdir(lock, 0700); err != nil {
		return err
	}
	defer os.Remove(lock)
	changes, err := b.integrationChanges(true)
	if err != nil {
		return err
	}
	for _, name := range []string{filepath.Join(b.root, "current"), filepath.Join(b.root, "bin/rea")} {
		c, e := planned(name, fileState{})
		if e != nil {
			return e
		}
		changes = append(changes, c)
	}
	// Verify every prefix before disabling selection. Delete only recorded entries, deepest first.
	for _, v := range vs {
		if err = b.verifyOwned(ctx, v); err != nil {
			return err
		}
	}
	undo, err := applyChanges(ctx, changes)
	if err != nil {
		return err
	}
	deleted := false
	for _, v := range vs {
		if err = ctx.Err(); err == nil {
			err = b.verifyOwned(ctx, v)
		}
		if err != nil {
			if !deleted {
				if e := undo(); e != nil {
					return fmt.Errorf("%v; rollback: %w", err, e)
				}
			}
			return err
		}
		deleted = true
		if err = b.deleteVersion(ctx, v); err != nil {
			return fmt.Errorf("partial removal; state retained for inspection: %w", err)
		}
	}
	c, err := planned(b.state, fileState{})
	if err != nil {
		return err
	}
	if err = replace(c); err != nil {
		return err
	}
	b.log("Removed native-owned REA only. Agent registrations, skills, captures, Evidence, caches, external/legacy REA and prerequisites remain untouched.")
	return nil
}
func (b *backend) deleteVersion(ctx context.Context, v string) error {
	rec, err := b.readReceipt(v)
	if err != nil {
		return err
	}
	dir := filepath.Join(b.root, "versions", v)
	r, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer r.Close()
	names := make([]string, 0, len(rec.Files))
	for name := range rec.Files {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		a, c := strings.Count(names[i], "/"), strings.Count(names[j], "/")
		if a != c {
			return a > c
		}
		return names[i] > names[j]
	})
	for _, name := range names {
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = r.Remove(name); err != nil {
			return err
		}
	}
	if err = r.Remove(receiptName); err != nil {
		return err
	}
	return os.Remove(dir)
}
