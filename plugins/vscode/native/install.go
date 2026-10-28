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
	"strings"
)

func (b *backend) install(ctx context.Context, version string) error {
	current, err := b.current()
	if err != nil {
		return err
	}
	spec, err := b.resolve(ctx, version)
	if err != nil {
		return err
	}
	if b.req.Operation == "update" && current != "" && newer(strings.TrimPrefix(current, "code_"), spec.Version) {
		return fmt.Errorf("update would downgrade; use explicit install/use instead")
	}
	v := "code_" + spec.Version
	dest := filepath.Join(b.root, v)
	if _, err = os.Lstat(dest); err == nil {
		b.log("Existing version preserved")
		return b.activate(ctx, v)
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
	hash := sha256.New()
	res, err := b.request(ctx, "GET", spec.URL, true)
	if err != nil {
		f.Close()
		return err
	}
	if res.ContentLength >= 0 && res.ContentLength != spec.Size {
		res.Body.Close()
		f.Close()
		return fmt.Errorf("artifact size changed since HEAD")
	}
	n, err := io.Copy(io.MultiWriter(f, hash), io.LimitReader(contextReader{ctx, res.Body}, spec.Size+1))
	res.Body.Close()
	if err == nil && (n != spec.Size || hex.EncodeToString(hash.Sum(nil)) != spec.SHA256) {
		err = fmt.Errorf("official artifact size/SHA256 mismatch")
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
	payload := filepath.Join(stage, "payload")
	if err = os.Mkdir(payload, 0700); err != nil {
		return err
	}
	root, err := os.OpenRoot(payload)
	if err != nil {
		return err
	}
	prefix := "VSCode-" + b.platform
	err = extractArchive(ctx, archive, root, prefix)
	if err == nil {
		err = validatePayload(root, spec.Version, b.platform, spec.Commit)
	}
	root.Close()
	if err != nil {
		return err
	}
	files, err := inventory(ctx, payload)
	if err != nil {
		return err
	}
	data, err := json.Marshal(receipt{1, "vscode", spec, files})
	if err != nil {
		return err
	}
	if len(data) > 32<<20 {
		return fmt.Errorf("receipt exceeds limit")
	}
	if err = os.WriteFile(filepath.Join(payload, receiptName), data, 0600); err != nil {
		return err
	}
	if err = os.Mkdir(filepath.Join(payload, "data"), 0700); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if _, err = os.Lstat(dest); !os.IsNotExist(err) {
		return fmt.Errorf("installation destination appeared during download")
	}
	if err = os.Rename(payload, dest); err != nil {
		return err
	}
	b.log("Installed portable VS Code " + spec.Version + ". Extensions, integration and data migration are separate explicit actions.")
	b.log("Linux requires a working Electron sandbox. ATM never grants setuid permissions or disables the sandbox.")
	return b.activate(ctx, v)
}
