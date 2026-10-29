//go:build linux

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"time"

	"golang.org/x/sys/unix"
)

type hermesRecord struct {
	Schema                 int
	Plugin, Provider, Arch string
	Source                 hermesSource
}

type hermesCompletion struct {
	Schema                  int
	Version, Commit, Python string
	Evidence                hermesCompletionEvidence
}

func hermesReadRecord(path string) (hermesRecord, error) {
	var rec hermesRecord
	f, err := regular(path, 0600, 16<<20)
	if err != nil {
		return rec, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (16<<20)+1))
	if err != nil || len(data) > 16<<20 {
		return rec, fmt.Errorf("invalid Hermes source receipt size")
	}
	err = decode(data, &rec, true)
	if err == nil && (rec.Schema != 1 || rec.Plugin != "ai" || rec.Provider != hermesAgent || (rec.Arch != "amd64" && rec.Arch != "arm64") || rec.Source.Version != hermesPMVersion || rec.Source.Commit != hermesPMCommit) {
		err = fmt.Errorf("unrecognized Hermes source/PM receipt")
	}
	return rec, err
}

func hermesWriteRecord(path string, rec hermesRecord) error {
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if len(data) > 16<<20 {
		return fmt.Errorf("Hermes receipt exceeds limit")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func (b *backend) hermesInspect(ctx context.Context) ([]string, string, error) {
	root := b.root(hermesAgent)
	if err := dirs(root, false, true); os.IsNotExist(err) {
		return nil, "", nil
	} else if err != nil {
		return nil, "", err
	}
	var own ownership
	if err := readJSON(filepath.Join(root, "owner.json"), &own); err != nil {
		return nil, "", err
	}
	if own != (ownership{1, "ai", hermesAgent}) {
		return nil, "", fmt.Errorf("foreign Hermes root preserved")
	}
	es, err := entries(root, 3)
	if err != nil {
		return nil, "", err
	}
	for _, e := range es {
		if e.Name() != "owner.json" && e.Name() != "versions" && e.Name() != "current.json" {
			return nil, "", fmt.Errorf("unknown Hermes entry preserved")
		}
	}
	var active struct{ Version string }
	if err := readJSON(filepath.Join(root, "current.json"), &active); err != nil && !os.IsNotExist(err) {
		return nil, "", err
	}
	es, err = entries(filepath.Join(root, "versions"), 128)
	if err != nil {
		return nil, "", err
	}
	var versions []string
	found := active.Version == ""
	for _, e := range es {
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
		if !e.IsDir() || !versionRE.MatchString(e.Name()) {
			return nil, "", fmt.Errorf("unknown Hermes version entry preserved")
		}
		dir := filepath.Join(root, "versions", e.Name())
		rec, err := hermesReadRecord(filepath.Join(dir, "source.json"))
		if err != nil {
			return nil, "", err
		}
		if rec.Source.Version != e.Name() || rec.Arch != b.arch {
			return nil, "", fmt.Errorf("Hermes receipt identity mismatch")
		}
		var complete hermesCompletion
		if err := readJSON(filepath.Join(dir, "complete.json"), &complete); os.IsNotExist(err) {
			if e.Name() == active.Version {
				return nil, "", fmt.Errorf("active Hermes version has no PM completion")
			}
			versions = append(versions, e.Name())
			continue
		} else if err != nil {
			return nil, "", err
		}
		if complete.Schema != 1 || complete.Version != e.Name() || complete.Commit != rec.Source.Commit {
			return nil, "", fmt.Errorf("invalid Hermes PM completion")
		}
		if err := verifyHermesTrackedSource(ctx, rec.Source, filepath.Join(dir, "source")); err != nil {
			return nil, "", err
		}
		if err := hermesOwnedExecutable(filepath.Join(dir, "python"), complete.Python); err != nil {
			return nil, "", err
		}
		if err := verifyHermesCompletion(ctx, dir, complete.Evidence); err != nil {
			return nil, "", err
		}
		if err := verifyHermesGeneratedSource(ctx, rec.Source, dir); err != nil {
			return nil, "", err
		}
		versions = append(versions, e.Name())
		found = found || active.Version == e.Name()
	}
	if !found {
		return nil, "", fmt.Errorf("active Hermes version missing")
	}
	sort.Strings(versions)
	return versions, active.Version, nil
}

func (b *backend) hermesStatus(ctx context.Context) error {
	vs, active, err := b.hermesInspect(ctx)
	if err != nil {
		return err
	}
	b.log(fmt.Sprintf(label("hermes_status"), len(vs), active))
	for _, v := range vs {
		path := filepath.Join(b.root(hermesAgent), "versions", v)
		if _, err := os.Lstat(filepath.Join(path, "complete.json")); os.IsNotExist(err) {
			b.log("Hermes Agent " + v + ": incomplete attempt retained at " + path + "; inspect manually; no automatic retry/removal")
		}
	}
	return ctx.Err()
}

func (b *backend) hermesMenu(ctx context.Context) ([]choice, error) {
	b.log(providers[hermesAgent].docs)
	b.log(label("hermes_scope"))
	b.log(label("hermes_consent"))
	b.log("Private root: " + b.root(hermesAgent))
	if err := b.hermesStatus(ctx); err != nil {
		return nil, err
	}
	base := []string{"--provider", hermesAgent, "--trust-download", "--accept-dependency-execution"}
	choices := []choice{
		{ID: "install", Label: label("hermes_install"), Operation: "install", Arguments: base},
		{ID: "custom", Label: label("hermes_custom"), Operation: "install", Arguments: append(append([]string{}, base...), "--version"), Prompt: label("version_prompt")},
		{ID: "update", Label: label("hermes_update"), Operation: "update", Arguments: base},
	}
	// Status permits incomplete attempts, but selection never does. A retained
	// lock also hides selection, including selection of the current version.
	if _, err := os.Lstat(b.root(hermesAgent) + ".lock"); os.IsNotExist(err) {
		var complete hermesCompletion
		if err := readJSON(filepath.Join(b.root(hermesAgent), "versions", hermesPMVersion, "complete.json"), &complete); err == nil {
			choices = append(choices, choice{ID: "use", Label: label("hermes_use"), Operation: "use", Arguments: []string{"--provider", hermesAgent, "--version", hermesPMVersion}})
		}
	}
	return append(choices, choice{ID: "back", Label: label("back"), Operation: "menu"}), ctx.Err()
}

func (b *backend) installHermesAgent(ctx context.Context, o options) (err error) {
	if err := b.hermesPrivilegePolicy(); err != nil {
		return err
	}
	_, current, err := b.hermesInspect(ctx)
	if err != nil {
		return err
	}
	if b.req.Operation == "update" && current == "" {
		return fmt.Errorf("no completed owned Hermes version to update")
	}
	root := b.root(hermesAgent)
	if err := dirs(filepath.Dir(root), true, false); err != nil {
		return err
	}
	lock := root + ".lock"
	if err := os.Mkdir(lock, 0700); err != nil {
		return fmt.Errorf("Hermes locked/interrupted; inspect %s: %w", lock, err)
	}
	retain := false
	defer func() {
		if !retain {
			_ = os.Remove(lock)
		} else if err != nil {
			err = fmt.Errorf("%w; partial Hermes attempt and lock retained at %s; inspect manually before retry, no automatic rollback", err, lock)
		}
	}()
	_, lockedCurrent, err := b.hermesInspect(ctx)
	if err != nil {
		return err
	}
	if lockedCurrent != current {
		return fmt.Errorf("Hermes selection changed")
	}
	source, err := b.resolveHermesSource(ctx, o.version)
	if err != nil {
		return err
	}
	if source.Version != hermesPMVersion || source.Commit != hermesPMCommit {
		return fmt.Errorf("Hermes PM contract not reviewed for this release; supported exact version is %s", hermesPMVersion)
	}
	if current != "" && source.Version != current && !newer(source.Version, current) {
		return fmt.Errorf("Hermes preview refuses downgrade")
	}
	dir := filepath.Join(root, "versions", source.Version)
	if _, err := os.Lstat(dir); err == nil {
		rec, err := hermesReadRecord(filepath.Join(dir, "source.json"))
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(rec.Source, source) {
			return fmt.Errorf("existing Hermes source metadata changed; preserved")
		}
		if current == source.Version {
			b.log(label("hermes_unchanged"))
			return nil
		}
		return fmt.Errorf("Hermes version already exists without active completion; preserved for manual inspection")
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := b.initialize(hermesAgent); err != nil {
		return err
	}
	currentPath := filepath.Join(root, "current.json")
	beforeCurrent, err := pathSnapshot(currentPath, false)
	if err != nil {
		return err
	}
	rootInfo, err := os.Stat(root)
	if err != nil {
		return err
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		return err
	}
	retain = true
	if err := hermesWriteRecord(filepath.Join(dir, "source.json"), hermesRecord{1, "ai", hermesAgent, b.arch, source}); err != nil {
		return err
	}
	for _, sub := range []string{"source", "home", "tmp"} {
		if err := os.Mkdir(filepath.Join(dir, sub), 0700); err != nil {
			return err
		}
	}
	if err := hermesHostTools(dir); err != nil {
		return err
	}
	sourcePath := filepath.Join(dir, "source")
	if err := b.downloadHermesSource(ctx, source, sourcePath); err != nil {
		return err
	}
	pins, err := readHermesPins(sourcePath, b.arch)
	if err != nil {
		return err
	}
	uv, err := b.hermesBootstrapUV(ctx, dir, pins)
	if err != nil {
		return err
	}
	_, uvBefore, err := hermesCompletionRead(ctx, dir, uv, 256<<20, true, false)
	if err != nil {
		return err
	}
	if err := b.boundary("hermes-before-bootstrap"); err != nil {
		return err
	}
	if err := verifyHermesSource(ctx, source, sourcePath); err != nil {
		return err
	}
	if err := b.hermesVerifyBootstrap(ctx, dir, pins, uvBefore); err != nil {
		return err
	}
	b.log(label("hermes_running"))
	python, err := b.hermesPreparePython(ctx, dir, uv, pins, uvBefore)
	if err != nil {
		return err
	}
	if err := verifyHermesSource(ctx, source, sourcePath); err != nil {
		return err
	}
	env := hermesEnvironment(dir)
	startedAt := time.Now().UTC()
	if _, err := b.hermesRun(ctx, python, []string{"-s", "-B", "-m", "pm.cli", "install", "--without", "agent-browser", "--without", "cua-driver"}, sourcePath, env); err != nil {
		return err
	}
	if _, err := b.hermesRun(ctx, python, []string{"-s", "-B", "-m", "pm.cli", "doctor"}, sourcePath, env); err != nil {
		return err
	}
	evidence, err := b.hermesCompletePM(ctx, dir, python, startedAt)
	if err != nil {
		return err
	}
	if err := verifyHermesTrackedSource(ctx, source, sourcePath); err != nil {
		return err
	}
	if err := b.boundary("hermes-before-complete"); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := dirs(root, false, true); err != nil {
		return err
	}
	nowRoot, err := os.Stat(root)
	if err != nil || !os.SameFile(rootInfo, nowRoot) {
		return fmt.Errorf("Hermes root changed before completion")
	}
	var own ownership
	if err := readJSON(filepath.Join(root, "owner.json"), &own); err != nil {
		return err
	}
	if own != (ownership{1, "ai", hermesAgent}) {
		return fmt.Errorf("Hermes ownership changed before completion")
	}
	recorded, err := hermesReadRecord(filepath.Join(dir, "source.json"))
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(recorded, hermesRecord{1, "ai", hermesAgent, b.arch, source}) {
		return fmt.Errorf("Hermes source receipt changed before completion")
	}
	if err := verifyHermesTrackedSource(ctx, source, sourcePath); err != nil {
		return err
	}
	if err := verifyHermesCompletion(ctx, dir, evidence); err != nil {
		return err
	}
	if err := verifyHermesGeneratedSource(ctx, source, dir); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(dir, "complete.json"), hermesCompletion{1, source.Version, source.Commit, python, evidence}); err != nil {
		return err
	}
	// The environment already lives at its final path. Only this small pointer is
	// published; Python environments and the source tree are never renamed.
	data, err := json.Marshal(struct{ Version string }{source.Version})
	if err != nil {
		return err
	}
	if err := b.hermesPublishSelection(ctx, currentPath, beforeCurrent, data); err != nil {
		return err
	}
	if _, selected, err := b.hermesInspect(ctx); err != nil {
		return err
	} else if selected != source.Version {
		return fmt.Errorf("Hermes selection changed after publication")
	}
	retain = false
	b.log(label("hermes_complete"))
	b.log("Private source: " + sourcePath + "; private HOME: " + filepath.Join(dir, "home"))
	return nil
}

func (b *backend) hermesPublishSelection(ctx context.Context, target string, before pathState, data []byte, verify ...func() error) error {
	stageParent := filepath.Dir(target)
	if len(verify) != 0 {
		// Keep temporary files outside the inspected owned inventory.
		stageParent += ".lock"
	}
	stage, err := os.MkdirTemp(stageParent, ".hermes-select-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	tmp := filepath.Join(stage, "current.json")
	var value struct{ Version string }
	if err := decode(data, &value, true); err != nil {
		return err
	}
	if err := writeJSON(tmp, value); err != nil {
		return err
	}
	staged, err := pathSnapshot(tmp, false)
	if err != nil {
		return err
	}
	if err := b.boundary("hermes-before-select"); err != nil {
		return err
	}
	for _, check := range verify {
		if err := check(); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	now, err := pathSnapshot(target, false)
	if err != nil {
		return err
	}
	if !pathUnchanged(now, before) {
		return fmt.Errorf("Hermes selection changed before publication")
	}
	last, err := pathSnapshot(tmp, false)
	if err != nil {
		return err
	}
	if !pathUnchanged(last, staged) {
		return fmt.Errorf("Hermes staged selection changed")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !before.Exists {
		return unix.Renameat2(unix.AT_FDCWD, tmp, unix.AT_FDCWD, target, unix.RENAME_NOREPLACE)
	}
	return os.Rename(tmp, target)
}
