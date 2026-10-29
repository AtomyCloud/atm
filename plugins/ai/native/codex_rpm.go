//go:build linux

package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

var codexRPMStageRE = regexp.MustCompile(`^/run/atm-ai-codex-rpm-[A-Za-z0-9]{10}$`)

type codexRPMJournal struct {
	Schema                                                         int
	Plugin, Provider, Phase, URL, SHA256, Input, Stage, PlanSHA256 string
	Size                                                           int64
	Package                                                        codexRPMMetadata
	Operation                                                      string
	Baseline                                                       codexRPMMetadata
}

type codexRPMPlan struct {
	Version string `json:"version"`
	RPMs    []struct {
		NEVRA  string `json:"nevra"`
		Action string `json:"action"`
		Reason string `json:"reason"`
		Repo   string `json:"repo_id"`
		Path   string `json:"package_path"`
	} `json:"rpms"`
}

// The first RPM slice permits only the requested package. Dependency changes,
// groups, erasures and upgrades require a separately reviewed workflow.
func validateCodexRPMPlan(data []byte, m codexRPMMetadata) error {
	if len(data) == 0 || len(data) > 64<<10 {
		return fmt.Errorf("invalid stored transaction size")
	}
	var p codexRPMPlan
	if err := decode(data, &p, true); err != nil {
		return err
	}
	if p.Version != "1.0" || len(p.RPMs) != 1 {
		return fmt.Errorf("DNF5 plan must contain only one RPM; install required dependencies separately")
	}
	r := p.RPMs[0]
	version := m.Version
	if m.Epoch != "0" {
		version = m.Epoch + ":" + version
	}
	nevra := m.Name + "-" + version + "-" + m.Release + "." + m.Arch
	// Schema 1.0: DNF5 5.2.17 stores @commandline; newer DNF5 wraps it
	// with @stored_transaction. Neither spelling permits a repository package.
	localRepo := r.Repo == "@commandline" || r.Repo == "@stored_transaction(@commandline)"
	if r.NEVRA != nevra || r.Action != "Install" || r.Reason != "User" || !localRepo || r.Path != "./packages/chatgpt.rpm" {
		return fmt.Errorf("DNF5 plan differs from the exact initial ChatGPT RPM installation")
	}
	return nil
}

func codexRPMPolicy(output string) error {
	if len(output) == 0 || len(output) > 64<<10 {
		return fmt.Errorf("invalid DNF5 configuration size")
	}
	values := map[string]string{}
	set := map[string]bool{}
	section := false
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line == "======== Main configuration: ========" && !section {
			section = true
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if !section || !codexRPMConfigKeyRE.MatchString(k) {
			return fmt.Errorf("unsupported DNF5 configuration output")
		}
		if _, exists := values[k]; exists {
			return fmt.Errorf("duplicate DNF5 configuration field")
		}
		values[k] = v
		set[k] = ok
	}
	for _, key := range []string{"installroot", "tsflags", "localpkg_gpgcheck"} {
		if !set[key] {
			return fmt.Errorf("missing DNF5 policy field %s", key)
		}
	}
	if values["installroot"] != "/" || values["tsflags"] != "" {
		return fmt.Errorf("custom DNF5 installroot/transaction flags unsupported; administrator policy preserved")
	}
	switch strings.ToLower(values["localpkg_gpgcheck"]) {
	case "true", "false", "1", "0":
	default:
		return fmt.Errorf("unknown local RPM signature policy")
	}
	// Older DNF5 does not expose this option. When exposed, native locking
	// must remain enabled; do not silently override administrator policy.
	if value, ok := values["skip_system_repo_lock"]; ok {
		if !set["skip_system_repo_lock"] || value != "false" && value != "False" && value != "0" {
			return fmt.Errorf("DNF5 system repository locking must remain enabled")
		}
	}
	return nil
}

var codexRPMConfigKeyRE = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// stat does not dereference the final component. Callers validate each parent
// inside the root-owned private stage before inspecting a child.
func codexRPMRootIdentity(output string, directory bool) (string, error) {
	fields := strings.Split(strings.TrimSuffix(output, "\n"), "\t")
	if len(fields) != 5 || !strings.HasSuffix(output, "\n") || fields[1] != "0" {
		return "", fmt.Errorf("untrusted RPM stage entry")
	}
	mode, err := strconv.ParseUint(fields[0], 16, 32)
	kind := uint64(unix.S_IFREG)
	if directory {
		kind = unix.S_IFDIR
	}
	if err != nil || mode&unix.S_IFMT != kind || mode&0022 != 0 || !directory && fields[2] != "1" {
		return "", fmt.Errorf("unsafe RPM stage entry type/mode/links")
	}
	for _, field := range fields[2:] {
		if _, err := strconv.ParseUint(field, 10, 64); err != nil {
			return "", fmt.Errorf("invalid RPM stage identity")
		}
	}
	return strings.Join([]string{fields[0], fields[1], fields[3], fields[4]}, "\t"), nil
}

