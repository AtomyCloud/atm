//go:build linux

package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// Synthetic fixture shaped after DNF5 base/transaction.cpp::serialize and
// transaction/transaction_sr.cpp::json_serialize, not history info --json.
const codexRPMRemovePlanFixture = `{
  "rpms":[
    {
      "nevra":"chatgpt-1.2026.100-1.x86_64",
      "action":"Remove",
      "reason":"User",
      "repo_id":"@System"
    }
  ],
  "version":"1.0"
}`

func codexRPMRemovePlanMetadata() codexRPMMetadata {
	return codexRPMMetadata{"chatgpt", "0", "1.2026.100", "1", "x86_64"}
}

func TestCodexRPMRemovalTarget(t *testing.T) {
	for _, arch := range []string{"x86_64", "aarch64"} {
		for _, tc := range []struct {
			name, epoch, version, release, evr string
		}{
			{"zero-epoch", "0", "1.2026.100", "1", "1.2026.100-1"},
			{"epoch", "2", "1.0", "1", "2:1.0-1"},
			{"max-epoch", "4294967295", "1.0", "1", "4294967295:1.0-1"},
			{"release", "0", "1.0", "2.fc43", "1.0-2.fc43"},
			{"tilde", "0", "1.0~rc1", "1", "1.0~rc1-1"},
			{"caret", "1", "1.0^git20261029", "1", "1:1.0^git20261029-1"},
			{"non-semver", "0", "vNext_1+a", "r2_1+build", "vNext_1+a-r2_1+build"},
			{"component-boundary", "0", strings.Repeat("v", 256), strings.Repeat("r", 256), strings.Repeat("v", 256) + "-" + strings.Repeat("r", 256)},
		} {
			t.Run(arch+"/"+tc.name, func(t *testing.T) {
				baseline := codexRPMMetadata{"chatgpt", tc.epoch, tc.version, tc.release, arch}
				want := "chatgpt-" + tc.evr + "." + arch
				got, err := codexRPMRemovalTarget(baseline)
				if err != nil || got != want {
					t.Fatalf("target = %q, %v; want %q", got, err, want)
				}
				data := strings.Replace(codexRPMRemovePlanFixture, "chatgpt-1.2026.100-1.x86_64", want, 1)
				if err := validateCodexRPMRemovePlan([]byte(data), baseline); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestCodexRPMRemovePlanAccepted(t *testing.T) {
	for _, arch := range []string{"x86_64", "aarch64"} {
		for _, reason := range []string{"None", "Dependency", "User", "Clean", "Weak Dependency", "Group", "External User"} {
			for _, emptyPath := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/empty-path=%t", arch, reason, emptyPath), func(t *testing.T) {
					baseline := codexRPMRemovePlanMetadata()
					baseline.Arch = arch
					data := strings.ReplaceAll(codexRPMRemovePlanFixture, "x86_64", arch)
					data = strings.Replace(data, `"User"`, `"`+reason+`"`, 1)
					if emptyPath {
						data = strings.Replace(data, `"repo_id":`, `"package_path":"","repo_id":`, 1)
					}
					if err := validateCodexRPMRemovePlan([]byte(data), baseline); err != nil {
						t.Fatal(err)
					}
				})
			}
		}
	}
	data := codexRPMRemovePlanFixture + strings.Repeat(" ", (64<<10)-len(codexRPMRemovePlanFixture))
	if err := validateCodexRPMRemovePlan([]byte(data), codexRPMRemovePlanMetadata()); err != nil {
		t.Fatalf("64 KiB boundary: %v", err)
	}
	if err := validateCodexRPMRemovePlan([]byte(data+" "), codexRPMRemovePlanMetadata()); err == nil {
		t.Fatal("oversized plan accepted")
	}
}

func TestCodexRPMRemovePlanRefusals(t *testing.T) {
	check := func(t *testing.T, data []byte) {
		t.Helper()
		if err := validateCodexRPMRemovePlan(data, codexRPMRemovePlanMetadata()); err == nil {
			t.Fatal("unsafe removal plan accepted")
		}
	}
	for name, data := range map[string]string{
		"empty": "", "null": "null", "array": "[]", "object": "{}", "boolean": "true",
		"truncated": codexRPMRemovePlanFixture[:80], "trailing": codexRPMRemovePlanFixture + `{}`,
		"garbage": codexRPMRemovePlanFixture + `!`, "bom": "\xef\xbb\xbf" + codexRPMRemovePlanFixture,
		"oversize":               codexRPMRemovePlanFixture + strings.Repeat(" ", 64<<10),
		"duplicate-version":      strings.Replace(codexRPMRemovePlanFixture, `"version":`, `"version":"1.0","version":`, 1),
		"duplicate-rpms":         strings.Replace(codexRPMRemovePlanFixture, `"rpms":`, `"rpms":[],"rpms":`, 1),
		"duplicate-action":       strings.Replace(codexRPMRemovePlanFixture, `"action":`, `"action":"Install","action":`, 1),
		"escaped-duplicate":      strings.Replace(codexRPMRemovePlanFixture, `"action":`, `"act\u0069on":"Remove","action":`, 1),
		"aliased-version":        strings.Replace(codexRPMRemovePlanFixture, `"version":`, `"Version":`, 1),
		"aliased-rpms":           strings.Replace(codexRPMRemovePlanFixture, `"rpms":`, `"RPMs":`, 1),
		"case-duplicate":         strings.Replace(codexRPMRemovePlanFixture, `"action":`, `"Action":"Remove","action":`, 1),
		"case-duplicate-version": strings.Replace(codexRPMRemovePlanFixture, `"version":`, `"Version":"1.0","version":`, 1),
		"case-duplicate-rpms":    strings.Replace(codexRPMRemovePlanFixture, `"rpms":`, `"RPMs":[],"rpms":`, 1),
		"zero-epoch-spelling":    strings.Replace(codexRPMRemovePlanFixture, "chatgpt-1.2026.100", "chatgpt-0:1.2026.100", 1),
		"deep-json":              strings.Repeat("[", 34) + "0" + strings.Repeat("]", 34),
	} {
		t.Run(name, func(t *testing.T) { check(t, []byte(data)) })
	}
	mutate := func(name string, change func(map[string]any, []any)) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			var p map[string]any
			if err := json.Unmarshal([]byte(codexRPMRemovePlanFixture), &p); err != nil {
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
	for _, field := range []string{"groups", "environments", "modules", "packages", "unknown"} {
		for _, value := range []any{nil, []any{}, []any{map[string]any{"id": "other", "action": "Remove"}}} {
			mutate(fmt.Sprintf("top-%s-%v", field, value), func(p map[string]any, _ []any) { p[field] = value })
		}
	}
	for _, version := range []any{"", "1.1", "2.0", "1.00", nil, 1, false, []any{}, map[string]any{}} {
		mutate(fmt.Sprintf("schema-%v", version), func(p map[string]any, _ []any) { p["version"] = version })
	}
	for _, field := range []string{"version", "rpms"} {
		mutate("missing-"+field, func(p map[string]any, _ []any) { delete(p, field) })
	}
	for _, value := range []any{nil, []any{}, "", false, 1, map[string]any{}} {
		mutate(fmt.Sprintf("rpms-%T-%v", value, value), func(p map[string]any, _ []any) { p["rpms"] = value })
	}
	for _, value := range []any{nil, map[string]any{}, []any{}, "", false, 1} {
		mutate(fmt.Sprintf("rpm-entry-%T", value), func(_ map[string]any, r []any) { r[0] = value })
	}
	mutate("duplicate-entry", func(p map[string]any, r []any) { p["rpms"] = append(r, r[0]) })
	for _, action := range []string{"Install", "Upgrade", "Downgrade", "Reinstall", "Remove", "Replaced", "Reason Change"} {
		mutate("additional-"+action, func(p map[string]any, r []any) {
			p["rpms"] = append(r, map[string]any{"nevra": "other-1-1.x86_64", "action": action, "reason": "Dependency", "repo_id": "@System"})
		})
	}
	for _, field := range []string{"nevra", "action", "reason", "repo_id", "package_path"} {
		if field != "package_path" {
			mutate("missing-"+field, func(_ map[string]any, r []any) { delete(r[0].(map[string]any), field) })
		}
		for _, value := range []any{nil, 42, false, []any{}, map[string]any{}} {
			mutate(fmt.Sprintf("%s-type-%T", field, value), func(_ map[string]any, r []any) { r[0].(map[string]any)[field] = value })
		}
		mutate("alias-"+field, func(_ map[string]any, r []any) {
			entry := r[0].(map[string]any)
			value := entry[field]
			if field == "package_path" {
				value = ""
			}
			entry[strings.ToUpper(field)] = value
			delete(entry, field)
		})
		mutate("case-duplicate-"+field, func(_ map[string]any, r []any) {
			entry := r[0].(map[string]any)
			value := entry[field]
			if field == "package_path" {
				value = ""
				entry[field] = value
			}
			entry[strings.ToUpper(field)] = value
		})
		t.Run("duplicate-"+field, func(t *testing.T) {
			value := map[string]string{
				"nevra": "chatgpt-1.2026.100-1.x86_64", "action": "Remove",
				"reason": "User", "repo_id": "@System", "package_path": "",
			}[field]
			pair := fmt.Sprintf("%q:%q,", field, value)
			data := strings.Replace(codexRPMRemovePlanFixture, `"repo_id":`, pair+pair+`"repo_id":`, 1)
			check(t, []byte(data))
		})
	}
	for field, values := range map[string][]string{
		"nevra":        {"", "chatgpt", "chatgpt.x86_64", "other-1.2026.100-1.x86_64", "chatgpt-extra-1.2026.100-1.x86_64", "chatgpt-1.2026.101-1.x86_64", "chatgpt-1.2026.100-2.x86_64", "chatgpt-1.2026.100-1.aarch64", "chatgpt-1:1.2026.100-1.x86_64", "chatgpt-*.x86_64", "../chatgpt-1.2026.100-1.x86_64", "chatgpt-1.2026.100-1.x86_64\x00", "chatgpt-1.2026.100-1.x86_64\n"},
		"action":       {"", "Install", "Upgrade", "Downgrade", "Reinstall", "Replaced", "Reason Change", "Removed", "Erase", "Obsoleted", "remove", "Remove ", "Unknown"},
		"reason":       {"", "user", "WeakDependency", "Unknown", "preservedUser", "User\x00", "User "},
		"repo_id":      {"", "fedora", "@commandline", "@stored_transaction(@commandline)", "@stored_transaction(@System)", "@system", "@System ", "@System\x00"},
		"package_path": {"./packages/chatgpt.rpm", "packages/chatgpt.rpm", "./packages/../chatgpt.rpm", "../chatgpt.rpm", "/tmp/chatgpt.rpm", "https://example.invalid/chatgpt.rpm", " ", "\x00"},
		"unknown":      {""}, "group_id": {"", "group"},
	} {
		for _, value := range values {
			mutate(field+"="+value, func(_ map[string]any, r []any) { r[0].(map[string]any)[field] = value })
		}
	}
}

func TestCodexRPMRemovalTargetRefusals(t *testing.T) {
	for field, values := range map[string][]string{
		"name":    {"", "ChatGPT", "other", "chatgpt-extra", "chatgpt*", "--all", "../chatgpt", "chatgpt\t0", "chatgpt\n", strings.Repeat("x", 1025)},
		"epoch":   {"", "00", "01", "+1", "-1", "1.0", "1:2", "4294967296", " 1", "1 ", "1\t", "1\n"},
		"version": {"", "../1", "/tmp/1", "1/2", "1\\2", "1-2", "1:2", "*", "1?", "1[0]", "1{2}", "--all", "1;echo", "$(id)", "1\t2", "1\n2", "1\x002", "1 2", strings.Repeat("v", 257)},
		"release": {"", "../1", "/tmp/1", "1/2", "1\\2", "1-2", "1:2", "*", "1?", "1[0]", "1\t2", "1\n2", "1\x002", "1 2", strings.Repeat("r", 257)},
		"arch":    {"", "amd64", "arm64", "i686", "noarch", "X86_64", "x86_64*", "../x86_64", "x86_64\t", "x86_64\n"},
	} {
		for _, value := range values {
			t.Run(fmt.Sprintf("%s=%q", field, value), func(t *testing.T) {
				baseline := codexRPMRemovePlanMetadata()
				switch field {
				case "name":
					baseline.Name = value
				case "epoch":
					baseline.Epoch = value
				case "version":
					baseline.Version = value
				case "release":
					baseline.Release = value
				case "arch":
					baseline.Arch = value
				}
				if target, err := codexRPMRemovalTarget(baseline); err == nil || target != "" {
					t.Fatalf("invalid baseline produced %q, %v", target, err)
				}
				if err := validateCodexRPMRemovePlan([]byte(codexRPMRemovePlanFixture), baseline); err == nil {
					t.Fatal("invalid baseline accepted")
				}
			})
		}
	}
	if target, err := codexRPMRemovalTarget(codexRPMMetadata{}); err == nil || target != "" {
		t.Fatalf("zero baseline produced %q, %v", target, err)
	}
}

func TestCodexRPMRemovePlanOriginalBaseline(t *testing.T) {
	for name, change := range map[string]func(*codexRPMMetadata){
		"epoch":        func(m *codexRPMMetadata) { m.Epoch = "1" },
		"version":      func(m *codexRPMMetadata) { m.Version = "1.2026.101" },
		"release":      func(m *codexRPMMetadata) { m.Release = "2" },
		"architecture": func(m *codexRPMMetadata) { m.Arch = "aarch64" },
	} {
		t.Run(name, func(t *testing.T) {
			baseline := codexRPMRemovePlanMetadata()
			changed := baseline
			change(&changed)
			target, err := codexRPMRemovalTarget(changed)
			if err != nil {
				t.Fatal(err)
			}
			data := strings.Replace(codexRPMRemovePlanFixture, "chatgpt-1.2026.100-1.x86_64", target, 1)
			if err := validateCodexRPMRemovePlan([]byte(data), baseline); err == nil {
				t.Fatal("replacement baseline accepted")
			}
			if err := validateCodexRPMRemovePlan([]byte(codexRPMRemovePlanFixture), changed); err == nil {
				t.Fatal("stale plan accepted")
			}
		})
	}
}
