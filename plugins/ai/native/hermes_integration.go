//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"

	"github.com/AtomyCloud/atm/internal/protocol"
	"golang.org/x/sys/unix"
)

const hermesBridgeLimit = int64(128 << 20)

type hermesBridgeReceipt struct {
	Schema                       int
	Plugin, Provider, Root, Arch string
	Executable                   hermesCompletionFile
}

type hermesBridgeState struct {
	receipt   pathState
	payload   hermesSelectionFile
	directory os.FileInfo
}

// The bin directory is published as a unit. Its small receipt independently
// owns the native payload, including while PATH enrollment is detached.
func (b *backend) inspectHermesBridge(ctx context.Context) (*hermesBridgeState, error) {
	root := b.root(hermesAgent)
	bin := filepath.Join(root, "bin")
	if err := dirs(bin, false, true); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return b.hermesBridgeSnapshot(ctx, bin)
}

func (b *backend) hermesBridgeSnapshot(ctx context.Context, bin string) (*hermesBridgeState, error) {
	if err := dirs(bin, false, true); err != nil {
		return nil, err
	}
	es, err := entries(bin, 2)
	if err != nil {
		return nil, err
	}
	if len(es) != 2 {
		return nil, fmt.Errorf("unowned/changed Hermes bridge inventory preserved")
	}
	for _, entry := range es {
		if entry.Name() != "hermes" && entry.Name() != "launcher.json" {
			return nil, fmt.Errorf("unowned/changed Hermes bridge inventory preserved")
		}
	}
	directory, err := os.Lstat(bin)
	if err != nil {
		return nil, err
	}
	receipt, err := pathSnapshot(filepath.Join(bin, "launcher.json"), false)
	if err != nil {
		return nil, err
	}
	var rec hermesBridgeReceipt
	if !receipt.Exists || receipt.Mode != 0600 || decode(receipt.Data, &rec, true) != nil || rec.Schema != 1 || rec.Plugin != "ai" || rec.Provider != hermesAgent || rec.Root != b.root(hermesAgent) || rec.Arch != b.arch {
		return nil, fmt.Errorf("invalid Hermes native bridge receipt")
	}
	path := filepath.Join(bin, "hermes")
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	_, hash, err := hermesCompletionRead(ctx, bin, path, hermesBridgeLimit, true, false)
	if err != nil {
		return nil, err
	}
	if hash != rec.Executable || hash.Mode != 0755 {
		return nil, fmt.Errorf("changed Hermes native bridge preserved")
	}
	if err := validateELF(path, b.arch, false); err != nil {
		return nil, err
	}
	last, err := os.Lstat(path)
	if err != nil || !os.SameFile(info, last) {
		return nil, fmt.Errorf("Hermes native bridge replaced during inspection")
	}
	return &hermesBridgeState{receipt, hermesSelectionFile{info, hash}, directory}, nil
}

func sameHermesBridge(before, after *hermesBridgeState) bool {
	if before == nil || after == nil {
		return before == nil && after == nil
	}
	return pathUnchanged(after.receipt, before.receipt) && before.payload.hash == after.payload.hash && os.SameFile(before.payload.info, after.payload.info) && os.SameFile(before.directory, after.directory)
}

func (b *backend) prepareHermesBridge(ctx context.Context, check func() error) (*hermesBridgeState, error) {
	old, err := b.inspectHermesBridge(ctx)
	if err != nil {
		return nil, err
	}
	if old != nil {
		return nil, fmt.Errorf("Hermes native bridge appeared before staging; preserved")
	}
	return b.copyHermesBridge(ctx, nil, check, func(stage string, staged *hermesBridgeState, checkCopy func() error) (*hermesBridgeState, error) {
		if err := checkCopy(); err != nil {
			return nil, err
		}
		root := b.root(hermesAgent)
		if err := unix.Renameat2(unix.AT_FDCWD, stage, unix.AT_FDCWD, filepath.Join(root, "bin"), unix.RENAME_NOREPLACE); err != nil {
			return nil, err
		}
		now, err := b.inspectHermesBridge(ctx)
		if err != nil || !sameHermesBridge(staged, now) {
			return nil, fmt.Errorf("published native bridge changed; retained at %s: %v", root, err)
		}
		return now, nil
	})
}

