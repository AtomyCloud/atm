//go:build linux

package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newRPMDependenciesFixture(t *testing.T, arch string) *rpmFixture {
	t.Helper()
	r := newRPMFixture(t, arch)
	r.dependencyMode = true
	r.installedDependencies = map[string]string{}
	a, _ := codexRPMArch(arch)
	r.dependencies = []codexRPMDependency{
		{Package: codexRPMMetadata{"lib-example", "0", "1.2.3", "2.fc44", a}, Path: "./packages/lib-example-1.2.3-2.fc44." + a + ".rpm", Repo: "@stored_transaction(fedora)"},
		{Package: codexRPMMetadata{"example-data", "1", "2.0", "1", "noarch"}, Path: "./packages/example-data-2.0-1.noarch.rpm", Repo: "@stored_transaction(updates)"},
	}
	return r
}

func TestCodexRPMDependenciesLifecycle(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		t.Run(arch, func(t *testing.T) {
			r := newRPMDependenciesFixture(t, arch)
			choices, err := r.f.b.codexRPMMenu(context.Background())
			if err != nil || len(choices) != 8 || len(r.calls) != 0 {
				t.Fatal(choices, err, r.calls)
			}
			if err = r.run(); err != nil {
				t.Fatal(err)
			}
			j, err := r.f.b.rpmJournal()
			if err != nil || j.Schema != 4 || j.DependencyCount != 2 || j.Phase != "complete" || r.replays != 1 {
				t.Fatal(j, err, r.replays)
			}
			integrationMissing(t, r.f.b.root(codexDesktopRPM)+".lock")
			for _, dep := range r.dependencies {
				if err := parseRPMDependencyMetadata(r.installedDependencies[dep.Package.Name], dep.Package); err != nil {
					t.Fatal(err)
				}
				if _, ok := r.files[filepath.Join(j.Stage, "transaction", dep.Path)]; !ok {
					t.Fatal("dependency evidence not retained")
				}
			}
			// Separately consented uninstall still removes only chatgpt.
			r.dependencyMode = false
			r.removing = true
			if err = r.run(); err != nil {
				t.Fatal(err)
			}
			if len(r.installedDependencies) != 2 {
				t.Fatal("removal deleted dependencies")
			}
		})
	}
	t.Run("no new dependencies", func(t *testing.T) {
		r := newRPMDependenciesFixture(t, "amd64")
		r.dependencies = nil
		if err := r.run(); err != nil {
			t.Fatal(err)
		}
		j, err := r.f.b.rpmJournal()
		if err != nil || j.Schema != 4 || j.DependencyCount != 0 {
			t.Fatal(j, err)
		}
	})
}

func TestCodexRPMDependenciesConsent(t *testing.T) {
	base := []string{"--provider", codexDesktopRPM, "--trust-download", "--accept-system-packages", "--accept-dependencies"}
	if _, err := parseOptions("install", base); err != nil {
		t.Fatal(err)
	}
	if _, err := parseOptions("update", base); err != nil {
		t.Fatal(err)
	}
	for _, op := range []string{"uninstall", "use", "remove", "integrate"} {
		if _, err := parseOptions(op, base); err == nil {
			t.Fatal("accepted dependencies for", op)
		}
	}
	for _, provider := range []string{"codex-cli", codexDesktopSystem, hermesAgent, hermesSystem, "ollama", "picoclaw"} {
		args := append([]string(nil), base...)
		args[1] = provider
		if _, err := parseOptions("install", args); err == nil {
			t.Fatal("accepted wrong provider", provider)
		}
	}
	for _, args := range [][]string{
		{"--provider", codexDesktopRPM, "--accept-dependencies"},
		{"--provider", codexDesktopRPM, "--accept-dependencies", "--trust-download"},
		{"--provider", codexDesktopRPM, "--accept-dependencies", "--accept-system-packages"},
		append(append([]string(nil), base...), "--accept-dependencies"),
		{"--provider", codexDesktopRPM, "--trust-download", "--accept-system-packages", "--accept-dependencies=true"},
	} {
		r := newRPMDependenciesFixture(t, "amd64")
		if err := r.f.run(context.Background(), "install", args...); err == nil {
			t.Fatal("missing or invalid consent accepted")
		}
		if len(r.calls) != 0 || r.f.calls != 0 {
			t.Fatal("consent failure executed commands")
		}
	}
	t.Run("default refuses dependencies", func(t *testing.T) {
		r := newRPMDependenciesFixture(t, "amd64")
		r.dependencyMode = false
		if err := r.run(); err == nil || r.replays != 0 {
			t.Fatal("default mode accepted dependency plan", err)
		}
	})
}

