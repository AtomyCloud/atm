package coreupdate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/AtomyCloud/atm/internal/install"
	"github.com/AtomyCloud/atm/internal/plugin"
)

type Result struct {
	State string // planned, staged, replaced, recovered, or recovery-required
	Path  string
	// Changed remains true if rename succeeded but completion recording failed.
	Changed bool
}

type journal struct {
	Schema         int          `json:"schema"`
	CurrentVersion string       `json:"current_version"`
	Old            install.File `json:"old"`
	Manifest       []byte       `json:"manifest"`
	Signature      []byte       `json:"signature"`
}

type state struct {
	journal
	next                  install.File
	committed, rolledBack bool
}

func (s *Service) boundary(name string) error {
	if s.checkpoint != nil {
		return s.checkpoint(name)
	}
	return nil
}

func (s *Service) result(state string) Result {
	return Result{State: state, Path: filepath.Join(s.p.Root, s.name())}
}

// Stage consumes a bounded raw executable from a caller-owned reader. It never
// downloads, extracts archives, executes payloads or changes the active binary.
// The reader must honor cancellation/deadlines for blocking reads. DryRun does
// not touch the reader or filesystem. Abandoned incomplete stages fail closed.
func (s *Service) Stage(ctx context.Context, raw, signature []byte, payload io.Reader) (Result, error) {
	m, err := s.Verify(raw, signature)
	if err != nil {
		return Result{}, err
	}
	result := s.result("planned")
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if s.p.DryRun {
		return result, nil
	}
	if payload == nil {
		return result, fmt.Errorf("missing executable reader")
	}
	st, err := s.open(true)
	if err != nil {
		return result, err
	}
	defer st.close()
	if _, err = st.root.Lstat(transaction); !errors.Is(err, os.ErrNotExist) {
		return result, ErrRecoveryRequired
	}
	old, err := fingerprint(ctx, st.root, s.name(), s.p.MaxBytes)
	if err != nil {
		return result, err
	}
	if old.SHA256 != s.p.BaselineSHA256 || old.Size < 64 || runtime.GOOS != "windows" && (old.Mode&0111 == 0 || old.Mode&0022 != 0) {
		return result, fmt.Errorf("unverified or modified installed executable")
	}
	if err = plugin.CheckBinary(filepath.Join(s.p.Root, s.name()), s.p.OS+"/"+s.p.Arch); err != nil {
		return result, err
	}
	if err = st.root.Mkdir(transaction, 0700); err != nil {
		return result, err
	}
	keep := false
	defer func() {
		if !keep {
			st.root.RemoveAll(transaction)
		}
	}()
	if err = s.boundary("created"); err != nil {
		return result, err
	}
	f, err := regular(st.root, s.name(), os.O_RDONLY, s.p.MaxBytes)
	if err != nil {
		return result, err
	}
	backup, copyErr := writeFile(ctx, st.root, transaction+"/backup", f, old.Size, 0600)
	f.Close()
	if copyErr != nil {
		return result, copyErr
	}
	if backup.SHA256 != old.SHA256 || backup.Size != old.Size {
		return result, fmt.Errorf("baseline changed during backup")
	}
	want := m.Files[s.name()]
	got, err := writeFile(ctx, st.root, transaction+"/candidate", payload, want.Size, 0700)
	if err != nil {
		return result, err
	}
	if got.SHA256 != want.SHA256 || got.Size != want.Size {
		return result, fmt.Errorf("payload SHA-256 or length mismatch")
	}
	if err = plugin.CheckBinary(filepath.Join(s.p.Root, transaction, "candidate"), s.p.OS+"/"+s.p.Arch); err != nil {
		return result, err
	}
	if err = match(ctx, st.root, s.name(), old); err != nil {
		return result, err
	}
	j := journal{1, s.p.CurrentVersion, old, append([]byte(nil), raw...), append([]byte(nil), signature...)}
	b, err := json.Marshal(j)
	if err != nil {
		return result, err
	}
	if _, err = writeFile(ctx, st.root, transaction+"/journal.json", bytes.NewReader(b), 64<<10, 0600); err != nil {
		return result, err
	}
	if _, err = s.load(ctx, st.root); err != nil {
		return result, err
	}
	if err = s.boundary("prepared"); err != nil {
		return result, err
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	keep = true
	return s.result("staged"), nil
}

func (s *Service) load(ctx context.Context, r *os.Root) (state, error) {
	var v state
	info, err := r.Lstat(transaction)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return v, ErrRecoveryRequired
	}
	d, err := r.Open(transaction)
	if err != nil {
		return v, err
	}
	entries, err := d.ReadDir(9)
	d.Close()
	if err != nil && err != io.EOF {
		return v, err
	}
	if len(entries) > 7 {
		return v, fmt.Errorf("unexpected CORE transaction entries")
	}
	for _, e := range entries {
		switch e.Name() {
		case "journal.json", "candidate", "backup", "committed", "rolledback", "replacement", "restore":
		default:
			return v, fmt.Errorf("unknown CORE transaction entry")
		}
		f, err := regular(r, transaction+"/"+e.Name(), os.O_RDONLY, max(s.p.MaxBytes, 64<<10))
		if err != nil {
			return v, err
		}
		f.Close()
	}
	b, err := readFile(r, transaction+"/journal.json", 64<<10)
	if err != nil {
		return v, fmt.Errorf("%w: %v", ErrRecoveryRequired, err)
	}
	if err = strictJSON(b, &v.journal); err != nil {
		return v, err
	}
	m, err := s.Verify(v.Manifest, v.Signature)
	if err != nil {
		return v, err
	}
	if v.Schema != 1 || v.CurrentVersion != s.p.CurrentVersion || v.Old.SHA256 != s.p.BaselineSHA256 ||
		v.Old.Size < 64 || v.Old.Size > s.p.MaxBytes || v.Old.Mode > 0777 ||
		runtime.GOOS != "windows" && (v.Old.Mode&0111 == 0 || v.Old.Mode&0022 != 0) {
		return v, fmt.Errorf("CORE journal baseline mismatch")
	}
	v.next = m.Files[s.name()]
	old, next := v.Old, v.next
	old.Mode = 0600
	next.Mode = 0700
	if err = match(ctx, r, transaction+"/backup", old); err != nil {
		return v, err
	}
	if err = match(ctx, r, transaction+"/candidate", next); err != nil {
		return v, err
	}
	for _, marker := range []struct {
		name, digest string
		flag         *bool
	}{
		{"committed", v.next.SHA256, &v.committed}, {"rolledback", v.Old.SHA256, &v.rolledBack},
	} {
		b, err := readFile(r, transaction+"/"+marker.name, 64)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return v, err
		}
		if string(b) != marker.digest {
			return v, fmt.Errorf("invalid CORE completion marker")
		}
		*marker.flag = true
	}
	return v, ctx.Err()
}