// Both enrollment and explicit refresh copy the running backend with the same
// identity/hash checks. The caller alone chooses the publication operation.
func (b *backend) copyHermesBridge(ctx context.Context, old *hermesBridgeState, check func() error, publish func(string, *hermesBridgeState, func() error) (*hermesBridgeState, error)) (*hermesBridgeState, error) {
	selfPath := os.Executable
	fixture := b.hermes != nil && b.hermes.selfPath != nil
	if fixture {
		selfPath = b.hermes.selfPath
	}
	source, err := selfPath()
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(source)
	if err != nil {
		return nil, err
	}
	verifySelf := func() error {
		if !fixture {
			running, err := os.Stat("/proc/self/exe")
			if err != nil || !os.SameFile(info, running) {
				return fmt.Errorf("current native backend executable identity changed")
			}
		}
		return nil
	}
	if err := verifySelf(); err != nil {
		return nil, err
	}
	_, original, err := hermesCompletionRead(ctx, filepath.Dir(source), source, hermesBridgeLimit, true, false)
	if err != nil {
		return nil, err
	}
	checkSource := func() error {
		_, now, err := hermesCompletionRead(ctx, filepath.Dir(source), source, hermesBridgeLimit, true, false)
		if err != nil {
			return err
		}
		last, err := os.Lstat(source)
		if err != nil || original != now || !os.SameFile(info, last) {
			return fmt.Errorf("native backend changed before publication")
		}
		return verifySelf()
	}
	if old != nil && original.SHA256 == old.payload.hash.SHA256 && original.Size == old.payload.hash.Size {
		if err := b.boundary("hermes-refresh-before-noop"); err != nil {
			return nil, err
		}
		if err := check(); err != nil {
			return nil, err
		}
		if err := checkSource(); err != nil {
			return nil, err
		}
		return old, ctx.Err()
	}
	f, err := regular(source, info.Mode().Perm(), hermesBridgeLimit)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, fmt.Errorf("current native backend changed before copying")
	}
	root := b.root(hermesAgent)
	stage, err := os.MkdirTemp(root+".lock", ".hermes-bridge-")
	if err != nil {
		return nil, err
	}
	// An interrupted or changed stage is retained under the provider lock.
	// The large payload is never serialized into the profile journal.
	fail := func(err error) (*hermesBridgeState, error) {
		return nil, fmt.Errorf("native bridge staging retained at %s: %w", stage, err)
	}
	target := filepath.Join(stage, "hermes")
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fail(err)
	}
	n, err := io.Copy(out, io.LimitReader(contextReader{ctx, f}, hermesBridgeLimit+1))
	if err == nil && (n != original.Size || n > hermesBridgeLimit) {
		err = fmt.Errorf("native backend size changed during copy")
	}
	if err == nil {
		err = out.Chmod(0755)
	}
	if err == nil {
		err = out.Sync()
	}
	err = errors.Join(err, out.Close())
	if err != nil {
		return fail(err)
	}
	_, copied, err := hermesCompletionRead(ctx, stage, target, hermesBridgeLimit, true, false)
	if err != nil {
		return fail(err)
	}
	if copied.SHA256 != original.SHA256 || copied.Size != original.Size {
		return fail(fmt.Errorf("native backend snapshot changed"))
	}
	if err := writeJSON(filepath.Join(stage, "launcher.json"), hermesBridgeReceipt{1, "ai", hermesAgent, root, b.arch, copied}); err != nil {
		return fail(err)
	}
	staged, err := b.hermesBridgeSnapshot(ctx, stage)
	if err != nil {
		return fail(err)
	}
	if err := b.boundary("hermes-bridge-before-publish"); err != nil {
		return fail(err)
	}
	if err := check(); err != nil {
		return fail(err)
	}
	checkCopy := func() error {
		if err := checkSource(); err != nil {
			return err
		}
		now, err := b.hermesBridgeSnapshot(ctx, stage)
		if err != nil || !sameHermesBridge(staged, now) {
			return fmt.Errorf("native bridge stage changed: %v", err)
		}
		return ctx.Err()
	}
	if err := checkCopy(); err != nil {
		return fail(err)
	}
	published, err := publish(stage, staged, checkCopy)
	if err != nil {
		return fail(err)
	}
	return published, nil
}

