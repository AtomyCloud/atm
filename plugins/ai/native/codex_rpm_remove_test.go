//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func newRPMRemovalFixture(t *testing.T, arch string) *rpmFixture {
	t.Helper()
	r := newRPMUpdateFixture(t, arch)
	r.updating, r.removing = false, true
	return r
}

func TestCodexRPMRemovalLifecycle(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		t.Run(arch, func(t *testing.T) {
			r := newRPMRemovalFixture(t, arch)
			baseline, err := parseCodexRPMMetadata(r.installed, arch)
			if err != nil {
				t.Fatal(err)
			}
			personal := filepath.Join(filepath.Dir(r.f.b.req.AppsDir), "private-chatgpt-settings")
			if err = os.WriteFile(personal, []byte("private fixture"), 0600); err != nil {
				t.Fatal(err)
			}
			resolve := r.f.b.codexRPM.resolve
			r.f.b.codexRPM.resolve = func(name string) (string, error) {
				if name == "install" || name == "sha256sum" || name == "ls" {
					t.Fatalf("removal resolved download/payload tool: %s", name)
				}
				return resolve(name)
			}
			if err = r.run(); err != nil {
				t.Fatal(err)
			}
			j, err := r.f.b.rpmJournal()
			if err != nil || j == nil || j.Schema != 3 || j.Operation != "uninstall" || j.Baseline != baseline || j.Phase != "complete" || j.URL != "" || j.Input != "" || j.Size != 0 || j.SHA256 != "" || j.Package != (codexRPMMetadata{}) || j.Stage == "" || !validSHA(j.PlanSHA256) {
				t.Fatalf("invalid removal evidence %+v: %v", j, err)
			}
			if r.f.calls != 0 || r.replays != 1 || r.present {
				t.Fatal("removal downloaded, did not replay, or left package present")
			}
			data, err := os.ReadFile(personal)
			if err != nil || string(data) != "private fixture" {
				t.Fatal("personal fixture changed", err)
			}
			integrationMissing(t, r.f.b.root(codexDesktopRPM)+".lock")
			calls := len(r.calls)
			r.f.b.codexRPMStatus()
			choices, err := r.f.b.codexRPMMenu(context.Background())
			if err != nil || len(choices) != 5 || len(r.calls) != calls {
				t.Fatal("removal status/menu executed package commands", err)
			}
			if err = r.run(); err == nil || r.replays != 1 {
				t.Fatal("absent package accepted for another removal", err)
			}
		})
	}
}

func TestCodexRPMRemovalConsentAndUnknownState(t *testing.T) {
	for _, extra := range [][]string{{"--trust-download"}, {"--version", "latest"}, {"--version", "1.0.0"}, {"--rocm"}, {"--accept-dependency-execution"}, {"--remove-path"}, {"--keep-preparation"}, {"--accept-system-packages"}} {
		r := newRPMRemovalFixture(t, "amd64")
		args := append([]string{"--provider", codexDesktopRPM, "--accept-system-packages"}, extra...)
		if err := r.f.run(context.Background(), "uninstall", args...); err == nil || len(r.calls) != 0 || r.f.calls != 0 {
			t.Fatal("invalid removal flags caused work", extra, err)
		}
	}
	r := newRPMRemovalFixture(t, "amd64")
	if err := r.f.run(context.Background(), "uninstall", "--provider", codexDesktopRPM); err == nil || len(r.calls) != 0 {
		t.Fatal("removal without consent caused work", err)
	}
	for _, state := range []string{"absent", "ambiguous", "architecture", "diagnostic"} {
		t.Run(state, func(t *testing.T) {
			r := newRPMRemovalFixture(t, "amd64")
			switch state {
			case "absent":
				r.present = false
			case "ambiguous":
				r.installed += r.installed
			case "architecture":
				r.installed = strings.Replace(r.installed, "x86_64", "aarch64", 1)
			case "diagnostic":
				r.fault = func(name string, args []string) error {
					if name == "rpm" && args[0] == "-qa" {
						return errors.New("database diagnostic")
					}
					return nil
				}
			}
			if err := r.run(); err == nil || r.stages != 0 || r.replays != 0 || r.f.calls != 0 {
				t.Fatal("unknown/absent state authorized removal", err)
			}
		})
	}
}

func TestCodexRPMRemovalPinsBaseline(t *testing.T) {
	for _, point := range []string{"codex-rpm-before-stage", "codex-rpm-before-store", "codex-rpm-before-replay", "last-policy-check"} {
		for _, change := range []string{"absent", "version", "epoch", "release"} {
			t.Run(point+"/"+change, func(t *testing.T) {
				r := newRPMRemovalFixture(t, "amd64")
				mutate := func() {
					switch change {
					case "absent":
						r.present = false
					case "version":
						r.installed = r.metadata()
					case "epoch":
						r.installed = strings.Replace(r.installed, "\t0\t", "\t2\t", 1)
					case "release":
						r.installed = strings.Replace(r.installed, "\t1\tx86_64", "\t2\tx86_64", 1)
					}
				}
				r.f.b.checkpoint = func(at string) error {
					if at == point {
						mutate()
					}
					return nil
				}
				versions := 0
				r.fault = func(name string, args []string) error {
					if name == "dnf5" && reflect.DeepEqual(args, []string{"--version"}) {
						versions++
						if point == "last-policy-check" && versions == 2 {
							mutate()
						}
					}
					return nil
				}
				if err := r.run(); err == nil || r.replays != 0 || r.f.calls != 0 {
					t.Fatal("changed baseline reached replay", err)
				}
			})
		}
	}
}

