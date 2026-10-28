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
	"strings"
)

type systemJournal struct {
	Schema                  int
	Plugin, Provider, Phase string
	Artifact                artifact
	Input, Staging          string
}

func (b *backend) systemMenu(ctx context.Context) ([]choice, error) {
	b.log(providers[hermesSystem].docs)
	b.log(label("system_scope"))
	b.log(label("system_consent"))
	if err := b.systemStatus(); err != nil {
		return nil, err
	}
	back := choice{ID: "back", Label: label("back"), Operation: "menu"}
	if b.arch != "amd64" {
		b.log("System DEB preview unavailable on this architecture; no fallback asset is selected.")
		return []choice{back}, ctx.Err()
	}
	base := []string{"--provider", hermesSystem, "--trust-download", "--accept-system-packages"}
	return []choice{
		{ID: "install", Label: label("system_install"), Operation: "install", Arguments: base},
		{ID: "custom", Label: label("system_custom"), Operation: "install", Arguments: append(append([]string{}, base...), "--version"), Prompt: label("version_prompt")},
		back,
	}, ctx.Err()
}

func (b *backend) systemRecord() (*systemJournal, error) {
	root := b.root(hermesSystem)
	if err := dirs(root, false, true); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var own ownership
	if err := readJSON(filepath.Join(root, "owner.json"), &own); err != nil {
		return nil, err
	}
	if own != (ownership{1, "ai", hermesSystem}) {
		return nil, fmt.Errorf("foreign system journal preserved")
	}
	es, err := entries(root, 2)
	if err != nil {
		return nil, err
	}
	for _, e := range es {
		if e.Name() != "owner.json" && e.Name() != "journal.json" {
			return nil, fmt.Errorf("unknown/interrupted system journal entry preserved")
		}
	}
	var rec systemJournal
	err = readJSON(filepath.Join(root, "journal.json"), &rec)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if rec.Schema != 1 || rec.Plugin != "ai" || rec.Provider != hermesSystem || rec.Artifact.Provider != hermesSystem || rec.Artifact.validate() != nil {
		return nil, fmt.Errorf("invalid system operation journal")
	}
	switch rec.Phase {
	case "prepared", "downloaded", "metadata-verified", "root-mktemp", "root-copy", "root-hash", "root-metadata", "apt-running", "installed", "root-cleanup", "complete", "failed", "unknown":
	default:
		return nil, fmt.Errorf("unknown journal phase")
	}
	return &rec, nil
}

func (b *backend) systemStatus() error {
	rec, err := b.systemRecord()
	if err != nil {
		return err
	}
	if rec == nil {
		b.log(label("system_no_journal"))
		return nil
	}
	b.log(fmt.Sprintf("%s: version=%s; phase=%s; journal=%s", label("system_journal"), rec.Artifact.Version, rec.Phase, filepath.Join(b.root(hermesSystem), "journal.json")))
	if rec.Phase != "complete" {
		b.log("Incomplete system operation: inspect processes, package-manager state, lock and staging manually. No automatic recovery or cleanup.")
	}
	return nil
}

