//go:build linux

package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// Shaped after DNF5 base/transaction.cpp::serialize and
// transaction/transaction_sr.cpp::json_serialize, not history info --json.
const codexRPMUpdatePlanFixture = `{"version":"1.0","rpms":[{"nevra":"chatgpt-1.2026.101-1.x86_64","action":"Upgrade","reason":"User","repo_id":"@stored_transaction(@commandline)","package_path":"./packages/chatgpt.rpm"},{"nevra":"chatgpt-1.2026.100-1.x86_64","action":"Replaced","reason":"User","repo_id":"@System"}]}`

func codexRPMUpdatePlanMetadata() (codexRPMMetadata, codexRPMMetadata) {
	return codexRPMMetadata{"chatgpt", "0", "1.2026.101", "1", "x86_64"},
		codexRPMMetadata{"chatgpt", "0", "1.2026.100", "1", "x86_64"}
}

func TestCodexRPMUpdatePlanAccepted(t *testing.T) {
	for _, arch := range []string{"x86_64", "aarch64"} {
		for _, repo := range []string{"@commandline", "@stored_transaction(@commandline)"} {
			for _, reason := range []string{"User", "Dependency", "Weak Dependency", "Group", "External User", "None", "Clean"} {
				for _, reverse := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/%s/reverse=%t", arch, repo, reason, reverse), func(t *testing.T) {
						candidate, baseline := codexRPMUpdatePlanMetadata()
						candidate.Arch, baseline.Arch = arch, arch
						data := strings.ReplaceAll(codexRPMUpdatePlanFixture, "x86_64", arch)
						data = strings.ReplaceAll(data, "@stored_transaction(@commandline)", repo)
						data = strings.ReplaceAll(data, `"User"`, `"`+reason+`"`)
						if reverse {
							var p codexRPMPlan
							if err := decode([]byte(data), &p, true); err != nil {
								t.Fatal(err)
							}
							p.RPMs[0], p.RPMs[1] = p.RPMs[1], p.RPMs[0]
							// Marshal also exercises an explicit empty outgoing path.
							encoded, err := json.Marshal(p)
							if err != nil {
								t.Fatal(err)
							}
							data = string(encoded)
						}
						if err := validateCodexRPMUpdatePlan([]byte(data), candidate, baseline); err != nil {
							t.Fatal(err)
						}
					})
				}
			}
		}
	}
}

