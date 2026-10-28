//go:build linux

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"golang.org/x/sys/unix"
)

func (b *backend) ollamaExecute(ctx context.Context, o options) error {
	switch b.req.Operation {
	case "install", "update":
		return b.ollamaInstall(ctx, o)
	case "use":
		return b.ollamaActivate(ctx, o.version)
	case "remove":
		return b.ollamaRemove(ctx, o.version)
	case "uninstall":
		return b.ollamaUninstall(ctx)
	}
	return fmt.Errorf("unsupported Ollama operation")
}

func (b *backend) ollamaGuidance() {
	b.log(label("ollama_scope"))
	p := shellQuote(filepath.Join(b.root("ollama"), "current", "bin", "ollama"))
	b.log(fmt.Sprintf(label("ollama_usage"), p, p, p))
}

func (b *backend) ollamaStatus(ctx context.Context) error {
	vs, current, err := b.ollamaInspect(ctx)
	if err != nil {
		return err
	}
	b.log(fmt.Sprintf(label("ollama_status"), len(vs), current))
	b.ollamaGuidance()
	return ctx.Err()
}

func (b *backend) ollamaInitialize(stage string) error {
	root := b.root("ollama")
	if _, err := os.Lstat(root); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	tmp := filepath.Join(stage, "root")
	if err := os.Mkdir(tmp, 0700); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(tmp, "owner.json"), ownership{1, "ai", "ollama"}); err != nil {
		return err
	}
	if err := os.Mkdir(filepath.Join(tmp, "versions"), 0700); err != nil {
		return err
	}
	return unix.Renameat2(unix.AT_FDCWD, tmp, unix.AT_FDCWD, root, unix.RENAME_NOREPLACE)
}

