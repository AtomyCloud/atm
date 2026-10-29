//go:build linux

package main

import (
	"context"
	"os"
	"strings"
	"testing"
)

func rpmDependencyRecord(m codexRPMMetadata) string {
	return strings.Join([]string{m.Name, m.Epoch, m.Version, m.Release, m.Arch}, "\t") + "\n"
}

func newRPMDependencyUpgradesFixture(t *testing.T, arch, operation string) *rpmFixture {
	t.Helper()
	r := newRPMDependenciesFixture(t, arch)
	r.dependencyUpgrades = true
	original := r.dependencies[0].Package
	original.Version = "1.0.0"
	r.dependencyBaselines = map[string]codexRPMMetadata{original.Name: original}
	r.installedDependencies[original.Name] = rpmDependencyRecord(original)
	if operation == "update" {
		r.updating, r.present = true, true
		r.installed = strings.Replace(r.metadata(), "1.2026.100", "1.2026.99", 1)
	}
	return r
}

func TestCodexRPMDependencyUpgradesLifecycle(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		for _, operation := range []string{"install", "update"} {
			t.Run(arch+"/"+operation, func(t *testing.T) {
				r := newRPMDependencyUpgradesFixture(t, arch, operation)
				r.installedDependencies["unrelated"] = "preserved\n"
				if err := r.run(); err != nil {
					t.Fatal(err)
				}
				j, err := r.f.b.rpmJournal()
				if err != nil || j.Schema != 6 || j.Operation != operation || j.Phase != "complete" || j.DependencyCount != 2 || j.DependencyUpgradeCount != 1 || r.replays != 1 {
					t.Fatal(j, err, r.replays)
				}
				for _, dep := range r.dependencies {
					if err := parseRPMDependencyMetadata(r.installedDependencies[dep.Package.Name], dep.Package); err != nil {
						t.Fatal(err)
					}
				}
				if r.installedDependencies["unrelated"] != "preserved\n" {
					t.Fatal("unrelated package changed")
				}
				integrationMissing(t, r.f.b.root(codexDesktopRPM)+".lock")
			})
		}
	}
	for _, empty := range []bool{false, true} {
		r := newRPMDependencyUpgradesFixture(t, "amd64", "install")
		r.dependencyBaselines = nil
		r.installedDependencies = map[string]string{}
		if empty {
			r.dependencies = nil
		}
		if err := r.run(); err != nil {
			t.Fatal(err)
		}
		j, err := r.f.b.rpmJournal()
		if err != nil || j.Schema != 6 || j.DependencyUpgradeCount != 0 || j.DependencyCount != len(r.dependencies) {
			t.Fatal(j, err)
		}
	}
}

func TestCodexRPMDependencyUpgradesNoop(t *testing.T) {
	r := newRPMDependencyUpgradesFixture(t, "amd64", "update")
	r.installed = r.metadata()
	if err := r.run(); err != nil {
		t.Fatal(err)
	}
	j, err := r.f.b.rpmJournal()
	if err != nil || j.Schema != 6 || j.Phase != "unchanged" || j.DependencyCount != 0 || j.DependencyUpgradeCount != 0 || j.Stage != "" || j.PlanSHA256 != "" || r.replays != 0 {
		t.Fatal(j, err)
	}
	if r.elevations != 0 {
		t.Fatal("no-op elevated", r.elevations)
	}
	for _, call := range r.calls {
		if !strings.HasPrefix(call, "rpm ") && call != "dnf5 --version" {
			t.Fatal("no-op performed mutation", call)
		}
	}
	original := r.dependencyBaselines[r.dependencies[0].Package.Name]
	if r.installedDependencies[original.Name] != rpmDependencyRecord(original) {
		t.Fatal("no-op repaired dependencies")
	}
}

func TestCodexRPMDependencyUpgradesConsent(t *testing.T) {
	base := []string{"--provider", codexDesktopRPM, "--trust-download", "--accept-system-packages", "--accept-dependencies", "--accept-dependency-upgrades"}
	for _, op := range []string{"install", "update"} {
		if _, err := parseOptions(op, base); err != nil {
			t.Fatal(err)
		}
		for i := 2; i < 5; i++ {
			args := append(append([]string{}, base[:i]...), base[i+1:]...)
			if _, err := parseOptions(op, args); err == nil {
				t.Fatal("missing consent accepted", args)
			}
		}
	}
	for _, op := range []string{"uninstall", "use", "integrate"} {
		if _, err := parseOptions(op, base); err == nil {
			t.Fatal(op)
		}
	}
	for _, provider := range []string{codexDesktopSystem, "codex-cli", "ollama", "picoclaw", hermesAgent} {
		args := append([]string{}, base...)
		args[1] = provider
		if _, err := parseOptions("install", args); err == nil {
			t.Fatal(provider)
		}
	}
	for _, flag := range []string{"--accept-dependency-upgrades", "--accept-dependency-upgrades=true"} {
		args := append(append([]string{}, base...), flag)
		if _, err := parseOptions("install", args); err == nil {
			t.Fatal(flag)
		}
	}
	r := newRPMDependencyUpgradesFixture(t, "amd64", "install")
	r.dependencyUpgrades = false
	if err := r.run(); err == nil || r.replays != 0 {
		t.Fatal("old consent authorized upgrades", err)
	}
}

