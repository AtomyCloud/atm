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
	"time"
)

func TestCodexUpdateReviewOriginalRecordAtBothGuards(t *testing.T) {
	for _, phase := range []int{1, 2} {
		for _, field := range []string{"presence", "status", "version", "architecture", "unknown"} {
			t.Run(fmt.Sprintf("phase=%d/%s", phase, field), func(t *testing.T) {
				f := newCodexUpdateFixture(t, "amd64", 1)
				before := f.pkg
				reads := 0
				f.guardRecord = func() (codexPackageRecord, error) {
					reads++
					rec := before
					if reads != phase {
						return rec, nil
					}
					switch field {
					case "presence":
						rec = codexPackageRecord{}
					case "status":
						rec.Status = "hold ok installed"
					case "version":
						rec.Version = "1:2026.1026.1-1"
					case "architecture":
						rec.Arch = "arm64"
					case "unknown":
						return codexPackageRecord{}, errors.New("package record became unknown")
					}
					return rec, nil
				}
				if err := f.run(context.Background(), "update", codexDesktopArgs()...); err == nil {
					t.Fatal("changed baseline accepted")
				}
				if reads != phase || f.pkg != before || f.hasCommand("rm") || f.hasCommand("rmdir") {
					t.Fatal("unexpected guard/cleanup effects", reads, f.pkg)
				}
				if _, ok := f.rootFiles[codexFixtureStage+"/guard-ok"]; ok {
					t.Fatal("final acknowledgement after baseline change")
				}
				rec, err := f.b.codexDesktopRecord()
				if err != nil || rec == nil || rec.Phase != "unknown" || rec.Operation != "update" || rec.Baseline != before {
					t.Fatalf("lost original operation evidence: %+v %v", rec, err)
				}
				if _, err := os.Lstat(f.b.root(codexDesktopSystem) + ".lock"); err != nil {
					t.Fatal("uncertain privileged operation unlocked", err)
				}
			})
		}
	}
}

func TestCodexUpdateReviewMarkerAuthority(t *testing.T) {
	base := codexAPTGuardMetadata{
		Schema: 2, Operation: "update", Stage: codexFixtureStage, Version: "1:2.0-1", Arch: "amd64",
		Baseline: codexPackageRecord{true, "install ok installed", "1:1.0-1", "amd64"},
		SHA256:   strings.Repeat("1", 64), HelperSHA256: strings.Repeat("2", 64), Nonce: strings.Repeat("3", 64), Size: 1,
	}
	if err := base.validate(codexFixtureStage); err != nil {
		t.Fatal(err)
	}
	marker, early := codexGuardMarker(base), codexGuardEarlyMarker(base)
	if bytes.Equal(marker, early) {
		t.Fatal("guard phases share acknowledgement")
	}
	for _, mutate := range []func(*codexAPTGuardMetadata){
		func(m *codexAPTGuardMetadata) { m.Operation, m.Baseline = "install", codexPackageRecord{} },
		func(m *codexAPTGuardMetadata) { m.Baseline.Version = "1:0.9-1" },
		func(m *codexAPTGuardMetadata) { m.Baseline.Status = "hold ok installed" },
		func(m *codexAPTGuardMetadata) { m.Baseline.Arch = "arm64" },
		func(m *codexAPTGuardMetadata) { m.Baseline.Present = false },
		func(m *codexAPTGuardMetadata) { m.Nonce = strings.Repeat("4", 64) },
	} {
		changed := base
		mutate(&changed)
		if bytes.Equal(marker, codexGuardMarker(changed)) || bytes.Equal(early, codexGuardEarlyMarker(changed)) {
			t.Fatal("acknowledgement is not bound to original transaction")
		}
	}
	data, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		strings.Replace(string(data), `"Operation":`, `"operation":`, 1),
		strings.Replace(string(data), `"Baseline":`, `"baseline":`, 1),
		strings.Replace(string(data), `"Status":`, `"status":`, 1),
		strings.Replace(string(data), `"Present":true,`, "", 1),
		strings.Replace(string(data), `"Operation":"update",`, "", 1),
		strings.Replace(string(data), `"Operation":"update"`, `"Operation":"update","Operation":"install"`, 1),
		strings.Replace(string(data), `"Status":"install ok installed"`, `"Status":"install ok installed","status":"hold ok installed"`, 1),
	} {
		var m codexAPTGuardMetadata
		if err := decode([]byte(bad), &m, true); err == nil {
			t.Fatalf("ambiguous guard authority accepted: %s", bad)
		}
	}
}

func TestCodexUpdateReviewComparisonFailures(t *testing.T) {
	for _, mode := range []string{"error", "output", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			s := &systemTools{resolve: func(string) (string, error) { return "/trusted/dpkg", nil }}
			s.run = func(context.Context, string, []string) (string, error) {
				calls++
				switch mode {
				case "error":
					return "", errors.New("failure without a predicate exit code")
				case "output":
					return "unexpected comparator output", nil
				default:
					cancel()
					return "", nil
				}
			}
			if _, err := compareCodexVersions(ctx, s, "1:2.0-1", "1:1.0-1"); err == nil || calls != 1 {
				t.Fatal("comparison error became a decision", err, calls)
			}
		})
	}
}

// Explicit opt-in: compares only literal version strings, never queries or changes packages.
func TestCodexUpdateReviewRealDebianOrdering(t *testing.T) {
	if os.Getenv("ATM_TEST_CODEX_DPKG_COMPARE") != "1" {
		t.Skip("read-only system dpkg comparator check is opt-in")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, tc := range []struct {
		candidate, baseline string
		want                int
	}{
		{"1:1.0-1", "99.0-99", 1},
		{"1.0-10", "1.0-2", 1},
		{"1.0~rc1", "1.0", -1},
		{"1.0-0", "1.0", 0},
		{"1:1.0", "1.0", 1},
		{"1.9", "1.10", -1},
	} {
		got, err := compareCodexVersions(ctx, defaultSystemTools(), tc.candidate, tc.baseline)
		if err != nil || got != tc.want {
			t.Fatalf("%q vs %q: got %d/%v want %d", tc.candidate, tc.baseline, got, err, tc.want)
		}
	}
}