func TestCodexRPMDependenciesRefusals(t *testing.T) {
	for _, operation := range []string{"install", "update"} {
		t.Run(operation, func(t *testing.T) { testCodexRPMDependenciesRefusals(t, operation) })
	}
}

func testCodexRPMDependenciesRefusals(t *testing.T, operation string) {
	for _, kind := range []string{"already-installed", "query-error", "changed-before-replay", "changed-at-last-check", "post-missing", "post-mismatch", "header-mismatch", "file-missing", "file-link", "hash-changed", "extra-file", "plan-upgrade", "plan-remove", "replay-error", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			r := newRPMDependenciesFixture(t, "amd64")
			if operation == "update" {
				r.updating, r.present = true, true
				r.installed = strings.Replace(r.metadata(), "1.2026.100", "1.2026.99", 1)
			}
			dep := r.dependencies[0]
			if kind == "already-installed" {
				r.installedDependencies[dep.Package.Name] = "existing\n"
			}
			r.fault = func(name string, args []string) error {
				if kind == "query-error" && name == "rpm" && args[0] == "-qa" && args[len(args)-1] == dep.Package.Name {
					return errors.New("query diagnostics")
				}
				if kind == "changed-at-last-check" && name == "dnf5" && len(args) == 1 && args[0] == "--version" && len(r.files) > 1 {
					r.installedDependencies[dep.Package.Name] = "concurrent\n"
				}
				if kind == "replay-error" && name == "dnf5" && strings.Contains(strings.Join(args, " "), " replay ") {
					return errors.New("replay failed")
				}
				return nil
			}
			r.f.b.checkpoint = func(point string) error {
				stage := "/run/atm-ai-codex-rpm-AbCd012345/transaction"
				path := filepath.Join(stage, dep.Path)
				if point == "codex-rpm-before-populate" {
					switch kind {
					case "header-mismatch":
						r.dependencies[0].Package.Version = "99"
					case "file-missing":
						delete(r.files, path)
					case "file-link":
						r.stats[path] = "a1ff\t0\t1\t1\t123\n"
					case "extra-file":
						r.files[stage+"/packages/extra.rpm"] = []byte("extra")
					}
				}
				if point == "codex-rpm-before-replay" {
					switch kind {
					case "changed-before-replay":
						r.installedDependencies[dep.Package.Name] = "new\n"
					case "hash-changed":
						r.files[path] = []byte("modified")
					case "cancel":
						return context.Canceled
					case "plan-upgrade", "plan-remove":
						var plan map[string]any
						if err := json.Unmarshal(r.files[stage+"/transaction.json"], &plan); err != nil {
							t.Fatal(err)
						}
						plan["rpms"].([]any)[1].(map[string]any)["action"] = strings.TrimPrefix(kind, "plan-")
						data, _ := json.Marshal(plan)
						r.files[stage+"/transaction.json"] = data
					}
				}
				if point == "codex-rpm-after-replay" {
					if kind == "post-missing" {
						delete(r.installedDependencies, dep.Package.Name)
					}
					if kind == "post-mismatch" {
						r.installedDependencies[dep.Package.Name] = "wrong\n"
					}
				}
				return nil
			}
			if err := r.run(); err == nil {
				t.Fatal("unsafe transaction succeeded")
			}
			want := 0
			if strings.HasPrefix(kind, "post-") {
				want = 1
			}
			if r.replays != want {
				t.Fatal("unexpected replay", r.replays)
			}
			if _, err := os.Stat(r.f.b.root(codexDesktopRPM) + ".lock"); err != nil {
				t.Fatal("uncertain evidence not retained", err)
			}
			calls := len(r.calls)
			if err := r.run(); err == nil || r.replays != want {
				t.Fatal("retry allowed", err, len(r.calls)-calls)
			}
		})
	}
}