func (b *backend) rpmTools() *systemTools {
	if b.codexRPM != nil {
		return b.codexRPM
	}
	s := defaultSystemTools()
	s.stage = func(p string) error { return checkRootStage(p, codexRPMStageRE) }
	return s
}

func (b *backend) rpmJournal() (*codexRPMJournal, error) {
	root := b.root(codexDesktopRPM)
	if err := dirs(root, false, true); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var own ownership
	if err := readJSON(filepath.Join(root, "owner.json"), &own); err != nil {
		return nil, err
	}
	if own != (ownership{1, "ai", codexDesktopRPM}) {
		return nil, fmt.Errorf("foreign RPM preparation preserved")
	}
	es, err := entries(root, 2)
	if err != nil {
		return nil, err
	}
	for _, e := range es {
		if e.Name() != "owner.json" && e.Name() != "journal.json" {
			return nil, fmt.Errorf("incomplete or unknown RPM journal entry")
		}
	}
	var r codexRPMJournal
	if err = readJSON(filepath.Join(root, "journal.json"), &r); err != nil {
		return nil, err
	}
	u, err := codexRPMURL(b.arch)
	if err != nil || (r.Schema != 1 && r.Schema != 2) || r.Plugin != "ai" || r.Provider != codexDesktopRPM || r.URL != u {
		return nil, fmt.Errorf("invalid RPM journal")
	}
	if r.Schema == 1 {
		if r.Operation != "" || r.Baseline != (codexRPMMetadata{}) {
			return nil, fmt.Errorf("legacy RPM journal cannot describe an update")
		}
	} else {
		switch r.Operation {
		case "install":
			if r.Baseline != (codexRPMMetadata{}) {
				return nil, fmt.Errorf("initial RPM journal cannot carry an installed baseline")
			}
		case "update":
			m := r.Baseline
			if _, e := parseCodexRPMMetadata(strings.Join([]string{m.Name, m.Epoch, m.Version, m.Release, m.Arch}, "\t")+"\n", b.arch); e != nil {
				return nil, fmt.Errorf("invalid RPM update baseline: %w", e)
			}
		default:
			return nil, fmt.Errorf("invalid RPM journal operation")
		}
	}
	switch r.Phase {
	case "prepared", "downloaded", "metadata-verified", "root-staged", "plan-verified", "replaying", "complete", "failed", "unknown":
	case "unchanged":
		if r.Schema != 2 || r.Operation != "update" || r.Package != r.Baseline || r.Size == 0 || r.Input == "" || r.Stage != "" || r.PlanSHA256 != "" {
			return nil, fmt.Errorf("invalid unchanged RPM evidence")
		}
	default:
		return nil, fmt.Errorf("invalid RPM journal phase")
	}
	if r.Size < 0 || r.Size > codexDesktopMaxDownload || (r.Size == 0) != (r.SHA256 == "") || r.SHA256 != "" && !validSHA(r.SHA256) || r.PlanSHA256 != "" && !validSHA(r.PlanSHA256) {
		return nil, fmt.Errorf("invalid RPM journal digest/size")
	}
	if r.Stage != "" && !codexRPMStageRE.MatchString(r.Stage) {
		return nil, fmt.Errorf("invalid RPM journal stage")
	}
	if r.Input != "" {
		if err = absolutePath(r.Input); err != nil || filepath.Dir(r.Input) != filepath.Dir(root) || !strings.HasPrefix(filepath.Base(r.Input), ".atm-ai-codex-rpm-input-") {
			return nil, fmt.Errorf("invalid RPM input evidence")
		}
	}
	if r.Package.Name != "" {
		m, e := parseCodexRPMMetadata(strings.Join([]string{r.Package.Name, r.Package.Epoch, r.Package.Version, r.Package.Release, r.Package.Arch}, "\t")+"\n", b.arch)
		if e != nil || m != r.Package {
			return nil, fmt.Errorf("invalid RPM header evidence")
		}
	}
	if r.Phase == "complete" && (r.Size == 0 || r.Package.Name == "" || r.Input == "" || r.Stage == "" || !validSHA(r.PlanSHA256)) {
		return nil, fmt.Errorf("incomplete RPM success evidence")
	}
	return &r, nil
}