func (s *Service) canReplace() error {
	// Never attempt a Windows in-use overwrite, helper process or delayed rename.
	if runtime.GOOS == "windows" || s.p.OS == "windows" || s.p.OS != runtime.GOOS || s.p.Arch != runtime.GOARCH ||
		(runtime.GOOS != "linux" && runtime.GOOS != "darwin") {
		return ErrUnsupported
	}
	return nil
}

func prepareCopy(ctx context.Context, r *os.Root, source, dest string, want install.File) error {
	if f, err := regular(r, dest, os.O_RDONLY, MaxBinaryBytes); err == nil {
		f.Close()
		if err = r.Remove(dest); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	f, err := regular(r, source, os.O_RDONLY, want.Size)
	if err != nil {
		return err
	}
	defer f.Close()
	got, err := writeFile(ctx, r, dest, f, want.Size, os.FileMode(want.Mode))
	if err != nil {
		return err
	}
	if got.SHA256 != want.SHA256 || got.Size != want.Size {
		return fmt.Errorf("transaction copy changed")
	}
	return match(ctx, r, dest, want)
}

func mark(r *os.Root, name, hash string) error {
	_, err := writeFile(context.Background(), r, transaction+"/"+name, bytes.NewBufferString(hash), 64, 0600)
	return err
}

// Replace retains both verified candidate and backup. Only same-host Linux/macOS
// rename is supported. A running old process must exit before using new services;
// this method does not restart it. All other ATM sessions must be quiescent.
// The immutable journal is written before replacement. A crash after rename but
// before its completion marker requires explicit Recover, never assumed success.
func (s *Service) Replace(ctx context.Context) (Result, error) {
	if err := s.trust(); err != nil {
		return Result{}, err
	}
	result := s.result("planned")
	if err := s.canReplace(); err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if s.p.DryRun {
		return result, nil
	}
	st, err := s.open(false)
	if err != nil {
		return result, err
	}
	defer st.close()
	v, err := s.load(ctx, st.root)
	if err != nil {
		return result, err
	}
	if v.rolledBack {
		return result, fmt.Errorf("transaction already recovered; retained for review")
	}
	if v.committed {
		if err = match(ctx, st.root, s.name(), v.next); err != nil {
			return result, err
		}
		return s.result("replaced"), nil
	}
	if err = match(ctx, st.root, s.name(), v.Old); err != nil {
		return result, fmt.Errorf("%w: %v", ErrRecoveryRequired, err)
	}
	if err = prepareCopy(ctx, st.root, transaction+"/candidate", transaction+"/replacement", v.next); err != nil {
		return result, err
	}
	if err = s.boundary("before-replace"); err != nil {
		return result, err
	}
	if err = match(ctx, st.root, s.name(), v.Old); err != nil {
		return result, err
	}
	if err = st.root.Rename(transaction+"/replacement", s.name()); err != nil {
		return result, err
	}
	result.State, result.Changed = "recovery-required", true
	if err = s.boundary("after-replace"); err != nil {
		return result, fmt.Errorf("%w: %v", ErrRecoveryRequired, err)
	}
	if err = mark(st.root, "committed", v.next.SHA256); err != nil {
		return result, fmt.Errorf("%w: %v", ErrRecoveryRequired, err)
	}
	result.State = "replaced"
	return result, nil
}

// Recover explicitly rolls back only this transaction. It re-verifies signature,
// baseline, backup and current bytes; unrelated local edits block all writes to
// the executable. Backups/journals are retained until explicit Finalize archives
// them. Automatic pruning, archive restoration and power-loss durability are not
// implemented.
func (s *Service) Recover(ctx context.Context) (Result, error) {
	if err := s.trust(); err != nil {
		return Result{}, err
	}
	result := s.result("planned")
	if err := s.canReplace(); err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if s.p.DryRun {
		return result, nil
	}
	st, err := s.open(false)
	if err != nil {
		return result, err
	}
	defer st.close()
	v, err := s.load(ctx, st.root)
	if err != nil {
		return result, err
	}
	if v.rolledBack {
		if err = match(ctx, st.root, s.name(), v.Old); err != nil {
			return result, err
		}
		return s.result("recovered"), nil
	}
	if err = match(ctx, st.root, s.name(), v.Old); err != nil {
		if err = match(ctx, st.root, s.name(), v.next); err != nil {
			return result, fmt.Errorf("current executable is neither verified old nor new: %w", err)
		}
		if err = prepareCopy(ctx, st.root, transaction+"/backup", transaction+"/restore", v.Old); err != nil {
			return result, err
		}
		if err = s.boundary("before-recover"); err != nil {
			return result, err
		}
		if err = match(ctx, st.root, s.name(), v.next); err != nil {
			return result, err
		}
		if err = st.root.Rename(transaction+"/restore", s.name()); err != nil {
			return result, err
		}
		result.State, result.Changed = "recovery-required", true
		if err = s.boundary("after-recover"); err != nil {
			return result, fmt.Errorf("%w: %v", ErrRecoveryRequired, err)
		}
	}
	if err = mark(st.root, "rolledback", v.Old.SHA256); err != nil {
		return result, fmt.Errorf("%w: %v", ErrRecoveryRequired, err)
	}
	result.State = "recovered"
	return result, nil
}

// CheckStartup is read-only. Prepared/interrupted or corrupt transactions block
// startup; completed and recovered transactions require full re-verification.
// It cannot make an already-running old executable into the new executable.
func (s *Service) CheckStartup(ctx context.Context) error {
	if err := s.trust(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	err := StartupGuard(s.p.Root)
	if err == nil {
		return nil
	}
	if !errors.Is(err, ErrRecoveryRequired) {
		return err
	}
	st, err := s.open(false)
	if err != nil {
		return err
	}
	defer st.close()
	v, err := s.load(ctx, st.root)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrRecoveryRequired, err)
	}
	if v.rolledBack {
		return match(ctx, st.root, s.name(), v.Old)
	}
	if v.committed {
		return match(ctx, st.root, s.name(), v.next)
	}
	return ErrRecoveryRequired
}
