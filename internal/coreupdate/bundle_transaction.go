package coreupdate

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const bundleJournalLimit = 32 << 20

type bundleJournal struct {
	Schema  int        `json:"schema"`
	Root    string     `json:"root"`
	Enroll  bool       `json:"enroll"`
	OldRaw  []byte     `json:"old_manifest"`
	OldSig  []byte     `json:"old_signature"`
	NextRaw []byte     `json:"next_manifest"`
	NextSig []byte     `json:"next_signature"`
	Before  bundleTree `json:"before"`
}

type bundleStore struct {
	parent           *os.Root
	lock, pluginLock *os.File
	base, tx         string
}

func (s *bundleStore) close() {
	if s.pluginLock != nil {
		s.pluginLock.Close()
	}
	s.lock.Close()
	s.parent.Close()
}

func bundleOpen(p BundlePolicy) (*bundleStore, error) {
	r, err := openRoot(filepath.Dir(p.Root))
	if err != nil {
		return nil, err
	}
	base := filepath.Base(p.Root)
	name := base + ".atm-package-lock"
	f, err := r.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err == nil {
		f.Close()
	} else if !errors.Is(err, os.ErrExist) {
		r.Close()
		return nil, err
	}
	f, err = regular(r, name, os.O_RDWR, 0)
	if err != nil {
		r.Close()
		return nil, err
	}
	if err = lockFile(f); err != nil {
		f.Close()
		r.Close()
		return nil, fmt.Errorf("%w: %v", ErrBusy, err)
	}
	s := &bundleStore{parent: r, lock: f, base: base, tx: base + BundleTransactionSuffix}
	// Cooperate with an existing native plugin lock. A quiescent user session is
	// still required: other tools/legacy processes do not share this root lock.
	pluginName := base + "/plugins/.atm-updates.lock"
	pf, err := regular(r, pluginName, os.O_RDWR, 0)
	if err == nil {
		if err = lockFile(pf); err != nil {
			pf.Close()
			s.close()
			return nil, fmt.Errorf("%w: plugin updater", ErrBusy)
		}
		s.pluginLock = pf
	} else if !errors.Is(err, os.ErrNotExist) {
		s.close()
		return nil, err
	}
	return s, nil
}

func bundleHost(p BundlePolicy) error {
	if p.OS != runtime.GOOS || p.Arch != runtime.GOARCH || !supported(runtime.GOOS, runtime.GOARCH) {
		return ErrUnsupported
	}
	if runtime.GOOS == "windows" {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		exe, err = filepath.EvalSymlinks(exe)
		if err != nil {
			return err
		}
		root := strings.ToLower(p.Root)
		exe = strings.ToLower(exe)
		if exe == root || strings.HasPrefix(exe, root+string(filepath.Separator)) {
			return fmt.Errorf("Windows package replacement requires an external trusted executable")
		}
	}
	return nil
}

func bundleBoundary(hook func(string) error, name string) error {
	if hook != nil {
		return hook(name)
	}
	return nil
}

// The process lock excludes other bundle mutations; quiescent external writers
// remain a precondition. Never intentionally replace even an empty destination.
func bundleRename(r *os.Root, from, to string) error {
	if _, err := r.Lstat(to); !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("package rename destination already exists or is unreadable: %s", to)
	}
	return r.Rename(from, to)
}

