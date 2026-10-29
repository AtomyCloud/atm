//go:build linux

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// Only this harmless command runs: the real package tools remain synthetic.
func codexComparisonFalse(t *testing.T) error {
	t.Helper()
	err := exec.Command("/bin/false").Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatal("missing false exit fixture", err)
	}
	return fmt.Errorf("synthetic dpkg: %w", err)
}

func newCodexUpdateFixture(t *testing.T, arch string, order int) *codexDesktopFixture {
	t.Helper()
	f := newCodexDesktopFixture(t, arch)
	f.pkg = codexPackageRecord{true, "install ok installed", "1:2026.1027.1-1", arch}
	if order == 0 {
		f.pkg.Version = f.version
	}
	if order < 0 {
		f.pkg.Version = "2:2026.1028.1-1"
	}
	s := f.b.codexDesktop.commands
	run := s.run
	falseResult := codexComparisonFalse(t)
	s.run = func(ctx context.Context, ex string, args []string) (string, error) {
		if ex != "/trusted/dpkg" {
			return run(ctx, ex, args)
		}
		f.calls = append(f.calls, systemCall{"dpkg", slices.Clone(args), false})
		if len(args) != 4 || args[0] != "--compare-versions" || args[1] != f.version || args[3] != f.pkg.Version {
			t.Fatal("unsafe comparator arguments", args)
		}
		if args[2] == "eq" && order == 0 || args[2] == "gt" && order > 0 || args[2] == "lt" && order < 0 {
			return "", nil
		}
		return "", falseResult
	}
	return f
}

func assertCodexNoPrivilege(t *testing.T, f *codexDesktopFixture) {
	t.Helper()
	for _, c := range f.calls {
		if c.elevated {
			t.Fatal("unexpected privilege", c)
		}
	}
	if f.hasCommand("apt-get") {
		t.Fatal("unexpected APT")
	}
}

func TestCodexDesktopUpdateNoOpReplacementLock(t *testing.T) {
	f := newCodexUpdateFixture(t, "amd64", 0)
	lock := f.b.root(codexDesktopSystem) + ".lock"
	var replacement os.FileInfo
	f.b.log = func(message string) {
		if message != label("codex_desktop_no_update") {
			return
		}
		if err := os.Mkdir(lock, 0700); err != nil {
			t.Fatal("next invocation could not acquire released lock", err)
		}
		var err error
		replacement, err = os.Lstat(lock)
		if err != nil {
			t.Fatal(err)
		}
	}
	mustRun(t, f.fixture, "update", codexDesktopArgs()...)
	assertCodexNoPrivilege(t, f)
	now, err := os.Lstat(lock)
	if err != nil || replacement == nil || !os.SameFile(replacement, now) {
		t.Fatal("no-op return removed the next invocation's lock", err)
	}
}

func TestCodexDesktopUpdateComparatorFatalAndAllFalse(t *testing.T) {
	fatal := exec.Command("/bin/sh", "-c", "exit 2").Run()
	var exit *exec.ExitError
	if !errors.As(fatal, &exit) || exit.ExitCode() != 2 {
		t.Fatal("missing fatal exit fixture", fatal)
	}
	fatal = fmt.Errorf("synthetic dpkg: %w", fatal)
	falseResult := codexComparisonFalse(t)
	for _, tc := range []struct {
		name    string
		failure error
		wantOps []string
	}{
		{"wrapped-exit2", fatal, []string{"eq"}},
		{"all-false", falseResult, []string{"eq", "gt", "lt"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newCodexUpdateFixture(t, "amd64", 1)
			before := f.pkg
			run := f.b.codexDesktop.commands.run
			var ops []string
			f.b.codexDesktop.commands.run = func(ctx context.Context, ex string, args []string) (string, error) {
				if ex != "/trusted/dpkg" {
					return run(ctx, ex, args)
				}
				if len(args) != 4 || args[0] != "--compare-versions" || args[1] != f.version || args[3] != before.Version {
					t.Fatal("unexpected comparator arguments", args)
				}
				ops = append(ops, args[2])
				return "", tc.failure
			}
			err := f.run(context.Background(), "update", codexDesktopArgs()...)
			if err == nil || !reflect.DeepEqual(ops, tc.wantOps) {
				t.Fatal("comparison failure accepted or retried", ops, err)
			}
			if tc.name == "wrapped-exit2" && !errors.Is(err, fatal) {
				t.Fatal("fatal comparator error lost", err)
			}
			if tc.name == "all-false" && !strings.Contains(err.Error(), "inconsistent dpkg version comparison") {
				t.Fatal("all-false result not rejected as inconsistent", err)
			}
			assertCodexNoPrivilege(t, f)
			if f.pkg != before {
				t.Fatal("comparator failure mutated package")
			}
			rec, err := f.b.codexDesktopRecord()
			if err != nil || rec == nil || rec.Phase != "failed" || rec.Baseline != before {
				t.Fatal("comparator failure lost evidence", rec, err)
			}
		})
	}
}