func (b *backend) ollamaInstall(ctx context.Context, o options) error {
	_, current, err := b.ollamaInspect(ctx)
	if err != nil {
		return err
	}
	if b.req.Operation == "update" && current == "" {
		return fmt.Errorf("no selected owned Ollama version to update")
	}
	a, err := b.resolve(ctx, "ollama", o.version)
	if err != nil {
		return err
	}
	if b.req.Operation == "update" && a.Version != current && !newer(a.Version, current) {
		return fmt.Errorf("Ollama update refuses downgrade; use explicit install/use")
	}
	target := b.ollamaVersion(a.Version)
	if _, err := os.Lstat(target); err == nil {
		rec, err := b.ollamaVerify(ctx, a.Version, target)
		if err != nil {
			return err
		}
		if rec.Artifact != a {
			return fmt.Errorf("Ollama artifact metadata changed; existing version preserved")
		}
		return b.ollamaActivate(ctx, a.Version)
	} else if !os.IsNotExist(err) {
		return err
	}
	stage, err := os.MkdirTemp(filepath.Dir(b.root("ollama")), ".atm-ollama-stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	archive := filepath.Join(stage, "bundle.tar.zst")
	f, err := os.OpenFile(archive, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	h := sha256.New()
	err = b.fetch(ctx, "ollama", a.URL, io.MultiWriter(f, h), a.Size)
	if err == nil {
		info, e := f.Stat()
		err = e
		if err == nil && (info.Size() != a.Size || hex.EncodeToString(h.Sum(nil)) != a.SHA256) {
			err = fmt.Errorf("Ollama compressed size/checksum mismatch")
		}
	}
	if err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err != nil {
		return err
	}
	if ce != nil {
		return ce
	}
	versionDir := filepath.Join(stage, "version")
	tree := filepath.Join(versionDir, "tree")
	if err = os.MkdirAll(tree, 0700); err != nil {
		return err
	}
	if err = ollamaExtract(ctx, archive, tree); err != nil {
		return err
	}
	if err = validateELF(filepath.Join(tree, "bin", "ollama"), b.arch, false); err != nil {
		return err
	}
	files, err := ollamaInventory(ctx, tree)
	if err != nil {
		return err
	}
	rec := ollamaReceipt{Schema: 1, Plugin: "ai", Artifact: a, Files: files}
	if err = ollamaWriteReceipt(versionDir, rec); err != nil {
		return err
	}
	if err = b.boundary("ollama-verified"); err != nil {
		return err
	}
	checked, err := b.ollamaVerify(ctx, a.Version, versionDir)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(rec, checked) {
		return fmt.Errorf("Ollama stage changed before publication")
	}
	if _, now, err := b.ollamaInspect(ctx); err != nil {
		return err
	} else if now != current {
		return fmt.Errorf("Ollama selection changed during staging")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = b.ollamaInitialize(stage); err != nil {
		return err
	}
	if err = unix.Renameat2(unix.AT_FDCWD, versionDir, unix.AT_FDCWD, target, unix.RENAME_NOREPLACE); err != nil {
		return err
	}
	if err = b.boundary("ollama-published"); err != nil {
		return fmt.Errorf("Ollama version published inactive; selection not committed: %w", err)
	}
	return b.ollamaActivate(ctx, a.Version)
}

func (b *backend) ollamaActivate(ctx context.Context, v string) error {
	_, previous, err := b.ollamaInspect(ctx)
	if err != nil {
		return err
	}
	rec, err := b.ollamaVerify(ctx, v, b.ollamaVersion(v))
	if err != nil {
		return err
	}
	stage, err := os.MkdirTemp(filepath.Dir(b.root("ollama")), ".atm-ollama-select-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	link := filepath.Join(stage, "current")
	if err = os.Symlink("versions/"+v+"/tree", link); err != nil {
		return err
	}
	stageInfo, err := os.Lstat(stage)
	if err != nil {
		return err
	}
	linkInfo, err := os.Lstat(link)
	if err != nil {
		return err
	}
	if err = b.boundary("ollama-before-activation"); err != nil {
		return err
	}
	if _, now, err := b.ollamaInspect(ctx); err != nil {
		return err
	} else if now != previous {
		return fmt.Errorf("Ollama selection changed before activation")
	}
	checked, err := b.ollamaVerify(ctx, v, b.ollamaVersion(v))
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(rec, checked) {
		return fmt.Errorf("Ollama receipt changed before activation")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = dirs(stage, false, true); err != nil {
		return err
	}
	stageNow, err := os.Lstat(stage)
	if err != nil || !os.SameFile(stageInfo, stageNow) {
		return fmt.Errorf("Ollama selection staging directory changed")
	}
	linkNow, err := os.Lstat(link)
	if err != nil || !os.SameFile(linkInfo, linkNow) || linkNow.Mode() != os.ModeSymlink|0777 || owner(linkNow) != uint32(os.Geteuid()) || !singleLink(linkNow) {
		return fmt.Errorf("Ollama staged selection link changed")
	}
	target, err := os.Readlink(link)
	if err != nil || target != "versions/"+v+"/tree" {
		return fmt.Errorf("Ollama staged selection target changed")
	}
	if now, err := b.ollamaCurrent(); err != nil {
		return err
	} else if now != previous {
		return fmt.Errorf("Ollama selection changed before commit")
	}
	if previous != v {
		if previous == "" {
			err = unix.Renameat2(unix.AT_FDCWD, link, unix.AT_FDCWD, filepath.Join(b.root("ollama"), "current"), unix.RENAME_NOREPLACE)
		} else {
			err = os.Rename(link, filepath.Join(b.root("ollama"), "current"))
		}
		if err != nil {
			return err
		}
	}
	b.log(fmt.Sprintf(label("ollama_selected"), v))
	b.ollamaGuidance()
	return nil
}

func (b *backend) ollamaRemove(ctx context.Context, v string) error {
	_, current, err := b.ollamaInspect(ctx)
	if err != nil {
		return err
	}
	if current == v {
		return fmt.Errorf("active Ollama version cannot be removed; select another or uninstall")
	}
	source := b.ollamaVersion(v)
	rec, err := b.ollamaVerify(ctx, v, source)
	if err != nil {
		return err
	}
	original, err := os.Lstat(source)
	if err != nil {
		return err
	}
	stage, err := os.MkdirTemp(filepath.Dir(b.root("ollama")), ".atm-ollama-remove-")
	if err != nil {
		return err
	}
	defer os.Remove(stage)
	detached := filepath.Join(stage, "version")
	if err = b.boundary("ollama-before-removal-detach"); err != nil {
		return err
	}
	if _, now, err := b.ollamaInspect(ctx); err != nil {
		return err
	} else if now != current {
		return fmt.Errorf("Ollama selection changed before removal")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = unix.Renameat2(unix.AT_FDCWD, source, unix.AT_FDCWD, detached, unix.RENAME_NOREPLACE); err != nil {
		return err
	}
	retained := func(err error) error {
		return fmt.Errorf("Ollama detached; remaining files retained for inspection at %s: %w", stage, err)
	}
	if err = b.boundary("ollama-after-removal-detach"); err != nil {
		return retained(err)
	}
	now, err := os.Lstat(detached)
	if err != nil || !os.SameFile(original, now) {
		return retained(fmt.Errorf("Ollama version directory changed during detach"))
	}
	checked, err := b.ollamaVerify(ctx, v, detached)
	if err != nil {
		return retained(err)
	}
	if !reflect.DeepEqual(rec, checked) {
		return retained(fmt.Errorf("Ollama receipt changed during detach"))
	}
	r, err := os.OpenRoot(detached)
	if err != nil {
		return retained(err)
	}
	defer r.Close()
	opened, err := r.Stat(".")
	if err != nil || !os.SameFile(original, opened) {
		return retained(fmt.Errorf("Ollama detached root changed"))
	}
	if err = b.ollamaDeleteTree(ctx, r, rec.Files); err != nil {
		return retained(err)
	}
	if err = ctx.Err(); err != nil {
		return retained(err)
	}
	remaining, err := ollamaReadReceipt(detached)
	if err != nil || !reflect.DeepEqual(rec, remaining) {
		return retained(fmt.Errorf("Ollama receipt changed during cleanup"))
	}
	if err = r.Remove("receipt.json"); err != nil {
		return retained(err)
	}
	if err = os.Remove(detached); err != nil {
		return retained(err)
	}
	b.log(fmt.Sprintf(label("ollama_removed"), v))
	return nil
}

func (b *backend) ollamaDeleteTree(ctx context.Context, r *os.Root, files map[string]ollamaEntry) error {
	// Explicit postorder deletion never recursively sweeps unrecorded files.
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		a, c := strings.Count(names[i], "/"), strings.Count(names[j], "/")
		if a != c {
			return a > c
		}
		return names[i] > names[j]
	})
	for _, name := range names {
		actual, _, err := ollamaEntryAt(ctx, r, "tree/"+name)
		if err != nil {
			return err
		}
		if actual != files[name] {
			return fmt.Errorf("Ollama entry changed before deletion: %s", name)
		}
		if err = r.Remove("tree/" + name); err != nil {
			return err
		}
		if err = b.boundary("ollama-removed-" + name); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return r.Remove("tree")
}

func (b *backend) ollamaUninstall(ctx context.Context) error {
	vs, current, err := b.ollamaInspect(ctx)
	if err != nil {
		return err
	}
	if err = b.boundary("ollama-before-uninstall-deselect"); err != nil {
		return err
	}
	now, selected, err := b.ollamaInspect(ctx)
	if err != nil {
		return err
	}
	if selected != current || !reflect.DeepEqual(vs, now) {
		return fmt.Errorf("Ollama installation changed before uninstall")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if current != "" {
		if err = os.Remove(filepath.Join(b.root("ollama"), "current")); err != nil {
			return err
		}
	}
	for _, v := range vs {
		if err = b.ollamaRemove(ctx, v); err != nil {
			return fmt.Errorf("Ollama uninstall incomplete; remaining inactive versions retained: %w", err)
		}
	}
	b.log(label("ollama_scope"))
	return nil
}
