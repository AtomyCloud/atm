//go:build linux

package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const codexDesktopTrust = "official-https-origin; local-sha256-continuity-only"

type codexDesktopJournal struct {
	Schema                                     int
	Operation                                  string
	Baseline                                   codexPackageRecord
	Plugin, Provider, Phase, LastPhase         string
	URL, Package, Version, Arch, SHA256, Trust string
	Size                                       int64
	Input, Staging                             string
	GuardSHA256, GuardConfigSHA256, GuardNonce string
}

func codexDesktopPhase(phase string) bool {
	switch phase {
	case "prepared", "downloaded", "metadata-verified", "root-mktemp", "root-copy", "root-hash", "root-metadata", "root-guard-copy", "root-guard-hash", "root-guard-config", "apt-running", "installed", "root-cleanup", "complete", "no-update", "failed", "unknown":
		return true
	}
	return false
}

func codexDesktopFinished(rec *codexDesktopJournal) bool {
	return rec == nil || rec.Phase == "complete" || rec.Phase == "no-update"
}

func (r *codexDesktopJournal) UnmarshalJSON(data []byte) error {
	type plain codexDesktopJournal
	var v plain
	if err := decode(data, &v, true); err != nil {
		return err
	}
	var optional []string
	if v.Schema == 1 {
		optional = []string{"Operation", "Baseline"}
	}
	if err := codexCanonicalJSON(data, v, optional...); err != nil {
		return err
	}
	*r = codexDesktopJournal(v)
	return nil
}

func (b *backend) codexDesktopRecord() (*codexDesktopJournal, error) {
	root := b.root(codexDesktopSystem)
	if err := dirs(root, false, true); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var own ownership
	if err := readJSON(filepath.Join(root, "owner.json"), &own); err != nil {
		return nil, err
	}
	if own != (ownership{1, "ai", codexDesktopSystem}) {
		return nil, fmt.Errorf("foreign ChatGPT Desktop journal preserved")
	}
	es, err := entries(root, 2)
	if err != nil {
		return nil, err
	}
	for _, e := range es {
		if e.Name() != "owner.json" && e.Name() != "journal.json" {
			return nil, fmt.Errorf("unknown/interrupted ChatGPT Desktop journal entry preserved")
		}
	}
	var rec codexDesktopJournal
	if err = readJSON(filepath.Join(root, "journal.json"), &rec); err != nil {
		return nil, err
	}
	want, urlErr := codexDesktopURL(rec.Arch)
	legacy := rec.Schema == 1 && rec.Operation == "" && rec.Baseline == (codexPackageRecord{})
	if (!legacy && (rec.Schema != 2 || !codexBaselineValid(rec.Operation, rec.Baseline, rec.Arch))) || rec.Plugin != "ai" || rec.Provider != codexDesktopSystem || rec.Package != "chatgpt" || urlErr != nil || rec.URL != want || rec.Trust != codexDesktopTrust || !codexDesktopPhase(rec.Phase) || !codexDesktopPhase(rec.LastPhase) || rec.Size < 0 || rec.Size > codexDesktopMaxDownload {
		return nil, fmt.Errorf("invalid ChatGPT Desktop operation journal")
	}
	if rec.Phase == "no-update" || rec.LastPhase == "no-update" {
		if rec.Schema != 2 || rec.Operation != "update" || rec.Staging != "" || rec.GuardSHA256 != "" || rec.GuardConfigSHA256 != "" || rec.GuardNonce != "" || rec.Phase == "no-update" && rec.LastPhase != "no-update" {
			return nil, fmt.Errorf("invalid no-update journal evidence")
		}
	}
	if (rec.Size == 0) != (rec.SHA256 == "") || rec.SHA256 != "" && !validSHA(rec.SHA256) || rec.Version != "" && !validDEBVersion(rec.Version) {
		return nil, fmt.Errorf("invalid ChatGPT Desktop journal evidence")
	}
	for _, hash := range []string{rec.GuardSHA256, rec.GuardConfigSHA256, rec.GuardNonce} {
		if hash != "" && !validSHA(hash) {
			return nil, fmt.Errorf("invalid APT guard journal evidence")
		}
	}
	if rec.LastPhase != "prepared" && (rec.Size == 0 || rec.LastPhase != "downloaded" && rec.Version == "") {
		return nil, fmt.Errorf("incomplete ChatGPT Desktop journal evidence")
	}
	// Input and Staging are observations, deliberately never cleanup authority.
	return &rec, nil
}

