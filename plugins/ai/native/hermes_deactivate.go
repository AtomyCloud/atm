//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// Deactivation preserves the entire preparation at its original paths. Only
// owned PATH blocks, the independent native bridge, and selection are removed.
func (b *backend) deactivateHermesAgent(ctx context.Context) (result error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	root := b.root(hermesAgent)
	if err := dirs(root, false, true); err != nil {
		return fmt.Errorf("Hermes deactivation requires an existing completed preparation: %w", err)
	}
	home, err := integrationHome()
	if err != nil {
		return err
	}
	locks := []string{root + ".lock", filepath.Join(home, ".atm-ai-path.lock")}
	ownedLocks := map[string]os.FileInfo{}
	evidence := map[string]pathState{}
	retiredBridge := ""
	retain, phase := false, "preflight"
	checkLocks := func() error {
		for path, before := range ownedLocks {
			if err := dirs(path, false, true); err != nil {
				return err
			}
			now, err := os.Lstat(path)
			if err != nil || !os.SameFile(before, now) {
				return fmt.Errorf("Hermes deactivation lock changed: %s", path)
			}
		}
		return ctx.Err()
	}
	defer func() {
		if err := checkLocks(); err != nil {
			result = errors.Join(result, err)
			retain = true
		}
		if !retain {
			for path, before := range evidence {
				now, err := pathSnapshot(path, false)
				if err != nil || !pathUnchanged(now, before) {
					result = errors.Join(result, fmt.Errorf("Hermes deactivation evidence changed: %s", path))
					retain = true
					break
				}
			}
		}
		if !retain {
			for path := range evidence {
				if err := os.Remove(path); err != nil {
					result = errors.Join(result, err)
					retain = true
					break
				}
			}
		}
		if !retain {
			for i := len(locks) - 1; i >= 0; i-- {
				if _, ours := ownedLocks[locks[i]]; ours {
					if err := os.Remove(locks[i]); err != nil {
						result = errors.Join(result, err)
						retain = true
						break
					}
				}
			}
		}
		if retain {
			result = errors.Join(result, fmt.Errorf("Hermes deactivation phase %s; state/locks retained at %s and %s; preparation/data preserved, activation may be partially removed; inspect manually, no automatic rollback", phase, locks[0], locks[1]))
		}
	}()
	for _, path := range locks {
		if err := os.Mkdir(path, 0700); err != nil {
			return fmt.Errorf("Hermes deactivation locked/interrupted; inspect %s: %w", path, err)
		}
		info, err := os.Lstat(path)
		if err != nil {
			retain = true
			return err
		}
		ownedLocks[path] = info
	}
	pinned, err := hermesSelectionSnapshot(ctx, root)
	if err != nil {
		return err
	}
	versions, current, err := b.hermesInspect(ctx)
	if err != nil {
		return err
	}
	if len(versions) != 1 || versions[0] != hermesPMVersion || (current != "" && current != hermesPMVersion) {
		return fmt.Errorf("Hermes deactivation requires the completed reviewed preparation")
	}
	pointerPath := filepath.Join(root, "current.json")
	pointer, err := pathSnapshot(pointerPath, false)
	if err != nil {
		return err
	}
	if pointer.Exists != (current != "") {
		return fmt.Errorf("Hermes deactivation selection is not recognized")
	}
	bridge, err := b.inspectHermesBridge(ctx)
	if err != nil {
		return err
	}
	if current == "" && bridge != nil {
		return fmt.Errorf("Hermes unselected bridge retained; inspect interrupted activation manually")
	}
	rec, err := b.inspectIntegrationContext(ctx, hermesAgent, current)
	if err != nil {
		return err
	}
	changes, err := b.integrationChanges(hermesAgent, home, rec, true)
	if err != nil {
		return err
	}
	paths := map[string]pathState{}
	for _, name := range append(append([]string{}, profileNames...), "") {
		path := filepath.Join(home, name)
		if name == "" {
			path = filepath.Join(root, "path.json")
		}
		state, err := pathSnapshot(path, false)
		if err != nil {
			return err
		}
		paths[path] = state
	}
	// Preserve identity of HOME and preparation evidence ancestors, including
	// private HOME/PM directories. No user data contents are read or journaled.
	directories := map[string]os.FileInfo{}
	versionDir := filepath.Join(root, "versions", hermesPMVersion)
	var complete hermesCompletion
	if err := readJSON(filepath.Join(versionDir, "complete.json"), &complete); err != nil {
		return err
	}
	dirPaths := []string{home, filepath.Join(versionDir, "home"), filepath.Join(versionDir, "home", ".hermes")}
	for _, file := range complete.Evidence.Files {
		for path := filepath.Dir(filepath.Join(versionDir, filepath.FromSlash(file.Path))); path != versionDir; path = filepath.Dir(path) {
			// hermesInspect already validated these relative evidence paths.
			if path == filepath.Dir(path) {
				return fmt.Errorf("Hermes completion directory escapes preparation")
			}
			dirPaths = append(dirPaths, path)
		}
	}
	for _, path := range dirPaths {
		if err := dirs(path, false, false); err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		directories[path] = info
	}
	checkAuthority := func() error {
		if err := checkLocks(); err != nil {
			return err
		}
		for path, before := range evidence {
			now, err := pathSnapshot(path, false)
			if err != nil || !pathUnchanged(now, before) {
				return fmt.Errorf("Hermes deactivation evidence changed: %s", path)
			}
		}
		for _, lock := range locks {
			es, err := entries(lock, 8)
			if err != nil {
				return err
			}
			for _, entry := range es {
				path := filepath.Join(lock, entry.Name())
				_, recorded := evidence[path]
				profileWrite := lock == locks[1] && phase == "detaching PATH" && (entry.Name() == "hermes-bridge.json" || entry.Name() == "journal.json")
				if !recorded && path != retiredBridge && !profileWrite {
					return fmt.Errorf("Hermes deactivation unknown lock entry preserved: %s", path)
				}
			}
		}
		nowHome, err := integrationHome()
		if err != nil || nowHome != home {
			return fmt.Errorf("Hermes deactivation HOME changed: %v", err)
		}
		for path, before := range directories {
			if err := dirs(path, false, false); err != nil {
				return err
			}
			now, err := os.Lstat(path)
			if err != nil || !os.SameFile(before, now) {
				return fmt.Errorf("Hermes deactivation directory replaced: %s", path)
			}
		}
		if err := pinned.verify(ctx, root); err != nil {
			return err
		}
		now, err := pathSnapshot(pointerPath, false)
		if err != nil || !pathUnchanged(now, pointer) {
			return fmt.Errorf("Hermes deactivation original selection changed: %v", err)
		}
		nowBridge, err := b.inspectHermesBridge(ctx)
		if err != nil || !sameHermesBridge(bridge, nowBridge) {
			return fmt.Errorf("Hermes deactivation original bridge changed: %v", err)
		}
		vs, active, err := b.hermesInspect(ctx)
		if err != nil {
			return err
		}
		if len(vs) != 1 || vs[0] != hermesPMVersion || active != current {
			return fmt.Errorf("Hermes deactivation inventory/selection changed")
		}
		return pinned.verify(ctx, root)
	}
	check := func() error {
		if err := checkAuthority(); err != nil {
			return err
		}
		for path, before := range paths {
			now, err := pathSnapshot(path, false)
			if err != nil || !pathUnchanged(now, before) {
				return fmt.Errorf("Hermes deactivation profile authority changed: %s: %v", path, err)
			}
		}
		return ctx.Err()
	}
	if err := check(); err != nil {
		return err
	}
	if current == "" {
		retain = true
		if err := b.boundary("hermes-deactivate-before-noop"); err != nil {
			return err
		}
		if err := check(); err != nil {
			return err
		}
		retain = false
		b.log(label("hermes_deactivate_unchanged"))
		return nil
	}
	retain = true
	record := func(name string, value any) error {
		path := filepath.Join(locks[0], name)
		if err := writeJSON(path, value); err != nil {
			return err
		}
		s, err := pathSnapshot(path, false)
		if err == nil {
			evidence[path] = s
		}
		return err
	}
	bridgeReceipt := pathState{}
	if bridge != nil {
		bridgeReceipt = bridge.receipt
	}
	if err := record("hermes-deactivate.json", struct {
		Schema        int
		Root          string
		Selection     pathState
		BridgeReceipt pathState
	}{1, root, pointer, bridgeReceipt}); err != nil {
		return err
	}
	if err := b.boundary("hermes-deactivate-preflight"); err != nil {
		return err
	}
	if err := check(); err != nil {
		return err
	}
	phase = "detaching PATH"
	if err := b.changeHermesIntegrationHeld(ctx, true, home, locks[1], changes, checkAuthority, false); err != nil {
		return err
	}
	// Only successful writes from the original, precomputed detach plan can
	// establish new profile identities. Unchanged files retain original identity.
	for _, c := range changes {
		now, err := pathSnapshot(c.Path, false)
		if err != nil || !samePathState(now, c.After) || (samePathState(c.Before, c.After) && !pathUnchanged(now, c.Before)) {
			return fmt.Errorf("Hermes detached profile changed: %s: %v", c.Path, err)
		}
		paths[c.Path] = now
	}
	phase = "PATH detached"
	if err := record("hermes-deactivate-detached.json", struct{ Schema int }{1}); err != nil {
		return err
	}
	if err := b.boundary("hermes-deactivate-after-detach"); err != nil {
		return err
	}
	if err := check(); err != nil {
		return err
	}
	if bridge != nil {
		retired := filepath.Join(locks[0], "hermes-retired-bin")
		if err := b.boundary("hermes-deactivate-before-retire"); err != nil {
			return err
		}
		if err := check(); err != nil {
			return err
		}
		if err := unix.Renameat2(unix.AT_FDCWD, filepath.Join(root, "bin"), unix.AT_FDCWD, retired, unix.RENAME_NOREPLACE); err != nil {
			return err
		}
		old := bridge
		bridge = nil
		retiredBridge = retired
		phase = "bridge retired; cleanup may be partial"
		if err := b.boundary("hermes-deactivate-after-retire"); err != nil {
			return err
		}
		if err := b.cleanHermesRetiredBridge(ctx, retired, old, check); err != nil {
			return err
		}
		retiredBridge = ""
	}
	phase = "PATH and bridge removed; selection retained"
	if err := record("hermes-deactivate-bridge-removed.json", struct{ Schema int }{1}); err != nil {
		return err
	}
	if err := b.boundary("hermes-deactivate-before-deselect"); err != nil {
		return err
	}
	if err := check(); err != nil {
		return err
	}
	if err := replacePath(pathChange{Path: pointerPath, Before: pointer}); err != nil {
		return err
	}
	pointer, current = pathState{}, ""
	phase = "selection removed"
	if err := b.boundary("hermes-deactivate-after-deselect"); err != nil {
		return err
	}
	if err := check(); err != nil {
		return err
	}
	retain = false
	b.log(fmt.Sprintf(label("hermes_deactivated"), versionDir))
	return nil
}
