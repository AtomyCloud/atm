//go:build linux

package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func codexRemoveArgs() []string {
	return []string{"--provider", codexDesktopSystem, "--accept-system-packages"}
}

func codexRemoveTestInput(baseline codexPackageRecord) string {
	var input strings.Builder
	input.WriteString("VERSION 3\nDir=/\nDir::State=var/lib/apt\nDir::State::status=/var/lib/dpkg/status\n")
	args := codexRemoveAPTArgs(codexFixtureStage)
	for i := 0; i < len(args); i++ {
		if args[i] == "-o" {
			i++
			line := args[i]
			// CommandLine::HandleOpt stores --no-download as integer 0.
			if line == "APT::Get::Download=false" {
				line = "APT::Get::Download=0"
			}
			input.WriteString(line + "\n")
		}
	}
	fmt.Fprintf(&input, "\nchatgpt %s %s none > - - none **REMOVE**\n", baseline.Version, baseline.Arch)
	return input.String()
}

type codexRemoveFixture struct {
	*codexDesktopFixture
	plan       string
	after      codexPackageRecord
	guardReads int
}

func newCodexRemoveFixture(t *testing.T, arch string) *codexRemoveFixture {
	t.Helper()
	f := &codexRemoveFixture{codexDesktopFixture: newCodexDesktopFixture(t, arch)}
	f.pkg = codexPackageRecord{true, "install ok installed", "2:2026.1028.1-9", arch}
	f.after = codexPackageRecord{true, "deinstall ok config-files", f.pkg.Version, arch}
	f.plan = codexRemoveTestInput(f.pkg)
	s := f.b.codexDesktop.commands
	run := s.run
	s.run = func(ctx context.Context, ex string, args []string) (string, error) {
		if ex != "/trusted/sudo" || f.b.req.Operation != "uninstall" {
			return run(ctx, ex, args)
		}
		prefix := append([]string{"-n", "--", "/trusted/env", "-i"}, codexSystemEnvironment()...)
		if len(args) <= len(prefix) || !reflect.DeepEqual(args[:len(prefix)], prefix) {
			t.Fatal("unsafe sudo", args)
		}
		if args[len(prefix)] != "/trusted/apt-get" {
			return run(ctx, ex, args)
		}
		args = args[len(prefix)+1:]
		if !reflect.DeepEqual(args, codexRemoveAPTArgs(codexFixtureStage)) {
			t.Fatal("unsafe removal args", args)
		}
		f.calls = append(f.calls, systemCall{"apt-get", slices.Clone(args), true})
		if f.fault == "apt-get" {
			return "", errors.New("synthetic APT failure")
		}
		if f.fault == "cancel-apt-get" {
			f.cancel()
			return "", ctx.Err()
		}
		guard := codexAPTGuardTools{
			stage: s.stage,
			lock: func() error {
				if f.fault == "guard-lock" {
					return errors.New("no frontend lock")
				}
				return nil
			},
			removePolicy: func(ctx context.Context) error { return checkCodexRemovePolicy(ctx, f.b.codexDesktop) },
			packageRecord: func() (codexPackageRecord, error) {
				f.guardReads++
				if f.guardRecord != nil {
					return f.guardRecord()
				}
				return f.pkg, nil
			},
			read: func(path string, _ os.FileMode, _ int64) ([]byte, error) {
				data, ok := f.rootFiles[path]
				if !ok {
					return nil, os.ErrNotExist
				}
				return slices.Clone(data), nil
			},
			hash: func(path string, _ os.FileMode, _ int64) (int64, string, error) {
				data, ok := f.rootFiles[path]
				if !ok {
					return 0, "", os.ErrNotExist
				}
				return int64(len(data)), fmt.Sprintf("%x", sha256.Sum256(data)), nil
			},
			fields: func(context.Context, string) (string, error) { t.Fatal("removal inspected DEB"); return "", nil },
			marker: func(path string, data []byte) error {
				if _, ok := f.rootFiles[path]; ok {
					return os.ErrExist
				}
				f.rootFiles[path] = slices.Clone(data)
				return nil
			},
		}
		if f.fault != "apt-noop" {
			if f.fault != "skip-early" {
				if err := runCodexAPTGuardPhase(ctx, codexFixtureStage, strings.NewReader(""), guard, true); err != nil {
					return "", err
				}
			}
			if f.fault != "skip-final" {
				if err := runCodexAPTGuard(ctx, codexFixtureStage, strings.NewReader(f.plan), guard); err != nil {
					return "", err
				}
			}
		}
		f.pkg = f.after
		if f.fault == "missing-early-ack" {
			delete(f.rootFiles, codexFixtureStage+"/remove-early")
		}
		if f.fault == "missing-final-ack" {
			delete(f.rootFiles, codexFixtureStage+"/remove-ok")
		}
		if f.fault == "wrong-final-ack" {
			f.rootFiles[codexFixtureStage+"/remove-ok"] = []byte("foreign marker")
		}
		return "synthetic nonpurge removal\n", nil
	}
	return f
}