func (b *backend) changeHermesIntegration(ctx context.Context, remove bool) (result error) {
	home, err := integrationHome()
	if err != nil {
		return err
	}
	lock := filepath.Join(home, ".atm-ai-path.lock")
	if err := os.Mkdir(lock, 0700); err != nil {
		return fmt.Errorf("AI profiles locked/interrupted; inspect %s: %w", lock, err)
	}
	lockInfo, err := os.Lstat(lock)
	if err != nil {
		return err
	}
	retain := false
	var evidence pathState
	evidencePath := filepath.Join(lock, "hermes-bridge.json")
	defer func() {
		now, err := os.Lstat(lock)
		if err != nil || !os.SameFile(lockInfo, now) {
			retain = true
		}
		if !retain && evidence.Exists {
			now, err := pathSnapshot(evidencePath, false)
			if err != nil || !pathUnchanged(now, evidence) {
				retain = true
			} else if os.Remove(evidencePath) != nil {
				retain = true
			}
		}
		if !retain && os.Remove(lock) != nil {
			retain = true
		}
		if retain {
			result = errors.Join(result, fmt.Errorf("Hermes PATH evidence retained at %s; inspect manually", lock))
		}
	}()
	_, current, err := b.hermesInspect(ctx)
	if err != nil {
		return err
	}
	if current != hermesPMVersion {
		return fmt.Errorf("Hermes PATH requires the selected completed reviewed version")
	}
	rec, err := b.inspectIntegrationContext(ctx, hermesAgent, current)
	if err != nil {
		return err
	}
	changes, err := b.integrationChanges(hermesAgent, home, rec, remove)
	if err != nil {
		return err
	}
	root := b.root(hermesAgent)
	pinned, err := hermesSelectionSnapshot(ctx, root)
	if err != nil {
		return err
	}
	pointer, err := pathSnapshot(filepath.Join(root, "current.json"), false)
	if err != nil {
		return err
	}
	checkSelection := func() error {
		if err := pinned.verify(ctx, root); err != nil {
			return err
		}
		_, active, err := b.hermesInspect(ctx)
		if err != nil {
			return err
		}
		now, err := pathSnapshot(filepath.Join(root, "current.json"), false)
		if err != nil || !pathUnchanged(now, pointer) || active != current {
			return fmt.Errorf("Hermes selection changed during PATH operation: %v", err)
		}
		return pinned.verify(ctx, root)
	}
	// Pin presence as well as absence before any mutation boundary. Only this
	// operation's successful staged publication may establish a new baseline.
	bridge, err := b.inspectHermesBridge(ctx)
	if err != nil {
		return err
	}
	check := func() error {
		if err := checkSelection(); err != nil {
			return err
		}
		now, err := b.inspectHermesBridge(ctx)
		if err != nil || !sameHermesBridge(bridge, now) {
			return fmt.Errorf("Hermes native bridge changed during PATH operation: %v", err)
		}
		return ctx.Err()
	}
	// This small evidence file describes the independently retained native copy.
	// No payload bytes or arbitrary cleanup paths are read back from this record.
	retain = true
	record := struct {
		Schema        int
		Root          string
		Remove        bool
		Selection     pathState
		Bridge        *hermesCompletionFile
		BridgeReceipt pathState
	}{Schema: 1, Root: root, Remove: remove, Selection: pointer}
	if bridge != nil {
		record.Bridge, record.BridgeReceipt = &bridge.payload.hash, bridge.receipt
	}
	if err := writeJSON(evidencePath, record); err != nil {
		return err
	}
	evidence, err = pathSnapshot(evidencePath, false)
	if err != nil {
		return err
	}
	if err := b.boundary("path-preflight"); err != nil {
		return err
	}
	if err := check(); err != nil {
		return err
	}
	if !remove && bridge == nil {
		bridge, err = b.prepareHermesBridge(ctx, check)
		if err != nil {
			return err
		}
	}
	before := func() error {
		if err := b.boundary("path-before-publish"); err != nil {
			return err
		}
		return check()
	}
	uncertain, err := b.applyPathChanges(ctx, lock, changes, before, check)
	if err != nil {
		return err
	}
	retain = uncertain
	if remove {
		b.log(label("path_removed"))
	} else {
		b.log(label("path_enabled"))
	}
	return nil
}

