//go:build linux

package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

type rpmFixture struct {
	f                     *fixture
	files                 map[string][]byte
	dirs                  map[string]bool
	stats                 map[string]string
	policy                string
	repo                  string
	present               bool
	installed             string
	updating              bool
	removing              bool
	dependencyMode        bool
	dependencies          []codexRPMDependency
	installedDependencies map[string]string
	replays               int
	stages                int
	calls                 []string
	fault                 func(string, []string) error
}

func newRPMFixture(t *testing.T, arch string) *rpmFixture {
	t.Helper()
	r := &rpmFixture{f: newFixture(t, arch), files: map[string][]byte{}, dirs: map[string]bool{}, stats: map[string]string{}, policy: rpmPolicyFixture, repo: "@commandline"}
	url, err := codexRPMURL(arch)
	if err != nil {
		t.Fatal(err)
	}
	r.f.responses[url] = []byte("synthetic RPM; never executed")
	s := &systemTools{uid: func() int { return 1234 }, resolve: func(n string) (string, error) { return "/fixture/" + n, nil }, stage: func(p string) error {
		if !codexRPMStageRE.MatchString(p) {
			return fmt.Errorf("wrong stage")
		}
		return nil
	}}
	s.run = func(ctx context.Context, path string, args []string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		name := filepath.Base(path)
		if name == "sudo" {
			prefix := append([]string{"-n", "--", "/fixture/env", "-i"}, codexSystemEnvironment()...)
			if len(args) <= len(prefix) || !reflect.DeepEqual(args[:len(prefix)], prefix) {
				t.Fatalf("bad elevation %q", args)
			}
			name = filepath.Base(args[len(prefix)])
			args = args[len(prefix)+1:]
		}
		r.calls = append(r.calls, name+" "+strings.Join(args, " "))
		if r.fault != nil {
			if e := r.fault(name, args); e != nil {
				return "", e
			}
		}
		switch name {
		case "rpm":
			if len(args) > 0 && args[0] == "--eval" {
				return "/usr/lib/sysimage/rpm\n", nil
			}
			queryFile := false
			for _, a := range args {
				if a == "--package" || a == "-p" || a == "-qp" {
					queryFile = true
				}
			}
			for _, dep := range r.dependencies {
				if queryFile && strings.HasSuffix(args[len(args)-1], "/"+filepath.Base(dep.Path)) {
					m := dep.Package
					return strings.Join([]string{m.Name, m.Epoch, m.Version, m.Release, m.Arch}, "\t") + "\n", nil
				}
				if !queryFile && args[len(args)-1] == dep.Package.Name {
					return r.installedDependencies[dep.Package.Name], nil
				}
			}
			if !queryFile && r.present && r.installed != "" {
				return r.installed, nil
			}
			if queryFile || r.present {
				return r.metadata(), nil
			}
			return "", nil
		case "dnf5":
			if reflect.DeepEqual(args, []string{"--version"}) {
				return "dnf5 version 5.2.17.0\n", nil
			}
			dnfOptions := rpmDNFOptions()
			if r.dependencyMode {
				dnfOptions = append(dnfOptions[:1:1], dnfOptions[2:]...)
			}
			if r.removing {
				dnfOptions = append(dnfOptions, "--setopt=clean_requirements_on_remove=False")
			}
			if len(args) < len(dnfOptions) || !reflect.DeepEqual(args[:len(dnfOptions)], dnfOptions) {
				t.Fatalf("unsafe DNF options %q", args)
			}
			args = args[len(dnfOptions):]
			if r.dependencyMode && args[0] == "--disable-repo=*" {
				args = args[1:]
				if args[0] != "replay" {
					t.Fatal("dependency store must use configured repos")
				}
			} else if r.dependencyMode && args[0] == "replay" {
				t.Fatal("dependency replay must disable external repos")
			}
			switch args[0] {
			case "--dump-main-config":
				return r.policy, nil
			case "install", "upgrade":
				if (args[0] == "upgrade") != r.updating {
					t.Fatalf("wrong install/update action %q", args)
				}
				if len(args) != 3 || !strings.HasPrefix(args[1], "--store=") {
					t.Fatalf("not a store-only operation: %q", args)
				}
				tx := strings.TrimPrefix(args[1], "--store=")
				r.dirs[tx] = true
				r.files[tx+"/transaction.json"] = r.plan(t)
				if len(r.dependencies) > 0 {
					r.dirs[tx+"/packages"] = true
					for _, dep := range r.dependencies {
						r.files[filepath.Join(tx, dep.Path)] = []byte("synthetic dependency " + dep.Package.Name)
					}
				}
				return "stored", nil
			case "remove":
				m, err := parseCodexRPMMetadata(r.installed, r.f.b.arch)
				if err != nil {
					t.Fatal(err)
				}
				target, err := codexRPMRemovalTarget(m)
				if err != nil || !r.removing || len(args) != 4 || args[1] != "--no-autoremove" || !strings.HasPrefix(args[2], "--store=") || args[3] != target {
					t.Fatalf("unsafe remove command: %q %v", args, err)
				}
				tx := strings.TrimPrefix(args[2], "--store=")
				r.dirs[tx] = true
				r.files[tx+"/transaction.json"] = r.plan(t)
				return "stored", nil
			case "replay":
				r.replays++
				if r.dependencyMode {
					r.installedDependencies = map[string]string{}
					for _, dep := range r.dependencies {
						m := dep.Package
						r.installedDependencies[m.Name] = strings.Join([]string{m.Name, m.Epoch, m.Version, m.Release, m.Arch}, "\t") + "\n"
					}
				}
				r.present = !r.removing
				if r.removing {
					r.installed = ""
				} else {
					r.installed = r.metadata()
				}
				return "done", nil
			default:
				t.Fatalf("unexpected DNF action: %q", args)
			}
		case "mktemp":
			r.stages++
			stage := "/run/atm-ai-codex-rpm-AbCd012345"
			if r.stages > 1 {
				stage = fmt.Sprintf("/run/atm-ai-codex-rpm-AbCd%06d", r.stages)
			}
			r.dirs[stage] = true
			return stage + "\n", nil
		case "install":
			if len(args) == 5 && reflect.DeepEqual(args[:4], []string{"-d", "-m", "0700", "--"}) {
				if r.dirs[args[4]] {
					t.Fatal("unexpected directory overwrite")
				}
				r.dirs[args[4]] = true
				return "", nil
			}
			if len(args) != 6 || !reflect.DeepEqual(args[:4], []string{"-T", "-m", "0600", "--"}) {
				t.Fatalf("unsafe root copy %q", args)
			}
			if _, exists := r.files[args[5]]; exists {
				t.Fatal("unexpected file overwrite", args[5])
			}
			data, ok := r.files[args[4]]
			var e error
			if !ok {
				data, e = os.ReadFile(args[4])
			}
			r.files[args[5]] = append([]byte(nil), data...)
			return "", e
		case "stat":
			if len(args) != 3 || args[0] != "--printf=%f\t%u\t%h\t%d\t%i\n" || args[1] != "--" {
				t.Fatalf("unsafe stat %q", args)
			}
			p := args[2]
			if value, ok := r.stats[p]; ok {
				return value, nil
			}
			mode := "8180"
			if r.dirs[p] {
				mode = "41c0"
			} else if _, ok := r.files[p]; !ok {
				return "", fmt.Errorf("missing fixture stat %s", p)
			}
			return mode + "\t0\t1\t1\t123\n", nil
		case "ls":
			if len(args) != 5 || !reflect.DeepEqual(args[:4], []string{"-A1", "--quoting-style=escape", "--color=never", "--"}) {
				t.Fatalf("unsafe listing %q", args)
			}
			var names []string
			for p := range r.files {
				if filepath.Dir(p) == args[4] {
					names = append(names, filepath.Base(p))
				}
			}
			for p := range r.dirs {
				if filepath.Dir(p) == args[4] {
					names = append(names, filepath.Base(p))
				}
			}
			sort.Strings(names)
			if len(names) == 0 {
				return "", nil
			}
			return strings.Join(names, "\n") + "\n", nil
		case "sha256sum":
			data, ok := r.files[args[1]]
			if !ok {
				return "", fmt.Errorf("missing fixture root file")
			}
			return fmt.Sprintf("%x  %s\n", sha256.Sum256(data), args[1]), nil
		case "cat":
			data, ok := r.files[args[1]]
			if !ok {
				return "", fmt.Errorf("missing fixture plan")
			}
			return string(data), nil
		default:
			t.Fatalf("unexpected tool %s %q", name, args)
		}
		return "", nil
	}
	s.quietRun = s.run
	r.f.b.codexRPM = s
	return r
}