func TestCodexRPMDependenciesJournal(t *testing.T) {
	r := newRPMDependenciesFixture(t, "amd64")
	if err := r.run(); err != nil {
		t.Fatal(err)
	}
	base, err := r.f.b.rpmJournal()
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*codexRPMJournal){
		func(j *codexRPMJournal) { j.Schema = 2 },
		func(j *codexRPMJournal) { j.Operation = "update" },
		func(j *codexRPMJournal) { j.DependencyCount = -1 },
		func(j *codexRPMJournal) { j.DependencyCount = 65 },
		func(j *codexRPMJournal) { j.Phase = "root-staged" },
		func(j *codexRPMJournal) { j.Phase = "prepared" },
		func(j *codexRPMJournal) { j.PlanSHA256 = "" },
		func(j *codexRPMJournal) { j.Stage = "" },
	} {
		j := *base
		mutate(&j)
		if err := r.f.b.saveRPMJournal(j); err != nil {
			t.Fatal(err)
		}
		if _, err := r.f.b.rpmJournal(); err == nil {
			t.Fatal("invalid dependency journal accepted", j)
		}
	}
}

func TestCodexRPMDependenciesRequireLocalSignatures(t *testing.T) {
	for _, operation := range []string{"install", "update"} {
		t.Run(operation, func(t *testing.T) { testCodexRPMDependenciesRequireLocalSignatures(t, operation) })
	}
}

func testCodexRPMDependenciesRequireLocalSignatures(t *testing.T, operation string) {
	for _, value := range []string{"false", "0", "", "unknown"} {
		t.Run(value, func(t *testing.T) {
			r := newRPMDependenciesFixture(t, "amd64")
			if operation == "update" {
				r.updating, r.present = true, true
				r.installed = strings.Replace(r.metadata(), "1.2026.100", "1.2026.99", 1)
			}
			r.policy = strings.Replace(r.policy, "localpkg_gpgcheck = 1", "localpkg_gpgcheck = "+value, 1)
			if err := r.run(); err == nil || r.replays != 0 {
				t.Fatal("unsigned replay permitted", err)
			}
			for _, call := range r.calls {
				if strings.Contains(call, "--store=") {
					t.Fatal("stored before signature policy verified")
				}
			}
		})
	}
	for _, value := range []string{"true", "True", "1"} {
		policy := strings.Replace(rpmPolicyFixture, "localpkg_gpgcheck = 1", "localpkg_gpgcheck = "+value, 1)
		if err := codexRPMPolicyWithDependencies(policy, false, true); err != nil {
			t.Fatal(value, err)
		}
	}
	// Existing no-dependency mode still respects the administrator's local policy.
	if err := codexRPMPolicy(strings.Replace(rpmPolicyFixture, "localpkg_gpgcheck = 1", "localpkg_gpgcheck = false", 1), false); err != nil {
		t.Fatal(err)
	}
}

func TestCodexRPMDependenciesRejectLegacyRepositoryIDs(t *testing.T) {
	for _, operation := range []string{"install", "update"} {
		t.Run(operation, func(t *testing.T) { testCodexRPMDependenciesRejectLegacyRepositoryIDs(t, operation) })
	}
}

func testCodexRPMDependenciesRejectLegacyRepositoryIDs(t *testing.T, operation string) {
	for _, repo := range []string{"fedora", "updates"} {
		t.Run(repo, func(t *testing.T) {
			r := newRPMDependenciesFixture(t, "amd64")
			if operation == "update" {
				r.updating, r.present = true, true
				r.installed = strings.Replace(r.metadata(), "1.2026.100", "1.2026.99", 1)
			}
			r.dependencies[0].Repo = repo
			if err := r.run(); err == nil || r.replays != 0 {
				t.Fatal("legacy repository plan accepted for dependency replay", err)
			}
			if _, err := os.Stat(r.f.b.root(codexDesktopRPM) + ".lock"); err != nil {
				t.Fatal("uncertain plan evidence not retained", err)
			}
		})
	}
}