func assertCodexRemoveNoDownloads(t *testing.T, f *codexRemoveFixture) {
	t.Helper()
	if f.fixture.calls != 0 || f.hasCommand("dpkg-deb") || f.hasCommand("dpkg") {
		t.Fatal("removal downloaded or inspected/compared DEBs", f.calls)
	}
	for _, c := range f.calls {
		for _, arg := range c.args {
			if strings.HasSuffix(arg, ".deb") {
				t.Fatal("removal used a DEB", c)
			}
		}
	}
}

func TestCodexDesktopRemoveSuccess(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		for _, absent := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/absent=%t", arch, absent), func(t *testing.T) {
				f := newCodexRemoveFixture(t, arch)
				if absent {
					f.after = codexPackageRecord{}
				}
				personal := filepath.Join(filepath.Dir(f.b.req.AppsDir), "untouched-profile")
				writeFixture(t, personal, []byte("keep personal data"), 0600)
				var logs []string
				f.b.log = func(s string) { logs = append(logs, s) }
				mustRun(t, f.fixture, "uninstall", codexRemoveArgs()...)
				rec, err := f.b.codexDesktopRecord()
				if err != nil || rec == nil || rec.Schema != 3 || rec.Operation != "remove" || rec.Phase != "complete" || rec.Version != f.after.Version && !absent || rec.SHA256 != "" || rec.URL != "" || rec.Size != 0 || rec.Trust != "" {
					t.Fatal(rec, err)
				}
				if f.guardReads != 2 || len(f.rootFiles) != 0 {
					t.Fatal("missing guarded removal/cleanup", f.guardReads, f.rootFiles)
				}
				for _, p := range []string{rec.Input, f.b.root(codexDesktopSystem) + ".lock", filepath.Join(f.b.root(codexDesktopSystem), "current")} {
					if _, err := os.Lstat(p); !os.IsNotExist(err) {
						t.Fatal("unexpected leftover", p, err)
					}
				}
				data, err := os.ReadFile(personal)
				if err != nil || string(data) != "keep personal data" {
					t.Fatal("personal data changed", err)
				}
				f.b.codexDesktopStatus()
				if !strings.Contains(strings.Join(logs, "\n"), "No package download, source URL or DEB digest applies") {
					t.Fatal("misleading removal journal")
				}
				assertCodexRemoveNoDownloads(t, f)
				calls := len(f.calls)
				if err := f.run(context.Background(), "uninstall", codexRemoveArgs()...); err == nil || len(f.calls) != calls {
					t.Fatal("repeated absent/config-files removal accepted", err)
				}
				// A finished removal never blocks a later eligible install/update.
				if absent {
					mustRun(t, f.fixture, "install", codexDesktopArgs()...)
				}
			})
		}
	}
}

