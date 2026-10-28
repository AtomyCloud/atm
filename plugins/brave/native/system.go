package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AtomyCloud/atm/internal/protocol"
)

const aptKeyURL = "https://brave-browser-apt-release.s3.brave.com/brave-browser-archive-keyring.gpg"
const aptKeyHash = "c85e85aa3d1783ffaa649ee8dbbc22af7f87192d304602d37e3018226b394788"
const rpmKeyURL = "https://brave-browser-rpm-release.s3.brave.com/brave-core.asc"
const rpmKeyHash = "bf978bd03e49f6006568d99f620763bf43a6455f6209dff791f52ebc6239f738"
const rpmRepo = "atm-brave-native"

type sysOptions struct{ action, plan string }
type sourceFile struct {
	Name string
	Data string
}
type systemPlan struct {
	Action, Manager, Arch, KeyHash string
	Sources                        []sourceFile
	Preferences                    []sourceFile
}

func systemOptions(action string, args []string) (sysOptions, error) {
	o := sysOptions{action: action}
	accept, removal := false, false
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if seen[a] {
			return o, fmt.Errorf("duplicate option")
		}
		seen[a] = true
		switch a {
		case "--accept-system-packages":
			accept = true
		case "--accept-removal":
			removal = true
		case "--plan":
			i++
			if i == len(args) || !validDigest(args[i]) {
				return o, fmt.Errorf("invalid plan digest")
			}
			o.plan = args[i]
		default:
			return o, fmt.Errorf("unknown system argument")
		}
	}
	if !accept {
		return o, fmt.Errorf("system changes require --accept-system-packages and CORE confirmation")
	}
	if action == "remove" && !removal {
		return o, fmt.Errorf("removal additionally requires --accept-removal")
	}
	if action == "install" && removal {
		return o, fmt.Errorf("removal acknowledgement not applicable")
	}
	return o, nil
}
func detectManager() (string, error) {
	if _, e := systemCommand("apt-get"); e == nil {
		return "apt-get", nil
	}
	if _, e := systemCommand("dnf"); e == nil {
		return "dnf", nil
	}
	return "", fmt.Errorf("trusted apt-get or dnf required")
}
func planHash(p systemPlan) string { v, _ := json.Marshal(p); return digest(v) }
func (b *backend) prepareSystem(action string) (systemPlan, error) {
	m, e := b.manager()
	p := systemPlan{Action: action, Manager: m, Arch: b.arch}
	if e != nil {
		return p, e
	}
	if m != "apt-get" && m != "dnf" {
		return p, fmt.Errorf("unsupported manager")
	}
	if action == "remove" {
		return p, nil
	}
	p.KeyHash = rpmKeyHash
	if m == "apt-get" {
		p.KeyHash = aptKeyHash
		p.Preferences, e = b.preferences()
		if e != nil {
			return p, e
		}
		p.Sources, e = b.sources()
	}
	return p, e
}
func aptSources(base string) ([]sourceFile, error) {
	var out []sourceFile
	var total int
	add := func(path, name string) error {
		if _, e := os.Lstat(path); os.IsNotExist(e) {
			return nil
		}
		v, e := readRegular(path, 1<<20)
		if e != nil {
			return e
		}
		if len(strings.TrimSpace(string(v))) == 0 {
			return nil
		}
		if e := trustedSystemFile(path, false); e != nil {
			return e
		}
		total += len(v)
		if total > 4<<20 {
			return fmt.Errorf("APT sources exceed 4 MiB")
		}
		// Do not reinterpret or silently adopt an existing Brave repository definition.
		if strings.Contains(strings.ToLower(string(v)), "brave") {
			return fmt.Errorf("existing Brave source declaration: %s; preserve and review it separately before native installation", path)
		}
		out = append(out, sourceFile{name, string(v)})
		return nil
	}
	if e := add(filepath.Join(base, "sources.list"), "system.list"); e != nil {
		return nil, e
	}
	d := filepath.Join(base, "sources.list.d")
	r, e := openDir(d)
	if os.IsNotExist(e) {
		return out, nil
	}
	if e != nil {
		return nil, e
	}
	defer r.Close()
	f, e := r.Open(".")
	if e != nil {
		return nil, e
	}
	entries, e := f.ReadDir(129)
	f.Close()
	if e != nil && e != io.EOF {
		return nil, e
	}
	if len(entries) > 128 {
		return nil, fmt.Errorf("too many APT source files")
	}
	safe := regexp.MustCompile(`^[A-Za-z0-9_-]+\.(list|sources)$`)
	for _, entry := range entries {
		n := entry.Name()
		if !strings.HasSuffix(n, ".list") && !strings.HasSuffix(n, ".sources") {
			continue
		}
		if !safe.MatchString(n) {
			return nil, fmt.Errorf("unsupported source filename")
		}
		if e = add(filepath.Join(d, n), "system-"+n); e != nil {
			return nil, e
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
func systemCommands(p systemPlan, stage string) [][]string {
	if p.Action == "remove" {
		if p.Manager == "apt-get" {
			return [][]string{{"apt-get", "-o", "APT::Get::AutomaticRemove=false", "-o", "APT::Get::Purge=false", "-y", "remove", "--", "brave-browser"}}
		}
		return [][]string{{"dnf", "--cacheonly", "--setopt=clean_requirements_on_remove=False", "-y", "remove", "brave-browser"}}
	}
	if p.Manager == "dnf" {
		arch := "x86_64"
		if p.Arch == "arm64" {
			arch = "aarch64"
		}
		return [][]string{{"dnf", "--repofrompath=" + rpmRepo + ",https://brave-browser-rpm-release.s3.brave.com/" + arch,
			"--setopt=*.excludepkgs=brave-browser", "--setopt=" + rpmRepo + ".excludepkgs=", "--setopt=" + rpmRepo + ".includepkgs=brave-browser",
			"--setopt=" + rpmRepo + ".gpgcheck=1", "--setopt=" + rpmRepo + ".repo_gpgcheck=0", "--setopt=" + rpmRepo + ".gpgkey=file://" + stage + "/brave.asc",
			"--setopt=" + rpmRepo + ".sslverify=True", "--setopt=" + rpmRepo + ".skip_if_unavailable=False", "--refresh", "-y", "install", "brave-browser"}}
	}
	base := []string{"apt-get", "-o", "Dir::Etc::sourcelist=" + stage + "/brave.sources", "-o", "Dir::Etc::sourceparts=" + stage + "/sources", "-o", "Dir::Etc::preferences=" + stage + "/preferences", "-o", "Dir::Etc::preferencesparts=" + stage + "/prefs", "-o", "APT::Get::AllowUnauthenticated=false", "-o", "Acquire::AllowInsecureRepositories=false", "-o", "Acquire::AllowDowngradeToInsecureRepositories=false", "-o", "APT::Update::Error-Mode=any", "-o", "APT::Get::AutomaticRemove=false", "-o", "APT::Get::Purge=false"}
	policy := append([]string{"apt-cache"}, base[1:]...)
	return [][]string{append(append([]string{}, base...), "update"), append(policy, "policy", "brave-browser"), append(append([]string{}, base...), "--no-remove", "--reinstall", "-y", "install", "--", "brave-browser")}
}
func (b *backend) describeSystem(p systemPlan) {
	b.log("System manager: " + p.Manager + "; package: brave-browser; architecture: " + p.Arch)
	b.log("Explicit system operation: dependencies, caches, package scripts and RPM trust database may change. No package ownership or rollback claimed. Profiles and existing repository/key files are preserved by ATM.")
	if p.Action == "install" {
		b.log("Temporary root-owned configuration; HTTPS key bootstrap pinned to reviewed SHA256 " + p.KeyHash)
		if p.Manager == "apt-get" {
			b.log("APT: scoped Signed-By and authenticated Release metadata. Administrator preferences and sources are copied unchanged and fingerprinted. A prohibited, foreign or ambiguous candidate is refused. Existing Brave source declarations require separate review.")
		} else {
			b.log("DNF: official RPM package gpgcheck=1; repo_gpgcheck=0 matches official repository (metadata is HTTPS-only). RPM system trust still applies. Other repositories exclude brave-browser for this invocation; no fallback after failure.")
		}
	}
	for _, cmd := range systemCommands(p, "/run/atm-brave-REVIEW") {
		v, _ := json.Marshal(cmd)
		b.log("Command argv: " + string(v))
	}
	b.log("Uses trusted absolute commands and sudo -n, never password reads or sudo -v preflight. Interrupted operations require manual process/package/journal inspection before retry.")
}
func (b *backend) systemMenu(view string) ([]protocol.Choice, error) {
	action := view
	if view == "commands" {
		action = "install"
	}
	p, e := b.prepareSystem(action)
	if e != nil {
		return nil, e
	}
	b.describeSystem(p)
	if view == "commands" {
		p.Action = "remove"
		b.describeSystem(p)
		return nil, nil
	}
	args := []string{"--accept-system-packages", "--plan", planHash(p)}
	if view == "remove" {
		args = append(args, "--accept-removal")
	}
	return []protocol.Choice{{ID: "apply", Label: label("apply-" + action), Operation: action, Arguments: args}}, nil
}
func (b *backend) fetchKey(ctx context.Context, p systemPlan) ([]byte, error) {
	url := rpmKeyURL
	if p.Manager == "apt-get" {
		url = aptKeyURL
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if e != nil {
		return nil, e
	}
	resp, e := b.client.Do(req)
	if e != nil {
		return nil, e
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("key HTTP status %d", resp.StatusCode)
	}
	v, e := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if e != nil {
		return nil, e
	}
	if len(v) > 1<<20 || digest(v) != p.KeyHash {
		return nil, fmt.Errorf("official signing key bytes changed; maintainer review required")
	}
	return v, nil
}
func modernAPT(s string) bool {
	f := strings.Fields(s)
	if len(f) < 2 || f[0] != "apt" {
		return false
	}
	v := strings.Split(f[1], ".")
	if len(v) < 2 {
		return false
	}
	a, e := strconv.Atoi(v[0])
	b, z := strconv.Atoi(v[1])
	return e == nil && z == nil && (a > 2 || a == 2 && b >= 2)
}

// All data passed to a privileged manager is copied into a root-owned /run tree,
// then verified byte-for-byte. _apt can traverse 0755 directories and read 0644 keys.
func (b *backend) applySystem(ctx context.Context, o sysOptions) error {
	p, e := b.prepareSystem(o.action)
	if e != nil {
		return e
	}
	if o.plan != "" && o.plan != planHash(p) {
		return fmt.Errorf("review plan changed")
	}
	b.describeSystem(p)
	j, release, e := openJournal(b.state)
	if e != nil {
		return e
	}
	launched, finished := false, false
	defer func() {
		if !launched || finished {
			release()
		}
	}()
	fresh, e := b.prepareSystem(o.action)
	if e != nil {
		return e
	}
	if planHash(fresh) != planHash(p) {
		return fmt.Errorf("review inputs changed under lock")
	}
	rec := operationRecord{Schema: 1, Plugin: "brave", Action: o.action, Status: "running", Phase: "prepared", Plan: planHash(p)}
	if e = j.save(rec); e != nil {
		return e
	}
	elevated := func(name string, args []string) (string, error) {
		if e := ctx.Err(); e != nil {
			return "", e
		}
		ex, e := b.resolve(name)
		if e != nil {
			return "", e
		}
		if b.uid() != 0 {
			sudo, e := b.resolve("sudo")
			if e != nil {
				return "", e
			}
			args = append([]string{"-n", "--preserve-env=DEBIAN_FRONTEND", "--", ex}, args...)
			ex = sudo
		}
		rec.Phase = name
		if e = j.save(rec); e != nil {
			return "", e
		}
		launched = true
		return b.run(ctx, ex, args)
	}
	var local, stage string
	defer func() {
		if local != "" && (!launched || ctx.Err() == nil) {
			_ = os.RemoveAll(local)
		}
	}()
	work := func() error {
		if o.action == "install" {
			key, e := b.fetch(ctx, p)
			if e != nil {
				return e
			}
			if p.Manager == "apt-get" {
				ex, e := b.resolve("apt-get")
				if e != nil {
					return e
				}
				v, e := b.run(ctx, ex, []string{"--version"})
				if e != nil {
					return e
				}
				if !modernAPT(v) {
					return fmt.Errorf("APT >=2.2 required for strict refresh errors")
				}
			}
			local, e = os.MkdirTemp("", "atm-brave-input-")
			if e != nil {
				return e
			}
			rec.Inputs = local
			output, e := elevated("mktemp", []string{"-d", "/run/atm-brave-XXXXXXXXXX"})
			if e != nil {
				return e
			}
			stage = strings.TrimSpace(output)
			if !regexp.MustCompile(`^/run/atm-brave-[A-Za-z0-9]{10}$`).MatchString(stage) {
				return fmt.Errorf("unexpected privileged staging path")
			}
			if e = b.stageCheck(stage); e != nil {
				return e
			}
			rec.Staging = stage
			if e = j.save(rec); e != nil {
				return e
			}
			if _, e = elevated("install", []string{"-d", "-m", "0755", "--", stage}); e != nil {
				return e
			}
			if _, e = elevated("install", []string{"-d", "-m", "0755", "--", stage + "/sources", stage + "/prefs"}); e != nil {
				return e
			}
			files := []sourceFile{}
			if p.Manager == "apt-get" {
				files = append(files, sourceFile{"brave.gpg", string(key)}, sourceFile{"brave.sources", "Types: deb\nURIs: https://brave-browser-apt-release.s3.brave.com\nSuites: stable\nComponents: main\nArchitectures: " + p.Arch + "\nSigned-By: " + stage + "/brave.gpg\n"}, sourceFile{"preferences", ""})
				for _, s := range p.Sources {
					files = append(files, sourceFile{"sources/" + s.Name, s.Data})
				}
				for _, s := range p.Preferences {
					if s.Name == "preferences" {
						files[2].Data = s.Data
					} else {
						files = append(files, sourceFile{"prefs/" + filepath.Base(s.Name), s.Data})
					}
				}
			} else {
				files = append(files, sourceFile{"brave.asc", string(key)})
			}
			for n, s := range files {
				src := filepath.Join(local, strconv.Itoa(n))
				if e = os.WriteFile(src, []byte(s.Data), 0600); e != nil {
					return e
				}
				target := stage + "/" + s.Name
				mode := "0644"
				private := s.Name != "brave.gpg" && s.Name != "brave.asc" && s.Name != "brave.sources"
				if private {
					mode = "0600"
				}
				if _, e = elevated("install", []string{"-m", mode, "--", src, target}); e != nil {
					return e
				}
				if private {
					if e = b.inputCheck(target, nil); e != nil {
						return e
					}
					checksum, e := elevated("sha256sum", []string{"--", target})
					if e != nil {
						return e
					}
					if strings.TrimSpace(checksum) != digest([]byte(s.Data))+"  "+target {
						return fmt.Errorf("private privileged source copy changed")
					}
				} else if e = b.inputCheck(target, []byte(s.Data)); e != nil {
					return e
				}
			}
		}
		candidate := ""
		for _, cmd := range systemCommands(p, stage) {
			if candidate != "" && cmd[0] == "apt-get" && cmd[len(cmd)-1] == "brave-browser" {
				cmd[len(cmd)-1] += "=" + candidate
			}
			out, e := elevated(cmd[0], cmd[1:])
			if out != "" {
				b.log("Package-manager output (bounded): " + out)
			}
			if e != nil {
				return e
			}
			if cmd[0] == "apt-cache" {
				candidate, e = officialCandidate(out, p.Arch)
				if e != nil {
					return e
				}
			}
		}
		return nil
	}
	e = work()
	if launched && ctx.Err() != nil {
		return fmt.Errorf("interrupted privileged operation: %w; running journal, lock and root staging retained; descendants may still run, inspect manually", ctx.Err())
	}
	if stage != "" {
		// Cleanup only the verified generated tree; never shared repositories/keyfiles.
		if b.stageValid(stage) {
			if _, ce := elevated("rm", []string{"--recursive", "--one-file-system", "--", stage}); ce != nil && e == nil {
				e = ce
			}
		} else if e == nil {
			e = fmt.Errorf("staging changed; preserved")
		}
	}
	if launched && ctx.Err() != nil {
		return fmt.Errorf("interrupted cleanup; journal/lock retained: %w", ctx.Err())
	}
	rec.Status = "completed"
	rec.Phase = "finished"
	if e != nil {
		rec.Status = "failed"
		rec.Phase = "stopped"
	}
	if se := j.save(rec); se != nil {
		return fmt.Errorf("operation outcome %v; journal error: %w", e, se)
	}
	finished = true
	if e != nil {
		return fmt.Errorf("system operation stopped: %w; partial changes may remain; inspect package state before retrying; no automatic rollback", e)
	}
	b.log("Requested package operations completed; system packages remain system-owned")
	return nil
}
func aptPreferences(base string) ([]sourceFile, error) {
	var out []sourceFile
	total := 0
	check := func(p, name string) error {
		if _, e := os.Lstat(p); os.IsNotExist(e) {
			return nil
		}
		v, e := readRegular(p, 1<<20)
		if e != nil {
			return e
		}
		if len(strings.TrimSpace(string(v))) == 0 {
			return nil
		}
		if e = trustedSystemFile(p, false); e != nil {
			return e
		}
		total += len(v)
		if total > 4<<20 {
			return fmt.Errorf("APT preferences exceed 4 MiB")
		}
		out = append(out, sourceFile{name, string(v)})
		return nil
	}
	if e := check(filepath.Join(base, "preferences"), "preferences"); e != nil {
		return nil, e
	}
	d := filepath.Join(base, "preferences.d")
	r, e := openDir(d)
	if os.IsNotExist(e) {
		return out, nil
	}
	if e != nil {
		return nil, e
	}
	defer r.Close()
	f, e := r.Open(".")
	if e != nil {
		return nil, e
	}
	entries, e := f.ReadDir(129)
	f.Close()
	if e != nil && e != io.EOF {
		return nil, e
	}
	if len(entries) > 128 {
		return nil, fmt.Errorf("too many APT preferences")
	}
	safe := regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	for _, entry := range entries {
		n := entry.Name()
		if strings.Contains(n, ".") && !strings.HasSuffix(n, ".pref") {
			continue
		}
		if !safe.MatchString(n) {
			return nil, fmt.Errorf("unsupported APT preference filename")
		}
		if e = check(filepath.Join(d, n), "parts/"+n); e != nil {
			return nil, e
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
func checkNewStage(stage string) error {
	i, e := os.Lstat(stage)
	if e != nil {
		return e
	}
	if !i.IsDir() || !rootOwned(i) || i.Mode().Perm() != 0700 {
		return fmt.Errorf("untrusted privileged stage")
	}
	return nil
}
func checkSystemInput(target string, want []byte) error {
	if e := trustedSystemFile(target, false); e != nil {
		return e
	}
	i, e := os.Lstat(target)
	if e != nil {
		return e
	}
	mode := os.FileMode(0644)
	if want == nil {
		mode = 0600
	}
	if i.Mode().Perm() != mode {
		return fmt.Errorf("privileged input has wrong visibility mode")
	}
	if want == nil {
		return nil
	} // Private contents are verified by privileged SHA256.
	v, e := readRegular(target, 4<<20)
	if e != nil {
		return e
	}
	if string(v) != string(want) {
		return fmt.Errorf("privileged input copy changed")
	}
	return nil
}

// LC_ALL=C policy output is deliberately parsed narrowly. Unknown/truncated
// formats fail closed; preferences alone cannot select an instance of a version.
func officialCandidate(output, arch string) (string, error) {
	if len(output) > 128<<10 {
		return "", fmt.Errorf("APT policy output too large")
	}
	candidate, current := "", ""
	official := false
	header := false
	version := regexp.MustCompile(`^[0-9][0-9A-Za-z.+:~\-]{0,127}$`)
	for _, line := range strings.Split(output, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if line == "brave-browser:" {
			if header {
				return "", fmt.Errorf("duplicate APT package policy")
			}
			header = true
			continue
		}
		if f[0] == "Candidate:" {
			if len(f) != 2 || candidate != "" || !version.MatchString(f[1]) {
				return "", fmt.Errorf("invalid APT candidate")
			}
			candidate = f[1]
			continue
		}
		if f[0] == "Installed:" || strings.TrimSpace(line) == "Version table:" {
			continue
		}
		if f[0] == "***" {
			f = f[1:]
		}
		if len(f) == 2 && version.MatchString(f[0]) {
			if _, e := strconv.Atoi(f[1]); e == nil {
				current = f[0]
				continue
			}
		}
		if current == "" {
			return "", fmt.Errorf("unknown APT policy format")
		}
		if _, e := strconv.Atoi(f[0]); e != nil {
			return "", fmt.Errorf("unknown APT policy priority")
		}
		if current != candidate {
			continue
		}
		if len(f) == 2 && f[1] == "/var/lib/dpkg/status" {
			continue
		}
		if len(f) != 5 || f[1] != "https://brave-browser-apt-release.s3.brave.com" || f[2] != "stable/main" || f[3] != arch || f[4] != "Packages" {
			return "", fmt.Errorf("APT candidate has ambiguous or foreign origin; refusing installation")
		}
		official = true
	}
	if !header || candidate == "" || !official {
		return "", fmt.Errorf("official authenticated Brave candidate unavailable")
	}
	return candidate, nil
}
func validStage(stage string) bool {
	if !regexp.MustCompile(`^/run/atm-brave-[A-Za-z0-9]{10}$`).MatchString(stage) {
		return false
	}
	i, e := os.Lstat(stage)
	return e == nil && i.IsDir() && rootOwned(i) && i.Mode().Perm()&0022 == 0
}

type tailOutput struct {
	mu   sync.Mutex
	data []byte
}

func (w *tailOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	const limit = 2048
	if len(p) >= limit {
		w.data = append(w.data[:0], p[len(p)-limit:]...)
	} else {
		if extra := len(w.data) + len(p) - limit; extra > 0 {
			w.data = w.data[extra:]
		}
		w.data = append(w.data, p...)
	}
	return n, nil
}
func commandEnvironment() []string {
	return []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "DEBIAN_FRONTEND=noninteractive", "TERM=dumb"}
}
func runCommand(ctx context.Context, path string, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = commandEnvironment()
	cmd.Dir = "/"
	cmd.Stdin = nil
	cmd.WaitDelay = time.Second
	out := &tailOutput{}
	cmd.Stdout = out
	cmd.Stderr = out
	e := cmd.Run()
	if ctx.Err() != nil {
		e = ctx.Err()
	}
	return string(out.data), e
}