const rpmPolicyFixture = "======== Main configuration: ========\ninstallroot = /\ntsflags = \nlocalpkg_gpgcheck = 1\noptional_metadata_types = \nproxy\nskip_system_repo_lock = false\nclean_requirements_on_remove = false\n"

func (r *rpmFixture) metadata() string {
	arch, _ := codexRPMArch(r.f.b.arch)
	return "chatgpt\t0\t1.2026.100\t1\t" + arch + "\n"
}
func (r *rpmFixture) plan(t *testing.T) []byte {
	t.Helper()
	arch, _ := codexRPMArch(r.f.b.arch)
	p := map[string]any{"version": "1.0", "rpms": []any{map[string]string{"nevra": "chatgpt-1.2026.100-1." + arch, "action": "Install", "reason": "User", "repo_id": r.repo, "package_path": "./packages/chatgpt.rpm"}}}
	for _, dep := range r.dependencies {
		m := dep.Package
		version := m.Version
		if m.Epoch != "0" {
			version = m.Epoch + ":" + version
		}
		p["rpms"] = append(p["rpms"].([]any), map[string]string{"nevra": m.Name + "-" + version + "-" + m.Release + "." + m.Arch, "action": "Install", "reason": "Dependency", "repo_id": dep.Repo, "package_path": dep.Path})
	}
	if r.removing {
		m, err := parseCodexRPMMetadata(r.installed, r.f.b.arch)
		if err != nil {
			t.Fatal(err)
		}
		target, err := codexRPMRemovalTarget(m)
		if err != nil {
			t.Fatal(err)
		}
		p["rpms"] = []any{map[string]string{"nevra": target, "action": "Remove", "reason": "User", "repo_id": "@System"}}
	}
	if r.updating {
		baseline, err := parseCodexRPMMetadata(r.installed, r.f.b.arch)
		if err != nil {
			t.Fatal(err)
		}
		version := baseline.Version
		if baseline.Epoch != "0" {
			version = baseline.Epoch + ":" + version
		}
		p["rpms"] = []any{
			map[string]string{"nevra": "chatgpt-1.2026.100-1." + arch, "action": "Upgrade", "reason": "User", "repo_id": r.repo, "package_path": "./packages/chatgpt.rpm"},
			map[string]string{"nevra": "chatgpt-" + version + "-" + baseline.Release + "." + arch, "action": "Replaced", "reason": "User", "repo_id": "@System"},
		}
	}
	d, e := json.Marshal(p)
	if e != nil {
		t.Fatal(e)
	}
	return d
}
func (r *rpmFixture) run() error {
	op := "install"
	if r.removing {
		return r.f.run(context.Background(), "uninstall", "--provider", codexDesktopRPM, "--accept-system-packages")
	}
	if r.updating {
		op = "update"
	}
	if r.dependencyMode {
		return r.f.run(context.Background(), op, "--provider", codexDesktopRPM, "--trust-download", "--accept-system-packages", "--accept-dependencies")
	}
	return r.f.run(context.Background(), op, "--provider", codexDesktopRPM, "--trust-download", "--accept-system-packages")
}