func hermesAliasApps(executable string) (string, error) {
	if err := absolutePath(executable); err != nil {
		return "", err
	}
	root := filepath.Dir(filepath.Dir(executable))
	apps := filepath.Dir(filepath.Dir(filepath.Dir(root)))
	if executable != filepath.Join(apps, providers[hermesAgent].path, "native-v1", "bin", "hermes") || absolutePath(apps) != nil {
		return "", fmt.Errorf("Hermes native alias requires its owned Apps layout")
	}
	return apps, nil
}

func parseHermesAlias(executable string, args []string, dry bool) (hermesLaunchOptions, error) {
	apps, err := hermesAliasApps(executable)
	if err != nil {
		return hermesLaunchOptions{}, err
	}
	// Adding the derived option first also makes any caller override a duplicate.
	return parseHermesLaunch(append([]string{"--apps-dir", apps}, args...), dry)
}

func (b *backend) verifyHermesAlias(ctx context.Context, executable string) (*hermesBridgeState, *pathReceipt, error) {
	apps, err := hermesAliasApps(executable)
	if err != nil || apps != b.req.AppsDir {
		return nil, nil, fmt.Errorf("Hermes native alias layout changed")
	}
	home, err := integrationHome()
	if err != nil {
		return nil, nil, err
	}
	if _, err := os.Lstat(filepath.Join(home, ".atm-ai-path.lock")); !os.IsNotExist(err) {
		return nil, nil, fmt.Errorf("Hermes PATH operation locked/interrupted")
	}
	_, current, err := b.hermesInspect(ctx)
	if err != nil {
		return nil, nil, err
	}
	rec, err := b.inspectIntegrationContext(ctx, hermesAgent, current)
	if err != nil || rec == nil {
		return nil, nil, fmt.Errorf("Hermes alias requires valid explicit PATH enrollment: %v", err)
	}
	bridge, err := b.inspectHermesBridge(ctx)
	if err != nil || bridge == nil {
		return nil, nil, fmt.Errorf("Hermes alias native bridge missing/changed: %v", err)
	}
	running, err := os.Stat("/proc/self/exe")
	if err != nil || !os.SameFile(bridge.payload.info, running) {
		return nil, nil, fmt.Errorf("Hermes alias running executable identity mismatch")
	}
	return bridge, rec, ctx.Err()
}

func hermesAliasMain(executable string, args []string) {
	o, err := parseHermesAlias(executable, args, os.Getenv("ATM_DRY_RUN") == "1")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if o.dry {
		fmt.Fprintln(os.Stdout, label("hermes_launch_dry"))
		return
	}
	b, err := newBackend(protocol.Request{PluginID: "ai", AppsDir: o.apps}, func(string) {})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	b.hermesAlias = executable
	fmt.Fprintln(os.Stderr, label("hermes_launch_notice"))
	result, err := b.launchHermes(context.Background(), o.args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	os.Exit(hermesLaunchExitStatus(result, err))
}

// Called only while launchHermes holds the provider lock; subsequent checks
// must compare the original authority rather than accept rewritten receipts.
func (b *backend) hermesAliasGuard(ctx context.Context) (func() error, error) {
	if b.hermesAlias == "" {
		return func() error { return nil }, nil
	}
	bridge, receipt, err := b.verifyHermesAlias(ctx, b.hermesAlias)
	if err != nil {
		return nil, err
	}
	path, err := pathSnapshot(filepath.Join(b.root(hermesAgent), "path.json"), false)
	if err != nil {
		return nil, err
	}
	return func() error {
		now, rec, err := b.verifyHermesAlias(ctx, b.hermesAlias)
		if err != nil {
			return err
		}
		next, err := pathSnapshot(filepath.Join(b.root(hermesAgent), "path.json"), false)
		if err != nil || !pathUnchanged(next, path) || !sameHermesBridge(bridge, now) || !reflect.DeepEqual(receipt, rec) {
			return fmt.Errorf("Hermes alias enrollment authority changed: %v", err)
		}
		return nil
	}, nil
}
