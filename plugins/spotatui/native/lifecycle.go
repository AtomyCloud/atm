package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/AtomyCloud/atm/internal/catalog"
)

func (b *backend) preflight(ctx context.Context) error {
	if err := realDirs(b.root, false); err != nil && !os.IsNotExist(err) {
		return err
	}
	v, err := b.current()
	if err != nil {
		return err
	}
	if v != "" {
		if err = b.verifyOwned(ctx, v); err != nil {
			return err
		}
	}
	if b.location == "local" {
		return b.checkLocalEntry()
	}
	return nil
}
func (b *backend) install(ctx context.Context, o options, d dependencies) error {
	current, err := b.current()
	if err != nil {
		return err
	}
	spec, err := b.resolve(ctx, o.version)
	if err != nil {
		return err
	}
	if b.req.Operation == "update" && current != "" && newer(current, spec.Version) {
		return fmt.Errorf("update would downgrade; explicitly install/select an older version")
	}
	dest := filepath.Join(b.root, spec.Version)
	if _, err = os.Lstat(dest); err == nil {
		if err = b.verifyOwned(ctx, spec.Version); err != nil {
			return err
		}
		rec, err := b.readReceipt(spec.Version)
		if err != nil {
			return err
		}
		if rec.Artifact.SHA256 != spec.SHA256 || rec.Artifact.URL != spec.URL {
			return fmt.Errorf("same version now has different upstream bytes; preserving existing version")
		}
		if err = b.refresh(ctx, spec.Version, d); err != nil {
			return err
		}
		return b.activate(ctx, spec.Version)
	} else if !os.IsNotExist(err) {
		return err
	}
	stage, err := os.MkdirTemp(b.root, ".atm-stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	archive := filepath.Join(stage, "archive.tar.gz")
	f, err := os.OpenFile(archive, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	h := sha256.New()
	err = b.fetch(ctx, spec.URL, io.MultiWriter(f, h), spec.Size)
	if err == nil {
		i, e := f.Stat()
		if e != nil {
			err = e
		} else if i.Size() != spec.Size {
			err = fmt.Errorf("archive size mismatch")
		}
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if hex.EncodeToString(h.Sum(nil)) != spec.SHA256 {
		return fmt.Errorf("official archive SHA-256 mismatch")
	}
	payload := filepath.Join(stage, "payload")
	if err = os.Mkdir(payload, 0700); err != nil {
		return err
	}
	r, err := os.OpenRoot(payload)
	if err != nil {
		return err
	}
	err = extractArchive(ctx, archive, r)
	if err == nil {
		err = validatePayload(r, spec.Version, b.platform)
	}
	r.Close()
	if err != nil {
		return err
	}
	if err = writeSupport(ctx, payload, d); err != nil {
		return err
	}
	files, err := inventory(ctx, payload)
	if err != nil {
		return err
	}
	data, err := json.Marshal(receipt{1, "spotatui", spec, files})
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(payload, receiptName), data, 0600); err != nil {
		return err
	}
	if err = b.preflight(ctx); err != nil {
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
	to, err := os.Open(b.root)
	if err != nil {
		return err
	}
	defer to.Close()
	if err = catalog.PublishDirectory(from, to, "payload", spec.Version); err != nil {
		return err
	}
	// Publication survives activation failure. No old version is removed here.
	return b.activate(ctx, spec.Version)
}
func (b *backend) readReceipt(v string) (receipt, error) {
	var rec receipt
	data, _, exists, err := readProfile(filepath.Join(b.root, v, receiptName))
	if err != nil {
		return rec, err
	}
	if !exists {
		return rec, fmt.Errorf("missing ownership receipt")
	}
	err = json.Unmarshal(data, &rec)
	return rec, err
}
func (b *backend) refresh(ctx context.Context, v string, d dependencies) error {
	if err := b.verifyOwned(ctx, v); err != nil {
		return err
	}
	if err := d.validate(); err != nil {
		return err
	}
	dir := filepath.Join(b.root, v)
	rec, err := b.readReceipt(v)
	if err != nil {
		return err
	}
	depPath := filepath.Join(dir, dependencyFile)
	before, mode, exists, err := readProfile(depPath)
	if err != nil {
		return err
	}
	after, err := json.Marshal(d)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(after)
	rec.Files[dependencyFile] = entry{Mode: uint32(mode), Size: int64(len(after)), SHA256: hex.EncodeToString(sum[:])}
	recPath := filepath.Join(dir, receiptName)
	oldRec, recMode, recExists, err := readProfile(recPath)
	if err != nil {
		return err
	}
	newRec, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	rollback, err := applyProfiles(ctx, []profilePlan{{depPath, before, after, mode, exists}, {recPath, oldRec, newRec, recMode, recExists}})
	if err != nil {
		return err
	}
	if err = b.verifyOwned(ctx, v); err != nil {
		if undo := rollback(); undo != nil {
			return fmt.Errorf("%v; refresh rollback: %w", err, undo)
		}
		return err
	}
	return nil
}
func (b *backend) activate(ctx context.Context, v string) error {
	if err := b.verifyOwned(ctx, v); err != nil {
		return err
	}
	if err := b.preflight(ctx); err != nil {
		return err
	}
	previous, err := b.current()
	if err != nil {
		return err
	}
	stage, err := os.MkdirTemp(b.root, ".atm-switch-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	link := filepath.Join(stage, "current")
	if err = os.Symlink(v, link); err != nil {
		return err
	}
	rollback := func() error { return nil }
	if b.location == "local" {
		rollback, err = b.publishLocal(ctx)
		if err != nil {
			return err
		}
	}
	fail := func(e error) error {
		if undo := rollback(); undo != nil {
			return fmt.Errorf("%v; local entry rollback: %w", e, undo)
		}
		return e
	}
	now, err := b.current()
	if err != nil {
		return fail(err)
	}
	if now != previous {
		return fail(fmt.Errorf("selection changed during activation"))
	}
	if err = ctx.Err(); err != nil {
		return fail(err)
	}
	if previous == "" {
		err = os.Symlink(v, filepath.Join(b.root, "current"))
	} else {
		err = os.Rename(link, filepath.Join(b.root, "current"))
	}
	if err != nil {
		return fail(err)
	}
	b.log("Selected native spotatui " + v + " at " + b.location + ". PATH integration remains explicit.")
	return nil
}
func (b *backend) remove(ctx context.Context, v string) error {
	current, err := b.current()
	if err != nil {
		return err
	}
	if current == v {
		return fmt.Errorf("cannot remove current version; select another first")
	}
	if err = b.verifyOwned(ctx, v); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return b.deleteVersion(ctx, v)
}
func (b *backend) deleteVersion(ctx context.Context, v string) error {
	if err := b.verifyOwned(ctx, v); err != nil {
		return err
	}
	// Delete only receipt-listed files; never recursively remove user additions.
	rec, err := b.readReceipt(v)
	if err != nil {
		return err
	}
	r, err := os.OpenRoot(filepath.Join(b.root, v))
	if err != nil {
		return err
	}
	defer r.Close()
	for name, e := range rec.Files {
		if os.FileMode(e.Mode).IsRegular() {
			if err = r.Remove(name); err != nil {
				return err
			}
		}
	}
	for _, name := range []string{"bin", "libexec", "shims"} {
		if err = r.Remove(name); err != nil {
			return err
		}
	}
	if err = r.Remove(receiptName); err != nil {
		return err
	}
	return os.Remove(filepath.Join(b.root, v))
}
func (b *backend) uninstall(ctx context.Context) error {
	vs, err := b.versionDirs(false)
	if err != nil {
		return err
	}
	owned := []string{}
	for _, v := range vs {
		if !b.hasReceipt(v) {
			b.log("Preserving unowned version " + v)
			continue
		}
		if err = b.verifyOwned(ctx, v); err != nil {
			return err
		}
		owned = append(owned, v)
	}
	current, err := b.current()
	if err != nil {
		return err
	}
	if err = b.removeIntegration(ctx); err != nil {
		return err
	}
	if b.location == "local" {
		if err = b.removeLocal(); err != nil {
			return err
		}
	}
	if current != "" {
		if err = os.Remove(filepath.Join(b.root, "current")); err != nil {
			return err
		}
	}
	for _, v := range owned {
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = b.deleteVersion(ctx, v); err != nil {
			return err
		}
	}
	b.log("Removed unchanged native-owned versions only; legacy/external installations, config, data, dependencies and unrelated files preserved")
	return nil
}