func TestCodexDesktopUpdateSuccess(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		for _, priorInstall := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/prior-install=%v", arch, priorInstall), func(t *testing.T) {
				f := newCodexUpdateFixture(t, arch, 1)
				if priorInstall {
					f.pkg = codexPackageRecord{}
					mustRun(t, f.fixture, "install", codexDesktopArgs()...)
					f.version = "1:2026.1029.1-1"
				}
				before := f.pkg
				mustRun(t, f.fixture, "update", codexDesktopArgs()...)
				rec, err := f.b.codexDesktopRecord()
				if err != nil || rec == nil || rec.Schema != 2 || rec.Operation != "update" || rec.Baseline != before || rec.Phase != "complete" || rec.LastPhase != "complete" {
					t.Fatal(rec, err)
				}
				if f.pkg != (codexPackageRecord{true, "install ok installed", f.version, arch}) {
					t.Fatal(f.pkg)
				}
				var apt []string
				for _, c := range f.calls {
					if c.name == "apt-get" {
						apt = c.args
					}
				}
				if !reflect.DeepEqual(apt, codexTransactionAPTArgs(codexFixtureStage, "update")) || !slices.Contains(apt, "--only-upgrade") {
					t.Fatal(apt)
				}
				for _, required := range []string{"APT::Get::Fix-Broken=false", "APT::Get::Fix-Missing=false", "APT::Get::ReInstall=false", "APT::Ignore-Hold=false", "APT::Get::allow-Downgrades=false", "APT::Get::allow-Change-Held-Packages=false", "--no-remove"} {
					if !slices.Contains(apt, required) {
						t.Fatal("missing update safety option", required)
					}
				}
				for _, path := range []string{rec.Input, f.b.root(codexDesktopSystem) + ".lock", filepath.Join(f.b.root(codexDesktopSystem), "current")} {
					if _, err := os.Lstat(path); !os.IsNotExist(err) {
						t.Fatal("unexpected retained/owned path", path, err)
					}
				}
				if len(f.rootFiles) != 0 {
					t.Fatal("staging not cleaned")
				}
			})
		}
	}
}

func TestCodexDesktopUpdateNoOp(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		for _, equivalent := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/equivalent=%v", arch, equivalent), func(t *testing.T) {
				f := newCodexUpdateFixture(t, arch, 0)
				if equivalent {
					f.version, f.pkg.Version = "1:1.0-0", "1:1.0"
				}
				before := f.pkg
				var logs []string
				f.b.log = func(s string) { logs = append(logs, s) }
				mustRun(t, f.fixture, "update", codexDesktopArgs()...)
				assertCodexNoPrivilege(t, f)
				if f.pkg != before || !strings.Contains(strings.Join(logs, "\n"), "no update performed") {
					t.Fatal(f.pkg, logs)
				}
				rec, err := f.b.codexDesktopRecord()
				if err != nil || rec == nil || rec.Phase != "no-update" || rec.LastPhase != "no-update" || rec.Operation != "update" || rec.Baseline != before || rec.GuardNonce != "" || rec.Staging != "" {
					t.Fatal(rec, err)
				}
				if _, err := os.Lstat(rec.Input); !os.IsNotExist(err) {
					t.Fatal("no-op input retained", err)
				}
				if _, err := os.Lstat(f.b.root(codexDesktopSystem) + ".lock"); !os.IsNotExist(err) {
					t.Fatal("no-op lock retained", err)
				}
				mustRun(t, f.fixture, "update", codexDesktopArgs()...)
				assertCodexNoPrivilege(t, f)
			})
		}
	}
}