func TestCodexDesktopRemoveEligibilityAndArguments(t *testing.T) {
	for _, change := range []func(*codexRemoveFixture){
		func(f *codexRemoveFixture) { f.pkg = codexPackageRecord{} },
		func(f *codexRemoveFixture) { f.pkg.Status = "hold ok installed" },
		func(f *codexRemoveFixture) { f.pkg.Status = "install ok unpacked" },
		func(f *codexRemoveFixture) { f.pkg.Status = "deinstall ok config-files" },
		func(f *codexRemoveFixture) { f.pkg.Status = "install reinstreq half-installed" },
		func(f *codexRemoveFixture) { f.pkg.Version = "bad" },
		func(f *codexRemoveFixture) { f.pkg.Arch = "arm64" },
		func(f *codexRemoveFixture) { f.fault = "db" },
		func(f *codexRemoveFixture) { f.b.arch = "386" },
	} {
		f := newCodexRemoveFixture(t, "amd64")
		change(f)
		if err := f.run(context.Background(), "uninstall", codexRemoveArgs()...); err == nil || len(f.calls) != 0 {
			t.Fatal("unsafe baseline used", err, f.calls)
		}
		assertCodexRemoveNoDownloads(t, f)
	}
	for _, extra := range [][]string{{"--trust-download"}, {"--version", "latest"}, {"--version", "1.2.3"}, {"--keep-preparation"}, {"--remove-path"}, {"--rocm"}, {"--network"}, {"--accept-system-packages=true"}, {"--accept-system-packages"}} {
		f := newCodexRemoveFixture(t, "amd64")
		if err := f.run(context.Background(), "uninstall", append(codexRemoveArgs(), extra...)...); err == nil || len(f.calls) != 0 || f.reads != 0 {
			t.Fatal("unsafe args accepted", extra, err)
		}
	}
	f := newCodexRemoveFixture(t, "amd64")
	if err := f.run(context.Background(), "uninstall", "--provider", codexDesktopSystem); err == nil || f.reads != 0 {
		t.Fatal("consent missing", err)
	}
}

func TestCodexDesktopRemoveFailuresRetain(t *testing.T) {
	for _, fault := range []string{"mktemp", "stage-path", "stage", "install", "sha256sum", "guard-copy-changed", "copy-changed", "concurrent", "pins-changed", "apt-get", "guard-lock", "skip-early", "skip-final", "apt-noop", "missing-early-ack", "missing-final-ack", "wrong-final-ack", "rm", "rmdir", "final-journal"} {
		t.Run(fault, func(t *testing.T) {
			f := newCodexRemoveFixture(t, "amd64")
			f.fault = fault
			if err := f.run(context.Background(), "uninstall", codexRemoveArgs()...); err == nil {
				t.Fatal("fault passed")
			}
			if _, err := os.Lstat(f.b.root(codexDesktopSystem) + ".lock"); err != nil {
				t.Fatal("uncertain lock lost", err)
			}
			rec, err := f.b.codexDesktopRecord()
			if fault != "final-journal" && (err != nil || rec == nil || rec.Phase != "unknown") {
				t.Fatal(rec, err)
			}
			assertCodexRemoveNoDownloads(t, f)
			for _, op := range []string{"install", "update", "uninstall"} {
				f.fault = ""
				f.pkg = codexPackageRecord{true, "install ok installed", f.version, "amd64"}
				args := codexDesktopArgs()
				if op == "install" {
					f.pkg = codexPackageRecord{}
				}
				if op == "uninstall" {
					args = codexRemoveArgs()
				}
				if err := f.run(context.Background(), op, args...); err == nil {
					t.Fatal("incomplete removal did not block", op)
				}
			}
			assertCodexRemoveNoDownloads(t, f)
		})
	}
}

func TestCodexDesktopRemoveCancel(t *testing.T) {
	for _, phase := range []string{"before", "mktemp", "install", "sha256sum", "apt-get", "rm", "rmdir"} {
		t.Run(phase, func(t *testing.T) {
			f := newCodexRemoveFixture(t, "arm64")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f.cancel, f.fault = cancel, "cancel-"+phase
			if phase == "before" {
				cancel()
			}
			if err := f.run(ctx, "uninstall", codexRemoveArgs()...); !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if phase != "before" {
				rec, err := f.b.codexDesktopRecord()
				if err != nil || rec == nil || rec.Phase != "unknown" {
					t.Fatal(rec, err)
				}
				if _, err := os.Lstat(f.b.root(codexDesktopSystem) + ".lock"); err != nil {
					t.Fatal(err)
				}
			}
			assertCodexRemoveNoDownloads(t, f)
		})
	}
}