func bundleLoad(r *os.Root, tx, root string) (bundleJournal, []byte, error) {
	var j bundleJournal
	i, err := r.Lstat(tx)
	if err != nil {
		return j, nil, err
	}
	if !i.IsDir() || i.Mode()&os.ModeSymlink != 0 {
		return j, nil, fmt.Errorf("unsafe package transaction")
	}
	b, err := readFile(r, tx+"/journal.json", bundleJournalLimit)
	if err != nil {
		return j, nil, err
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err = d.Decode(&j); err != nil {
		return j, nil, err
	}
	if err = d.Decode(new(any)); err != io.EOF {
		return j, nil, fmt.Errorf("trailing transaction JSON")
	}
	if j.Schema != 1 || j.Root != root || len(j.Before.Files) == 0 || len(j.Before.Files)+len(j.Before.Dirs) > packageEntries+1 {
		return j, nil, fmt.Errorf("invalid package transaction identity")
	}
	return j, b, nil
}

func bundleJournalTrust(j bundleJournal, p BundlePolicy) error {
	old, err := VerifyPackageManifest(j.OldRaw, j.OldSig, p.PublicKey, p.OS, p.Arch)
	if err != nil {
		return err
	}
	next, err := VerifyPackageManifest(j.NextRaw, j.NextSig, p.PublicKey, p.OS, p.Arch)
	if err != nil {
		return err
	}
	if j.Enroll {
		if !bytes.Equal(j.OldRaw, j.NextRaw) || !bytes.Equal(j.OldSig, j.NextSig) {
			return fmt.Errorf("enrollment identity changed")
		}
	} else if !newer(next.Version, old.Version) {
		return fmt.Errorf("non-increasing transaction")
	}
	return nil
}

func bundleMarker(r *os.Root, tx, name string, journal []byte) (bool, error) {
	b, err := readFile(r, tx+"/"+name, 64)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if string(b) != fmt.Sprintf("%x", sha256.Sum256(journal)) {
		return false, fmt.Errorf("corrupt package transaction marker")
	}
	return true, nil
}

func bundleMark(r *os.Root, tx, name string, journal []byte) error {
	if ok, err := bundleMarker(r, tx, name, journal); err != nil || ok {
		return err
	}
	b := []byte(fmt.Sprintf("%x", sha256.Sum256(journal)))
	// A crash before the rename leaves a scratch file, never a false completion
	// marker. Recovery can retry without deleting or trusting that scratch file.
	tmp := tx + "/.marker-" + rand.Text()
	if _, err := writeFile(context.Background(), r, tmp, bytes.NewReader(b), 64, 0600); err != nil {
		return err
	}
	return r.Rename(tmp, tx+"/"+name)
}

// BundleStartupGuard is read-only and permits development/source roots when no
// sibling transaction exists. It authenticates no key; typed signed recovery
// validates the journal and backup before mutation. Corrupt/partial state blocks.
func BundleStartupGuard(root string) error {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || filepath.Dir(root) == root {
		return fmt.Errorf("clean absolute startup root required")
	}
	r, err := os.OpenRoot(filepath.Dir(root))
	if err != nil {
		return err
	}
	defer r.Close()
	tx := filepath.Base(root) + BundleTransactionSuffix
	if _, err = r.Lstat(tx); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	_, b, err := bundleLoad(r, tx, root)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrRecoveryRequired, err)
	}
	rolled, err := bundleMarker(r, tx, "rolledback", b)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrRecoveryRequired, err)
	}
	committed, err := bundleMarker(r, tx, "committed", b)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrRecoveryRequired, err)
	}
	recovering, err := bundleMarker(r, tx, "recovering", b)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrRecoveryRequired, err)
	}
	if !rolled && (!committed || recovering) {
		return ErrRecoveryRequired
	}
	i, err := r.Lstat(filepath.Base(root))
	if err != nil || !i.IsDir() || i.Mode()&os.ModeSymlink != 0 {
		return ErrRecoveryRequired
	}
	return nil
}

func bundleCheckOld(ctx context.Context, p BundlePolicy, j bundleJournal, location string) error {
	r, err := openRoot(location)
	if err != nil {
		return err
	}
	defer r.Close()
	state, err := bundleInstalled(ctx, r, p, j.OldRaw, j.OldSig)
	if err != nil {
		return err
	}
	if !bundleSame(state.tree, j.Before) {
		return fmt.Errorf("original package tree changed; preserved without rollback")
	}
	if !j.Enroll {
		raw, err := readFile(r, BundleManifestName, MaxManifestBytes)
		if err != nil {
			return err
		}
		sig, err := readFile(r, BundleSignatureName, 64)
		if err != nil {
			return err
		}
		if !bytes.Equal(raw, j.OldRaw) || !bytes.Equal(sig, j.OldSig) {
			return fmt.Errorf("original signed baseline changed")
		}
	}
	return nil
}

func (s *bundleStore) archiveCompleted(ctx context.Context, p BundlePolicy, hook func(string) error) error {
	if _, err := s.parent.Lstat(s.tx); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if err := BundleStartupGuard(p.Root); err != nil {
		return err
	}
	j, b, err := bundleLoad(s.parent, s.tx, p.Root)
	if err != nil {
		return err
	}
	if err = bundleJournalTrust(j, p); err != nil {
		return err
	}
	rolled, err := bundleMarker(s.parent, s.tx, "rolledback", b)
	if err != nil {
		return err
	}
	if rolled {
		r, e := openRoot(p.Root)
		if e != nil {
			return e
		}
		_, e = bundleInstalled(ctx, r, p, j.OldRaw, j.OldSig)
		r.Close()
		if e != nil {
			return e
		}
	} else {
		r, err := openRoot(p.Root)
		if err != nil {
			return err
		}
		state, err := bundleInstalled(ctx, r, p, nil, nil)
		r.Close()
		if err != nil {
			return err
		}
		if !bytes.Equal(state.raw, j.NextRaw) || !bytes.Equal(state.sig, j.NextSig) {
			return fmt.Errorf("completed transaction identity no longer installed")
		}
		if err = bundleCheckOld(ctx, p, j, filepath.Join(p.Root+BundleTransactionSuffix, "backup")); err != nil {
			return err
		}
	}
	history := s.base + ".atm-package-history-" + rand.Text()
	if err = s.parent.Mkdir(history, 0700); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = bundleRename(s.parent, s.tx, history+"/transaction"); err != nil {
		return err
	}
	return bundleBoundary(hook, "history-archived")
}