func TestCodexRPMRemovalUncertainty(t *testing.T) {
	for _, fault := range []string{"plan", "policy", "replay", "post-present", "post-unknown", "cancel", "lock", "journal"} {
		t.Run(fault, func(t *testing.T) {
			r := newRPMRemovalFixture(t, "amd64")
			r.fault = func(name string, args []string) error {
				if fault == "replay" && name == "dnf5" && strings.Contains(" "+strings.Join(args, " ")+" ", " replay ") {
					return errors.New("uncertain replay")
				}
				if fault == "post-unknown" && name == "rpm" && args[0] == "-qa" && r.replays != 0 {
					return errors.New("unknown post-state")
				}
				return nil
			}
			r.f.b.checkpoint = func(point string) error {
				if point == "codex-rpm-before-replay" {
					switch fault {
					case "plan":
						r.files["/run/atm-ai-codex-rpm-AbCd012345/transaction/transaction.json"] = []byte("{}")
					case "policy":
						r.policy = strings.Replace(r.policy, "clean_requirements_on_remove = false", "clean_requirements_on_remove = true", 1)
					case "cancel":
						return context.Canceled
					case "lock":
						return os.Rename(r.f.b.root(codexDesktopRPM)+".lock", r.f.b.root(codexDesktopRPM)+".lock-moved")
					case "journal":
						return os.WriteFile(filepath.Join(r.f.b.root(codexDesktopRPM), ".journal-next"), []byte("foreign fixture"), 0600)
					}
				}
				if fault == "post-present" && point == "codex-rpm-after-replay" {
					r.present, r.installed = true, r.metadata()
				}
				return nil
			}
			if err := r.run(); err == nil || r.f.calls != 0 {
				t.Fatal("uncertain removal accepted or downloaded", err)
			}
			if fault != "post-present" && fault != "post-unknown" && r.replays != 0 {
				t.Fatal("uncertain plan reached replay")
			}
			before := r.replays
			if err := r.run(); err == nil || r.replays != before {
				t.Fatal("uncertain state allowed another removal", err)
			}
		})
	}
}

func TestCodexRPMRemovalPolicyAndJournal(t *testing.T) {
	for _, value := range []string{"true", "1", "unknown", ""} {
		r := newRPMRemovalFixture(t, "amd64")
		r.policy = strings.Replace(r.policy, "clean_requirements_on_remove = false", "clean_requirements_on_remove = "+value, 1)
		if err := r.run(); err == nil || r.replays != 0 {
			t.Fatal("dependency cleanup policy accepted", value, err)
		}
	}
	for _, previous := range []string{"schema1-install", "schema2-install", "schema2-update"} {
		t.Run(previous, func(t *testing.T) {
			r := newRPMFixture(t, "amd64")
			if previous == "schema2-update" {
				r = newRPMUpdateFixture(t, "amd64")
			}
			if err := r.run(); err != nil {
				t.Fatal(err)
			}
			old, err := r.f.b.rpmJournal()
			if err != nil {
				t.Fatal(err)
			}
			if previous == "schema1-install" {
				old.Schema, old.Operation, old.Baseline = 1, "", codexRPMMetadata{}
				if err = r.f.b.saveRPMJournal(*old); err != nil {
					t.Fatal(err)
				}
			}
			r.updating, r.removing = false, true
			reads := r.f.calls
			if err = r.run(); err != nil || r.f.calls != reads {
				t.Fatal("removal from legacy history failed/downloaded", err)
			}
			if _, err = os.Stat(filepath.Join(old.Input, "chatgpt.rpm")); err != nil {
				t.Fatal("prior input was deleted", err)
			}
			j, err := r.f.b.rpmJournal()
			if err != nil {
				t.Fatal(err)
			}
			for _, field := range []string{"url", "size", "input", "package", "phase", "baseline", "schema", "prepared-evidence", "staged-missing", "plan-missing", "replay-missing"} {
				bad := *j
				switch field {
				case "url":
					bad.URL, _ = codexRPMURL("amd64")
				case "size":
					bad.Size = 1
				case "input":
					bad.Input = old.Input
				case "package":
					bad.Package = old.Package
				case "phase":
					bad.Phase = "downloaded"
				case "baseline":
					bad.Baseline = codexRPMMetadata{}
				case "schema":
					bad.Schema = 2
				case "prepared-evidence":
					bad.Phase = "prepared"
				case "staged-missing":
					bad.Phase, bad.Stage, bad.PlanSHA256 = "root-staged", "", ""
				case "plan-missing":
					bad.Phase, bad.PlanSHA256 = "plan-verified", ""
				case "replay-missing":
					bad.Phase, bad.Stage = "replaying", ""
				}
				if err = r.f.b.saveRPMJournal(bad); err != nil {
					t.Fatal(err)
				}
				if _, err = r.f.b.rpmJournal(); err == nil {
					t.Fatal(fmt.Sprintf("invalid removal journal accepted: %s", field))
				}
			}
		})
	}
}
