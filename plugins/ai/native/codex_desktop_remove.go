//go:build linux

package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func codexRemovePhase(phase string) bool {
	switch phase {
	case "prepared", "root-mktemp", "root-guard-copy", "root-guard-hash", "apt-running", "removed", "root-cleanup", "complete", "failed", "unknown":
		return true
	}
	return false
}

func validateCodexRemoveJournal(r codexDesktopJournal) error {
	if r.Schema != 3 || r.Operation != "remove" || r.Plugin != "ai" || r.Provider != codexDesktopSystem || r.Package != "chatgpt" || (r.Arch != "amd64" && r.Arch != "arm64") || !codexUpdateEligible(r.Baseline, r.Arch) || r.Version != r.Baseline.Version || r.URL != "" || r.SHA256 != "" || r.Size != 0 || r.Trust != "" || !codexRemovePhase(r.Phase) || !codexRemovePhase(r.LastPhase) {
		return fmt.Errorf("invalid ChatGPT Desktop removal journal")
	}
	if r.LastPhase == "failed" || r.LastPhase == "unknown" || r.Phase != r.LastPhase && r.Phase != "failed" && r.Phase != "unknown" {
		return fmt.Errorf("inconsistent removal journal phase")
	}
	for _, hash := range []string{r.GuardSHA256, r.GuardConfigSHA256, r.GuardNonce} {
		if hash != "" && !validSHA(hash) {
			return fmt.Errorf("invalid removal guard evidence")
		}
	}
	if r.LastPhase != "prepared" && (!validSHA(r.GuardSHA256) || !validSHA(r.GuardNonce)) {
		return fmt.Errorf("missing removal helper evidence")
	}
	if r.LastPhase != "prepared" && r.LastPhase != "root-mktemp" && (!validSHA(r.GuardConfigSHA256) || !codexDesktopStageRE.MatchString(r.Staging)) {
		return fmt.Errorf("missing removal staging evidence")
	}
	// Observations only: no path from this record ever authorizes cleanup/replay.
	return nil
}

func codexRemovedPostcondition(current, baseline codexPackageRecord) bool {
	return current == (codexPackageRecord{}) || current == (codexPackageRecord{true, "deinstall ok config-files", baseline.Version, baseline.Arch})
}

