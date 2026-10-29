//go:build linux

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type hermesSelectionFile struct {
	info os.FileInfo
	hash hermesCompletionFile
}

// Pin the existing receipts, not freshly generated evidence. A coherent rewrite
// of source plus completion must not become a new baseline during selection.
type hermesSelectionState struct {
	dirs  map[string]os.FileInfo
	files map[string]hermesSelectionFile
}

func hermesSelectionSnapshot(ctx context.Context, root string) (hermesSelectionState, error) {
	s := hermesSelectionState{dirs: map[string]os.FileInfo{}, files: map[string]hermesSelectionFile{}}
	dir := filepath.Join(root, "versions", hermesPMVersion)
	for _, path := range []string{root, root + ".lock", filepath.Join(root, "versions"), dir, filepath.Join(dir, "source")} {
		if err := dirs(path, false, true); err != nil {
			return s, err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return s, err
		}
		s.dirs[path] = info
	}
	for _, path := range []string{filepath.Join(root, "owner.json"), filepath.Join(dir, "source.json"), filepath.Join(dir, "complete.json")} {
		info, err := os.Lstat(path)
		if err != nil {
			return s, err
		}
		_, hash, err := hermesCompletionRead(ctx, root, path, hermesCompletionMetadataLimit, false, false)
		if err != nil {
			return s, err
		}
		last, err := os.Lstat(path)
		if err != nil || !os.SameFile(info, last) {
			return s, fmt.Errorf("Hermes selection receipt replaced while reading: %s", path)
		}
		s.files[path] = hermesSelectionFile{info, hash}
	}
	return s, ctx.Err()
}

func (s hermesSelectionState) verify(ctx context.Context, root string) error {
	now, err := hermesSelectionSnapshot(ctx, root)
	if err != nil {
		return err
	}
	for path, info := range s.dirs {
		if !os.SameFile(info, now.dirs[path]) {
			return fmt.Errorf("Hermes selection directory replaced: %s", path)
		}
	}
	for path, before := range s.files {
		after := now.files[path]
		if before.hash != after.hash || !os.SameFile(before.info, after.info) {
			return fmt.Errorf("Hermes selection receipt changed: %s", path)
		}
	}
	return ctx.Err()
}

func (b *backend) useHermesAgent(ctx context.Context, version string) (err error) {
	if version != hermesPMVersion {
		return fmt.Errorf("Hermes selection requires reviewed exact --version %s", hermesPMVersion)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	root := b.root(hermesAgent)
	if err := dirs(root, false, true); err != nil {
		return err
	}
	lock := root + ".lock"
	if err := os.Mkdir(lock, 0700); err != nil {
		return fmt.Errorf("Hermes locked/interrupted; inspect %s: %w", lock, err)
	}
	lockInfo, err := os.Lstat(lock)
	if err != nil {
		return fmt.Errorf("Hermes lock identity unavailable; inspect %s: %w", lock, err)
	}
	retain := false
	defer func() {
		now, statErr := os.Lstat(lock)
		if statErr != nil || !os.SameFile(lockInfo, now) {
			retain = true
			if err == nil {
				err = fmt.Errorf("Hermes selection lock changed")
			}
		}
		if !retain {
			if removeErr := os.Remove(lock); removeErr != nil {
				retain = true
				err = fmt.Errorf("Hermes selection lock cleanup failed: %w; prior result: %v", removeErr, err)
			}
		}
		if retain {
			err = fmt.Errorf("%w; Hermes selection state and lock retained at %s; selection may have changed; inspect manually, no automatic rollback", err, lock)
		}
	}()
	currentPath := filepath.Join(root, "current.json")
	before, err := pathSnapshot(currentPath, false)
	if err != nil {
		return err
	}
	pinned, err := hermesSelectionSnapshot(ctx, root)
	if err != nil {
		return err
	}
	verify := func() error {
		if err := pinned.verify(ctx, root); err != nil {
			return err
		}
		versions, _, err := b.hermesInspect(ctx)
		if err != nil {
			return err
		}
		// Only one source/PM contract has been reviewed. Do not imply a
		// multi-version inventory by accepting additional preparations.
		if len(versions) != 1 || versions[0] != version {
			return fmt.Errorf("Hermes selectable inventory changed")
		}
		return pinned.verify(ctx, root)
	}
	if err := verify(); err != nil {
		return err
	}
	if before.Exists {
		var selected struct{ Version string }
		if err := decode(before.Data, &selected, true); err != nil {
			return err
		}
		if selected.Version == version {
			if err := b.boundary("hermes-before-use-noop"); err != nil {
				return err
			}
			if err := verify(); err != nil {
				return err
			}
			now, err := pathSnapshot(currentPath, false)
			if err != nil {
				return err
			}
			if !pathUnchanged(now, before) {
				return fmt.Errorf("Hermes selection changed before no-op")
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			b.log(label("hermes_unchanged"))
			return nil
		}
	}
	data, err := json.Marshal(struct{ Version string }{version})
	if err != nil {
		return err
	}
	// Preserve the previous pointer for manual inspection if publication or its
	// final verification fails. This journal is not an automatic rollback promise.
	retain = true
	journal := filepath.Join(lock, "selection.json")
	if err := writeJSON(journal, struct {
		Schema int
		Before pathState
		After  string
	}{1, before, version}); err != nil {
		return err
	}
	if err := b.hermesPublishSelection(ctx, currentPath, before, data, verify); err != nil {
		return err
	}
	if err := b.boundary("hermes-after-select"); err != nil {
		return err
	}
	if err := verify(); err != nil {
		return err
	}
	var selected struct{ Version string }
	if err := readJSON(currentPath, &selected); err != nil {
		return err
	}
	if selected.Version != version {
		return fmt.Errorf("Hermes selection changed after publication")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Remove(journal); err != nil {
		return err
	}
	retain = false
	b.log(label("hermes_selected"))
	return nil
}