func TestCodexRPMUpdatePlanRPMVersions(t *testing.T) {
	for _, tc := range []struct {
		name, oldEpoch, oldVersion, oldRelease, newEpoch, newVersion, newRelease, oldNEVRA, newNEVRA string
	}{
		{"epoch", "1", "9.0", "1", "2", "1.0", "1", "chatgpt-1:9.0-1.x86_64", "chatgpt-2:1.0-1.x86_64"},
		{"zero-to-epoch", "0", "9.0", "1", "1", "1.0", "1", "chatgpt-9.0-1.x86_64", "chatgpt-1:1.0-1.x86_64"},
		{"release", "0", "1.0", "1.fc43", "0", "1.0", "2.fc43", "chatgpt-1.0-1.fc43.x86_64", "chatgpt-1.0-2.fc43.x86_64"},
		{"tilde", "0", "1.0~rc1", "1", "0", "1.0", "1", "chatgpt-1.0~rc1-1.x86_64", "chatgpt-1.0-1.x86_64"},
		{"caret", "0", "1.0", "1", "0", "1.0^git20261029", "1", "chatgpt-1.0-1.x86_64", "chatgpt-1.0^git20261029-1.x86_64"},
		{"non-semver", "0", "vNext_1+a", "1", "0", "vNext_2+a", "1", "chatgpt-vNext_1+a-1.x86_64", "chatgpt-vNext_2+a-1.x86_64"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate, baseline := codexRPMUpdatePlanMetadata()
			candidate.Epoch, candidate.Version, candidate.Release = tc.newEpoch, tc.newVersion, tc.newRelease
			baseline.Epoch, baseline.Version, baseline.Release = tc.oldEpoch, tc.oldVersion, tc.oldRelease
			data := strings.ReplaceAll(codexRPMUpdatePlanFixture, "chatgpt-1.2026.101-1.x86_64", tc.newNEVRA)
			data = strings.ReplaceAll(data, "chatgpt-1.2026.100-1.x86_64", tc.oldNEVRA)
			if err := validateCodexRPMUpdatePlan([]byte(data), candidate, baseline); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCodexRPMUpdatePlanRefusals(t *testing.T) {
	candidate, baseline := codexRPMUpdatePlanMetadata()
	check := func(t *testing.T, data []byte) {
		t.Helper()
		if err := validateCodexRPMUpdatePlan(data, candidate, baseline); err == nil {
			t.Fatal("unsafe update plan accepted")
		}
	}
	for name, data := range map[string]string{
		"empty": "", "null": "null", "array": "[]", "object": "{}", "truncated": codexRPMUpdatePlanFixture[:80],
		"trailing": codexRPMUpdatePlanFixture + `{}`, "garbage": codexRPMUpdatePlanFixture + `!`,
		"oversize":            codexRPMUpdatePlanFixture + strings.Repeat(" ", 64<<10),
		"duplicate-version":   strings.Replace(codexRPMUpdatePlanFixture, `"version":"1.0"`, `"version":"1.0","version":"1.0"`, 1),
		"duplicate-rpms":      strings.Replace(codexRPMUpdatePlanFixture, `"rpms":`, `"rpms":[],"rpms":`, 1),
		"duplicate-action":    strings.Replace(codexRPMUpdatePlanFixture, `"action":"Upgrade"`, `"action":"Install","action":"Upgrade"`, 1),
		"escaped-duplicate":   strings.Replace(codexRPMUpdatePlanFixture, `"action":"Upgrade"`, `"act\u0069on":"Upgrade","action":"Upgrade"`, 1),
		"duplicate-baseline":  strings.Replace(codexRPMUpdatePlanFixture, `"action":"Replaced"`, `"action":"Replaced","action":"Replaced"`, 1),
		"aliased-version":     strings.Replace(codexRPMUpdatePlanFixture, `"version":`, `"Version":`, 1),
		"aliased-rpms":        strings.Replace(codexRPMUpdatePlanFixture, `"rpms":`, `"RPMs":`, 1),
		"case-duplicate":      strings.Replace(codexRPMUpdatePlanFixture, `"action":"Upgrade"`, `"Action":"Install","action":"Upgrade"`, 1),
		"zero-epoch-spelling": strings.Replace(codexRPMUpdatePlanFixture, "chatgpt-1.2026.101", "chatgpt-0:1.2026.101", 1),
	} {
		t.Run(name, func(t *testing.T) { check(t, []byte(data)) })
	}
	mutate := func(name string, change func(map[string]any, []any)) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			var p map[string]any
			if err := json.Unmarshal([]byte(codexRPMUpdatePlanFixture), &p); err != nil {
				t.Fatal(err)
			}
			change(p, p["rpms"].([]any))
			data, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			check(t, data)
		})
	}
	for _, field := range []string{"groups", "environments", "modules", "unknown"} {
		mutate("top-"+field, func(p map[string]any, _ []any) { p[field] = []any{} })
	}
	for _, version := range []any{"", "1.1", "2.0", nil, 1} {
		mutate(fmt.Sprintf("schema-%v", version), func(p map[string]any, _ []any) { p["version"] = version })
	}
	mutate("missing-version", func(p map[string]any, _ []any) { delete(p, "version") })
	mutate("missing-rpms", func(p map[string]any, _ []any) { delete(p, "rpms") })
	mutate("null-rpms", func(p map[string]any, _ []any) { p["rpms"] = nil })
	mutate("empty-rpms", func(p map[string]any, _ []any) { p["rpms"] = []any{} })
	mutate("only-upgrade", func(p map[string]any, r []any) { p["rpms"] = r[:1] })
	mutate("only-replaced", func(p map[string]any, r []any) { p["rpms"] = r[1:] })
	for _, action := range []string{"Install", "Remove", "Replaced", "Downgrade", "Reinstall", "Upgrade"} {
		mutate("additional-"+action, func(p map[string]any, r []any) {
			p["rpms"] = append(r, map[string]any{"nevra": "other-1-1.x86_64", "action": action, "reason": "Dependency", "repo_id": "@System"})
		})
	}
	for i := range 2 {
		prefix := fmt.Sprintf("rpm-%d/", i)
		mutate(prefix+"duplicate-entry", func(p map[string]any, r []any) { p["rpms"] = []any{r[i], r[i]} })
		mutate(prefix+"null", func(_ map[string]any, r []any) { r[i] = nil })
		for _, field := range []string{"nevra", "action", "reason", "repo_id", "package_path"} {
			if field != "package_path" || i == 0 {
				mutate(prefix+"missing-"+field, func(_ map[string]any, r []any) { delete(r[i].(map[string]any), field) })
			}
			for _, value := range []any{nil, 42, false, []any{}, map[string]any{}} {
				mutate(fmt.Sprintf("%s%s-type-%T", prefix, field, value), func(_ map[string]any, r []any) { r[i].(map[string]any)[field] = value })
			}
			mutate(prefix+"alias-"+field, func(_ map[string]any, r []any) {
				entry := r[i].(map[string]any)
				entry[strings.ToUpper(field)] = ""
				delete(entry, field)
			})
		}
		for field, values := range map[string][]string{
			"nevra":        {"", "other-1-1.x86_64", "chatgpt-extra-1.2026.101-1.x86_64", "chatgpt-1.2026.099-1.x86_64", "chatgpt-1.2026.101-2.x86_64", "chatgpt-1.2026.101-1.aarch64", "chatgpt-1:1.2026.101-1.x86_64"},
			"action":       {"", "Install", "Remove", "Removed", "Downgrade", "Reinstall", "Upgraded", "Obsoleted", "Reason Change", "upgrade", "Unknown"},
			"reason":       {"", "user", "WeakDependency", "Unknown", "preservedUser"},
			"repo_id":      {"", "fedora", "@stored_transaction(fedora)", "@system", "@stored_transaction(@System)"},
			"package_path": {"packages/chatgpt.rpm", "./packages/../chatgpt.rpm", "/tmp/chatgpt.rpm", "https://example.invalid/chatgpt.rpm", "./packages/other.rpm"},
			"unknown":      {"value"}, "group_id": {"group"},
		} {
			for _, value := range values {
				mutate(prefix+field+"="+value, func(_ map[string]any, r []any) { r[i].(map[string]any)[field] = value })
			}
		}
	}
	mutate("incoming-system", func(_ map[string]any, r []any) { r[0].(map[string]any)["repo_id"] = "@System" })
	mutate("incoming-empty-path", func(_ map[string]any, r []any) { r[0].(map[string]any)["package_path"] = "" })
	mutate("outgoing-local", func(_ map[string]any, r []any) { r[1].(map[string]any)["repo_id"] = "@commandline" })
	mutate("outgoing-stored-local", func(_ map[string]any, r []any) {
		r[1].(map[string]any)["repo_id"] = "@stored_transaction(@commandline)"
	})
	mutate("outgoing-artifact", func(_ map[string]any, r []any) { r[1].(map[string]any)["package_path"] = "./packages/chatgpt.rpm" })
	mutate("swapped-identities", func(_ map[string]any, r []any) {
		a, b := r[0].(map[string]any), r[1].(map[string]any)
		a["nevra"], b["nevra"] = b["nevra"], a["nevra"]
	})
}