func (b *backend) removeCodexDesktop(ctx context.Context, o options) (err error) {
	if b.req.Operation != "uninstall" || o.provider != codexDesktopSystem || !o.acceptSystem || o.version != "latest" || o.trust || o.acceptPM || o.rocm || o.removePath || o.refreshLauncher || o.keepPreparation {
		return fmt.Errorf("ChatGPT Desktop uninstall requires only --provider codex-desktop-system --accept-system-packages")
	}
	if b.arch != "amd64" && b.arch != "arm64" {
		return fmt.Errorf("ChatGPT Desktop removal requires Linux APT amd64/arm64")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	b.log(label("codex_desktop_remove_scope"))
	t := b.codexDesktopTools()
	s := t.commands
	before, err := t.packageRecord()
	if err != nil {
		return fmt.Errorf("ChatGPT package state unknown: %w", err)
	}
	if !codexUpdateEligible(before, b.arch) {
		return fmt.Errorf("removal requires exact install ok installed, valid-version, matching-architecture chatgpt without hold; state preserved")
	}
	for _, name := range []string{"apt-get", "apt-config", "env", "mktemp", "install", "sha256sum", "rm", "rmdir"} {
		if _, err = s.resolve(name); err != nil {
			return err
		}
	}
	if s.uid() != 0 {
		if _, err = s.resolve("sudo"); err != nil {
			return err
		}
	}
	if err = checkCodexRemovePolicy(ctx, t); err != nil {
		return err
	}
	prior, err := b.codexDesktopRecord()
	if err != nil {
		return err
	}
	if !codexDesktopFinished(prior) {
		return fmt.Errorf("incomplete ChatGPT Desktop operation; inspect manually")
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
	defer func() {
		if !retain {
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
	rec := codexDesktopJournal{Schema: 3, Operation: "remove", Baseline: before, Plugin: "ai", Provider: codexDesktopSystem, Phase: "prepared", LastPhase: "prepared", Package: "chatgpt", Version: before.Version, Arch: before.Arch}
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
		err = fmt.Errorf("%w; no rollback or journal cleanup; journal=%s; retained input=%s root=%s; privileged descendants may still run; inspect manually; journal save error=%v", err, filepath.Join(root, "journal.json"), rec.Input, rec.Staging, saveErr)
	}()
	setPhase := func(phase string) error {
		rec.Phase, rec.LastPhase = phase, phase
		return b.saveCodexDesktopRecord(rec)
	}
	if err = setPhase("prepared"); err != nil {
		return err
	}
	call := func(name string, args []string, phase string) (string, error) {
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
		if s.uid() != 0 {
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
		privileged = true
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
	if t.executable == nil {
		return fmt.Errorf("native removal guard image unavailable")
	}
	image, err := t.executable()
	if err != nil {
		return err
	}
	imageInfo, imageErr := image.Stat()
	if imageErr != nil || !imageInfo.Mode().IsRegular() || imageInfo.Size() <= 0 || imageInfo.Size() > codexGuardMaxImage {
		image.Close()
		return fmt.Errorf("invalid running removal backend image")
	}
	localGuard := filepath.Join(rec.Input, codexGuardName)
	f, err := os.OpenFile(localGuard, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		image.Close()
		return err
	}
	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(f, h), io.LimitReader(image, codexGuardMaxImage+1))
	image.Close()
	guardInfo, statErr := f.Stat()
	syncErr, closeErr := f.Sync(), f.Close()
	if copyErr != nil || statErr != nil || syncErr != nil || closeErr != nil || n != imageInfo.Size() {
		return fmt.Errorf("cannot prepare bounded removal guard image")
	}
	rec.GuardSHA256 = hex.EncodeToString(h.Sum(nil))
	var nonce [32]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return err
	}
	rec.GuardNonce = hex.EncodeToString(nonce[:])
	out, err := call("mktemp", []string{"-d", "/run/atm-ai-codex-desktop-XXXXXXXXXX"}, "root-mktemp")
	if err != nil {
		return err
	}
	stage := strings.TrimSuffix(out, "\n")
	if !codexDesktopStageRE.MatchString(stage) {
		return fmt.Errorf("unexpected privileged removal staging path; inspect /run manually")
	}
	rec.Staging = stage
	if err = setPhase("root-mktemp"); err != nil {
		return err
	}
	if err = s.stage(stage); err != nil {
		return err
	}
	m := codexRemoveGuardMetadata{3, "remove", before, stage, rec.GuardSHA256, rec.GuardNonce}
	localConfig := filepath.Join(rec.Input, "guard.json")
	if err = writeJSON(localConfig, m); err != nil {
		return err
	}
	configInfo, err := os.Lstat(localConfig)
	if err != nil {
		return err
	}
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	rec.GuardConfigSHA256 = fmt.Sprintf("%x", sha256.Sum256(append(data, '\n')))
	for _, file := range []struct{ local, name, mode, digest string }{
		{localGuard, codexGuardName, "0500", rec.GuardSHA256}, {localConfig, "guard.json", "0600", rec.GuardConfigSHA256},
	} {
		if err = s.stage(stage); err != nil {
			return err
		}
		if _, err = call("install", []string{"-T", "-m", file.mode, "--", file.local, stage + "/" + file.name}, "root-guard-copy"); err != nil {
			return err
		}
		if err = s.stage(stage); err != nil {
			return err
		}
		out, err = call("sha256sum", []string{"--", stage + "/" + file.name}, "root-guard-hash")
		if err != nil {
			return err
		}
		if out != file.digest+"  "+stage+"/"+file.name+"\n" {
			return fmt.Errorf("root removal guard image/config continuity mismatch")
		}
	}
	if err = checkCodexRemovePolicy(ctx, t); err != nil {
		return err
	}
	current, err := t.packageRecord()
	if err != nil {
		return err
	}
	if current != before {
		return fmt.Errorf("chatgpt original removal baseline changed concurrently")
	}
	if err = s.stage(stage); err != nil {
		return err
	}
	out, err = call("apt-get", codexRemoveAPTArgs(stage), "apt-running")
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
	if !codexRemovedPostcondition(current, before) {
		return fmt.Errorf("APT success without exact removed chatgpt postcondition; state uncertain")
	}
	for _, ack := range []struct {
		name  string
		early bool
	}{{"remove-early", true}, {"remove-ok", false}} {
		if err = s.stage(stage); err != nil {
			return err
		}
		out, err = call("sha256sum", []string{"--", stage + "/" + ack.name}, "apt-running")
		if err != nil {
			return err
		}
		if out != fmt.Sprintf("%x", sha256.Sum256(codexRemoveMarker(m, ack.early)))+"  "+stage+"/"+ack.name+"\n" {
			return fmt.Errorf("APT returned without verified removal acknowledgements")
		}
	}
	if err = setPhase("removed"); err != nil {
		return err
	}
	// Only this invocation's verified mktemp result and files authorize cleanup.
	for _, name := range []string{codexGuardName, "guard.json", "remove-early", "remove-ok"} {
		if err = s.stage(stage); err != nil {
			return err
		}
		if _, err = call("rm", []string{"--", stage + "/" + name}, "root-cleanup"); err != nil {
			return err
		}
	}
	if err = s.stage(stage); err != nil {
		return err
	}
	if _, err = call("rmdir", []string{"--", stage}, "root-cleanup"); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = cleanupCodexInput(rec.Input, inputInfo, []codexPrivateFile{{localGuard, guardInfo}, {localConfig, configInfo}}); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = setPhase("complete"); err != nil {
		return err
	}
	retain = false
	b.log(label("codex_desktop_remove_complete"))
	return nil
}
