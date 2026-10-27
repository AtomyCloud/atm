package updates

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"

	"github.com/AtomyCloud/atm/internal/catalog"
)

type Backup struct {
	ID        string `json:"id"`
	Component string `json:"component"`
	Commit    string `json:"commit,omitempty"`
	Phase     string `json:"phase"`
}
type transaction struct {
	Backup
	Schema        int       `json:"schema"`
	Home          string    `json:"home"`
	Plugins       string    `json:"plugins"`
	Plugin        string    `json:"plugin"`
	Before        inventory `json:"before"`
	After         inventory `json:"after"`
	ReceiptBefore []byte    `json:"receipt_before"`
	ReceiptAfter  []byte    `json:"receipt_after"`
}

var backupPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

func (s *Service) backupDir(id string) string { return filepath.Join(s.state, "native-backups", id) }
func (s *Service) stageDir(id string) string {
	return filepath.Join(s.options.PluginsDir, ".atm-update-"+id)
}
func (s *Service) pendingPath() string {
	return filepath.Join(s.options.PluginsDir, ".atm-update-pending.json")
}

func (s *Service) guard() error {
	for _, p := range []string{s.options.Home, s.options.PluginsDir, s.state} {
		if err := safeAncestors(p); err != nil {
			return err
		}
		if err := refuseGit(p); err != nil {
			return err
		}
	}
	if _, err := os.Lstat(filepath.Join(s.options.Home, ".atm-core-update-pending")); !os.IsNotExist(err) {
		return fmt.Errorf("resolve legacy CORE recovery first")
	}
	return nil
}