func TestCodexRPMUpdateDependenciesLifecycle(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		t.Run(arch, func(t *testing.T) {
			r := newRPMDependenciesFixture(t, arch)
			r.updating, r.present = true, true
			r.installed = strings.Replace(r.metadata(), "1.2026.100", "1.2026.99", 1)
			baseline, _ := parseCodexRPMMetadata(r.installed, arch)
			if err := r.run(); err != nil {
				t.Fatal(err)
			}
			j, err := r.f.b.rpmJournal()
			if err != nil || j.Schema != 5 || j.Operation != "update" || j.Baseline != baseline || j.Phase != "complete" || j.DependencyCount != 2 || r.replays != 1 {
				t.Fatal(j, err, r.replays)
			}
			integrationMissing(t, r.f.b.root(codexDesktopRPM)+".lock")
			for _, dep := range r.dependencies {
				if err := parseRPMDependencyMetadata(r.installedDependencies[dep.Package.Name], dep.Package); err != nil {
					t.Fatal(err)
				}
			}
			// A repeat only records a no-op, never repairs missing dependencies.
			r.installedDependencies = map[string]string{}
			r.calls = nil
			if err := r.run(); err != nil {
				t.Fatal(err)
			}
			j, err = r.f.b.rpmJournal()
			if err != nil || j.Schema != 5 || j.Phase != "unchanged" || j.DependencyCount != 0 || j.Stage != "" || j.PlanSHA256 != "" || r.replays != 1 || len(r.installedDependencies) != 0 {
				t.Fatal(j, err)
			}
			for _, call := range r.calls {
				if !strings.HasPrefix(call, "rpm ") && call != "dnf5 --version" {
					t.Fatal("no-op performed privileged work", call)
				}
				if strings.HasSuffix(call, "lib-example") || strings.HasSuffix(call, "example-data") {
					t.Fatal("no-op queried dependencies", call)
				}
			}
			j.DependencyCount = 1
			if err := r.f.b.saveRPMJournal(*j); err != nil {
				t.Fatal(err)
			}
			if _, err := r.f.b.rpmJournal(); err == nil {
				t.Fatal("no-op with dependency evidence accepted")
			}
		})
	}
}

func TestCodexRPMUpdateDependenciesBaseline(t *testing.T) {
	for _, point := range []string{"initial-absent", "initial-ambiguous", "codex-rpm-before-stage", "codex-rpm-before-store", "codex-rpm-before-replay", "codex-rpm-before-unchanged", "codex-rpm-after-replay"} {
		t.Run(point, func(t *testing.T) {
			r := newRPMDependenciesFixture(t, "amd64")
			r.updating, r.present = true, true
			r.installed = strings.Replace(r.metadata(), "1.2026.100", "1.2026.99", 1)
			if point == "initial-absent" {
				r.present = false
			}
			if point == "initial-ambiguous" {
				r.installed += r.installed
			}
			if point == "codex-rpm-before-unchanged" {
				r.installed = r.metadata()
			}
			r.f.b.checkpoint = func(got string) error {
				if got == point {
					r.present = false
				}
				return nil
			}
			if err := r.run(); err == nil {
				t.Fatal("changed/unknown target accepted")
			}
			want := 0
			if point == "codex-rpm-after-replay" {
				want = 1
			}
			if r.replays != want {
				t.Fatal("unexpected replay", r.replays)
			}
		})
	}
}

func TestCodexRPMUpdateDependenciesHistory(t *testing.T) {
	for _, schema := range []int{1, 2, 3, 4, 5} {
		t.Run(string(rune('0'+schema)), func(t *testing.T) {
			r := newRPMDependenciesFixture(t, "amd64")
			r.dependencies = nil
			r.dependencyMode = schema == 4 || schema == 5
			if schema == 5 {
				r.updating, r.present = true, true
				r.installed = strings.Replace(r.metadata(), "1.2026.100", "1.2026.99", 1)
			}
			if err := r.run(); err != nil {
				t.Fatal(err)
			}
			j, err := r.f.b.rpmJournal()
			if err != nil {
				t.Fatal(err)
			}
			if schema == 1 {
				j.Schema, j.Operation = 1, ""
				if err := r.f.b.saveRPMJournal(*j); err != nil {
					t.Fatal(err)
				}
			}
			if schema == 3 {
				r.removing = true
				if err := r.run(); err != nil {
					t.Fatal(err)
				}
				r.removing = false
			}
			r.dependencyMode, r.updating, r.present = true, true, true
			r.installed = strings.Replace(r.metadata(), "1.2026.100", "1.2026.99", 1)
			if err := r.run(); err != nil {
				t.Fatal("history refused", err)
			}
			j, err = r.f.b.rpmJournal()
			if err != nil || j.Schema != 5 || j.Phase != "complete" || j.DependencyCount != 0 {
				t.Fatal(j, err)
			}
			for _, mutate := range []func(*codexRPMJournal){
				func(j *codexRPMJournal) { j.Operation = "install" },
				func(j *codexRPMJournal) { j.Baseline = codexRPMMetadata{} },
				func(j *codexRPMJournal) { j.Schema = 4 },
				func(j *codexRPMJournal) { j.PlanSHA256 = "" },
			} {
				bad := *j
				mutate(&bad)
				if err := r.f.b.saveRPMJournal(bad); err != nil {
					t.Fatal(err)
				}
				if _, err := r.f.b.rpmJournal(); err == nil {
					t.Fatal("malformed update history accepted")
				}
			}
		})
	}
}