func TestCodexDesktopUpdateRefusals(t *testing.T) {
	for _, change := range []func(*codexDesktopFixture){
		func(f *codexDesktopFixture) { f.pkg = codexPackageRecord{} },
		func(f *codexDesktopFixture) { f.pkg.Status = "hold ok installed" },
		func(f *codexDesktopFixture) { f.pkg.Status = "install ok unpacked" },
		func(f *codexDesktopFixture) { f.pkg.Status = "install reinstreq half-installed" },
		func(f *codexDesktopFixture) { f.pkg.Version = "not-a-version" },
		func(f *codexDesktopFixture) { f.pkg.Arch = "arm64" },
		func(f *codexDesktopFixture) { f.fault = "db" },
	} {
		f := newCodexUpdateFixture(t, "amd64", 1)
		change(f)
		before := f.pkg
		if err := f.run(context.Background(), "update", codexDesktopArgs()...); err == nil {
			t.Fatal("unsafe baseline accepted", before)
		}
		if f.pkg != before || len(f.calls) != 0 || f.fixture.calls != 0 {
			t.Fatal("refusal caused work")
		}
	}
	for _, args := range [][]string{
		{"--provider", codexDesktopSystem},
		{"--provider", codexDesktopSystem, "--trust-download"},
		{"--provider", codexDesktopSystem, "--accept-system-packages"},
		append(codexDesktopArgs(), "--version", "1.2.3"),
		append(codexDesktopArgs(), "--accept-system-packages=true"),
		append(codexDesktopArgs(), "--rocm"),
		append(codexDesktopArgs(), "--accept-dependency-execution"),
	} {
		f := newCodexUpdateFixture(t, "amd64", 1)
		if err := f.run(context.Background(), "update", args...); err == nil {
			t.Fatal("consent/option accepted", args)
		}
		if len(f.calls) != 0 || f.reads != 0 || f.fixture.calls != 0 {
			t.Fatal("invalid consent caused work")
		}
	}
	for name := range providers {
		if name == codexDesktopSystem {
			continue
		}
		if _, err := parseOptions("update", []string{"--provider", name, "--trust-download", "--accept-system-packages"}); err == nil {
			t.Fatal("consent leaked to provider", name)
		}
	}
}

func TestCodexDesktopUpdateDowngrade(t *testing.T) {
	f := newCodexUpdateFixture(t, "amd64", -1)
	before := f.pkg
	if err := f.run(context.Background(), "update", codexDesktopArgs()...); err == nil || !strings.Contains(err.Error(), "downgrade refused") {
		t.Fatal(err)
	}
	assertCodexNoPrivilege(t, f)
	if f.pkg != before {
		t.Fatal("downgrade mutated package")
	}
	rec, err := f.b.codexDesktopRecord()
	if err != nil || rec == nil || rec.Phase != "failed" || rec.Baseline != before {
		t.Fatal(rec, err)
	}
	if _, err := os.Stat(filepath.Join(rec.Input, "chatgpt.deb")); err != nil {
		t.Fatal("lost failed evidence", err)
	}
	if err := f.run(context.Background(), "update", codexDesktopArgs()...); err == nil {
		t.Fatal("incomplete attempt retried")
	}
}

func TestCodexDesktopUpdateMenu(t *testing.T) {
	f := newCodexUpdateFixture(t, "arm64", 1)
	choices, err := f.b.menu(context.Background(), codexDesktopSystem)
	if err != nil || len(choices) != 2 || choices[0].Operation != "update" || !strings.Contains(choices[0].Label, "system") {
		t.Fatal(choices, err)
	}
	if _, err := parseOptions(choices[0].Operation, choices[0].Arguments); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 0 || f.fixture.calls != 0 {
		t.Fatal("menu resolved latest")
	}
	f.pkg.Status = "hold ok installed"
	choices, err = f.b.menu(context.Background(), codexDesktopSystem)
	if err != nil || len(choices) != 1 {
		t.Fatal(choices, err)
	}
}

func TestCodexDesktopUpdateGuardAndPostconditionFailures(t *testing.T) {
	for _, fault := range []string{"guard-lock", "skip-early", "skip-final", "apt-noop", "missing-early-ack", "missing-final-ack", "postcondition", "rm", "rmdir", "final-journal"} {
		t.Run(fault, func(t *testing.T) {
			f := newCodexUpdateFixture(t, "amd64", 1)
			f.fault = fault
			if err := f.run(context.Background(), "update", codexDesktopArgs()...); err == nil {
				t.Fatal("fault accepted")
			}
			if _, err := os.Lstat(f.b.root(codexDesktopSystem) + ".lock"); err != nil {
				t.Fatal("uncertainty lost lock", err)
			}
			if !slices.Contains([]string{"rm", "rmdir", "final-journal"}, fault) && f.hasCommand("rm") {
				t.Fatal("uncertain transaction cleaned")
			}
		})
	}
	for _, post := range []codexPackageRecord{{}, {true, "hold ok installed", "1:2026.1028.1-1", "amd64"}, {true, "install ok installed", "1:2026.1027.1-1", "amd64"}, {true, "install ok installed", "1:2026.1028.1-1", "arm64"}} {
		f := newCodexUpdateFixture(t, "amd64", 1)
		run := f.b.codexDesktop.commands.run
		f.b.codexDesktop.commands.run = func(ctx context.Context, ex string, args []string) (string, error) {
			out, err := run(ctx, ex, args)
			if slices.Contains(args, "/trusted/apt-get") {
				f.pkg = post
			}
			return out, err
		}
		if err := f.run(context.Background(), "update", codexDesktopArgs()...); err == nil {
			t.Fatal("postcondition accepted", post)
		}
		if f.hasCommand("rm") {
			t.Fatal("postcondition failure cleaned")
		}
	}
}

