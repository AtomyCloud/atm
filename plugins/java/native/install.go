package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

func (b *backend) versionDirs(validate bool) ([]string, error) {
	if err := realDirs(b.root, false); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	out := []string{}
	for _, vendor := range vendors {
		dir := filepath.Join(b.root, vendor)
		if err := realDirs(dir, false); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return nil, err
		}
		f, err := os.Open(dir)
		if err != nil {
			return nil, err
		}
		entries, readErr := f.ReadDir(10001)
		f.Close()
		if readErr != nil && readErr != io.EOF {
			return nil, readErr
		}
		if len(entries) > 10000 || len(out)+len(entries) > 10000 {
			return nil, fmt.Errorf("too many Java version entries")
		}
		for _, e := range entries {
			if !e.IsDir() || !safeLabel(e.Name()) {
				continue
			}
			target := vendor + "/" + e.Name()
			if !validate || b.validInstalled(target) == nil {
				out = append(out, target)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		av, aa, _ := strings.Cut(out[i], "/")
		bv, bb, _ := strings.Cut(out[j], "/")
		if av != bv {
			return av < bv
		}
		if actualVersionRE.MatchString(aa) && actualVersionRE.MatchString(bb) {
			return newer(aa, bb)
		}
		return aa < bb
	})
	return out, nil
}
func (b *backend) actualVersion(target string) (string, error) {
	if err := b.validInstalled(target); err != nil {
		return "", err
	}
	r, err := os.OpenRoot(filepath.Join(b.root, target))
	if err != nil {
		return "", err
	}
	defer r.Close()
	return validatePayload(r, b.platform)
}
func (b *backend) updateOptions(o options) (options, error) {
	if o.explicitVendor || o.url != "" {
		return o, nil
	}
	current, err := b.current()
	if err != nil {
		return o, err
	}
	if current == "" {
		return o, fmt.Errorf("update requires an active JDK or explicit --vendor")
	}
	vendor, _, _ := strings.Cut(current, "/")
	if vendor == "custom" {
		return o, fmt.Errorf("custom update requires explicit --vendor custom --version LABEL --url HTTPS_URL --sha256 HASH")
	}
	args := []string{"--vendor", vendor}
	if o.explicitVersion {
		args = append(args, "--version", o.version)
	} else {
		actual, err := b.actualVersion(current)
		if err != nil {
			return o, err
		}
		major := strings.FieldsFunc(actual, func(r rune) bool { return r == '.' || r == '_' || r == '+' || r == '-' })[0]
		args = append(args, "--version", major)
	}
	return parseOptions(args)
}
func (b *backend) install(ctx context.Context, o options) error {
	current, err := b.current()
	if err != nil {
		return err
	}
	if b.req.Operation == "update" {
		o, err = b.updateOptions(o)
		if err != nil {
			return err
		}
	}
	spec, err := b.resolve(ctx, o)
	if err != nil {
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
	digest := sha256.New()
	maximum := limits.DownloadBytes
	if spec.Size > 0 {
		maximum = spec.Size
	}
	b.log("Downloading checksummed JDK for " + o.vendor + " " + o.version)
	resolved, size, downloadErr := b.fetch(ctx, spec.URL, spec.policy(), io.MultiWriter(f, digest), maximum)
	if downloadErr == nil {
		downloadErr = f.Sync()
	}
	closeErr := f.Close()
	if downloadErr != nil {
		return downloadErr
	}
	if closeErr != nil {
		return closeErr
	}
	if size <= 0 || (spec.Size > 0 && size != spec.Size) {
		return fmt.Errorf("JDK archive size mismatch")
	}
	spec.Size = size
	spec.ResolvedURL = resolved
	if hex.EncodeToString(digest.Sum(nil)) != spec.SHA256 {
		return fmt.Errorf("JDK archive SHA-256 mismatch")
	}
	if spec.ChecksumFilename != "" {
		u, _ := url.Parse(resolved)
		if path.Base(u.Path) != spec.ChecksumFilename {
			return fmt.Errorf("sidecar filename does not match resolved JDK archive")
		}
	}
	payload := filepath.Join(stage, "payload")
	if err = os.Mkdir(payload, 0700); err != nil {
		return err
	}
	root, err := os.OpenRoot(payload)
	if err != nil {
		return err
	}
	spec.StripPrefix, err = extractArchive(ctx, archive, root)
	if err == nil {
		spec.ActualVersion, err = validatePayload(root, b.platform)
	}
	root.Close()
	if err != nil {
		return err
	}
	if !spec.Custom && strings.Contains(spec.ActualVersion, "-ea") {
		return fmt.Errorf("official GA JDK returned an early-access payload")
	}
	if o.vendor != "custom" && !compatible(spec.ActualVersion, spec.Requested) {
		return fmt.Errorf("JAVA_VERSION %s is incompatible with requested %s", spec.ActualVersion, spec.Requested)
	}
	version := spec.ActualVersion
	if o.vendor == "custom" {
		version = o.version
	}
	target := o.vendor + "/" + version
	spec.Version = target
	if _, err = normalizeTarget(target); err != nil {
		return err
	}
	if b.req.Operation == "update" && strings.HasPrefix(current, o.vendor+"/") {
		previous, err := b.actualVersion(current)
		if err != nil {
			return err
		}
		if !strings.Contains(previous, "-ea") && !strings.Contains(spec.ActualVersion, "-ea") && newer(previous, spec.ActualVersion) {
			return fmt.Errorf("update would downgrade active JAVA_VERSION %s", previous)
		}
	}
	dest := filepath.Join(b.root, target)
	if _, err = os.Lstat(dest); err == nil {
		if !b.hasReceipt(target) {
			return fmt.Errorf("existing legacy/unowned %s is preserved; select it with use instead", target)
		}
		if err = b.verifyOwned(ctx, target); err != nil {
			return err
		}
		rec, err := b.readReceipt(target)
		if err != nil {
			return err
		}
		if rec.Artifact.SHA256 != spec.SHA256 || rec.Artifact.ActualVersion != spec.ActualVersion {
			return fmt.Errorf("different artifact already exists at %s; no replacement or update was performed; use a new custom label for an explicitly pinned alternative", target)
		}
		b.log("Identical verified JDK already installed; selecting " + target)
		return b.activate(ctx, target)
	} else if !os.IsNotExist(err) {
		return err
	}
	files, err := inventory(ctx, payload)
	if err != nil {
		return err
	}
	data, err := json.Marshal(receipt{1, "java", spec, files})
	if err != nil {
		return err
	}
	if len(data) > 8<<20 {
		return fmt.Errorf("Java receipt exceeds limit")
	}
	if err = os.WriteFile(filepath.Join(payload, receiptName), data, 0600); err != nil {
		return err
	}
	if err = realDirs(filepath.Join(b.root, o.vendor), true); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if _, err = os.Lstat(dest); !os.IsNotExist(err) {
		return fmt.Errorf("JDK destination appeared during installation")
	}
	if err = os.Rename(payload, dest); err != nil {
		return err
	}
	return b.activate(ctx, target)
}