func applyBundle(ctx context.Context, p BundlePolicy, plan *BundlePlan, consent bool) (result BundleResult, err error) {
	if plan == nil {
		return result, fmt.Errorf("missing package plan")
	}
	result = BundleResult{State: "planned", Root: p.Root, Info: plan.Info()}
	if err = bundlePolicy(p); err != nil {
		return result, err
	}
	if p.Root != plan.p.Root || p.OS != plan.p.OS || p.Arch != plan.p.Arch || !bytes.Equal(p.PublicKey, plan.p.PublicKey) {
		return result, fmt.Errorf("package policy differs from preview")
	}
	recheck := func() error {
		fresh, e := prepareBundle(ctx, p, plan.raw, plan.sig, plan.archive, plan.enroll)
		if e != nil {
			return e
		}
		if !bundleSame(fresh.before, plan.before) || !bytes.Equal(fresh.oldRaw, plan.oldRaw) || !bytes.Equal(fresh.oldSig, plan.oldSig) {
			return fmt.Errorf("installation changed since package preview")
		}
		return nil
	}
	if err = recheck(); err != nil {
		return result, err
	}
	if p.DryRun {
		return result, nil
	}
	if !consent {
		return result, ErrConsentRequired
	}
	if err = bundleHost(p); err != nil {
		return result, err
	}
	s, err := bundleOpen(p)
	if err != nil {
		return result, err
	}
	defer s.close()
	if err = recheck(); err != nil {
		return result, err
	}
	if err = s.archiveCompleted(ctx, p, plan.checkpoint); err != nil {
		return result, err
	}
	j := bundleJournal{Schema: 1, Root: p.Root, Enroll: plan.enroll, OldRaw: plan.oldRaw, OldSig: plan.oldSig, NextRaw: plan.raw, NextSig: plan.sig, Before: plan.before}
	journal, err := json.Marshal(j)
	if err != nil {
		return result, err
	}
	reserve := s.base + ".atm-package-reserve-" + rand.Text()
	if err = s.parent.Mkdir(reserve, 0700); err != nil {
		return result, err
	}
	reserved := true
	defer func() {
		if reserved {
			s.parent.RemoveAll(reserve)
		}
	}()
	if _, err = writeFile(ctx, s.parent, reserve+"/journal.json", bytes.NewReader(journal), bundleJournalLimit, 0600); err != nil {
		return result, err
	}
	if err = bundleBoundary(plan.checkpoint, "journal-ready"); err != nil {
		return result, err
	}
	if err = bundleRename(s.parent, reserve, s.tx); err != nil {
		return result, err
	}
	reserved = false
	result.State = "recovery-required"
	result.Backup = filepath.Join(p.Root+BundleTransactionSuffix, "backup")
	if err = bundleBoundary(plan.checkpoint, "created"); err != nil {
		return result, err
	}
	if err = s.parent.Mkdir(s.tx+"/candidate", 0700); err != nil {
		return result, err
	}
	old, err := openRoot(p.Root)
	if err != nil {
		return result, err
	}
	candidate, err := s.parent.OpenRoot(s.tx + "/candidate")
	if err != nil {
		old.Close()
		return result, err
	}
	err = bundleStage(ctx, old, candidate, plan)
	var staged bundleTree
	if err == nil {
		staged, err = bundleScan(ctx, candidate)
	}
	candidate.Close()
	old.Close()
	if err != nil {
		return result, err
	}
	if err = bundleBoundary(plan.checkpoint, "candidate-ready"); err != nil {
		return result, err
	}
	if err = bundleCheckOld(ctx, p, j, p.Root); err != nil {
		return result, err
	}
	if err = bundleBoundary(plan.checkpoint, "before-backup"); err != nil {
		return result, err
	}
	checkCandidate := func(location string, checkCtx context.Context) error {
		r, e := openRoot(location)
		if e != nil {
			return e
		}
		defer r.Close()
		got, e := bundleScan(checkCtx, r)
		if e != nil {
			return e
		}
		if !bundleSame(got, staged) {
			return fmt.Errorf("staged package changed before completion")
		}
		return nil
	}
	if err = checkCandidate(filepath.Join(p.Root+BundleTransactionSuffix, "candidate"), ctx); err != nil {
		return result, err
	}
	if err = bundleCheckOld(ctx, p, j, p.Root); err != nil {
		return result, err
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if err = bundleRename(s.parent, s.base, s.tx+"/backup"); err != nil {
		return result, err
	}
	result.Changed = true
	if err = bundleBoundary(plan.checkpoint, "after-backup"); err != nil {
		return result, err
	}
	if err = checkCandidate(filepath.Join(p.Root+BundleTransactionSuffix, "candidate"), ctx); err != nil {
		return result, err
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if err = bundleRename(s.parent, s.tx+"/candidate", s.base); err != nil {
		return result, err
	}
	if err = bundleBoundary(plan.checkpoint, "after-activate"); err != nil {
		return result, err
	}
	if err = checkCandidate(p.Root, context.Background()); err != nil {
		return result, err
	}
	// The candidate is already verified. After publication late cancellation must
	// not pretend nothing changed; completion recording proceeds synchronously.
	if err = bundleMark(s.parent, s.tx, "committed", journal); err != nil {
		return result, err
	}
	result.State = "applied"
	if plan.enroll {
		result.State = "enrolled"
	}
	result.Info.State = result.State
	return result, bundleBoundary(plan.checkpoint, "committed")
}

func recoverBundle(ctx context.Context, p BundlePolicy, consent bool, hook func(string) error) (result BundleResult, err error) {
	result = BundleResult{State: "planned", Root: p.Root, Backup: filepath.Join(p.Root+BundleTransactionSuffix, "backup")}
	if err = bundlePolicy(p); err != nil {
		return result, err
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	// Preview and refusal do not even create the persistent lock file.
	if p.DryRun {
		return result, nil
	}
	if !consent {
		return result, ErrConsentRequired
	}
	if err = bundleHost(p); err != nil {
		return result, err
	}
	s, err := bundleOpen(p)
	if err != nil {
		return result, err
	}
	defer s.close()
	j, b, err := bundleLoad(s.parent, s.tx, p.Root)
	if err != nil {
		return result, err
	}
	if err = bundleJournalTrust(j, p); err != nil {
		return result, err
	}
	old, err := VerifyPackageManifest(j.OldRaw, j.OldSig, p.PublicKey, p.OS, p.Arch)
	if err != nil {
		return result, err
	}
	result.Info = BundleInfo{Root: p.Root, State: "planned", Manifest: old, Baseline: old, CurrentVersion: old.Version, TargetVersion: old.Version, Transaction: p.Root + BundleTransactionSuffix}
	rolled, err := bundleMarker(s.parent, s.tx, "rolledback", b)
	if err != nil {
		return result, err
	}
	if rolled {
		if err = bundleCheckOld(ctx, p, j, p.Root); err != nil {
			return result, err
		}
		result.State = "recovered"
		result.Info.State = result.State
		return result, nil
	}
	backup := true
	if _, e := s.parent.Lstat(s.tx + "/backup"); errors.Is(e, os.ErrNotExist) {
		backup = false
	} else if e != nil {
		return result, e
	}
	location := p.Root
	if backup {
		location = result.Backup
	}
	if err = bundleCheckOld(ctx, p, j, location); err != nil {
		return result, err
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if err = bundleMark(s.parent, s.tx, "recovering", b); err != nil {
		return result, err
	}
	result.State = "recovery-required"
	if err = bundleBoundary(hook, "recovering"); err != nil {
		return result, err
	}
	if backup {
		if _, e := s.parent.Lstat(s.base); e == nil {
			// Preserve the displaced candidate intact, even when users edited it
			// after activation. Only exact receipts identify it; never delete it.
			r, e := openRoot(p.Root)
			if e != nil {
				return result, e
			}
			raw, e := readFile(r, BundleManifestName, MaxManifestBytes)
			sig, se := readFile(r, BundleSignatureName, 64)
			r.Close()
			if e != nil || se != nil || !bytes.Equal(raw, j.NextRaw) || !bytes.Equal(sig, j.NextSig) {
				return result, fmt.Errorf("active tree has unknown package identity; preserved")
			}
			if _, e = s.parent.Lstat(s.tx + "/rejected"); !errors.Is(e, os.ErrNotExist) {
				return result, fmt.Errorf("recovery quarantine already occupied")
			}
			if err = bundleBoundary(hook, "before-quarantine"); err != nil {
				return result, err
			}
			if err = bundleRename(s.parent, s.base, s.tx+"/rejected"); err != nil {
				return result, err
			}
			result.Changed = true
			if err = bundleBoundary(hook, "after-quarantine"); err != nil {
				return result, err
			}
		} else if !errors.Is(e, os.ErrNotExist) {
			return result, e
		}
		if err = bundleRename(s.parent, s.tx+"/backup", s.base); err != nil {
			return result, err
		}
		result.Changed = true
		if err = bundleBoundary(hook, "after-restore"); err != nil {
			return result, err
		}
	}
	if err = bundleMark(s.parent, s.tx, "rolledback", b); err != nil {
		return result, err
	}
	result.State = "recovered"
	result.Info.State = result.State
	return result, bundleBoundary(hook, "recovered")
}
