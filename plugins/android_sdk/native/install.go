package main

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var licenseIDRE = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)

func (b *backend) checkLatest(require bool) error {
	directory := filepath.Join(b.root, "cmdline-tools")
	if err := realDirs(directory, false); os.IsNotExist(err) && !require {
		return nil
	} else if err != nil {
		return err
	}
	target, err := os.Readlink(filepath.Join(directory, "latest"))
	if os.IsNotExist(err) && !require {
		return nil
	}
	if err != nil || target != toolsDirectory {
		return fmt.Errorf("unowned/changed cmdline-tools/latest preserved")
	}
	if _, err = b.readReceipt(toolsID); err != nil {
		return err
	}
	return nil
}
func (b *backend) ensureLatest() error {
	if _, err := b.readReceipt(toolsID); err != nil {
		return err
	}
	if err := b.checkLatest(false); err != nil {
		return err
	}
	link := filepath.Join(b.root, "cmdline-tools/latest")
	if _, err := os.Lstat(link); os.IsNotExist(err) {
		return os.Symlink(toolsDirectory, link)
	}
	return nil
}
func (b *backend) install(ctx context.Context, plan installPlan, update bool) error {
	if plan.Root != b.root || len(plan.Packages) == 0 || len(plan.Packages) > 32 {
		return fmt.Errorf("invalid installation plan")
	}
	if err := b.checkLatest(false); err != nil {
		return err
	}
	// Preflight every destination before publishing any package.
	for _, s := range plan.Packages {
		dir, err := packageDir(s.ID)
		if err != nil || dir != s.Directory {
			return fmt.Errorf("invalid planned package destination")
		}
		full := filepath.Join(b.root, dir)
		if _, err = os.Lstat(full); err == nil {
			rec, err := b.verifyOwned(ctx, s.ID)
			if err != nil {
				return err
			}
			cmp := compare(rec.Spec.Revision, s.Revision)
			if cmp > 0 {
				return fmt.Errorf("refusing package downgrade: %s", s.ID)
			}
			if cmp == 0 && (rec.Spec.ChecksumType != s.ChecksumType || rec.Spec.Checksum != s.Checksum) {
				return fmt.Errorf("same-revision artifact changed; preserve and inspect %s", s.ID)
			}
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	if err := b.recordLicenses(ctx, plan); err != nil {
		return err
	}
	for _, s := range plan.Packages {
		if err := ctx.Err(); err != nil {
			return err
		}
		if rec, err := b.readReceipt(s.ID); err == nil && compare(rec.Spec.Revision, s.Revision) == 0 {
			b.log("Already installed and verified: " + s.ID)
			continue
		}
		if err := b.installPackage(ctx, s); err != nil {
			return fmt.Errorf("package %s failed; earlier verified packages may already be installed: %w", s.ID, err)
		}
		if s.ID == toolsID {
			if err := b.ensureLatest(); err != nil {
				return err
			}
		}
	}
	if _, err := b.readReceipt(toolsID); err == nil {
		if err = b.ensureLatest(); err != nil {
			return err
		}
	}
	b.javaStatus()
	b.log("SDK packages installed natively; Java/sdkmanager were not executed. PATH integration is a separate explicit action.")
	return nil
}
func (b *backend) recordLicenses(ctx context.Context, plan installPlan) error {
	dir := filepath.Join(b.root, "licenses")
	if err := realDirs(dir, true); err != nil {
		return err
	}
	seen := map[string]string{}
	var plans []profilePlan
	for _, s := range plan.Packages {
		l := s.License
		if old, ok := seen[l.ID]; ok {
			if old != l.Text {
				return fmt.Errorf("conflicting license texts")
			}
			continue
		}
		seen[l.ID] = l.Text
		file := filepath.Join(dir, l.ID)
		before, mode, exists, err := readProfile(file)
		if err != nil {
			return err
		}
		token := licenseHash(l.Text)
		present := false
		for _, line := range strings.Split(string(before), "\n") {
			if line != "" && !sha1RE.MatchString(line) {
				return fmt.Errorf("unrecognized license file contents; preserving %s", file)
			}
			if line == token {
				present = true
			}
		}
		if present {
			continue
		}
		after := append([]byte(nil), before...)
		if len(after) > 0 && after[len(after)-1] != '\n' {
			after = append(after, '\n')
		}
		after = append(after, []byte(token+"\n")...)
		plans = append(plans, profilePlan{file, before, after, mode, exists})
	}
	_, err := applyProfiles(ctx, plans)
	return err
}
func (b *backend) installPackage(ctx context.Context, s packageSpec) error {
	stage, err := os.MkdirTemp(b.root, ".atm-stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	archive := filepath.Join(stage, "archive.zip")
	f, err := os.OpenFile(archive, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	var published hash.Hash
	switch s.ChecksumType {
	case "sha1":
		published = sha1.New()
	case "sha256":
		published = sha256.New()
	default:
		f.Close()
		return fmt.Errorf("unsupported published checksum")
	}
	local := sha256.New()
	res, err := b.get(ctx, s.URL)
	if err != nil {
		f.Close()
		return err
	}
	if res.ContentLength >= 0 && res.ContentLength != s.Size {
		res.Body.Close()
		f.Close()
		return fmt.Errorf("archive size differs from Google metadata")
	}
	n, err := io.Copy(io.MultiWriter(f, published, local), io.LimitReader(contextReader{ctx, res.Body}, s.Size+1))
	res.Body.Close()
	if err == nil && (n != s.Size || hex.EncodeToString(published.Sum(nil)) != s.Checksum) {
		err = fmt.Errorf("official checksum/size mismatch")
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
	err = extractZIP(ctx, archive, root)
	if err == nil {
		err = validatePayload(root, s)
	}
	if err == nil {
		var data []byte
		data, err = localMetadata(s)
		if err == nil {
			err = root.WriteFile("package.xml", data, 0600)
		}
	}
	root.Close()
	if err != nil {
		return err
	}
	files, err := inventory(ctx, payload)
	if err != nil {
		return err
	}
	data, err := json.Marshal(receipt{1, "android_sdk", s, hex.EncodeToString(local.Sum(nil)), files})
	if err != nil {
		return err
	}
	if len(data) > 64<<20 {
		return fmt.Errorf("package receipt too large")
	}
	if err = os.WriteFile(filepath.Join(payload, receiptName), data, 0600); err != nil {
		return err
	}
	destination := filepath.Join(b.root, s.Directory)
	if err = realDirs(filepath.Dir(destination), true); err != nil {
		return err
	}
	backup := ""
	if _, err = os.Lstat(destination); err == nil {
		if _, err = b.verifyOwned(ctx, s.ID); err != nil {
			return err
		}
		history := filepath.Join(filepath.Dir(b.root), ".atm-sdk-history")
		if err = realDirs(history, true); err != nil {
			return err
		}
		container, err := os.MkdirTemp(history, "package-")
		if err != nil {
			return err
		}
		backup = filepath.Join(container, "previous")
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = os.Rename(destination, backup); err != nil {
			return err
		}
		b.log("Previous package retained: " + backup)
	} else if !os.IsNotExist(err) {
		return err
	}
	rollback := func(cause error) error {
		if backup != "" {
			if _, e := os.Lstat(destination); !os.IsNotExist(e) {
				return fmt.Errorf("%v; destination appeared, previous package retained: %s", cause, backup)
			}
			if e := os.Rename(backup, destination); e != nil {
				return fmt.Errorf("%v; restore previous package from %s: %w", cause, backup, e)
			}
		}
		return cause
	}
	if err = ctx.Err(); err != nil {
		return rollback(err)
	}
	if _, err = os.Lstat(destination); !os.IsNotExist(err) {
		return rollback(fmt.Errorf("package destination appeared"))
	}
	if err = os.Rename(payload, destination); err != nil {
		return rollback(err)
	}
	b.log("Installed " + s.ID + " revision " + s.Revision.String())
	return nil
}