func (b *backend) saveSystemRecord(rec systemJournal) error {
	root := b.root(hermesSystem)
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

func systemAPTArgs(deb string) []string {
	return []string{
		"-o", "APT::Get::AutomaticRemove=false", "-o", "APT::Get::Purge=false",
		"-o", "APT::Get::allow-Downgrades=false", "-o", "APT::Get::allow-Change-Held-Packages=false",
		"-o", "APT::Get::allow-Remove-Essential=false", "-o", "APT::Get::AllowUnauthenticated=false",
		"-o", "Acquire::AllowInsecureRepositories=false", "-o", "Acquire::AllowDowngradeToInsecureRepositories=false",
		"--no-remove", "-y", "install", "--", deb,
	}
}

func checkSystemPolicy(ctx context.Context, s *systemTools) error {
	ex, err := s.resolve("apt-config")
	if err != nil {
		return err
	}
	// The output is data only, never sourced. Refuse custom preference locations
	// rather than examining the wrong directory and bypassing administrator pins.
	out, err := s.run(ctx, ex, []string{"shell", "ATM_PREF", "Dir::Etc::preferences/f", "ATM_PARTS", "Dir::Etc::preferencesparts/d"})
	if err != nil {
		return err
	}
	if out != "ATM_PREF='/etc/apt/preferences'\nATM_PARTS='/etc/apt/preferences.d/'\n" {
		return fmt.Errorf("custom/unknown APT preference locations unsupported; use administrator-managed installation")
	}
	return s.policy()
}

func (b *backend) installHermesSystem(ctx context.Context, o options) (err error) {
	// Defense in depth for internal callers, before tool lookup, state or network.
	if b.req.Operation != "install" || o.provider != hermesSystem || !o.trust || !o.acceptSystem || b.arch != "amd64" {
		return fmt.Errorf("Hermes system DEB requires install, Linux amd64, --trust-download and --accept-system-packages")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	b.log(label("system_scope"))
	b.log(label("system_consent"))
	s := b.system
	if s == nil {
		s = defaultSystemTools()
	}
	for _, name := range []string{"apt-get", "apt-config", "dpkg-deb", "mktemp", "install", "sha256sum", "rm", "rmdir"} {
		if _, err = s.resolve(name); err != nil {
			return err
		}
	}
	if s.uid() != 0 {
		if _, err = s.resolve("sudo"); err != nil {
			return err
		}
	}
	if err = checkSystemPolicy(ctx, s); err != nil {
		return err
	}
	before, beforeArch, err := s.installed()
	if err != nil {
		return err
	}
	if before != "" && (!versionRE.MatchString(before) || beforeArch != b.arch) {
		return fmt.Errorf("unsupported existing system package; no adoption")
	}
	prior, err := b.systemRecord()
	if err != nil {
		return err
	}
	if prior != nil && prior.Phase != "complete" {
		return fmt.Errorf("incomplete previous system operation; inspect journal/processes/package manager manually before retrying")
	}
	root := b.root(hermesSystem)
	if err = dirs(filepath.Dir(root), true, false); err != nil {
		return err
	}
	lock := root + ".lock"
	if err = os.Mkdir(lock, 0700); err != nil {
		return fmt.Errorf("system operation locked/interrupted; inspect %s: %w", lock, err)
	}
	retain := false
	defer func() {
		if !retain {
			_ = os.Remove(lock)
		}
	}()
	prior, err = b.systemRecord()
	if err != nil || prior != nil && prior.Phase != "complete" {
		return fmt.Errorf("system journal changed or interrupted under lock: %v", err)
	}
	a, err := b.resolve(ctx, hermesSystem, o.version)
	if err != nil {
		return err
	}
	if before != "" && !newer(a.Version, before) {
		if a.Version == before {
			b.log("Exact Hermes Desktop version is already installed by the system package manager; no adoption or installation performed.")
			return nil
		}
		return fmt.Errorf("system downgrade refused; existing version %s", before)
	}
	if _, err = os.Lstat(root); os.IsNotExist(err) {
		if err = os.Mkdir(root, 0700); err != nil {
			return err
		}
		if err = writeJSON(filepath.Join(root, "owner.json"), ownership{1, "ai", hermesSystem}); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	rec := systemJournal{Schema: 1, Plugin: "ai", Provider: hermesSystem, Phase: "prepared", Artifact: a}
	if err = b.saveSystemRecord(rec); err != nil {
		return err
	}
	privileged := false
	defer func() {
		if err == nil {
			return
		}
		rec.Phase = "failed"
		if privileged {
			rec.Phase = "unknown"
			retain = true
		}
		saveErr := b.saveSystemRecord(rec)
		err = fmt.Errorf("%w; no rollback claimed; inspect journal %s and retained staging (input=%s, root=%s) manually; privileged descendants may still run; journal save error=%v", err, filepath.Join(root, "journal.json"), rec.Input, rec.Staging, saveErr)
	}()
	setPhase := func(phase string) error {
		rec.Phase = phase
		return b.saveSystemRecord(rec)
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
			args = append([]string{"-n", "--preserve-env=DEBIAN_FRONTEND", "--", ex}, args...)
			ex = sudo
		}
		privileged = privileged || elevated
		out, e := s.run(ctx, ex, args)
		if ctx.Err() != nil {
			e = ctx.Err()
		}
		return out, e
	}
	rec.Input, err = os.MkdirTemp(filepath.Dir(root), ".atm-ai-system-input-")
	if err != nil {
		return err
	}
	if err = setPhase("prepared"); err != nil {
		return err
	}
	local := filepath.Join(rec.Input, "hermes-desktop.deb")
	f, err := os.OpenFile(local, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	h := sha256.New()
	err = b.fetch(ctx, hermesSystem, a.URL, io.MultiWriter(f, h), a.Size)
	info, statErr := f.Stat()
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if statErr != nil || closeErr != nil || info.Size() != a.Size || hex.EncodeToString(h.Sum(nil)) != a.SHA256 {
		return fmt.Errorf("DEB byte length/hash verification failed")
	}
	out, err := call("dpkg-deb", []string{"--field", local, "Package", "Version", "Architecture"}, false, "downloaded")
	if err != nil {
		return err
	}
	if err = verifyDEBFields(out, a); err != nil {
		return err
	}
	if err = setPhase("metadata-verified"); err != nil {
		return err
	}
	out, err = call("mktemp", []string{"-d", "/run/atm-ai-hermes-XXXXXXXXXX"}, true, "root-mktemp")
	if err != nil {
		return err
	}
	stage := strings.TrimSuffix(out, "\n")
	if !rootStageRE.MatchString(stage) {
		return fmt.Errorf("unexpected privileged staging path; inspect /run manually")
	}
	rec.Staging = stage
	if err = s.stage(stage); err != nil {
		return err
	}
	target := stage + "/hermes-desktop.deb"
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
	if out != a.SHA256+"  "+target+"\n" {
		return fmt.Errorf("privileged DEB copy checksum mismatch")
	}
	out, err = call("dpkg-deb", []string{"--field", target, "Package", "Version", "Architecture"}, true, "root-metadata")
	if err != nil {
		return err
	}
	if err = verifyDEBFields(out, a); err != nil {
		return err
	}
	if err = checkSystemPolicy(ctx, s); err != nil {
		return err
	}
	current, currentArch, err := s.installed()
	if err != nil {
		return err
	}
	if current != before || currentArch != beforeArch {
		return fmt.Errorf("system package changed concurrently; inspect before retry")
	}
	if err = s.stage(stage); err != nil {
		return err
	}
	out, err = call("apt-get", systemAPTArgs(target), true, "apt-running")
	if out != "" {
		b.log("APT output (bounded): " + out)
	}
	if err != nil {
		return err
	}
	current, currentArch, err = s.installed()
	if err != nil {
		return err
	}
	if current != a.Version || currentArch != a.Arch {
		return fmt.Errorf("APT returned success but installed package identity/version did not match")
	}
	if err = setPhase("installed"); err != nil {
		return err
	}
	if err = s.stage(stage); err != nil {
		return err
	}
	// Only this transaction's verified mktemp result authorizes cleanup. Journal
	// paths are observations, never recovery/deletion authority. No recursive rm.
	if _, err = call("rm", []string{"--", target}, true, "root-cleanup"); err != nil {
		return err
	}
	if _, err = call("rmdir", []string{"--", stage}, true, "root-cleanup"); err != nil {
		return err
	}
	if err = os.Remove(local); err != nil {
		return err
	}
	if err = os.Remove(rec.Input); err != nil {
		return err
	}
	if err = setPhase("complete"); err != nil {
		return err
	}
	b.log(label("system_complete"))
	return nil
}
