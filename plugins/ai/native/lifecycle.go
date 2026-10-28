//go:build linux

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/AtomyCloud/atm/internal/install"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func newer(a, b string) bool {
	aa, bb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < 3; i++ {
		x, _ := strconv.Atoi(aa[i])
		y, _ := strconv.Atoi(bb[i])
		if x != y {
			return x > y
		}
	}
	return false
}
func (b *backend) install(ctx context.Context, o options) error {
	_, current, err := b.inspect(ctx, o.provider, true)
	if err != nil {
		return err
	}
	if b.req.Operation == "update" && current == "" {
		return fmt.Errorf("no selected owned version to update")
	}
	a, err := b.resolve(ctx, o.provider, o.version)
	if err != nil {
		return err
	}
	if b.req.Operation == "update" && a.Version != current && !newer(a.Version, current) {
		return fmt.Errorf("update refuses downgrade; choose exact install/use explicitly")
	}
	target := filepath.Join(b.root(o.provider), "versions", a.Version)
	if _, err = os.Lstat(target); err == nil {
		rec, e := b.verifyVersion(ctx, o.provider, a.Version, true)
		if e != nil {
			return e
		}
		if rec.Artifact != a {
			return fmt.Errorf("existing version has different artifact metadata; preserved")
		}
		return b.activate(ctx, o.provider, a.Version)
	} else if !os.IsNotExist(err) {
		return err
	}
	stage, err := os.MkdirTemp(filepath.Dir(b.root(o.provider)), ".atm-ai-stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	versionDir := filepath.Join(stage, "version")
	if err = os.Mkdir(versionDir, 0700); err != nil {
		return err
	}
	tool := filepath.Join(versionDir, "tool")
	if a.Format == "appimage" {
		err = b.downloadAppImage(ctx, a, tool)
	} else {
		err = b.extractBinary(ctx, a, stage, tool)
	}
	if err != nil {
		return err
	}
	if err = validateELF(tool, b.arch, a.Format == "appimage"); err != nil {
		return err
	}
	f, err := regular(tool, 0755, maxBinary)
	if err != nil {
		return err
	}
	h := sha256.New()
	size, err := io.Copy(h, io.LimitReader(contextReader{ctx, f}, maxBinary+1))
	f.Close()
	if err != nil {
		return err
	}
	if size == 0 || size > maxBinary {
		return fmt.Errorf("binary size exceeds limit")
	}
	rec := receipt{1, "ai", a, hex.EncodeToString(h.Sum(nil)), size}
	if err = writeJSON(filepath.Join(versionDir, "receipt.json"), rec); err != nil {
		return err
	}
	if err = b.boundary("verified"); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = b.initialize(o.provider); err != nil {
		return err
	}
	if err = unix.Renameat2(unix.AT_FDCWD, versionDir, unix.AT_FDCWD, target, unix.RENAME_NOREPLACE); err != nil {
		return err
	}
	if err = b.boundary("published"); err != nil {
		return fmt.Errorf("version staged inactive; activation not committed: %w", err)
	}
	return b.activate(ctx, o.provider, a.Version)
}
func (b *backend) extractBinary(ctx context.Context, a artifact, stage, tool string) error {
	client := *b.client
	client.Transport = exactSizeTransport{base: client.Transport, size: a.Size}
	manager := install.Manager{
		Root: filepath.Join(stage, "archive-store"), PluginID: "ai", Client: &client,
		Limits: install.Limits{DownloadBytes: a.Size, ExpandedBytes: 1 << 30, FileBytes: maxBinary, Entries: 10000},
	}
	spec := install.Spec{Version: a.Version, URL: a.URL, SHA256: a.SHA256, Format: "tar.gz", Origins: []string{"https://github.com", "https://release-assets.githubusercontent.com", "https://objects.githubusercontent.com", "https://github-releases.githubusercontent.com"}}
	result, err := manager.Install(ctx, spec)
	if err != nil {
		return err
	}
	// Only the documented standalone executable is published. README/sample config
	// entries remain in private staging, never replacing the user's configuration.
	f, err := regular(filepath.Join(result.Path, a.Entry), 0700, maxBinary)
	if err != nil {
		return err
	}
	defer f.Close()
	dst, err := os.OpenFile(tool, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	n, err := io.Copy(dst, io.LimitReader(contextReader{ctx, f}, maxBinary+1))
	if err == nil && (n == 0 || n > maxBinary) {
		err = fmt.Errorf("binary size limit")
	}
	if err == nil {
		err = dst.Chmod(0755)
	}
	if err == nil {
		err = dst.Sync()
	}
	ce := dst.Close()
	if err != nil {
		return err
	}
	return ce
}
func (b *backend) downloadAppImage(ctx context.Context, a artifact, tool string) error {
	dst, err := os.OpenFile(tool, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	h := sha256.New()
	err = b.fetch(ctx, a.Provider, a.URL, io.MultiWriter(dst, h), a.Size)
	if err == nil {
		info, e := dst.Stat()
		err = e
		if e == nil && (info.Size() != a.Size || hex.EncodeToString(h.Sum(nil)) != a.SHA256) {
			err = fmt.Errorf("AppImage size/checksum mismatch")
		}
	}
	if err == nil {
		err = dst.Chmod(0755)
	}
	if err == nil {
		err = dst.Sync()
	}
	ce := dst.Close()
	if err != nil {
		return err
	}
	return ce
}
func (b *backend) activate(ctx context.Context, id, v string) error {
	rec, err := b.verifyVersion(ctx, id, v, true)
	if err != nil {
		return err
	}
	previous, err := b.current(id)
	if err != nil {
		return err
	}
	if previous == v {
		b.log("Already active: " + v)
		return nil
	}
	stage, err := os.MkdirTemp(filepath.Dir(b.root(id)), ".atm-ai-select-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	link := filepath.Join(stage, "current")
	if err = os.Symlink("versions/"+v+"/tool", link); err != nil {
		return err
	}
	if err = b.boundary("before-activation"); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	checked, err := b.verifyVersion(ctx, id, v, true)
	if err != nil {
		return err
	}
	if checked != rec {
		return fmt.Errorf("version receipt changed before activation; selection preserved")
	}
	now, err := b.current(id)
	if err != nil {
		return err
	}
	if now != previous {
		return fmt.Errorf("selection changed before commit")
	}
	path := filepath.Join(b.root(id), "current")
	if previous == "" {
		err = unix.Renameat2(unix.AT_FDCWD, link, unix.AT_FDCWD, path, unix.RENAME_NOREPLACE)
	} else {
		err = os.Rename(link, path)
	}
	if err != nil {
		return err
	}
	b.log("Selected " + providers[id].name + " " + v + ". Run explicitly: " + path)
	b.log("No executable was run; PATH and user data are unchanged.")
	return nil
}
func (b *backend) remove(ctx context.Context, id, v string) error {
	current, err := b.current(id)
	if err != nil {
		return err
	}
	if current == v {
		return fmt.Errorf("active version cannot be removed; select another version or uninstall owned preview versions")
	}
	rec, err := b.verifyVersion(ctx, id, v, true)
	if err != nil {
		return err
	}
	source := filepath.Join(b.root(id), "versions", v)
	original, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(filepath.Dir(b.root(id)), ".atm-ai-remove-")
	if err != nil {
		return err
	}
	// Never recursively retry cleanup: failed removal may contain changed files.
	defer os.Remove(stage)
	detached := filepath.Join(stage, "version")
	if err = b.boundary("before-removal-detach"); err != nil {
		return err
	}
	if err = unix.Renameat2(unix.AT_FDCWD, source, unix.AT_FDCWD, detached, unix.RENAME_NOREPLACE); err != nil {
		return err
	}
	retained := func(err error) error {
		return fmt.Errorf("version detached; remaining files retained for inspection at %s: %w", stage, err)
	}
	if err = b.boundary("after-removal-detach"); err != nil {
		return retained(err)
	}
	now, err := os.Lstat(detached)
	if err != nil {
		return retained(err)
	}
	if !os.SameFile(original, now) {
		return retained(fmt.Errorf("version directory changed before removal"))
	}
	checked, err := b.verifyVersionAt(ctx, id, v, detached, true)
	if err != nil {
		return retained(err)
	}
	if checked != rec {
		return retained(fmt.Errorf("version receipt changed before removal"))
	}
	r, err := os.OpenRoot(detached)
	if err != nil {
		return retained(err)
	}
	defer r.Close()
	opened, err := r.Stat(".")
	if err != nil || !os.SameFile(original, opened) {
		return retained(fmt.Errorf("detached directory changed while opening"))
	}
	for _, name := range []string{"tool", "receipt.json"} {
		if err = ctx.Err(); err != nil {
			return retained(err)
		}
		if err = r.Remove(name); err != nil {
			return retained(err)
		}
		if err = b.boundary("removed-" + name); err != nil {
			return retained(err)
		}
	}
	if err = os.Remove(detached); err != nil {
		return retained(err)
	}
	b.log("Removed owned version " + v + "; user data retained")
	return nil
}
func (b *backend) uninstall(ctx context.Context, id string) error {
	vs, current, err := b.inspect(ctx, id, true)
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if current != "" {
		if err = os.Remove(filepath.Join(b.root(id), "current")); err != nil {
			return err
		}
	}
	// Once deselected, cancellation/failure may leave some retained inactive versions.
	// Do not report a rollback or remove any unverified/manual installation.
	for _, v := range vs {
		if err = b.remove(ctx, id, v); err != nil {
			return fmt.Errorf("uninstall incomplete; remaining inactive versions preserved: %w", err)
		}
	}
	b.log(label("preserved"))
	return nil
}