func TestCodexRPMUpdatePlanMetadataRefusals(t *testing.T) {
	for _, target := range []string{"candidate", "baseline", "both"} {
		for name, change := range map[string]func(*codexRPMMetadata){
			"name":              func(m *codexRPMMetadata) { m.Name = "other" },
			"arch":              func(m *codexRPMMetadata) { m.Arch = "aarch64" },
			"unsupported-arch":  func(m *codexRPMMetadata) { m.Arch = "i686" },
			"epoch":             func(m *codexRPMMetadata) { m.Epoch = "1" },
			"empty-epoch":       func(m *codexRPMMetadata) { m.Epoch = "" },
			"bad-epoch":         func(m *codexRPMMetadata) { m.Epoch = "01" },
			"version":           func(m *codexRPMMetadata) { m.Version = "9" },
			"empty-version":     func(m *codexRPMMetadata) { m.Version = "" },
			"invalid-version":   func(m *codexRPMMetadata) { m.Version = "1\t2" },
			"release":           func(m *codexRPMMetadata) { m.Release = "2" },
			"oversized-release": func(m *codexRPMMetadata) { m.Release = strings.Repeat("a", 257) },
		} {
			t.Run(target+"/"+name, func(t *testing.T) {
				candidate, baseline := codexRPMUpdatePlanMetadata()
				if target != "baseline" {
					change(&candidate)
				}
				if target != "candidate" {
					change(&baseline)
				}
				if err := validateCodexRPMUpdatePlan([]byte(codexRPMUpdatePlanFixture), candidate, baseline); err == nil {
					t.Fatal("unbound or invalid metadata accepted")
				}
			})
		}
	}
	candidate, baseline := codexRPMUpdatePlanMetadata()
	data := strings.ReplaceAll(codexRPMUpdatePlanFixture, "1.2026.101", "1.2026.100")
	if err := validateCodexRPMUpdatePlan([]byte(data), baseline, baseline); err == nil {
		t.Fatal("equal-version update accepted")
	}
	data = codexRPMUpdatePlanFixture + strings.Repeat(" ", (64<<10)-len(codexRPMUpdatePlanFixture))
	if err := validateCodexRPMUpdatePlan([]byte(data), candidate, baseline); err != nil {
		t.Fatalf("64 KiB boundary: %v", err)
	}
	if err := validateCodexRPMUpdatePlan([]byte(data+" "), candidate, baseline); err == nil {
		t.Fatal("oversized plan accepted")
	}
}