func codexRPMJournalFinished(r *codexRPMJournal) bool {
	return r == nil || r.Phase == "complete" || r.Phase == "unchanged"
}

func (b *backend) saveRPMJournal(r codexRPMJournal) error {
	root := b.root(codexDesktopRPM)
	if err := dirs(root, false, true); err != nil {
		return err
	}
	var own ownership
	if err := readJSON(filepath.Join(root, "owner.json"), &own); err != nil {
		return err
	}
	if own != (ownership{1, "ai", codexDesktopRPM}) {
		return fmt.Errorf("RPM journal owner changed")
	}
	tmp := filepath.Join(root, ".journal-next")
	if err := writeJSON(tmp, r); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(root, "journal.json"))
}

func (b *backend) codexRPMStatus() {
	r, err := b.rpmJournal()
	if err != nil {
		b.log(fmt.Sprintf(label("codex_rpm_unknown"), err))
		return
	}
	if r == nil {
		b.log(label("codex_rpm_unobserved"))
		return
	}
	op := r.Operation
	if r.Schema == 1 {
		op = "install"
	}
	b.log(fmt.Sprintf(label("codex_rpm_record"), op, r.Phase, r.Package.Version, r.Package.Release, r.Package.Arch))
}

func (b *backend) codexRPMMenu(ctx context.Context) ([]choice, error) {
	b.log(providers[codexDesktopRPM].docs)
	b.log(label("codex_rpm_scope"))
	b.codexRPMStatus()
	back := choice{ID: "back", Label: label("back"), Operation: "menu"}
	r, err := b.rpmJournal()
	_, lockErr := os.Lstat(b.root(codexDesktopRPM) + ".lock")
	if err != nil || !codexRPMJournalFinished(r) || !os.IsNotExist(lockErr) {
		return []choice{back}, ctx.Err()
	}
	return []choice{
		{ID: "install", Label: label("codex_rpm_install"), Operation: "install", Arguments: []string{"--provider", codexDesktopRPM, "--trust-download", "--accept-system-packages"}},
		{ID: "update", Label: label("codex_rpm_update"), Operation: "update", Arguments: []string{"--provider", codexDesktopRPM, "--trust-download", "--accept-system-packages"}},
		back,
	}, ctx.Err()
}

func rpmDNFOptions() []string {
	return []string{"--assumeyes", "--disable-repo=*", "--setopt=allow_downgrade=False", "--setopt=install_weak_deps=False", "--setopt=skip_broken=False", "--setopt=skip_unavailable=False"}
}

func rpmCall(ctx context.Context, s *systemTools, name string, args []string, elevated bool) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	ex, err := s.resolve(name)
	if err != nil {
		return "", err
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
	return s.run(ctx, ex, args)
}