func TestCodexRPMDependencyUpgradesRefusals(t *testing.T) {
	for _, operation := range []string{"install", "update"} {
		for _, kind := range []string{"missing", "ambiguous", "changed-before-replay", "changed-at-last-check", "post-missing", "post-old", "post-ambiguous"} {
			t.Run(operation+"/"+kind, func(t *testing.T) {
				r := newRPMDependencyUpgradesFixture(t, "amd64", operation)
				name := r.dependencies[0].Package.Name
				original := r.installedDependencies[name]
				if kind == "missing" {
					delete(r.installedDependencies, name)
				}
				if kind == "ambiguous" {
					r.installedDependencies[name] += original
				}
				r.fault = func(command string, args []string) error {
					if kind == "changed-at-last-check" && command == "dnf5" && len(args) == 1 && args[0] == "--version" && len(r.files) > 1 {
						r.installedDependencies[name] = "concurrent\n"
					}
					return nil
				}
				r.f.b.checkpoint = func(point string) error {
					if point == "codex-rpm-before-replay" && kind == "changed-before-replay" {
						r.installedDependencies[name] = "concurrent\n"
					}
					if point == "codex-rpm-after-replay" {
						switch kind {
						case "post-missing":
							delete(r.installedDependencies, name)
						case "post-old":
							r.installedDependencies[name] = original
						case "post-ambiguous":
							r.installedDependencies[name] += original
						}
					}
					return nil
				}
				if err := r.run(); err == nil {
					t.Fatal("unsafe upgrade succeeded")
				}
				want := 0
				if strings.HasPrefix(kind, "post-") {
					want = 1
				}
				if r.replays != want {
					t.Fatal(r.replays, want)
				}
				if _, err := os.Stat(r.f.b.root(codexDesktopRPM) + ".lock"); err != nil {
					t.Fatal("failure evidence lost", err)
				}
				if err := r.run(); err == nil || r.replays != want {
					t.Fatal("unsafe retry", err)
				}
			})
		}
	}
}

func TestCodexRPMDependencyUpgradesPolicyAndArtifacts(t *testing.T) {
	for _, operation := range []string{"install", "update"} {
		for _, kind := range []string{"unsigned", "raw-repo", "changed-file", "cancel"} {
			t.Run(operation+"/"+kind, func(t *testing.T) {
				r := newRPMDependencyUpgradesFixture(t, "amd64", operation)
				if kind == "unsigned" {
					r.policy = strings.Replace(r.policy, "localpkg_gpgcheck = 1", "localpkg_gpgcheck = false", 1)
				}
				if kind == "raw-repo" {
					r.dependencies[0].Repo = "fedora"
				}
				r.f.b.checkpoint = func(point string) error {
					if point == "codex-rpm-before-replay" {
						if kind == "cancel" {
							return context.Canceled
						}
						if kind == "changed-file" {
							for path := range r.files {
								if strings.HasSuffix(path, strings.TrimPrefix(r.dependencies[0].Path, "./")) {
									r.files[path] = []byte("changed")
								}
							}
						}
					}
					return nil
				}
				if err := r.run(); err == nil || r.replays != 0 {
					t.Fatal("unsafe replay permitted", kind, err)
				}
				if kind == "unsigned" {
					for _, call := range r.calls {
						if strings.Contains(call, "--store=") {
							t.Fatal("stored without signature checks")
						}
					}
				} else if _, err := os.Stat(r.f.b.root(codexDesktopRPM) + ".lock"); err != nil {
					t.Fatal("failure evidence lost", err)
				}
			})
		}
	}
}

func TestCodexRPMDependencyUpgradesJournalAndMenu(t *testing.T) {
	r := newRPMDependencyUpgradesFixture(t, "amd64", "install")
	choices, err := r.f.b.codexRPMMenu(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, choice := range choices {
		if strings.HasSuffix(choice.ID, "-dependency-upgrades") {
			count++
			o, err := parseOptions(choice.Operation, choice.Arguments)
			if err != nil || !o.acceptDependencies || !o.acceptDependencyUpgrades {
				t.Fatal(choice, err)
			}
		}
	}
	if count != 2 {
		t.Fatal(choices)
	}
	if err := r.run(); err != nil {
		t.Fatal(err)
	}
	base, err := r.f.b.rpmJournal()
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*codexRPMJournal){
		func(j *codexRPMJournal) { j.DependencyUpgradeCount = -1 },
		func(j *codexRPMJournal) { j.DependencyUpgradeCount = 3 },
		func(j *codexRPMJournal) { j.Schema = 4 },
		func(j *codexRPMJournal) { j.Schema = 5; j.Operation = "update" },
		func(j *codexRPMJournal) { j.Operation = "uninstall" },
		func(j *codexRPMJournal) { j.Phase = "prepared" },
		func(j *codexRPMJournal) { j.Stage = "" },
		func(j *codexRPMJournal) { j.PlanSHA256 = "" },
	} {
		j := *base
		mutate(&j)
		if err := r.f.b.saveRPMJournal(j); err != nil {
			t.Fatal(err)
		}
		if _, err := r.f.b.rpmJournal(); err == nil {
			t.Fatal("invalid schema6 accepted", j)
		}
	}
}
