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
)

func (b *backend) install(ctx context.Context, a options) error {
	if err := a.validate(); err != nil {
		return err
	}
	v := a.dir()
	dest := filepath.Join(b.root, v)
	dirs, err := b.versionDirs(false)
	if err != nil {
		return err
	}
	for _, dir := range dirs {
		other, err := b.spec(dir)
		if err == nil && other.target() == a.target() && dir != v {
			return fmt.Errorf("id/version already belongs to another installation; refusing ambiguity")
		}
	}
	if _, err := os.Lstat(dest); err == nil {
		if err = b.validInstalled(v); err != nil {
			return err
		}
		old, err := b.spec(v)
		if err != nil {
			return err
		}
		if old.ID != a.ID {
			return fmt.Errorf("directory label belongs to another installation")
		}
		if b.hasReceipt(v) {
			rec, err := b.readReceipt(v)
			if err != nil {
				return err
			}
			if rec.Artifact != a {
				return fmt.Errorf("different artifact for existing installation; preserving it")
			}
		}
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
	f, err := os.OpenFile(archive, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	b.log("Downloading " + a.Name + " " + a.Version)
	err = b.fetch(ctx, a.URL, io.MultiWriter(f, hash))
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
	if hex.EncodeToString(hash.Sum(nil)) != a.SHA256 {
		return fmt.Errorf("Android Studio SHA-256 mismatch")
	}
	payload := filepath.Join(stage, "payload")
	if err = os.Mkdir(payload, 0700); err != nil {
		return err
	}
	root, err := os.OpenRoot(payload)
	if err != nil {
		return err
	}
	err = extractArchive(ctx, archive, root, "android-studio")
	var id identity
	if err == nil {
		id, err = validatePayload(root, a)
	}
	root.Close()
	if err != nil {
		return err
	}
	files, err := inventory(ctx, payload)
	if err != nil {
		return err
	}
	data, err := json.Marshal(receipt{Schema: 1, Plugin: "android_studio", Artifact: a, Identity: id, Files: files})
	if err != nil {
		return err
	}
	if len(data) > 32<<20 {
		return fmt.Errorf("receipt exceeds limit")
	}
	if err = os.WriteFile(filepath.Join(payload, receiptName), data, 0600); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if _, err = os.Lstat(dest); !os.IsNotExist(err) {
		return fmt.Errorf("destination appeared during installation")
	}
	if err = os.Rename(payload, dest); err != nil {
		return err
	}
	return b.activate(ctx, v)
}