func TestCodexRPMInitialLifecycle(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		t.Run(arch, func(t *testing.T) {
			r := newRPMFixture(t, arch)
			if _, e := r.f.b.menu(context.Background(), codexDesktopRPM); e != nil {
				t.Fatal(e)
			}
			r.f.b.codexRPMStatus()
			if len(r.calls) != 0 || r.f.calls != 0 {
				t.Fatal("menu/status executed commands or network")
			}
			if e := r.run(); e != nil {
				t.Fatal(e)
			}
			if r.replays != 1 || !r.present {
				t.Fatal("no installation")
			}
			j, e := r.f.b.rpmJournal()
			if e != nil || j == nil || j.Phase != "complete" {
				t.Fatal(j, e)
			}
			integrationMissing(t, r.f.b.root(codexDesktopRPM)+".lock")
			if _, e = os.Stat(filepath.Join(j.Input, "chatgpt.rpm")); e != nil {
				t.Fatal("retained input missing", e)
			}
			before := r.f.calls
			if e = r.run(); e == nil || r.replays != 1 || r.f.calls != before {
				t.Fatal("existing package was modified", e)
			}
		})
	}
}

func TestCodexRPMPlanRefusals(t *testing.T) {
	r := newRPMFixture(t, "amd64")
	m, e := parseCodexRPMMetadata(r.metadata(), "amd64")
	if e != nil {
		t.Fatal(e)
	}
	valid := string(r.plan(t))
	for name, data := range map[string]string{
		"duplicate":     strings.Replace(valid, `"version":"1.0"`, `"version":"1.0","version":"1.0"`, 1),
		"schema":        strings.Replace(valid, `"1.0"`, `"2.0"`, 1),
		"upgrade":       strings.Replace(valid, `"Install"`, `"Upgrade"`, 1),
		"traversal":     strings.Replace(valid, "packages/chatgpt.rpm", "../chatgpt.rpm", 1),
		"remote":        strings.Replace(valid, "packages/chatgpt.rpm", "https://example.invalid/file.rpm", 1),
		"repo":          strings.Replace(valid, "@commandline", "fedora", 1),
		"path-spelling": strings.Replace(valid, "./packages/chatgpt.rpm", "packages/chatgpt.rpm", 1),
		"version":       strings.Replace(valid, "1.2026.100-1", "0.1-1", 1),
		"architecture":  strings.Replace(valid, "x86_64", "aarch64", 1),
		"unknown":       strings.TrimSuffix(valid, "}") + `,"groups":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if e := validateCodexRPMPlan([]byte(data), m); e == nil {
				t.Fatal("accepted unsafe plan")
			}
		})
	}
}

func TestCodexRPMFailuresNeverReplay(t *testing.T) {
	for _, failure := range []string{"header", "copy", "plan", "stored-copy", "existing-before-replay", "policy", "cancel"} {
		t.Run(failure, func(t *testing.T) {
			r := newRPMFixture(t, "amd64")
			r.fault = func(name string, args []string) error {
				if failure == "header" && name == "rpm" && len(args) > 0 && args[0] == "-qp" {
					return fmt.Errorf("bad header")
				}
				if failure == "copy" && name == "sha256sum" {
					r.files[args[1]] = []byte("changed")
				}
				if failure == "plan" && name == "cat" {
					r.files[args[1]] = []byte(`{"version":"1.0","rpms":[]}`)
				}
				if failure == "stored-copy" && name == "sha256sum" && strings.Contains(args[1], "/packages/") {
					r.files[args[1]] = []byte("changed")
				}
				if failure == "policy" && name == "dnf5" && strings.Contains(strings.Join(args, " "), "--dump-main-config") {
					return fmt.Errorf("unsupported policy")
				}
				return nil
			}
			r.f.b.checkpoint = func(p string) error {
				if p == "codex-rpm-before-replay" {
					if failure == "cancel" {
						return context.Canceled
					}
					if failure == "existing-before-replay" {
						r.present = true
					}
				}
				return nil
			}
			if e := r.run(); e == nil {
				t.Fatal("failure accepted")
			}
			if r.replays != 0 {
				t.Fatal("failed preflight replayed")
			}
			if _, e := os.Stat(r.f.b.root(codexDesktopRPM) + ".lock"); e != nil {
				t.Fatal("missing retained lock", e)
			}
		})
	}
}

func TestCodexRPMUncertainReplayAndPostcheck(t *testing.T) {
	for _, kind := range []string{"timeout", "missing", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			r := newRPMFixture(t, "amd64")
			r.fault = func(name string, args []string) error {
				if kind == "timeout" && name == "dnf5" && strings.Contains(strings.Join(args, " "), " replay ") {
					return context.DeadlineExceeded
				}
				return nil
			}
			r.f.b.checkpoint = func(p string) error {
				if p == "codex-rpm-after-replay" {
					if kind == "missing" {
						r.present = false
					}
					if kind == "cancel" {
						return context.Canceled
					}
				}
				return nil
			}
			e := r.run()
			if e == nil {
				t.Fatal("uncertain result accepted")
			}
			if kind == "cancel" && !errors.Is(e, context.Canceled) {
				t.Fatal(e)
			}
			j, e := r.f.b.rpmJournal()
			if e != nil || j.Phase != "unknown" {
				t.Fatal(j, e)
			}
			before := r.replays
			if e = r.run(); e == nil || r.replays != before {
				t.Fatal("uncertainty did not block repeat", e)
			}
		})
	}
}

func TestCodexRPMOptionsAndNoopViews(t *testing.T) {
	for _, op := range []string{"use", "remove", "uninstall", "integrate"} {
		if _, e := parseOptions(op, []string{"--provider", codexDesktopRPM, "--trust-download", "--accept-system-packages"}); e == nil {
			t.Fatal(op)
		}
	}
	for _, op := range []string{"install", "update"} {
		for _, flags := range [][]string{{}, {"--trust-download"}, {"--accept-system-packages"}, {"--trust-download", "--accept-system-packages", "--version", "1.2.3"}, {"--trust-download", "--accept-system-packages", "--rocm"}} {
			if _, e := parseOptions(op, append([]string{"--provider", codexDesktopRPM}, flags...)); e == nil {
				t.Fatal(op, flags)
			}
		}
		if _, e := parseOptions(op, []string{"--provider", codexDesktopRPM, "--trust-download", "--accept-system-packages"}); e != nil {
			t.Fatal(e)
		}
	}
}

func newRPMUpdateFixture(t *testing.T, arch string) *rpmFixture {
	t.Helper()
	r := newRPMFixture(t, arch)
	r.updating, r.present = true, true
	r.installed = strings.Replace(r.metadata(), "1.2026.100", "1.2026.99", 1)
	return r
}

func TestCodexRPMUpdateLifecycle(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		t.Run(arch, func(t *testing.T) {
			r := newRPMUpdateFixture(t, arch)
			baseline, err := parseCodexRPMMetadata(r.installed, arch)
			if err != nil {
				t.Fatal(err)
			}
			if err = r.run(); err != nil {
				t.Fatal(err)
			}
			j, err := r.f.b.rpmJournal()
			if err != nil || j == nil || j.Schema != 2 || j.Operation != "update" || j.Baseline != baseline || j.Phase != "complete" || j.Package == baseline || r.replays != 1 {
				t.Fatalf("update evidence %+v, replays=%d, err=%v", j, r.replays, err)
			}
			integrationMissing(t, r.f.b.root(codexDesktopRPM)+".lock")
			if _, err = os.Stat(filepath.Join(j.Input, "chatgpt.rpm")); err != nil {
				t.Fatal("update evidence not retained", err)
			}
		})
	}
}

func TestCodexRPMUpdateRefusesMissingUnknownAndChangedBaseline(t *testing.T) {
	for _, mode := range []string{"absent", "query-error", "multiple", "architecture"} {
		t.Run(mode, func(t *testing.T) {
			r := newRPMUpdateFixture(t, "amd64")
			switch mode {
			case "absent":
				r.present = false
			case "query-error":
				r.fault = func(name string, args []string) error {
					if name == "rpm" && args[0] == "-qa" {
						return errors.New("unknown database")
					}
					return nil
				}
			case "multiple":
				r.installed += r.installed
			case "architecture":
				r.installed = strings.Replace(r.installed, "x86_64", "aarch64", 1)
			}
			if err := r.run(); err == nil || r.replays != 0 || r.f.calls != 0 {
				t.Fatalf("invalid original state accepted: %v", err)
			}
		})
	}
	for _, point := range []string{"codex-rpm-before-stage", "codex-rpm-before-store", "codex-rpm-before-replay"} {
		for _, change := range []string{"removed", "version", "epoch", "release"} {
			t.Run(point+"/"+change, func(t *testing.T) {
				r := newRPMUpdateFixture(t, "amd64")
				r.f.b.checkpoint = func(got string) error {
					if got == point {
						switch change {
						case "removed":
							r.present = false
						case "version":
							r.installed = r.metadata()
						case "epoch":
							r.installed = strings.Replace(r.installed, "\t0\t", "\t1\t", 1)
						case "release":
							r.installed = strings.Replace(r.installed, "\t1\tx86_64", "\t2\tx86_64", 1)
						}
					}
					return nil
				}
				if err := r.run(); err == nil || r.replays != 0 {
					t.Fatalf("changed baseline reached replay: %v", err)
				}
			})
		}
	}
}

func TestCodexRPMUpdateUnchangedAndLegacyJournal(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprint(legacy), func(t *testing.T) {
			r := newRPMFixture(t, "amd64")
			if legacy {
				if err := r.run(); err != nil {
					t.Fatal(err)
				}
				j, err := r.f.b.rpmJournal()
				if err != nil {
					t.Fatal(err)
				}
				j.Schema, j.Operation, j.Baseline = 1, "", codexRPMMetadata{}
				if err = r.f.b.saveRPMJournal(*j); err != nil {
					t.Fatal(err)
				}
				if _, err = r.f.b.rpmJournal(); err != nil {
					t.Fatal("legacy initial journal rejected", err)
				}
			}
			r.updating, r.present, r.installed = true, true, r.metadata()
			r.calls, r.replays = nil, 0
			if err := r.run(); err != nil {
				t.Fatal(err)
			}
			j, err := r.f.b.rpmJournal()
			if err != nil || j == nil || j.Schema != 2 || j.Operation != "update" || j.Phase != "unchanged" || j.Baseline != j.Package || j.Stage != "" || r.replays != 0 {
				t.Fatalf("invalid no-op: %+v %v", j, err)
			}
			for _, call := range r.calls {
				if !strings.HasPrefix(call, "rpm ") && call != "dnf5 --version" {
					t.Fatalf("unchanged version performed privileged work: %s", call)
				}
			}
			integrationMissing(t, r.f.b.root(codexDesktopRPM)+".lock")
			j.Baseline = codexRPMMetadata{}
			if err = r.f.b.saveRPMJournal(*j); err != nil {
				t.Fatal(err)
			}
			if _, err = r.f.b.rpmJournal(); err == nil {
				t.Fatal("update journal with missing baseline accepted")
			}
		})
	}
}

func TestCodexRPMUpdateLateBaselineAndPostcondition(t *testing.T) {
	for _, mode := range []string{"late-query", "replay-error", "post-absent", "post-old", "no-op-changed"} {
		t.Run(mode, func(t *testing.T) {
			r := newRPMUpdateFixture(t, "amd64")
			original := r.installed
			if mode == "no-op-changed" {
				r.installed = r.metadata()
			}
			if mode == "late-query" || mode == "replay-error" {
				versions := 0
				r.fault = func(name string, args []string) error {
					if name == "dnf5" && reflect.DeepEqual(args, []string{"--version"}) {
						versions++
						if mode == "late-query" && versions == 2 {
							r.present = false
						}
					}
					if mode == "replay-error" && name == "dnf5" && len(args) > len(rpmDNFOptions()) && args[len(rpmDNFOptions())] == "replay" {
						return errors.New("uncertain update")
					}
					return nil
				}
			}
			r.f.b.checkpoint = func(point string) error {
				if point == "codex-rpm-after-replay" {
					if mode == "post-absent" {
						r.present = false
					} else if mode == "post-old" {
						r.installed = original
					}
				}
				if point == "codex-rpm-before-unchanged" && mode == "no-op-changed" {
					r.present = false
				}
				return nil
			}
			if err := r.run(); err == nil {
				t.Fatal("uncertain update succeeded")
			}
			if (mode == "late-query" || mode == "no-op-changed") && r.replays != 0 {
				t.Fatal("changed baseline reached replay")
			}
			if _, err := os.Stat(r.f.b.root(codexDesktopRPM) + ".lock"); err != nil {
				t.Fatal("uncertain update lost lock", err)
			}
		})
	}
}

func TestCodexRPMPolicyOutput(t *testing.T) {
	for _, valid := range []string{
		rpmPolicyFixture,
		strings.ReplaceAll(rpmPolicyFixture, "skip_system_repo_lock = false\n", ""),
		strings.ReplaceAll(rpmPolicyFixture, "localpkg_gpgcheck = 1", "localpkg_gpgcheck = false"),
	} {
		if err := codexRPMPolicy(valid, false); err != nil {
			t.Fatal(err)
		}
	}
	for name, invalid := range map[string]string{
		"ini-not-dump":     strings.Replace(rpmPolicyFixture, "======== Main configuration: ========", "[main]", 1),
		"lock-disabled":    strings.Replace(rpmPolicyFixture, "skip_system_repo_lock = false", "skip_system_repo_lock = true", 1),
		"lock-unset":       strings.Replace(rpmPolicyFixture, "skip_system_repo_lock = false", "skip_system_repo_lock", 1),
		"lock-unknown":     strings.Replace(rpmPolicyFixture, "skip_system_repo_lock = false", "skip_system_repo_lock = maybe", 1),
		"required-unset":   strings.Replace(rpmPolicyFixture, "tsflags = ", "tsflags", 1),
		"duplicate-unset":  rpmPolicyFixture + "proxy\n",
		"duplicate-header": rpmPolicyFixture + "======== Main configuration: ========\n",
		"custom-root":      strings.Replace(rpmPolicyFixture, "installroot = /", "installroot = /other", 1),
		"custom-flags":     strings.Replace(rpmPolicyFixture, "tsflags = ", "tsflags = justdb", 1),
		"oversized":        strings.Repeat("x", (64<<10)+1),
	} {
		t.Run(name, func(t *testing.T) {
			if codexRPMPolicy(invalid, false) == nil {
				t.Fatal("unsafe policy accepted")
			}
		})
	}
}

func TestCodexRPMStoredPayloadPopulation(t *testing.T) {
	const stage = "/run/atm-ai-codex-rpm-AbCd012345"
	const tx = stage + "/transaction"
	const packages = tx + "/packages"
	for _, kind := range []string{"absent", "empty", "already-copied", "bad-copy", "package-link", "directory-link", "transaction-link", "extra-entry", "stat-error", "listing-error"} {
		t.Run(kind, func(t *testing.T) {
			r := newRPMFixture(t, "amd64")
			// Exercise the newer schema-1.0 repo spelling as well as the default 5.2.17 fixture.
			r.repo = "@stored_transaction(@commandline)"
			populating := false
			r.f.b.checkpoint = func(point string) error {
				if point != "codex-rpm-before-populate" {
					return nil
				}
				populating = true
				if kind != "absent" {
					r.dirs[packages] = true
				}
				switch kind {
				case "already-copied", "bad-copy", "package-link":
					r.files[packages+"/chatgpt.rpm"] = append([]byte(nil), r.files[stage+"/chatgpt.rpm"]...)
					if kind == "bad-copy" {
						r.files[packages+"/chatgpt.rpm"] = []byte("foreign payload")
					}
					if kind == "package-link" {
						r.stats[packages+"/chatgpt.rpm"] = "a1ff\t0\t1\t1\t999\n"
					}
				case "directory-link":
					r.stats[packages] = "a1ff\t0\t1\t1\t999\n"
				case "transaction-link":
					r.stats[tx] = "a1ff\t0\t1\t1\t999\n"
				case "extra-entry":
					r.files[packages+"/foreign.rpm"] = []byte("preserve")
				}
				return nil
			}
			r.fault = func(name string, args []string) error {
				if populating && (kind == "stat-error" && name == "stat" || kind == "listing-error" && name == "ls") {
					return errors.New("injected inspection failure")
				}
				return nil
			}
			err := r.run()
			wantSuccess := kind == "absent" || kind == "empty" || kind == "already-copied"
			if (err == nil) != wantSuccess || (r.replays == 1) != wantSuccess {
				t.Fatalf("replays=%d error=%v", r.replays, err)
			}
			copies := 0
			for _, call := range r.calls {
				if strings.HasPrefix(call, "install -T ") && strings.HasSuffix(call, " "+packages+"/chatgpt.rpm") {
					copies++
				}
			}
			wantCopies := 0
			if kind == "absent" || kind == "empty" {
				wantCopies = 1
			}
			if copies != wantCopies {
				t.Fatalf("copied existing or untrusted entry: %d", copies)
			}
		})
	}
}

func TestCodexRPMLockIdentityBoundaries(t *testing.T) {
	for _, point := range []string{"codex-rpm-before-stage", "codex-rpm-before-store", "codex-rpm-before-populate", "codex-rpm-before-replay", "codex-rpm-after-replay", "codex-rpm-before-journal-complete"} {
		for _, replace := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/replace=%t", point, replace), func(t *testing.T) {
				r := newRPMFixture(t, "amd64")
				lock := r.f.b.root(codexDesktopRPM) + ".lock"
				var replacement os.FileInfo
				callsAtMutation := -1
				r.f.b.checkpoint = func(p string) error {
					if p != point {
						return nil
					}
					callsAtMutation = len(r.calls)
					if err := os.Remove(lock); err != nil {
						t.Fatal(err)
					}
					if replace {
						if err := os.Mkdir(lock, 0700); err != nil {
							t.Fatal(err)
						}
						replacement, _ = os.Lstat(lock)
					}
					return nil
				}
				if err := r.run(); err == nil || !strings.Contains(err.Error(), "lock changed") {
					t.Fatal("lost lock accepted", err)
				}
				if callsAtMutation < 0 || len(r.calls) != callsAtMutation {
					t.Fatal("continued commands after lock loss")
				}
				if replace {
					now, err := os.Lstat(lock)
					if err != nil || !os.SameFile(now, replacement) {
						t.Fatal("replacement lock removed", err)
					}
				} else {
					integrationMissing(t, lock)
				}
				j, err := r.f.b.rpmJournal()
				if err != nil || j == nil || j.Phase == "complete" {
					t.Fatal("published success after lock loss", j, err)
				}
			})
		}
	}
}

func TestCodexRPMLateMutationsAndJournalFailures(t *testing.T) {
	for _, kind := range []string{"plan", "policy", "journal-missing", "journal-replaced", "journal-temp-replay", "journal-temp-complete", "journal-error-replay", "journal-error-complete"} {
		t.Run(kind, func(t *testing.T) {
			r := newRPMFixture(t, "amd64")
			point := "codex-rpm-before-journal-replaying"
			if strings.HasSuffix(kind, "complete") {
				point = "codex-rpm-before-journal-complete"
			}
			root := r.f.b.root(codexDesktopRPM)
			journal := filepath.Join(root, "journal.json")
			mutated := false
			r.f.b.checkpoint = func(p string) error {
				if p != point {
					return nil
				}
				mutated = true
				switch kind {
				case "plan":
					for path, data := range r.files {
						if strings.HasSuffix(path, "/transaction.json") {
							r.files[path] = append(data, '\n')
						}
					}
				case "policy":
					r.policy = strings.Replace(r.policy, "localpkg_gpgcheck = 1", "localpkg_gpgcheck = 0", 1)
				case "journal-missing":
					if err := os.Remove(journal); err != nil {
						t.Fatal(err)
					}
				case "journal-replaced":
					if err := os.WriteFile(journal, []byte("foreign evidence\n"), 0600); err != nil {
						t.Fatal(err)
					}
				case "journal-temp-replay", "journal-temp-complete":
					if err := os.WriteFile(filepath.Join(root, ".journal-next"), []byte("preserve"), 0600); err != nil {
						t.Fatal(err)
					}
				case "journal-error-replay", "journal-error-complete":
					return errors.New("injected journal publication failure")
				}
				return nil
			}
			if err := r.run(); err == nil || !mutated {
				t.Fatal("late mutation accepted", err)
			}
			wantReplays := 0
			if strings.HasSuffix(kind, "complete") {
				wantReplays = 1
			}
			if r.replays != wantReplays {
				t.Fatal("unexpected replay", r.replays)
			}
			if _, err := os.Stat(root + ".lock"); err != nil {
				t.Fatal("uncertainty lock missing", err)
			}
			switch kind {
			case "journal-missing":
				integrationMissing(t, journal)
			case "journal-replaced":
				data, err := os.ReadFile(journal)
				if err != nil || string(data) != "foreign evidence\n" {
					t.Fatal("replaced journal overwritten", err)
				}
			case "journal-temp-replay", "journal-temp-complete":
				data, err := os.ReadFile(filepath.Join(root, ".journal-next"))
				if err != nil || string(data) != "preserve" {
					t.Fatal("pending evidence overwritten", err)
				}
			default:
				j, err := r.f.b.rpmJournal()
				if err != nil || j == nil || j.Phase == "complete" {
					t.Fatal(j, err)
				}
			}
		})
	}
}
