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

// Refresh changes only the independently owned bin tree. Profile snapshots are
// authority checks, never rollback data; no profile or selection is rewritten.
func (b *backend) refreshHermesLauncher(ctx context.Context) (result error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	root := b.root(hermesAgent)
	if err := dirs(root, false, true); err != nil {
		return err
	}
	home, err := integrationHome()
	if err != nil {
		return err
	}
	locks := []string{root + ".lock", filepath.Join(home, ".atm-ai-path.lock")}
	ownedLocks := map[string]os.FileInfo{}
	evidence := map[string]pathState{}
	retain := false
	checkLocks := func() error {
		for path, before := range ownedLocks {
			if err := dirs(path, false, true); err != nil {
				return err
			}
			now, err := os.Lstat(path)
			if err != nil || !os.SameFile(before, now) {
				return fmt.Errorf("Hermes refresh lock changed: %s", path)
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
					result = errors.Join(result, fmt.Errorf("Hermes refresh journal changed: %s", path))
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
			result = errors.Join(result, fmt.Errorf("Hermes launcher refresh state/locks retained at %s and %s; inspect manually, no automatic recovery", locks[0], locks[1]))
		}
	}()
	for _, path := range locks {
		if err := os.Mkdir(path, 0700); err != nil {
			return fmt.Errorf("Hermes refresh locked/interrupted; inspect %s: %w", path, err)
		}
		info, err := os.Lstat(path)
		if err != nil {
			retain = true
			return err
		}
		ownedLocks[path] = info
	}
	homeInfo, err := os.Lstat(home)
	if err != nil {
		return err
	}
	_, current, err := b.hermesInspect(ctx)
	if err != nil {
		return err
	}
	if current != hermesPMVersion {
		return fmt.Errorf("Hermes refresh requires the selected completed reviewed version")
	}
	old, err := b.inspectHermesBridge(ctx)
	if err != nil {
		return err
	}
	if old == nil {
		return fmt.Errorf("Hermes refresh requires an existing owned native bridge; enroll separately")
	}
	pinned, err := hermesSelectionSnapshot(ctx, root)
	if err != nil {
		return err
	}
	paths := []string{filepath.Join(root, "current.json"), filepath.Join(root, "path.json")}
	for _, name := range profileNames {
		paths = append(paths, filepath.Join(home, name))
	}
	unchanged := map[string]pathState{}
	for _, path := range paths {
		s, err := pathSnapshot(path, false)
		if err != nil {
			return err
		}
		unchanged[path] = s
	}
	checkAuthority := func() error {
		if err := checkLocks(); err != nil {
			return err
		}
		nowHome, err := integrationHome()
		if err != nil || nowHome != home {
			return fmt.Errorf("Hermes refresh HOME authority changed: %v", err)
		}
		now, err := os.Lstat(home)
		if err != nil || !os.SameFile(homeInfo, now) {
			return fmt.Errorf("Hermes refresh HOME replaced")
		}
		if err := pinned.verify(ctx, root); err != nil {
			return err
		}
		for path, before := range unchanged {
			now, err := pathSnapshot(path, false)
			if err != nil || !pathUnchanged(now, before) {
				return fmt.Errorf("Hermes refresh profile/selection authority changed: %s: %v", path, err)
			}
		}
		_, active, err := b.hermesInspect(ctx)
		if err != nil {
			return err
		}
		if active != current {
			return fmt.Errorf("Hermes refresh selection changed")
		}
		return pinned.verify(ctx, root)
	}
	checkBridge := func(expected *hermesBridgeState) error {
		if err := checkAuthority(); err != nil {
			return err
		}
		now, err := b.inspectHermesBridge(ctx)
		if err != nil || !sameHermesBridge(expected, now) {
			return fmt.Errorf("Hermes refresh original bridge authority changed: %v", err)
		}
		return ctx.Err()
	}
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
	// Small manual-inspection evidence only; the multi-MiB payload stays on disk.
	retain = true
	if err := record("hermes-refresh.json", struct {
		Schema  int
		Root    string
		Old     hermesCompletionFile
		Receipt pathState
	}{1, root, old.payload.hash, old.receipt}); err != nil {
		return err
	}
	if err := b.boundary("hermes-refresh-preflight"); err != nil {
		return err
	}
	if err := checkBridge(old); err != nil {
		return err
	}
	refreshed, err := b.copyHermesBridge(ctx, old, func() error { return checkBridge(old) }, func(stage string, staged *hermesBridgeState, checkCopy func() error) (*hermesBridgeState, error) {
		if err := record("hermes-refresh-stage.json", struct {
			Schema int
			Stage  string
			New    hermesCompletionFile
		}{1, stage, staged.payload.hash}); err != nil {
			return nil, err
		}
		// The shared copier has just rechecked source and stage. Recheck original
		// authority too; never adopt a coherent replacement as the old baseline.
		if err := b.boundary("hermes-refresh-before-exchange"); err != nil {
			return nil, err
		}
		if err := checkBridge(old); err != nil {
			return nil, err
		}
		if err := checkCopy(); err != nil {
			return nil, err
		}
		if err := unix.Renameat2(unix.AT_FDCWD, stage, unix.AT_FDCWD, filepath.Join(root, "bin"), unix.RENAME_EXCHANGE); err != nil {
			return nil, err
		}
		if err := b.boundary("hermes-refresh-after-exchange"); err != nil {
			return nil, err
		}
		checkNew := func() error { return checkBridge(staged) }
		if err := checkNew(); err != nil {
			return nil, err
		}
		if err := b.cleanHermesRetiredBridge(ctx, stage, old, checkNew); err != nil {
			return nil, err
		}
		return staged, nil
	})
	if err != nil {
		return err
	}
	if err := checkBridge(refreshed); err != nil {
		return err
	}
	retain = false
	if sameHermesBridge(old, refreshed) {
		b.log(label("hermes_refresh_unchanged"))
	} else {
		b.log(label("hermes_refreshed"))
	}
	return nil
}

// No recursive cleanup. Verify the exact remaining owned inventory before each
// unlink, and anchor file deletion to the already opened old directory.
func (b *backend) cleanHermesRetiredBridge(ctx context.Context, stage string, old *hermesBridgeState, check func() error) error {
	fd, err := unix.Open(stage, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	dir := os.NewFile(uintptr(fd), stage)
	defer dir.Close()
	info, err := dir.Stat()
	if err != nil || !os.SameFile(old.directory, info) {
		return fmt.Errorf("Hermes retired bridge directory replaced")
	}
	remaining := map[string]bool{"hermes": true, "launcher.json": true}
	verify := func() error {
		if err := check(); err != nil {
			return err
		}
		if err := dirs(stage, false, true); err != nil {
			return err
		}
		now, err := os.Lstat(stage)
		if err != nil || !os.SameFile(old.directory, now) {
			return fmt.Errorf("Hermes retired bridge changed during cleanup")
		}
		es, err := entries(stage, 2)
		if err != nil || len(es) != len(remaining) {
			return fmt.Errorf("Hermes retired bridge inventory changed: %v", err)
		}
		for _, entry := range es {
			if !remaining[entry.Name()] {
				return fmt.Errorf("Hermes retired bridge foreign entry preserved")
			}
		}
		if remaining["hermes"] {
			p := filepath.Join(stage, "hermes")
			now, err := os.Lstat(p)
			if err != nil || !os.SameFile(old.payload.info, now) {
				return fmt.Errorf("Hermes retired payload identity changed")
			}
			_, hash, err := hermesCompletionRead(ctx, stage, p, hermesBridgeLimit, true, false)
			if err != nil || hash != old.payload.hash {
				return fmt.Errorf("Hermes retired payload changed: %v", err)
			}
		}
		if remaining["launcher.json"] {
			now, err := pathSnapshot(filepath.Join(stage, "launcher.json"), false)
			if err != nil || !pathUnchanged(now, old.receipt) {
				return fmt.Errorf("Hermes retired receipt changed: %v", err)
			}
		}
		return ctx.Err()
	}
	for _, name := range []string{"hermes", "launcher.json"} {
		if err := b.boundary("hermes-refresh-before-cleanup-" + name); err != nil {
			return err
		}
		if err := verify(); err != nil {
			return err
		}
		if err := unix.Unlinkat(fd, name, 0); err != nil {
			return err
		}
		delete(remaining, name)
	}
	if err := b.boundary("hermes-refresh-before-cleanup-directory"); err != nil {
		return err
	}
	if err := verify(); err != nil {
		return err
	}
	return os.Remove(stage)
}