func TestCodexDesktopRemoveLockedBaselineAndPlan(t *testing.T) {
	for _, at := range []int{1, 2} {
		for _, field := range []string{"version", "arch", "held", "absent", "unknown"} {
			t.Run(fmt.Sprintf("%d/%s", at, field), func(t *testing.T) {
				f := newCodexRemoveFixture(t, "amd64")
				f.guardRecord = func() (codexPackageRecord, error) {
					p := f.pkg
					if f.guardReads == at {
						switch field {
						case "version":
							p.Version = "1.0"
						case "arch":
							p.Arch = "arm64"
						case "held":
							p.Status = "hold ok installed"
						case "absent":
							p = codexPackageRecord{}
						case "unknown":
							return p, errors.New("unknown")
						}
					}
					return p, nil
				}
				if err := f.run(context.Background(), "uninstall", codexRemoveArgs()...); err == nil {
					t.Fatal("changed locked baseline accepted")
				}
				if _, ok := f.rootFiles[codexFixtureStage+"/remove-ok"]; ok {
					t.Fatal("bad plan acknowledged")
				}
			})
		}
	}
	for _, extra := range []string{"chatgpt 1.0 amd64 none > - - none **REMOVE**\n", "other 1.0 amd64 none > - - none **REMOVE**\n", "other 1.0 amd64 none = 1.0 amd64 none **CONFIGURE**\n", "other - - none < 1.0 amd64 none /var/cache/apt/archives/other.deb\n"} {
		f := newCodexRemoveFixture(t, "amd64")
		f.plan += extra
		if err := f.run(context.Background(), "uninstall", codexRemoveArgs()...); err == nil {
			t.Fatal("extra action accepted", extra)
		}
		if _, ok := f.rootFiles[codexFixtureStage+"/remove-ok"]; ok {
			t.Fatal("extra action acknowledged")
		}
	}
}

func TestCodexDesktopRemovePostconditions(t *testing.T) {
	for _, post := range []codexPackageRecord{{true, "install ok installed", "1.0", "amd64"}, {true, "deinstall ok config-files", "1.0", "amd64"}, {true, "deinstall ok config-files", "2:2026.1028.1-9", "arm64"}, {true, "purge ok not-installed", "", ""}, {false, "", "1.0", "amd64"}} {
		f := newCodexRemoveFixture(t, "amd64")
		f.after = post
		if err := f.run(context.Background(), "uninstall", codexRemoveArgs()...); err == nil {
			t.Fatal("foreign/uncertain postcondition accepted", post)
		}
		if f.hasCommand("rm") || f.hasCommand("rmdir") {
			t.Fatal("uncertain state cleaned")
		}
	}
	f := newCodexRemoveFixture(t, "amd64")
	read := f.b.codexDesktop.packageRecord
	f.b.codexDesktop.packageRecord = func() (codexPackageRecord, error) {
		if f.guardReads == 2 {
			return codexPackageRecord{}, errors.New("unknown after APT")
		}
		return read()
	}
	if err := f.run(context.Background(), "uninstall", codexRemoveArgs()...); err == nil || f.hasCommand("rm") {
		t.Fatal("unknown postcondition accepted", err)
	}
}

func TestCodexDesktopRemoveInheritedPolicies(t *testing.T) {
	for _, key := range codexRemoveFalseOptions {
		f := newCodexRemoveFixture(t, "amd64")
		run := f.b.codexDesktop.commands.run
		f.b.codexDesktop.commands.run = func(ctx context.Context, ex string, args []string) (string, error) {
			out, err := run(ctx, ex, args)
			if ex == "/trusted/apt-config" && reflect.DeepEqual(args, codexConfigDumpArgs) {
				out += key + "=true\n"
			}
			return out, err
		}
		if err := f.run(context.Background(), "uninstall", codexRemoveArgs()...); err == nil {
			t.Fatal("inherited policy accepted", key)
		}
		assertCodexNoPrivilege(t, f.codexDesktopFixture)
	}
}

