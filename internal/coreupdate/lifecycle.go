package coreupdate

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/AtomyCloud/atm/internal/install"
)

const historyPrefix = ".atm-coreupdate-history-"

var ErrConsentRequired = errors.New("CORE transaction finalization requires explicit consent")

type FinalizeResult struct {
	// HistoryPath contains the retained transaction after Finalized becomes true.
	HistoryPath string
	// Finalized stays true if an error occurs after the atomic directory move.
	Finalized bool
}

func (s *Service) finalizable(ctx context.Context, r *os.Root) error {
	v, err := s.load(ctx, r)
	if err != nil {
		return err
	}
	var current install.File
	switch {
	case v.rolledBack:
		// Recovery after a committed replacement legitimately retains both markers.
		current = v.Old
	case v.committed:
		current = v.next
	default:
		return ErrRecoveryRequired
	}
	for _, name := range []string{"replacement", "restore"} {
		if _, err := r.Lstat(transaction + "/" + name); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: unresolved transaction scratch file %s", ErrRecoveryRequired, name)
		}
	}
	return match(ctx, r, s.name(), current)
}

// Finalize archives a completed or explicitly recovered experimental binary-only
// transaction, freeing its active slot. It never changes executable bytes or
// deletes history. Consent acknowledges that Recover will no longer address this
// transaction. Use its original trusted Policy, not the next update's policy.
// A subsequent Stage requires an independently trusted baseline/current version.
//
// Like Replace, this requires same-host Linux/macOS and quiescent external
// writers. A private, exclusively created history directory prevents collisions
// with earlier history. Interruption before the move retains the transaction and
// may leave an empty history directory; retry creates another without deleting it.
// After the move, finalization is complete even if cancellation arrives. A retry
// with no active transaction returns an error, never infers success from history.
// History is for inspection, not an automatic restore or baseline-adoption API.
// DryRun needs no consent and performs no filesystem I/O. No power-loss durability
// or protection against hostile same-user writers is promised.
func (s *Service) Finalize(ctx context.Context, consent bool) (FinalizeResult, error) {
	var result FinalizeResult
	if err := s.trust(); err != nil {
		return result, err
	}
	if err := s.canReplace(); err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if s.p.DryRun {
		return result, nil
	}
	if !consent {
		return result, ErrConsentRequired
	}
	st, err := s.open(false)
	if err != nil {
		return result, err
	}
	defer st.close()
	if err = s.finalizable(ctx, st.root); err != nil {
		return result, err
	}
	if err = s.boundary("before-finalize"); err != nil {
		return result, err
	}
	if err = s.finalizable(ctx, st.root); err != nil {
		return result, err
	}
	history := historyPrefix + rand.Text()
	if err = st.root.Mkdir(history, 0700); err != nil {
		return result, err
	}
	if err = s.boundary("finalize-reserved"); err != nil {
		return result, err
	}
	// Recheck signature, markers, backup, candidate and current bytes immediately
	// before releasing the startup guard. Never clean up failed reservations.
	if err = s.finalizable(ctx, st.root); err != nil {
		return result, err
	}
	dest := history + "/" + transaction
	if _, err = st.root.Lstat(dest); !errors.Is(err, os.ErrNotExist) {
		return result, fmt.Errorf("%w: history destination already exists or is unreadable", ErrRecoveryRequired)
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if err = st.root.Rename(transaction, dest); err != nil {
		return result, err
	}
	result.HistoryPath, result.Finalized = filepath.Join(s.p.Root, dest), true
	return result, s.boundary("after-finalize")
}