func (b *backend) saveCodexDesktopRecord(rec codexDesktopJournal) error {
	root := b.root(codexDesktopSystem)
	if err := dirs(root, false, true); err != nil {
		return err
	}
	stage := filepath.Join(root, ".journal-next")
	if err := writeJSON(stage, rec); err != nil {
		return err
	}
	if err := os.Rename(stage, filepath.Join(root, "journal.json")); err != nil {
		return err
	}
	f, err := os.Open(root)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func (b *backend) logCodexPackageRecord() (codexPackageRecord, error) {
	rec, err := b.codexDesktopTools().packageRecord()
	if err != nil {
		b.log(fmt.Sprintf(label("codex_desktop_unknown"), err))
	} else if rec.Present {
		b.log(fmt.Sprintf(label("codex_desktop_observed"), rec.Status, rec.Version, rec.Arch))
	} else {
		b.log(label("codex_desktop_absent"))
	}
	return rec, err
}

func (b *backend) codexDesktopStatus() {
	b.logCodexPackageRecord()
	if rec, err := b.codexDesktopRecord(); err != nil {
		b.log(fmt.Sprintf(label("codex_desktop_journal_unknown"), err))
	} else if rec != nil {
		b.log(fmt.Sprintf(label("codex_desktop_journal"), rec.Phase, rec.LastPhase, rec.Version, rec.Size, rec.SHA256))
	}
}

func (b *backend) codexDesktopMenu(ctx context.Context) ([]choice, error) {
	b.log(providers[codexDesktopSystem].docs)
	b.log(label("codex_desktop_scope"))
	b.log(label("codex_desktop_consent"))
	b.log(label("codex_desktop_guard"))
	pkg, pkgErr := b.logCodexPackageRecord()
	back := choice{ID: "back", Label: label("back"), Operation: "menu"}
	rec, err := b.codexDesktopRecord()
	if err != nil {
		b.log(fmt.Sprintf(label("codex_desktop_journal_unknown"), err))
	}
	if rec != nil {
		b.log(fmt.Sprintf(label("codex_desktop_journal"), rec.Phase, rec.LastPhase, rec.Version, rec.Size, rec.SHA256))
	}
	_, archErr := codexDesktopURL(b.arch)
	_, lockErr := os.Lstat(b.root(codexDesktopSystem) + ".lock")
	if pkgErr != nil || err != nil || !codexDesktopFinished(rec) || archErr != nil || !os.IsNotExist(lockErr) {
		return []choice{back}, ctx.Err()
	}
	if pkg.Present {
		if !codexUpdateEligible(pkg, b.arch) {
			return []choice{back}, ctx.Err()
		}
		return []choice{{ID: "update", Label: label("codex_desktop_update"), Operation: "update", Arguments: []string{"--provider", codexDesktopSystem, "--trust-download", "--accept-system-packages"}}, back}, ctx.Err()
	}
	return []choice{{ID: "install", Label: label("codex_desktop_install"), Operation: "install", Arguments: []string{"--provider", codexDesktopSystem, "--trust-download", "--accept-system-packages"}}, back}, ctx.Err()
}

func (b *backend) installCodexDesktop(ctx context.Context, o options) (err error) {
	operation := b.req.Operation
	if (operation != "install" && operation != "update") || o.provider != codexDesktopSystem || o.version != "latest" || !o.trust || !o.acceptSystem || o.acceptPM || o.rocm || o.removePath || o.refreshLauncher || o.keepPreparation {
		return fmt.Errorf("ChatGPT Desktop latest-only install/update requires --trust-download and --accept-system-packages; other operations remain Pending")
	}
	url, err := codexDesktopURL(b.arch)
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	b.log(label("codex_desktop_scope"))
	b.log(label("codex_desktop_consent"))
	b.log(label("codex_desktop_guard"))
	t := b.codexDesktopTools()
	s := t.commands
	before, err := t.packageRecord()
	if err != nil {
		return fmt.Errorf("ChatGPT package state unknown: %w", err)
	}
	if !codexBaselineValid(operation, before, b.arch) {
		return fmt.Errorf("chatgpt state preserved: install requires absence; update requires exact installed, valid-version, matching-architecture state without hold")
	}
	for _, name := range []string{"apt-get", "apt-config", "dpkg-deb", "env", "mktemp", "install", "sha256sum", "rm", "rmdir"} {
		if _, err = s.resolve(name); err != nil {
			return err
		}
	}
	if s.uid() != 0 {
		if _, err = s.resolve("sudo"); err != nil {
			return err
		}
	}
	if err = checkCodexSystemPolicy(ctx, t); err != nil {
		return err
	}
	prior, err := b.codexDesktopRecord()
	if err != nil {
		return err
	}
	if !codexDesktopFinished(prior) {
		return fmt.Errorf("incomplete ChatGPT Desktop operation; inspect journal, processes and package manager manually")
	}
	root := b.root(codexDesktopSystem)
	if err = dirs(filepath.Dir(root), true, false); err != nil {
		return err
	}
	lock := root + ".lock"
	if err = os.Mkdir(lock, 0700); err != nil {
		return fmt.Errorf("ChatGPT Desktop operation locked/interrupted: %w", err)
	}
	retain := false
	lockReleaseAttempted := false
	defer func() {
		if !retain && !lockReleaseAttempted {
			_ = os.Remove(lock)
		}
	}()
	prior, err = b.codexDesktopRecord()
	if err != nil || !codexDesktopFinished(prior) {
		return fmt.Errorf("ChatGPT Desktop journal changed under lock: %v", err)
	}
	retain = true
	if _, err = os.Lstat(root); os.IsNotExist(err) {
		if err = os.Mkdir(root, 0700); err != nil {
			return err
		}
		if err = writeJSON(filepath.Join(root, "owner.json"), ownership{1, "ai", codexDesktopSystem}); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	rec := codexDesktopJournal{Schema: 2, Operation: operation, Baseline: before, Plugin: "ai", Provider: codexDesktopSystem, Phase: "prepared", LastPhase: "prepared", URL: url, Package: "chatgpt", Arch: b.arch, Trust: codexDesktopTrust}
	privileged := false
	defer func() {
		if err == nil {
			return
		}
		rec.Phase = "failed"
		if privileged {
			rec.Phase = "unknown"
		}
		saveErr := b.saveCodexDesktopRecord(rec)
		retain = privileged || saveErr != nil
		err = fmt.Errorf("%w; no rollback claimed; journal=%s; retained input=%s root=%s; privileged descendants may still run; inspect manually; journal save error=%v", err, filepath.Join(root, "journal.json"), rec.Input, rec.Staging, saveErr)
	}()
	setPhase := func(phase string) error {
		rec.Phase, rec.LastPhase = phase, phase
		return b.saveCodexDesktopRecord(rec)
	}
	if err = setPhase("prepared"); err != nil {
		return err
	}
	call := func(name string, args []string, elevated bool, phase string) (string, error) {
		if e := ctx.Err(); e != nil {
			return "", e
		}
		if e := setPhase(phase); e != nil {
			return "", e
		}
		ex, e := s.resolve(name)
		if e != nil {
			return "", e
		}
		if elevated && s.uid() != 0 {
			sudo, e := s.resolve("sudo")
			if e != nil {
				return "", e
			}
			env, e := s.resolve("env")
			if e != nil {
				return "", e
			}
			prefix := append([]string{"-n", "--", env, "-i"}, codexSystemEnvironment()...)
			args = append(append(prefix, ex), args...)
			ex = sudo
		}
		privileged = privileged || elevated
		out, e := s.run(ctx, ex, args)
		if ctx.Err() != nil {
			e = ctx.Err()
		}
		return out, e
	}
	rec.Input, err = os.MkdirTemp(filepath.Dir(root), ".atm-ai-codex-desktop-input-")
	if err != nil {
		return err
	}
	inputInfo, err := os.Lstat(rec.Input)
	if err != nil {
		return err
	}
	if err = setPhase("prepared"); err != nil {
		return err
	}
	local := filepath.Join(rec.Input, "chatgpt.deb")
	f, err := os.OpenFile(local, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	rec.Size, rec.SHA256, err = b.downloadCodexDesktop(ctx, url, f)
	info, statErr := f.Stat()
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if statErr != nil || closeErr != nil || info.Size() != rec.Size {
		return fmt.Errorf("ChatGPT DEB local size/sync verification failed")
	}
	out, err := call("dpkg-deb", []string{"--field", local, "Package", "Version", "Architecture"}, false, "downloaded")
	if err != nil {
		return err
	}
	rec.Version, err = codexDEBFields(out, b.arch)
	if err != nil {
		return err
	}
	if err = setPhase("metadata-verified"); err != nil {
		return err
	}
	if operation == "update" {
		order, e := compareCodexVersions(ctx, s, rec.Version, before.Version)
		if e != nil {
			return e
		}
		if order < 0 {
			return fmt.Errorf("latest ChatGPT DEB is older than the observed package; downgrade refused")
		}
		if order == 0 {
			current, e := t.packageRecord()
			if e != nil {
				return e
			}
			if current != before {
				return fmt.Errorf("chatgpt original baseline changed before no-update result")
			}
			if err = ctx.Err(); err != nil {
				return err
			}
			if err = cleanupCodexInput(rec.Input, inputInfo, []codexPrivateFile{{local, info}}); err != nil {
				return err
			}
			current, err = t.packageRecord()
			if err != nil {
				return err
			}
			if current != before {
				return fmt.Errorf("chatgpt original baseline changed during no-update cleanup")
			}
			if err = ctx.Err(); err != nil {
				return err
			}
			if err = setPhase("no-update"); err != nil {
				return err
			}
			// Never retry this release: a subsequent invocation may acquire the path.
			lockReleaseAttempted = true
			if err = os.Remove(lock); err != nil {
				return err
			}
			retain = false
			b.log(label("codex_desktop_no_update"))
			return nil
		}
	}
	if t.executable == nil {
		return fmt.Errorf("native guard image unavailable")
	}
	image, err := t.executable()
	if err != nil {
		return err
	}
	imageInfo, imageErr := image.Stat()
	if imageErr != nil || !imageInfo.Mode().IsRegular() || imageInfo.Size() <= 0 || imageInfo.Size() > codexGuardMaxImage {
		image.Close()
		return fmt.Errorf("invalid running backend image")
	}
	localGuard := filepath.Join(rec.Input, codexGuardName)
	guardFile, err := os.OpenFile(localGuard, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		image.Close()
		return err
	}
	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(guardFile, h), io.LimitReader(image, codexGuardMaxImage+1))
	image.Close()
	guardInfo, guardStatErr := guardFile.Stat()
	syncErr := guardFile.Sync()
	closeErr = guardFile.Close()
	if copyErr != nil || guardStatErr != nil || syncErr != nil || closeErr != nil || n != imageInfo.Size() {
		return fmt.Errorf("cannot prepare bounded native guard image")
	}
	rec.GuardSHA256 = hex.EncodeToString(h.Sum(nil))
	var nonce [32]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return err
	}
	rec.GuardNonce = hex.EncodeToString(nonce[:])
	out, err = call("mktemp", []string{"-d", "/run/atm-ai-codex-desktop-XXXXXXXXXX"}, true, "root-mktemp")
	if err != nil {
		return err
	}
	stage := strings.TrimSuffix(out, "\n")
	if !codexDesktopStageRE.MatchString(stage) {
		return fmt.Errorf("unexpected privileged staging path; inspect /run manually")
	}
	rec.Staging = stage
	if err = setPhase("root-mktemp"); err != nil {
		return err
	}
	if err = s.stage(stage); err != nil {
		return err
	}
	target := stage + "/chatgpt.deb"
	if _, err = call("install", []string{"-T", "-m", "0600", "--", local, target}, true, "root-copy"); err != nil {
		return err
	}
	if err = s.stage(stage); err != nil {
		return err
	}
	out, err = call("sha256sum", []string{"--", target}, true, "root-hash")
	if err != nil {
		return err
	}
	if out != rec.SHA256+"  "+target+"\n" {
		return fmt.Errorf("privileged ChatGPT DEB copy checksum mismatch")
	}
	out, err = call("dpkg-deb", []string{"--field", target, "Package", "Version", "Architecture"}, true, "root-metadata")
	if err != nil {
		return err
	}
	v, err := codexDEBFields(out, b.arch)
	if err != nil {
		return err
	}
	if v != rec.Version {
		return fmt.Errorf("privileged ChatGPT DEB version changed")
	}
	guardMetadata := codexAPTGuardMetadata{Schema: 2, Operation: operation, Baseline: before, Stage: stage, Version: rec.Version, Arch: rec.Arch, SHA256: rec.SHA256, Size: rec.Size, HelperSHA256: rec.GuardSHA256, Nonce: rec.GuardNonce}
	configData, err := encodeCodexGuardMetadata(guardMetadata)
	if err != nil {
		return err
	}
	localConfig := filepath.Join(rec.Input, "guard.json")
	if err = writeJSON(localConfig, guardMetadata); err != nil {
		return err
	}
	configData = append(configData, '\n')
	configInfo, err := os.Lstat(localConfig)
	if err != nil {
		return err
	}
	rec.GuardConfigSHA256 = fmt.Sprintf("%x", sha256.Sum256(configData))
	for _, file := range []struct{ local, name, mode, digest string }{
		{localGuard, codexGuardName, "0500", rec.GuardSHA256}, {localConfig, "guard.json", "0600", rec.GuardConfigSHA256},
	} {
		if err = s.stage(stage); err != nil {
			return err
		}
		if _, err = call("install", []string{"-T", "-m", file.mode, "--", file.local, stage + "/" + file.name}, true, "root-guard-copy"); err != nil {
			return err
		}
		if err = s.stage(stage); err != nil {
			return err
		}
		out, err = call("sha256sum", []string{"--", stage + "/" + file.name}, true, "root-guard-hash")
		if err != nil {
			return err
		}
		if out != file.digest+"  "+stage+"/"+file.name+"\n" {
			return fmt.Errorf("root native guard image/config continuity mismatch")
		}
	}
	if err = checkCodexSystemPolicy(ctx, t); err != nil {
		return err
	}
	current, err := t.packageRecord()
	if err != nil {
		return err
	}
	if current != before {
		return fmt.Errorf("chatgpt original package baseline changed concurrently; existing state preserved")
	}
	if err = s.stage(stage); err != nil {
		return err
	}
	out, err = call("apt-get", codexTransactionAPTArgs(stage, operation), true, "apt-running")
	if out != "" {
		b.log("APT output (bounded): " + out)
	}
	if err != nil {
		return err
	}
	current, err = t.packageRecord()
	if err != nil {
		return err
	}
	if !current.Present || current.Status != "install ok installed" || current.Version != rec.Version || current.Arch != rec.Arch {
		return fmt.Errorf("APT returned success but observed chatgpt package identity/version/architecture/state did not match")
	}
	if err = s.stage(stage); err != nil {
		return err
	}
	for _, ack := range []struct {
		name string
		data []byte
	}{{"guard-early", codexGuardEarlyMarker(guardMetadata)}, {"guard-ok", codexGuardMarker(guardMetadata)}} {
		out, err = call("sha256sum", []string{"--", stage + "/" + ack.name}, true, "apt-running")
		if err != nil {
			return err
		}
		if out != fmt.Sprintf("%x", sha256.Sum256(ack.data))+"  "+stage+"/"+ack.name+"\n" {
			return fmt.Errorf("APT returned without verified locked baseline-guard acknowledgements")
		}
	}
	if err = setPhase("installed"); err != nil {
		return err
	}
	if err = s.stage(stage); err != nil {
		return err
	}
	// Cleanup authority comes only from this invocation's verified mktemp result.
	for _, name := range []string{"chatgpt.deb", codexGuardName, "guard.json", "guard-early", "guard-ok"} {
		if err = s.stage(stage); err != nil {
			return err
		}
		if _, err = call("rm", []string{"--", stage + "/" + name}, true, "root-cleanup"); err != nil {
			return err
		}
	}
	if err = s.stage(stage); err != nil {
		return err
	}
	if _, err = call("rmdir", []string{"--", stage}, true, "root-cleanup"); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = cleanupCodexInput(rec.Input, inputInfo, []codexPrivateFile{{local, info}, {localGuard, guardInfo}, {localConfig, configInfo}}); err != nil {
		return err
	}
	if err = setPhase("complete"); err != nil {
		return err
	}
	retain = false
	b.log(label("codex_desktop_complete"))
	return nil
}

type codexPrivateFile struct {
	path string
	info os.FileInfo
}

func cleanupCodexInput(dir string, original os.FileInfo, files []codexPrivateFile) error {
	if err := dirs(dir, false, true); err != nil {
		return err
	}
	now, err := os.Lstat(dir)
	if err != nil || !os.SameFile(original, now) {
		return fmt.Errorf("input directory changed; retained for inspection")
	}
	for _, file := range files {
		f, err := regular(file.path, 0600, codexDesktopMaxDownload)
		if err != nil {
			return err
		}
		now, statErr := f.Stat()
		closeErr := f.Close()
		if statErr != nil || closeErr != nil || !os.SameFile(file.info, now) {
			return fmt.Errorf("input file changed; retained for inspection")
		}
		if err := os.Remove(file.path); err != nil {
			return err
		}
	}
	return os.Remove(dir)
}
