//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

// Build the effective hook input from literal command options, not host APT.
func codexRemoveReviewInput(t *testing.T, baseline codexPackageRecord) string {
	t.Helper()
	var config strings.Builder
	config.WriteString("VERSION 3\nDir=/\nDir::State=var/lib/apt\n")
	args := codexRemoveAPTArgs(codexFixtureStage)
	for i := 0; i < len(args); i++ {
		if args[i] == "-o" {
			i++
			if i == len(args) {
				t.Fatal("missing APT option value")
			}
			config.WriteString(args[i] + "\n")
		}
	}
	return config.String() + fmt.Sprintf("\nchatgpt %s %s none > - - none **REMOVE**\n", baseline.Version, baseline.Arch)
}

func TestCodexRemoveReviewActionBoundary(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		baseline := codexPackageRecord{true, "install ok installed", "1:2.0-1", arch}
		good := codexRemoveReviewInput(t, baseline)
		if err := validateCodexAPTRemoveInput(good, baseline, codexFixtureStage); err != nil {
			t.Fatal(err)
		}
		for _, bad := range []string{
			strings.Replace(good, "VERSION 3", "VERSION 2", 1),
			strings.Replace(good, "chatgpt 1:2.0-1", "other 1:2.0-1", 1),
			strings.Replace(good, "chatgpt 1:2.0-1", "chatgpt 1:1.0-1", 1),
			strings.Replace(good, arch+" none >", "i386 none >", 1),
			strings.Replace(good, "**REMOVE**", "**CONFIGURE**", 1),
			strings.Replace(good, "**REMOVE**", "/tmp/chatgpt.deb", 1),
			strings.Replace(good, "> - - none", "= 1:2.0-1 "+arch+" none", 1),
			good + "other 1.0 amd64 none > - - none **REMOVE**\n",
			good + fmt.Sprintf("chatgpt %s %s none > - - none **REMOVE**\n", baseline.Version, arch),
			strings.TrimSuffix(good, "\n"), good + "\n", good + "\x00",
		} {
			if err := validateCodexAPTRemoveInput(bad, baseline, codexFixtureStage); err == nil {
				t.Fatalf("unsafe plan accepted: %q", bad)
			}
		}
		for _, key := range []string{"APT::Get::Purge", "APT::Get::AutomaticRemove", "APT::Get::AutomaticRemove::Kernels", "APT::Get::Download", "APT::Get::Fix-Broken", "APT::Get::Fix-Missing", "APT::Get::Fix-Policy-Broken", "APT::Ignore-Hold", "DPkg::ConfigurePending", "DPkg::TriggersPending", "DPkg::NoTriggers", "DPkg::Selection::Remove::Approved", "DPkg::Selection::Current::SaveAndRestore"} {
			setting := key + "=false\n"
			if !strings.Contains(good, setting) {
				t.Fatalf("required explicit option missing: %s", key)
			}
			for _, bad := range []string{
				strings.Replace(good, setting, key+"=true\n", 1),
				strings.Replace(good, setting, "", 1),
				strings.Replace(good, setting, setting+strings.ToLower(key)+"=true\n", 1),
			} {
				if err := validateCodexAPTRemoveInput(bad, baseline, codexFixtureStage); err == nil {
					t.Fatalf("unsafe/missing policy %s accepted", key)
				}
			}
		}
	}
}

func TestCodexRemoveReviewMetadataAuthority(t *testing.T) {
	m := codexRemoveGuardMetadata{Schema: 3, Operation: "remove", Stage: codexFixtureStage,
		Baseline:     codexPackageRecord{true, "install ok installed", "1:2.0-1", "amd64"},
		HelperSHA256: strings.Repeat("1", 64), Nonce: strings.Repeat("2", 64)}
	if err := m.validate(codexFixtureStage); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		strings.Replace(string(data), `"Operation":`, `"operation":`, 1),
		strings.Replace(string(data), `"Baseline":`, `"baseline":`, 1),
		strings.Replace(string(data), `"Status":`, `"status":`, 1),
		strings.Replace(string(data), `"Present":true,`, "", 1),
		strings.Replace(string(data), `"Schema":3`, `"Schema":3,"Schema":2`, 1),
		strings.Replace(string(data), `"Operation":"remove"`, `"Operation":"remove","SHA256":"`+strings.Repeat("3", 64)+`"`, 1),
	} {
		var decoded codexRemoveGuardMetadata
		if err := decode([]byte(bad), &decoded, true); err == nil {
			t.Fatalf("ambiguous removal metadata accepted: %s", bad)
		}
	}
	for _, early := range []bool{true, false} {
		marker := codexRemoveMarker(m, early)
		for _, change := range []func(*codexRemoveGuardMetadata){
			func(v *codexRemoveGuardMetadata) { v.Operation = "install" },
			func(v *codexRemoveGuardMetadata) { v.Baseline.Version = "1:1.0-1" },
			func(v *codexRemoveGuardMetadata) { v.Baseline.Arch = "arm64" },
			func(v *codexRemoveGuardMetadata) { v.Baseline.Status = "hold ok installed" },
			func(v *codexRemoveGuardMetadata) { v.Nonce = strings.Repeat("4", 64) },
		} {
			changed := m
			change(&changed)
			if bytes.Equal(marker, codexRemoveMarker(changed, early)) {
				t.Fatal("marker not bound to original removal authority")
			}
		}
		if bytes.Equal(marker, codexRemoveMarker(m, !early)) {
			t.Fatal("guard phases share marker")
		}
	}
}