func TestCodexDesktopUpdateNoOpRecheckAndCleanup(t *testing.T) {
	for _, fault := range []string{"baseline", "cleanup", "cancel", "final-baseline"} {
		t.Run(fault, func(t *testing.T) {
			f := newCodexUpdateFixture(t, "amd64", 0)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			read := f.b.codexDesktop.packageRecord
			f.b.codexDesktop.packageRecord = func() (codexPackageRecord, error) {
				pkg, err := read()
				if f.reads == 2 {
					switch fault {
					case "baseline":
						pkg.Version += ".1"
					case "cancel":
						cancel()
					case "cleanup":
						rec, err := f.b.codexDesktopRecord()
						if err != nil {
							t.Fatal(err)
						}
						writeFixture(t, filepath.Join(rec.Input, "foreign"), []byte("preserve"), 0600)
					}
				}
				if f.reads == 3 && fault == "final-baseline" {
					pkg.Status = "hold ok installed"
				}
				return pkg, err
			}
			if err := f.run(ctx, "update", codexDesktopArgs()...); err == nil {
				t.Fatal("no-op fault accepted")
			}
			assertCodexNoPrivilege(t, f)
			rec, err := f.b.codexDesktopRecord()
			if err != nil || rec == nil || rec.Phase != "failed" || rec.LastPhase != "metadata-verified" {
				t.Fatal(rec, err)
			}
			if fault == "cleanup" {
				data, err := os.ReadFile(filepath.Join(rec.Input, "foreign"))
				if err != nil || string(data) != "preserve" {
					t.Fatal("foreign input removed", err)
				}
			}
			if err := f.run(context.Background(), "update", codexDesktopArgs()...); err == nil {
				t.Fatal("failed no-op replayed")
			}
		})
	}
}

func TestCodexDesktopUpdateCancellation(t *testing.T) {
	for _, phase := range []string{"dpkg-deb", "mktemp", "apt-get", "rmdir"} {
		t.Run(phase, func(t *testing.T) {
			f := newCodexUpdateFixture(t, "amd64", 1)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f.cancel, f.fault = cancel, "cancel-"+phase
			if err := f.run(ctx, "update", codexDesktopArgs()...); !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			rec, err := f.b.codexDesktopRecord()
			want := "unknown"
			if phase == "dpkg-deb" {
				want = "failed"
				assertCodexNoPrivilege(t, f)
			}
			if err != nil || rec == nil || rec.Phase != want {
				t.Fatal(rec, err)
			}
		})
	}
}

func TestCodexDesktopUpdateLegacyJournal(t *testing.T) {
	f := newCodexUpdateFixture(t, "amd64", 1)
	f.pkg = codexPackageRecord{}
	mustRun(t, f.fixture, "install", codexDesktopArgs()...)
	rec, err := f.b.codexDesktopRecord()
	if err != nil {
		t.Fatal(err)
	}
	rec.Schema, rec.Operation, rec.Baseline = 1, "", codexPackageRecord{}
	data, _ := json.Marshal(rec)
	var legacy map[string]json.RawMessage
	if err := json.Unmarshal(data, &legacy); err != nil {
		t.Fatal(err)
	}
	delete(legacy, "Operation")
	delete(legacy, "Baseline")
	data, _ = json.Marshal(legacy)
	path := filepath.Join(f.b.root(codexDesktopSystem), "journal.json")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, path, data, 0600)
	if rec, err := f.b.codexDesktopRecord(); err != nil || rec == nil || rec.Schema != 1 {
		t.Fatal(rec, err)
	}
	f.version = "1:2026.1029.1-1"
	before := f.pkg
	mustRun(t, f.fixture, "update", codexDesktopArgs()...)
	rec, err = f.b.codexDesktopRecord()
	if err != nil || rec.Schema != 2 || rec.Operation != "update" || rec.Baseline != before {
		t.Fatal(rec, err)
	}
}