// The OS lock survives no process; the legacy-compatible directory lock prevents
// concurrent legacy updates. Only our own abandoned marker may be reclaimed.
func (s *Service) lock(recovery bool) (func(), error) {
	if err := s.guard(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(s.state, 0700); err != nil {
		return nil, err
	}
	lockPath := filepath.Join(s.options.PluginsDir, ".atm-updates.lock")
	if err := safeAncestors(lockPath); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(lockPath); err == nil && !info.Mode().IsRegular() {
		return nil, fmt.Errorf("unsafe update lock")
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = lockFile(f); err != nil {
		f.Close()
		return nil, fmt.Errorf("component update already locked: %w", err)
	}
	legacy := filepath.Join(s.state, "lock")
	if err = safeAncestors(legacy); err != nil {
		f.Close()
		return nil, err
	}
	if err = os.Mkdir(legacy, 0700); err != nil {
		b, e := readOptional(filepath.Join(legacy, "owner.json"))
		var owner struct {
			Native bool `json:"native"`
		}
		if !recovery || e != nil || json.Unmarshal(b, &owner) != nil || !owner.Native {
			f.Close()
			return nil, fmt.Errorf("legacy update lock present; inspect its owner before retrying")
		}
		if err = os.Remove(filepath.Join(legacy, "owner.json")); err != nil {
			f.Close()
			return nil, err
		}
		if err = os.Remove(legacy); err != nil {
			f.Close()
			return nil, err
		}
		if err = os.Mkdir(legacy, 0700); err != nil {
			f.Close()
			return nil, err
		}
	}
	release := func() { os.Remove(filepath.Join(legacy, "owner.json")); os.Remove(legacy); f.Close() }
	if err = writeJSON(filepath.Join(legacy, "owner.json"), map[string]any{"pid": os.Getpid(), "native": true}); err != nil {
		release()
		return nil, err
	}
	return release, nil
}
func (s *Service) noPending() error {
	b, err := readOptional(s.pendingPath())
	if err != nil {
		return err
	}
	if b != nil {
		return fmt.Errorf("plugin recovery pending; run Recover first")
	}
	return nil
}

func (s *Service) Apply(ctx context.Context, p *Plan, consent bool) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, catalog.DownloadTimeout)
	defer cancel()
	result := Result{Status: "failed"}
	if p == nil || p.owner != s || p.download == nil {
		return result, fmt.Errorf("plan belongs to another service or is invalid")
	}
	id := p.Info().ID
	result.Component = "plugin:" + id
	if s.options.DryRun {
		result.Status = "success"
		result.Detail = "dry-run: no update performed"
		return result, nil
	}
	if !consent {
		return result, fmt.Errorf("explicit consent required")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	unlock, err := s.lock(false)
	if err != nil {
		return result, err
	}
	defer unlock()
	if err = s.noPending(); err != nil {
		return result, err
	}
	if err = s.unchanged(id, p.before, p.receipt); err != nil {
		return result, err
	}
	token, err := newID()
	if err != nil {
		return result, err
	}
	stage := s.stageDir(token)
	if err = os.Mkdir(stage, 0700); err != nil {
		return result, err
	}
	pending := false
	defer func() {
		if !pending {
			os.RemoveAll(stage)
		}
	}()
	if _, err = s.client.Download(ctx, p.download, stage); err != nil {
		return result, err
	}
	next := filepath.Join(stage, id)
	from, err := os.OpenRoot(s.root(id))
	if err != nil {
		return result, err
	}
	defer from.Close()
	to, err := os.OpenRoot(next)
	if err != nil {
		return result, err
	}
	defer to.Close()
	for n, f := range p.before {
		if err = ctx.Err(); err != nil {
			return result, err
		}
		if n == ".atm-origin.json" {
			continue
		}
		if f.Mode == "dir" {
			if err = to.MkdirAll(n, 0700); err != nil {
				return result, err
			}
			continue
		}
		if _, owned := p.owned[n]; owned {
			continue
		}
		if err = copyFile(from, to, n, f); err != nil {
			return result, err
		}
	}
	if err = to.Close(); err != nil {
		return result, err
	}
	from.Close()
	after, err := scan(next)
	if err != nil {
		return result, err
	}
	if !matches(after, p.download.Fingerprints()) {
		return result, fmt.Errorf("staged ownership readback mismatch")
	}
	receipt, err := readOptional(filepath.Join(next, ".atm-origin.json"))
	if err != nil {
		return result, err
	}
	tx := transaction{Backup: Backup{ID: token, Component: result.Component, Commit: p.Info().Commit, Phase: "prepared"}, Schema: 1, Home: s.options.Home, Plugins: s.options.PluginsDir, Plugin: id, Before: p.before, After: after, ReceiptBefore: p.receipt, ReceiptAfter: receipt}
	result, err = s.publish(ctx, &tx, next, &pending)
	return result, err
}
func newID() (string, error) {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	return hex.EncodeToString(b), err
}
func (s *Service) unchanged(id string, want inventory, receipt []byte) error {
	actual, err := scan(s.root(id))
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(want, actual) {
		return fmt.Errorf("local files changed since preparation")
	}
	b, err := readOptional(s.receiptPath(id))
	if err != nil {
		return err
	}
	if !bytes.Equal(b, receipt) {
		return fmt.Errorf("origin receipt changed since preparation")
	}
	return nil
}
func (s *Service) publish(ctx context.Context, tx *transaction, next string, pending *bool) (Result, error) {
	result := Result{Component: tx.Component, Status: "failed", BackupID: tx.ID}
	backup := s.backupDir(tx.ID)
	if err := os.MkdirAll(filepath.Dir(backup), 0700); err != nil {
		return result, err
	}
	if err := os.Mkdir(backup, 0700); err != nil {
		return result, err
	}
	if err := copyTree(ctx.Err, s.root(tx.Plugin), filepath.Join(backup, "before"), tx.Before); err != nil {
		return result, err
	}
	if err := writeJSON(filepath.Join(backup, "metadata.json"), tx); err != nil {
		return result, err
	}
	if err := s.unchanged(tx.Plugin, tx.Before, tx.ReceiptBefore); err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := writeJSON(s.pendingPath(), map[string]string{"backup": tx.ID, "state": s.state}); err != nil {
		return result, err
	}
	*pending = true
	fail := func(err error) (Result, error) {
		recovery := s.recoverTransaction(tx)
		if recovery == nil {
			*pending = false
		} else {
			err = errors.Join(err, fmt.Errorf("recovery required for %s: %w", tx.ID, recovery))
		}
		return result, err
	}
	old := filepath.Join(s.stageDir(tx.ID), "old")
	if err := noReplace(s.root(tx.Plugin), old); err != nil {
		return fail(err)
	}
	actual, err := scan(old)
	if err != nil || !reflect.DeepEqual(actual, tx.Before) {
		// Put concurrently edited files back unchanged; never replace them with backup.
		rollback := noReplace(old, s.root(tx.Plugin))
		if rollback == nil {
			os.Remove(s.pendingPath())
			*pending = false
		}
		return result, errors.Join(fmt.Errorf("local files changed during publication"), err, rollback)
	}
	if err = ctx.Err(); err != nil {
		return fail(err)
	}
	if err = noReplace(next, s.root(tx.Plugin)); err != nil {
		return fail(err)
	}
	actual, err = scan(s.root(tx.Plugin))
	if err != nil || !reflect.DeepEqual(actual, tx.After) {
		return fail(errors.Join(fmt.Errorf("publication readback mismatch"), err))
	}
	if err = s.setReceipt(tx.Plugin, tx.ReceiptAfter); err != nil {
		return fail(err)
	}
	tx.Phase = "applied"
	if err = writeJSON(filepath.Join(backup, "metadata.json"), tx); err != nil {
		return fail(err)
	}
	if err = os.Remove(s.pendingPath()); err != nil {
		return fail(err)
	}
	*pending = false
	result.Status = "updated"
	result.RestartRequired = true
	return result, nil
}
func (s *Service) setReceipt(id string, b []byte) error {
	name := s.receiptPath(id)
	if err := safeAncestors(name); err != nil {
		return err
	}
	if b == nil {
		err := os.Remove(name)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return atomicBytes(name, b)
}

func (s *Service) readBackup(id string) (transaction, error) {
	var tx transaction
	if !backupPattern.MatchString(id) {
		return tx, fmt.Errorf("invalid backup ID")
	}
	b, err := readOptional(filepath.Join(s.backupDir(id), "metadata.json"))
	if err != nil {
		return tx, err
	}
	if err = json.Unmarshal(b, &tx); err != nil {
		return tx, err
	}
	if tx.Schema != 1 || tx.ID != id || tx.Home != s.options.Home || tx.Plugins != s.options.PluginsDir || validID(tx.Plugin) != nil || tx.Component != "plugin:"+tx.Plugin {
		return tx, fmt.Errorf("backup identity/scope mismatch")
	}
	for _, inv := range []inventory{tx.Before, tx.After} {
		if len(inv) == 0 || len(inv) > 10000 {
			return tx, fmt.Errorf("invalid backup inventory")
		}
		for n, f := range inv {
			if n != ".atm-origin.json" && !catalog.ValidPayloadPath(n) {
				return tx, fmt.Errorf("unsafe backup path")
			}
			if f.Mode != "dir" && (!commitPattern.MatchString(f.SHA) || (f.Mode != "100644" && f.Mode != "100755")) {
				return tx, fmt.Errorf("invalid backup fingerprint")
			}
		}
	}
	actual, err := scan(filepath.Join(s.backupDir(id), "before"))
	if err != nil {
		return tx, err
	}
	if !reflect.DeepEqual(actual, tx.Before) {
		return tx, fmt.Errorf("backup contents changed")
	}
	return tx, nil
}
func (s *Service) Backups() ([]Backup, error) {
	dir := filepath.Join(s.state, "native-backups")
	if err := safeAncestors(dir); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []Backup{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := []Backup{}
	for _, e := range entries {
		if !backupPattern.MatchString(e.Name()) {
			continue
		}
		tx, err := s.readBackup(e.Name())
		if err != nil {
			return result, err
		}
		result = append(result, tx.Backup)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func (s *Service) Recover(ctx context.Context, consent bool) (Result, error) {
	result := Result{Status: "failed"}
	if s.options.DryRun {
		result.Status = "success"
		result.Detail = "dry-run: no recovery performed"
		return result, nil
	}
	if !consent {
		return result, fmt.Errorf("explicit recovery consent required")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	unlock, err := s.lock(true)
	if err != nil {
		return result, err
	}
	defer unlock()
	b, err := readOptional(s.pendingPath())
	if err != nil {
		return result, err
	}
	if b == nil {
		result.Status = "current"
		result.Detail = "No pending plugin recovery"
		return result, nil
	}
	var pointer struct {
		Backup string `json:"backup"`
		State  string `json:"state"`
	}
	if err = json.Unmarshal(b, &pointer); err != nil {
		return result, err
	}
	if pointer.State != s.state {
		return result, fmt.Errorf("pending recovery belongs to another state root")
	}
	tx, err := s.readBackup(pointer.Backup)
	if err != nil {
		return result, err
	}
	result.Component = tx.Component
	result.BackupID = tx.ID
	if err = s.recoverTransaction(&tx); err != nil {
		return result, err
	}
	result.Status = "success"
	result.Detail = "Interrupted plugin update recovered"
	result.RestartRequired = true
	return result, nil
}
func (s *Service) recoverTransaction(tx *transaction) error {
	if _, err := s.readBackup(tx.ID); err != nil {
		return err
	}
	receipt, err := readOptional(s.receiptPath(tx.Plugin))
	if err != nil {
		return err
	}
	if !bytes.Equal(receipt, tx.ReceiptBefore) && !bytes.Equal(receipt, tx.ReceiptAfter) {
		return fmt.Errorf("recovery refused: receipt changed")
	}
	var current inventory
	if _, e := os.Lstat(s.root(tx.Plugin)); e == nil {
		current, err = scan(s.root(tx.Plugin))
		if err != nil {
			return err
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	if current != nil && !reflect.DeepEqual(current, tx.Before) && !reflect.DeepEqual(current, tx.After) {
		return fmt.Errorf("recovery refused: plugin changed")
	}
	if !reflect.DeepEqual(current, tx.Before) {
		old := filepath.Join(s.stageDir(tx.ID), "old")
		inv, err := scan(old)
		if err != nil || !reflect.DeepEqual(inv, tx.Before) {
			return fmt.Errorf("recovery refused: displaced plugin missing or changed")
		}
		if current != nil {
			if err = noReplace(s.root(tx.Plugin), filepath.Join(s.stageDir(tx.ID), "discard")); err != nil {
				return err
			}
		}
		if err = noReplace(old, s.root(tx.Plugin)); err != nil {
			return err
		}
	}
	if err = s.setReceipt(tx.Plugin, tx.ReceiptBefore); err != nil {
		return err
	}
	tx.Phase = "recovered"
	if err = writeJSON(filepath.Join(s.backupDir(tx.ID), "metadata.json"), tx); err != nil {
		return err
	}
	if err = os.Remove(s.pendingPath()); err != nil {
		return err
	}
	// Only known transaction staging is discarded after verified recovery.
	return os.RemoveAll(s.stageDir(tx.ID))
}

func (s *Service) Restore(ctx context.Context, id string, consent bool) (Result, error) {
	result := Result{Status: "failed", BackupID: id}
	if s.options.DryRun {
		result.Status = "success"
		result.Detail = "dry-run: no restore performed"
		return result, nil
	}
	if !consent {
		return result, fmt.Errorf("explicit restore consent required")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	unlock, err := s.lock(false)
	if err != nil {
		return result, err
	}
	defer unlock()
	if err = s.noPending(); err != nil {
		return result, err
	}
	saved, err := s.readBackup(id)
	if err != nil {
		return result, err
	}
	result.Component = saved.Component
	if saved.Phase != "applied" {
		return result, fmt.Errorf("only successfully applied backups can be restored")
	}
	if err = s.unchanged(saved.Plugin, saved.After, saved.ReceiptAfter); err != nil {
		return result, err
	}
	token, err := newID()
	if err != nil {
		return result, err
	}
	stage := s.stageDir(token)
	if err = os.Mkdir(stage, 0700); err != nil {
		return result, err
	}
	pending := false
	defer func() {
		if !pending {
			os.RemoveAll(stage)
		}
	}()
	next := filepath.Join(stage, saved.Plugin)
	if err = copyTree(ctx.Err, filepath.Join(s.backupDir(id), "before"), next, saved.Before); err != nil {
		return result, err
	}
	if err = checkLocalAPI(next, saved.Plugin); err != nil {
		return result, err
	}
	tx := transaction{Backup: Backup{ID: token, Component: saved.Component, Phase: "prepared"}, Schema: 1, Home: s.options.Home, Plugins: s.options.PluginsDir, Plugin: saved.Plugin, Before: saved.After, After: saved.Before, ReceiptBefore: saved.ReceiptAfter, ReceiptAfter: saved.ReceiptBefore}
	result, err = s.publish(ctx, &tx, next, &pending)
	if err == nil {
		result.Status = "success"
		result.Detail = "Plugin backup restored"
	}
	return result, err
}