func TestCodexRemoveReviewPostcondition(t *testing.T) {
	before := codexPackageRecord{true, "install ok installed", "1:2.0-1", "amd64"}
	residual := codexPackageRecord{true, "deinstall ok config-files", before.Version, before.Arch}
	for _, good := range []codexPackageRecord{{}, residual} {
		if !codexRemovedPostcondition(good, before) {
			t.Fatal("valid nonpurge result refused", good)
		}
	}
	for _, bad := range []codexPackageRecord{
		before,
		{true, "deinstall ok config-files", "1:1.0-1", before.Arch},
		{true, "deinstall ok config-files", before.Version, "arm64"},
		{true, "deinstall reinstreq half-installed", before.Version, before.Arch},
		{true, "purge ok config-files", before.Version, before.Arch},
		{false, "install ok installed", before.Version, before.Arch},
	} {
		if codexRemovedPostcondition(bad, before) {
			t.Fatal("unexpected state accepted as removed", bad)
		}
	}
}

func TestCodexRemoveReviewOriginalStateGuards(t *testing.T) {
	m := codexRemoveGuardMetadata{Schema: 3, Operation: "remove", Stage: codexFixtureStage,
		Baseline:     codexPackageRecord{true, "install ok installed", "1:2.0-1", "amd64"},
		HelperSHA256: strings.Repeat("1", 64), Nonce: strings.Repeat("2", 64)}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	for _, early := range []bool{true, false} {
		for _, fault := range []string{"none", "absent", "held", "version", "arch", "unknown", "policy", "lock", "helper", "early-marker", "cancel"} {
			if early && fault == "early-marker" {
				continue
			}
			t.Run(fmt.Sprintf("early=%t/%s", early, fault), func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				writes := 0
				tools := codexAPTGuardTools{
					stage: func(string) error { return nil },
					lock: func() error {
						if fault == "lock" {
							return errors.New("lost frontend lock")
						}
						return nil
					},
					removePolicy: func(context.Context) error {
						if fault == "policy" {
							return errors.New("policy changed")
						}
						return nil
					},
					read: func(path string, _ os.FileMode, _ int64) ([]byte, error) {
						if path == codexFixtureStage+"/guard.json" {
							return data, nil
						}
						if path != codexFixtureStage+"/remove-early" {
							t.Fatal("unexpected read", path)
						}
						if fault == "early-marker" {
							return []byte("foreign acknowledgement"), nil
						}
						return codexRemoveMarker(m, true), nil
					},
					hash: func(string, os.FileMode, int64) (int64, string, error) {
						if fault == "helper" {
							return 1, strings.Repeat("5", 64), nil
						}
						return 1, m.HelperSHA256, nil
					},
					packageRecord: func() (codexPackageRecord, error) {
						rec := m.Baseline
						switch fault {
						case "absent":
							rec = codexPackageRecord{}
						case "held":
							rec.Status = "hold ok installed"
						case "version":
							rec.Version = "1:1.0-1"
						case "arch":
							rec.Arch = "arm64"
						case "unknown":
							return rec, errors.New("unknown package state")
						case "cancel":
							cancel()
						}
						return rec, nil
					},
					marker: func(path string, payload []byte) error {
						writes++
						if !bytes.Equal(payload, codexRemoveMarker(m, early)) {
							t.Fatal("wrong removal marker")
						}
						return nil
					},
				}
				err := runCodexAPTGuardPhase(ctx, codexFixtureStage, strings.NewReader(codexRemoveReviewInput(t, m.Baseline)), tools, early)
				if fault == "none" {
					if err != nil || writes != 1 {
						t.Fatal(err, writes)
					}
				} else if err == nil || writes != 0 {
					t.Fatal("unsafe guard acknowledged", fault, err, writes)
				}
			})
		}
	}
}