func (b *backend) installOrUpdateCodexRPM(ctx context.Context, o options) (result error) {
	op := b.req.Operation
	updating := op == "update"
	if (op != "install" && !updating) || o.provider != codexDesktopRPM || o.version != "latest" || !o.trust || !o.acceptSystem || o.acceptPM || o.rocm || o.removePath || o.keepPreparation || o.refreshLauncher || o.serviceAction != "" || o.acceptService {
		return fmt.Errorf("explicit latest-only RPM install/update consent required")
	}
	b.log(label("codex_rpm_scope"))
	s := b.rpmTools()
	url, err := codexRPMURL(b.arch)
	if err != nil {
		return err
	}
	for _, name := range []string{"rpm", "dnf5", "cat", "stat", "ls", "env", "mktemp", "install", "sha256sum"} {
		if _, err = s.resolve(name); err != nil {
			return err
		}
	}
	if s.uid() != 0 {
		if _, err = s.resolve("sudo"); err != nil {
			return err
		}
	}
	version, err := rpmCall(ctx, s, "dnf5", []string{"--version"}, false)
	if err != nil || !strings.HasPrefix(version, "dnf5 version 5.") {
		return fmt.Errorf("DNF5 is required; no DNF4 fallback: %v", err)
	}
	baseline, present, err := installedCodexRPM(ctx, s, b.arch)
	if err != nil || present != updating {
		return fmt.Errorf("RPM %s refused: initial install requires absence; update requires exactly one installed package; unknown state is preserved: %v", op, err)
	}
	checkBaseline := func() error {
		got, found, e := installedCodexRPM(ctx, s, b.arch)
		if e != nil || found != updating || got != baseline {
			return fmt.Errorf("RPM %s original package state changed or is unknown: %v", op, e)
		}
		return ctx.Err()
	}
	prior, err := b.rpmJournal()
	if err != nil {
		return err
	}
	if !codexRPMJournalFinished(prior) {
		return fmt.Errorf("unfinished RPM operation; inspect retained evidence manually")
	}
	root := b.root(codexDesktopRPM)
	if err = dirs(filepath.Dir(root), true, false); err != nil {
		return err
	}
	lock := root + ".lock"
	if err = os.Mkdir(lock, 0700); err != nil {
		return fmt.Errorf("RPM operation locked: %w", err)
	}
	// Keep the inode alive: removal/recreation must not pass via inode reuse.
	lockFile, err := os.OpenFile(lock, os.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer lockFile.Close()
	lockInfo, err := lockFile.Stat()
	if err != nil {
		return err
	}
	checkLock := func() error {
		i, e := os.Lstat(lock)
		if e != nil || !os.SameFile(i, lockInfo) || !i.IsDir() || i.Mode().Perm() != 0700 || owner(i) != owner(lockInfo) {
			return fmt.Errorf("RPM lock changed; inspect manually")
		}
		return nil
	}
	retain := false
	defer func() {
		if retain {
			return
		}
		if e := checkLock(); e != nil {
			result = fmt.Errorf("%v; RPM lock changed; inspect manually", result)
			return
		}
		if e := os.Remove(lock); e != nil {
			result = fmt.Errorf("%v; RPM lock release failed: %w", result, e)
		}
	}()
	prior, err = b.rpmJournal()
	if err != nil || !codexRPMJournalFinished(prior) {
		return fmt.Errorf("RPM journal changed under lock: %v", err)
	}
	if err = checkBaseline(); err != nil {
		return err
	}
	if _, err = os.Lstat(root); os.IsNotExist(err) {
		if err = os.Mkdir(root, 0700); err != nil {
			return err
		}
		if err = writeJSON(filepath.Join(root, "owner.json"), ownership{1, "ai", codexDesktopRPM}); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	rootFile, err := os.OpenFile(root, os.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer rootFile.Close()
	rootInfo, err := rootFile.Stat()
	if err != nil {
		return err
	}
	persisted := prior
	checkState := func() error {
		if e := checkLock(); e != nil {
			return e
		}
		i, e := os.Lstat(root)
		if e != nil || !os.SameFile(rootInfo, i) || !i.IsDir() || i.Mode().Perm() != 0700 {
			return fmt.Errorf("RPM journal directory changed")
		}
		if persisted != nil {
			got, e := b.rpmJournal()
			if e != nil || got == nil || *got != *persisted {
				return fmt.Errorf("RPM journal changed: %v", e)
			}
		}
		return nil
	}
	boundary := func(point string) error {
		if e := b.boundary(point); e != nil {
			return e
		}
		return checkState()
	}
	call := func(name string, args []string) (string, error) {
		if e := checkState(); e != nil {
			return "", e
		}
		out, e := rpmCall(ctx, s, name, args, true)
		if e != nil {
			return "", e
		}
		return out, checkState()
	}
	r := codexRPMJournal{Schema: 2, Plugin: "ai", Provider: codexDesktopRPM, Phase: "prepared", URL: url, Operation: op, Baseline: baseline}
	retain = true
	privileged := false
	defer func() {
		if result != nil {
			r.Phase = "failed"
			if privileged {
				r.Phase = "unknown"
			}
			e := checkState()
			if e == nil {
				e = b.saveRPMJournal(r)
			}
			result = fmt.Errorf("%w; no rollback; retained input=%s stage=%s journal=%s (save=%v); privileged descendants may still run", result, r.Input, r.Stage, root, e)
		}
	}()
	phase := func(p string) error {
		if e := boundary("codex-rpm-before-journal-" + p); e != nil {
			return e
		}
		r.Phase = p
		if e := b.saveRPMJournal(r); e != nil {
			return e
		}
		saved := r
		persisted = &saved
		return checkState()
	}
	if err = phase("prepared"); err != nil {
		return err
	}
	r.Input, err = os.MkdirTemp(filepath.Dir(root), ".atm-ai-codex-rpm-input-")
	if err != nil {
		return err
	}
	if err = phase("prepared"); err != nil {
		return err
	}
	local := filepath.Join(r.Input, "chatgpt.rpm")
	f, err := os.OpenFile(local, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	r.Size, r.SHA256, err = b.downloadCodexRPM(ctx, f)
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
	if err = phase("downloaded"); err != nil {
		return err
	}
	r.Package, err = queryCodexRPM(ctx, s, local, b.arch)
	if err != nil {
		return err
	}
	verifyInput := func() error {
		f, e := regular(local, 0600, codexDesktopMaxDownload)
		if e != nil {
			return e
		}
		defer f.Close()
		h := sha256.New()
		n, e := io.Copy(h, io.LimitReader(f, r.Size+1))
		if e != nil || n != r.Size || fmt.Sprintf("%x", h.Sum(nil)) != r.SHA256 {
			return fmt.Errorf("downloaded RPM changed")
		}
		return ctx.Err()
	}
	if err = verifyInput(); err != nil {
		return err
	}
	if err = phase("metadata-verified"); err != nil {
		return err
	}
	if updating && r.Package == baseline {
		if err = boundary("codex-rpm-before-unchanged"); err != nil {
			return err
		}
		if err = checkBaseline(); err != nil {
			return err
		}
		if err = phase("unchanged"); err != nil {
			return err
		}
		retain = false
		b.log(label("codex_rpm_unchanged"))
		return nil
	}
	if err = boundary("codex-rpm-before-stage"); err != nil {
		return err
	}
	if err = checkBaseline(); err != nil {
		return err
	}
	privileged = true
	out, err := call("mktemp", []string{"-d", "/run/atm-ai-codex-rpm-XXXXXXXXXX"})
	if err != nil {
		return err
	}
	r.Stage = strings.TrimSuffix(out, "\n")
	if !codexRPMStageRE.MatchString(r.Stage) {
		r.Stage = ""
		return fmt.Errorf("unexpected RPM root staging; inspect /run manually")
	}
	if err = phase("root-staged"); err != nil {
		return err
	}
	if err = s.stage(r.Stage); err != nil {
		return err
	}
	target := r.Stage + "/chatgpt.rpm"
	if _, err = call("install", []string{"-T", "-m", "0600", "--", local, target}); err != nil {
		return err
	}
	checkHash := func(path string) error {
		if e := s.stage(r.Stage); e != nil {
			return e
		}
		x, e := call("sha256sum", []string{"--", path})
		if e != nil {
			return e
		}
		if x != r.SHA256+"  "+path+"\n" {
			return fmt.Errorf("root RPM continuity mismatch")
		}
		return nil
	}
	if err = checkHash(target); err != nil {
		return err
	}
	dnf := func(args ...string) (string, error) {
		return call("dnf5", append(rpmDNFOptions(), args...))
	}
	policy, err := dnf("--dump-main-config")
	if err != nil {
		return err
	}
	if err = codexRPMPolicy(policy); err != nil {
		return err
	}
	db, err := call("rpm", []string{"--eval", "%{_dbpath}"})
	if err != nil {
		return err
	}
	if db != "/usr/lib/sysimage/rpm\n" && db != "/var/lib/rpm\n" {
		return fmt.Errorf("custom RPM database path refused")
	}
	tx := r.Stage + "/transaction"
	if err = boundary("codex-rpm-before-store"); err != nil {
		return err
	}
	if err = checkBaseline(); err != nil {
		return err
	}
	action := "install"
	if updating {
		action = "upgrade"
	}
	if _, err = dnf(action, "--store="+tx, target); err != nil {
		return err
	}
	identity := func(path string, directory bool) (string, error) {
		if e := s.stage(r.Stage); e != nil {
			return "", e
		}
		x, e := call("stat", []string{"--printf=%f\t%u\t%h\t%d\t%i\n", "--", path})
		if e != nil {
			return "", e
		}
		return codexRPMRootIdentity(x, directory)
	}
	txIdentity, err := identity(tx, true)
	if err != nil {
		return err
	}
	checkTransaction := func() error {
		now, e := identity(tx, true)
		if e != nil || now != txIdentity {
			return fmt.Errorf("stored RPM transaction directory changed: %v", e)
		}
		return nil
	}
	readPlan := func() ([]byte, error) {
		if e := checkTransaction(); e != nil {
			return nil, e
		}
		before, e := identity(tx+"/transaction.json", false)
		if e != nil {
			return nil, e
		}
		x, e := call("cat", []string{"--", tx + "/transaction.json"})
		if e != nil {
			return nil, e
		}
		after, e := identity(tx+"/transaction.json", false)
		if e != nil || before != after {
			return nil, fmt.Errorf("stored RPM plan identity changed: %v", e)
		}
		return []byte(x), e
	}
	plan, err := readPlan()
	if err != nil {
		return err
	}
	if updating {
		err = validateCodexRPMUpdatePlan(plan, r.Package, baseline)
	} else {
		err = validateCodexRPMPlan(plan, r.Package)
	}
	if err != nil {
		return err
	}
	r.PlanSHA256 = fmt.Sprintf("%x", sha256.Sum256(plan))
	if err = boundary("codex-rpm-before-populate"); err != nil {
		return err
	}
	// GNU install has no exclusive-create mode. Establish absence via a bounded
	// listing in the verified root-private tree; never overwrite an existing RPM.
	list := func(path string) (string, error) {
		if _, e := identity(path, true); e != nil {
			return "", e
		}
		return call("ls", []string{"-A1", "--quoting-style=escape", "--color=never", "--", path})
	}
	if err = checkTransaction(); err != nil {
		return err
	}
	names, err := list(tx)
	if err != nil {
		return err
	}
	hasPackages := false
	for _, name := range strings.Split(strings.TrimSuffix(names, "\n"), "\n") {
		switch name {
		case "transaction.json", "comps":
		case "packages":
			hasPackages = true
		default:
			return fmt.Errorf("unexpected stored RPM transaction entry")
		}
	}
	packages := tx + "/packages"
	if !hasPackages {
		if _, err = call("install", []string{"-d", "-m", "0700", "--", packages}); err != nil {
			return err
		}
	}
	packagesIdentity, err := identity(packages, true)
	if err != nil {
		return err
	}
	names, err = list(packages)
	if err != nil {
		return err
	}
	stored := packages + "/chatgpt.rpm"
	switch names {
	case "":
		if _, err = identity(target, false); err != nil {
			return err
		}
		if err = checkHash(target); err != nil {
			return err
		}
		if _, err = call("install", []string{"-T", "-m", "0600", "--", target, stored}); err != nil {
			return err
		}
	case "chatgpt.rpm\n": // DNF5 versions which already copied it: verify, never overwrite.
	default:
		return fmt.Errorf("unexpected stored RPM package entries")
	}
	checkStored := func() error {
		if e := checkTransaction(); e != nil {
			return e
		}
		now, e := identity(packages, true)
		if e != nil || now != packagesIdentity {
			return fmt.Errorf("stored RPM package directory changed: %v", e)
		}
		if _, e = identity(stored, false); e != nil {
			return e
		}
		return checkHash(stored)
	}
	if err = checkStored(); err != nil {
		return err
	}
	if err = phase("plan-verified"); err != nil {
		return err
	}
	if err = boundary("codex-rpm-before-replay"); err != nil {
		return err
	}
	if err = phase("replaying"); err != nil {
		return err
	}
	if err = checkBaseline(); err != nil {
		return err
	}
	now, err := dnf("--dump-main-config")
	if err != nil || now != policy {
		return fmt.Errorf("DNF5 policy changed: %v", err)
	}
	now, err = call("rpm", []string{"--eval", "%{_dbpath}"})
	if err != nil || now != db {
		return fmt.Errorf("RPM database policy changed: %v", err)
	}
	now, err = call("dnf5", []string{"--version"})
	if err != nil || now != version {
		return fmt.Errorf("DNF5 version changed: %v", err)
	}
	plan, err = readPlan()
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(plan)) != r.PlanSHA256 {
		return fmt.Errorf("stored RPM plan changed: %v", err)
	}
	if err = checkStored(); err != nil {
		return err
	}
	if err = checkBaseline(); err != nil {
		return err
	}
	if _, err = dnf("replay", tx); err != nil {
		return err
	}
	if err = boundary("codex-rpm-after-replay"); err != nil {
		return err
	}
	got, present, err := installedCodexRPM(ctx, s, b.arch)
	if err != nil || !present || got != r.Package {
		return fmt.Errorf("RPM post-state not verified: %v", err)
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = phase("complete"); err != nil {
		return err
	}
	retain = false
	message := "codex_rpm_done"
	if updating {
		message = "codex_rpm_updated"
	}
	b.log(fmt.Sprintf(label(message), r.Package.Version, r.Package.Release, r.Stage))
	return nil
}