func TestCodexDesktopRemoveJournalCrossOperation(t *testing.T) {
	for _, priorOperation := range []string{"legacy-install", "install", "update"} {
		for _, complete := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/complete=%t", priorOperation, complete), func(t *testing.T) {
				f := newCodexRemoveFixture(t, "amd64")
				root := f.b.root(codexDesktopSystem)
				if err := dirs(root, true, true); err != nil {
					t.Fatal(err)
				}
				if err := writeJSON(filepath.Join(root, "owner.json"), ownership{1, "ai", codexDesktopSystem}); err != nil {
					t.Fatal(err)
				}
				retained := filepath.Join(filepath.Dir(f.b.req.AppsDir), "foreign-retained-input")
				writeFixture(t, retained, []byte("never cleanup from journal"), 0600)
				url, _ := codexDesktopURL("amd64")
				r := codexDesktopJournal{Schema: 2, Operation: priorOperation, Plugin: "ai", Provider: codexDesktopSystem, Phase: "failed", LastPhase: "metadata-verified", URL: url, Package: "chatgpt", Version: f.pkg.Version, Arch: "amd64", Size: 1, SHA256: strings.Repeat("1", 64), Trust: codexDesktopTrust, Input: retained, Staging: retained}
				if priorOperation == "legacy-install" {
					r.Schema, r.Operation = 1, ""
				}
				if priorOperation == "update" {
					r.Baseline = f.pkg
				}
				if complete {
					r.Phase, r.LastPhase = "complete", "complete"
				}
				if err := f.b.saveCodexDesktopRecord(r); err != nil {
					t.Fatal(err)
				}
				if _, err := f.b.codexDesktopRecord(); err != nil {
					t.Fatal("invalid starting journal", err)
				}
				err := f.run(context.Background(), "uninstall", codexRemoveArgs()...)
				if complete {
					if err != nil {
						t.Fatal("completed journal blocked removal", err)
					}
				} else {
					if err == nil {
						t.Fatal("incomplete prior operation ignored")
					}
					assertCodexNoPrivilege(t, f.codexDesktopFixture)
					got, err := f.b.codexDesktopRecord()
					if err != nil || *got != r {
						t.Fatal("prior journal replaced", got, err)
					}
				}
				data, err := os.ReadFile(retained)
				if err != nil || string(data) != "never cleanup from journal" {
					t.Fatal("journal cleanup occurred", err)
				}
				assertCodexRemoveNoDownloads(t, f)
			})
		}
	}
	for _, operation := range []string{"install", "update", "uninstall"} {
		t.Run("incomplete-removal-blocks-"+operation+"-without-lock", func(t *testing.T) {
			f := newCodexRemoveFixture(t, "arm64")
			f.fault = "apt-get"
			if err := f.run(context.Background(), "uninstall", codexRemoveArgs()...); err == nil {
				t.Fatal("failure not injected")
			}
			before, err := f.b.codexDesktopRecord()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(f.b.root(codexDesktopSystem) + ".lock"); err != nil {
				t.Fatal(err)
			}
			f.fault = ""
			args := codexDesktopArgs()
			if operation == "install" {
				f.pkg = codexPackageRecord{}
			}
			if operation == "uninstall" {
				args = codexRemoveArgs()
			}
			calls := len(f.calls)
			if err := f.run(context.Background(), operation, args...); err == nil {
				t.Fatal("missing lock bypassed incomplete removal")
			}
			for _, c := range f.calls[calls:] {
				if c.elevated {
					t.Fatal("elevation on blocked retry", c)
				}
			}
			after, err := f.b.codexDesktopRecord()
			if err != nil || *before != *after {
				t.Fatal("retained removal journal changed", after, err)
			}
			if _, err := os.Lstat(before.Input); err != nil {
				t.Fatal("retained input removed", err)
			}
			if len(f.rootFiles) == 0 {
				t.Fatal("root evidence removed")
			}
			assertCodexRemoveNoDownloads(t, f)
		})
	}
}

func TestCodexDesktopRemovePinnedPendingAndTriggerPolicy(t *testing.T) {
	f := newCodexRemoveFixture(t, "amd64")
	for _, key := range []string{"DPkg::ConfigurePending", "DPkg::TriggersPending", "DPkg::NoTriggers", "DPkg::Selection::Remove::Approved", "DPkg::Selection::Current::SaveAndRestore"} {
		setting := key + "=false"
		if !slices.Contains(codexRemoveAPTArgs(codexFixtureStage), setting) {
			t.Fatal("missing explicit pin", key)
		}
		for _, bad := range []string{strings.Replace(f.plan, setting+"\n", "", 1), strings.Replace(f.plan, setting, key+"=true", 1)} {
			if err := validateCodexAPTRemoveInput(bad, f.pkg, codexFixtureStage); err == nil {
				t.Fatal("pending/trigger policy bypass", key)
			}
		}
	}
}
