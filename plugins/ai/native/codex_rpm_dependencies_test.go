//go:build linux

package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// Synthetic stored-transaction fixture, not DNF history JSON. The dependency
// filename intentionally differs from its NEVRA: DNF uses location basenames.
const codexRPMDependencyPlanFixture = `{"version":"1.0","rpms":[
{"nevra":"chatgpt-1.0-1.x86_64","action":"Install","reason":"User","repo_id":"@commandline","package_path":"./packages/chatgpt.rpm"},
{"nevra":"lib-test++-2:1.0~rc1-3.fc43.x86_64","action":"Install","reason":"Dependency","repo_id":"@stored_transaction(updates)","package_path":"./packages/payload.rpm"},
{"nevra":"shared-data-1.0-1.noarch","action":"Install","reason":"Dependency","repo_id":"@stored_transaction(copr:example.repo)","package_path":"./packages/shared-data.rpm"}
]}`

func dependencyPlanTarget() codexRPMMetadata {
	return codexRPMMetadata{"chatgpt", "0", "1.0", "1", "x86_64"}
}

func dependencyPlanMutation(t *testing.T, change func(map[string]any, []any)) []byte {
	t.Helper()
	var p map[string]any
	if err := json.Unmarshal([]byte(codexRPMDependencyPlanFixture), &p); err != nil {
		t.Fatal(err)
	}
	change(p, p["rpms"].([]any))
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestCodexRPMDependencyPlanAccepted(t *testing.T) {
	for _, arch := range []string{"x86_64", "aarch64"} {
		for _, local := range []string{"@commandline", "@stored_transaction(@commandline)"} {
			t.Run(arch+local, func(t *testing.T) {
				target := dependencyPlanTarget()
				target.Arch = arch
				data := strings.ReplaceAll(codexRPMDependencyPlanFixture, "x86_64", arch)
				data = strings.Replace(data, `"@commandline"`, fmt.Sprintf("%q", local), 1)
				want := []codexRPMDependency{
					{codexRPMMetadata{"lib-test++", "2", "1.0~rc1", "3.fc43", arch}, "./packages/payload.rpm", "@stored_transaction(updates)"},
					{codexRPMMetadata{"shared-data", "0", "1.0", "1", "noarch"}, "./packages/shared-data.rpm", "@stored_transaction(copr:example.repo)"},
				}
				got, err := validateCodexRPMDependencyPlan([]byte(data), target)
				if err != nil || !reflect.DeepEqual(got, want) {
					t.Fatalf("got %#v, %v; want %#v", got, err, want)
				}
			})
		}
	}
	for _, count := range []int{0, 1, 64, 65} {
		t.Run(fmt.Sprintf("dependencies-%d", count), func(t *testing.T) {
			data := dependencyPlanMutation(t, func(p map[string]any, r []any) {
				items := []any{}
				for i := 0; i < count; i++ {
					items = append(items, map[string]any{
						"nevra": fmt.Sprintf("dep-%d-1-1.noarch", i), "action": "Install", "reason": "Dependency",
						"repo_id": "@stored_transaction(fedora)", "package_path": fmt.Sprintf("./packages/dep-%d.rpm", i),
					})
				}
				p["rpms"] = append(items, r[0]) // Target need not be first.
			})
			got, err := validateCodexRPMDependencyPlan(data, dependencyPlanTarget())
			if count == 65 {
				if err == nil || got != nil {
					t.Fatalf("65 dependencies accepted: %#v, %v", got, err)
				}
			} else if err != nil || len(got) != count {
				t.Fatalf("got %d dependencies, %v", len(got), err)
			}
		})
	}
	data := codexRPMDependencyPlanFixture + strings.Repeat(" ", (64<<10)-len(codexRPMDependencyPlanFixture))
	if _, err := validateCodexRPMDependencyPlan([]byte(data), dependencyPlanTarget()); err != nil {
		t.Fatalf("64 KiB boundary: %v", err)
	}
	if got, err := validateCodexRPMDependencyPlan([]byte(data+" "), dependencyPlanTarget()); err == nil || got != nil {
		t.Fatal("oversized plan accepted")
	}
	for _, epoch := range []string{"1", "4294967295"} {
		target := dependencyPlanTarget()
		target.Epoch = epoch
		data := strings.Replace(codexRPMDependencyPlanFixture, "chatgpt-1.0", "chatgpt-"+epoch+":1.0", 1)
		if _, err := validateCodexRPMDependencyPlan([]byte(data), target); err != nil {
			t.Fatalf("target epoch %s: %v", epoch, err)
		}
	}
}

func TestCodexRPMDependencyPlanRequiresWrappedRepositories(t *testing.T) {
	for _, repo := range []string{"fedora", "updates", "copr:example.repo", strings.Repeat("r", 256)} {
		for _, index := range []int{1, 2} {
			t.Run(fmt.Sprintf("entry-%d/%s", index, repo), func(t *testing.T) {
				for _, wrapped := range []bool{false, true} {
					id := repo
					if wrapped {
						id = "@stored_transaction(" + repo + ")"
					}
					data := dependencyPlanMutation(t, func(_ map[string]any, r []any) { r[index].(map[string]any)["repo_id"] = id })
					got, err := validateCodexRPMDependencyPlan(data, dependencyPlanTarget())
					if wrapped {
						if err != nil || len(got) != 2 || got[index-1].Repo != id {
							t.Fatalf("wrapped repository: %#v, %v", got, err)
						}
					} else if err == nil || got != nil {
						t.Fatalf("raw configured repository accepted: %#v, %v", got, err)
					}
				}
			})
		}
	}
	for _, repo := range []string{"@commandline", "@stored_transaction(@commandline)"} {
		data := dependencyPlanMutation(t, func(p map[string]any, r []any) {
			r[0].(map[string]any)["repo_id"] = repo
			p["rpms"] = r[:1]
		})
		if err := validateCodexRPMPlan(data, dependencyPlanTarget()); err != nil {
			t.Fatalf("default no-dependency compatibility: %v", err)
		}
		if got, err := validateCodexRPMDependencyPlan(data, dependencyPlanTarget()); err != nil || len(got) != 0 {
			t.Fatalf("empty dependency set compatibility: %#v, %v", got, err)
		}
	}
	for _, repo := range []string{
		"@Stored_transaction(updates)", "@stored_transaction(updates) ", " @stored_transaction(updates)",
		"@stored_transaction(updates)extra", "@stored_transaction(updates\x00)",
		"@stored_transaction(" + strings.Repeat("r", 257) + ")",
	} {
		data := dependencyPlanMutation(t, func(_ map[string]any, r []any) { r[1].(map[string]any)["repo_id"] = repo })
		if got, err := validateCodexRPMDependencyPlan(data, dependencyPlanTarget()); err == nil || got != nil {
			t.Fatalf("invalid wrapper accepted: %#v, %v", got, err)
		}
	}
}

func TestCodexRPMDependencyPlanRefusals(t *testing.T) {
	check := func(t *testing.T, data []byte) {
		t.Helper()
		if got, err := validateCodexRPMDependencyPlan(data, dependencyPlanTarget()); err == nil || got != nil {
			t.Fatalf("unsafe plan returned %#v, %v", got, err)
		}
	}
	for name, data := range map[string]string{
		"empty": "", "null": "null", "array": "[]", "object": "{}", "boolean": "true",
		"truncated": codexRPMDependencyPlanFixture[:100], "trailing": codexRPMDependencyPlanFixture + `{}`,
		"garbage": codexRPMDependencyPlanFixture + "!", "bom": "\xef\xbb\xbf" + codexRPMDependencyPlanFixture,
		"deep":              strings.Repeat("[", 34) + "0" + strings.Repeat("]", 34),
		"duplicate-version": strings.Replace(codexRPMDependencyPlanFixture, `"version":`, `"version":"1.0","version":`, 1),
		"duplicate-rpms":    strings.Replace(codexRPMDependencyPlanFixture, `"rpms":`, `"rpms":[],"rpms":`, 1),
		"escaped-duplicate": strings.Replace(codexRPMDependencyPlanFixture, `"action":`, `"act\u0069on":"Install","action":`, 1),
	} {
		t.Run(name, func(t *testing.T) { check(t, []byte(data)) })
	}
	mutate := func(name string, change func(map[string]any, []any)) {
		t.Helper()
		t.Run(name, func(t *testing.T) { check(t, dependencyPlanMutation(t, change)) })
	}
	for _, key := range []string{"version", "rpms"} {
		mutate("missing-"+key, func(p map[string]any, _ []any) { delete(p, key) })
		mutate("alias-"+key, func(p map[string]any, _ []any) { p[strings.ToUpper(key)] = p[key]; delete(p, key) })
		mutate("case-duplicate-"+key, func(p map[string]any, _ []any) { p[strings.ToUpper(key)] = p[key] })
		for _, value := range []any{nil, 1, true, "", "1.1", "1.00", []any{}, map[string]any{}} {
			mutate(fmt.Sprintf("%s-type-%T-%v", key, value, value), func(p map[string]any, _ []any) { p[key] = value })
		}
	}
	for _, key := range []string{"groups", "environments", "modules", "packages", "unknown"} {
		for _, value := range []any{nil, []any{}, map[string]any{}} {
			mutate(fmt.Sprintf("unknown-%s-%T", key, value), func(p map[string]any, _ []any) { p[key] = value })
		}
	}
	mutate("missing-target", func(p map[string]any, r []any) { p["rpms"] = r[1:] })
	mutate("duplicate-target", func(p map[string]any, r []any) { p["rpms"] = append(r, r[0]) })
	mutate("duplicate-dependency", func(p map[string]any, r []any) { p["rpms"] = append(r, r[1]) })
	mutate("same-name-different-version", func(_ map[string]any, r []any) { r[2].(map[string]any)["nevra"] = "lib-test++-9-9.noarch" })
	mutate("duplicate-path", func(_ map[string]any, r []any) { r[2].(map[string]any)["package_path"] = "./packages/payload.rpm" })
	for _, index := range []int{0, 1, 2} {
		for _, value := range []any{nil, "", 1, false, []any{}, map[string]any{}} {
			mutate(fmt.Sprintf("entry-%d-%T", index, value), func(_ map[string]any, r []any) { r[index] = value })
		}
		for _, key := range []string{"nevra", "action", "reason", "repo_id", "package_path"} {
			mutate(fmt.Sprintf("missing-%d-%s", index, key), func(_ map[string]any, r []any) { delete(r[index].(map[string]any), key) })
			mutate(fmt.Sprintf("alias-%d-%s", index, key), func(_ map[string]any, r []any) {
				entry := r[index].(map[string]any)
				entry[strings.ToUpper(key)] = entry[key]
				delete(entry, key)
			})
			mutate(fmt.Sprintf("case-duplicate-%d-%s", index, key), func(_ map[string]any, r []any) {
				entry := r[index].(map[string]any)
				entry[strings.ToUpper(key)] = entry[key]
			})
			for _, value := range []any{nil, "", 1, false, []any{}, map[string]any{}} {
				mutate(fmt.Sprintf("field-%d-%s-%T", index, key, value), func(_ map[string]any, r []any) { r[index].(map[string]any)[key] = value })
			}
			t.Run(fmt.Sprintf("duplicate-%d-%s", index, key), func(t *testing.T) {
				data := dependencyPlanMutation(t, func(_ map[string]any, _ []any) {})
				needle := fmt.Sprintf("%q:", key)
				// Every occurrence is duplicated, including dependency entries.
				check(t, []byte(strings.ReplaceAll(string(data), needle, needle+`"ignored",`+needle)))
			})
		}
		for _, key := range []string{"group_id", "unknown"} {
			mutate(fmt.Sprintf("unknown-entry-%d-%s", index, key), func(_ map[string]any, r []any) { r[index].(map[string]any)[key] = "" })
		}
		for _, action := range []string{"Upgrade", "Downgrade", "Reinstall", "Replaced", "Remove", "Reason Change", "install", "Install "} {
			mutate(fmt.Sprintf("action-%d-%s", index, action), func(_ map[string]any, r []any) { r[index].(map[string]any)["action"] = action })
		}
	}
	for key, values := range map[string][]string{
		"nevra":        {"chatgpt-2.0-1.x86_64", "chatgpt-0:1.0-1.x86_64", "chatgpt-1.0-1.noarch", "chatgpt-1.0-1.aarch64", "other-1.0-1.x86_64"},
		"reason":       {"Dependency", "Weak Dependency", "Group", "user"},
		"repo_id":      {"updates", "@System", "@stored_transaction(updates)", "@stored_transaction(@System)"},
		"package_path": {"./packages/other.rpm", "packages/chatgpt.rpm", "./packages/../chatgpt.rpm"},
	} {
		for _, value := range values {
			mutate("target-"+key+"="+value, func(_ map[string]any, r []any) { r[0].(map[string]any)[key] = value })
		}
	}
	for key, values := range map[string][]string{
		"nevra":        {"chatgpt-2-1.x86_64", "lib-0:1-1.x86_64", "lib-01:1-1.x86_64", "lib-4294967296:1-1.x86_64", "lib--1.x86_64", "lib-1-.x86_64", "lib-1-1.aarch64", "lib-1-1.i686", "lib-1-1.src", "lib-1-1.X86_64", "../lib-1-1.x86_64", "lib*-1-1.x86_64", "--lib-1-1.x86_64", "lib-1-1.x86_64\n", "lib-1-1.x86_64\x00", strings.Repeat("a", 257) + "-1-1.x86_64", "lib-" + strings.Repeat("v", 257) + "-1.x86_64", "lib-1-" + strings.Repeat("r", 257) + ".x86_64", strings.Repeat("x", 1025)},
		"reason":       {"User", "Weak Dependency", "Group", "None", "Clean", "External User", "dependency", "Dependency "},
		"repo_id":      {"@System", "@commandline", "@stored_transaction(@System)", "@stored_transaction(@commandline)", "@stored_transaction(@stored_transaction(updates))", "@stored_transaction(updates", "@stored_transaction()", "@stored_transaction(updates))", "updates ", "updates\n", "updates\x00", "../updates", "https://repo.invalid", "update*", strings.Repeat("r", 257)},
		"package_path": {"./packages/chatgpt.rpm", "packages/dep.rpm", "/packages/dep.rpm", "../packages/dep.rpm", "./packages/../dep.rpm", "./packages/sub/dep.rpm", "./packages//dep.rpm", "./packages/.hidden.rpm", "./packages/-option.rpm", "./packages/dep\\evil.rpm", "./packages/dep.rpm\n", "./packages/dep\x00.rpm", "./packages/dep*.rpm", "./packages/dep?.rpm", "./packages/dep.rpm/", "./packages/dep.RPM", "./packages/dep.rpm?x", "https://repo.invalid/dep.rpm", "./packages/" + strings.Repeat("f", 252) + ".rpm"},
	} {
		for _, value := range values {
			mutate("dependency-"+key+"="+value, func(_ map[string]any, r []any) { r[1].(map[string]any)[key] = value })
		}
	}
}

func TestRPMDependencyMetadata(t *testing.T) {
	for _, arch := range []string{"x86_64", "aarch64", "noarch"} {
		goarch := "amd64"
		if arch == "aarch64" {
			goarch = "arm64"
		}
		for _, epoch := range []string{"0", "2", "4294967295"} {
			m := codexRPMMetadata{"lib-stdc++", epoch, "1.0^git1", "2.fc43", arch}
			record := fmt.Sprintf("%s\t%s\t%s\t%s\t%s\n", m.Name, m.Epoch, m.Version, m.Release, m.Arch)
			if err := validateRPMDependencyPackage(m, goarch); err != nil {
				t.Fatal(err)
			}
			if err := parseRPMDependencyMetadata(record, m); err != nil {
				t.Fatal(err)
			}
			for _, bad := range []string{"", strings.TrimSuffix(record, "\n"), record + record, record + "\n", " " + record, record + "warning", "warning\n" + record, strings.ReplaceAll(record, "\t", " "), strings.ReplaceAll(record, "\n", "\r\n"), strings.Repeat("x", 1025)} {
				if err := parseRPMDependencyMetadata(bad, m); err == nil {
					t.Fatalf("accepted noncanonical query output %q", bad)
				}
			}
			for _, field := range []int{0, 1, 2, 3, 4} {
				fields := strings.Split(strings.TrimSuffix(record, "\n"), "\t")
				fields[field] += "1"
				if err := parseRPMDependencyMetadata(strings.Join(fields, "\t")+"\n", m); err == nil {
					t.Fatalf("accepted mismatched query field %d", field)
				}
			}
		}
	}
	m := codexRPMMetadata{strings.Repeat("n", 256), "4294967295", strings.Repeat("v", 256), strings.Repeat("r", 256), "noarch"}
	if err := validateRPMDependencyPackage(m, "arm64"); err != nil {
		t.Fatalf("bounded components: %v", err)
	}
	if err := parseRPMDependencyMetadata(fmt.Sprintf("%s\t%s\t%s\t%s\t%s\n", m.Name, m.Epoch, m.Version, m.Release, m.Arch), m); err != nil {
		t.Fatal(err)
	}
}

func TestRPMDependencyMetadataRefusals(t *testing.T) {
	for field, values := range map[string][]string{
		"name":    {"", "chatgpt", "--all", "../lib", "lib*", "lib?", "lib[0]", "lib/part", "lib\\part", "lib\t0", "lib\n", "lib\x00", "lib name", strings.Repeat("n", 257)},
		"epoch":   {"", "00", "01", "+1", "-1", "(none)", "1.0", "4294967296", "1:2", " 1", "1\n"},
		"version": {"", "../v", "v-1", "v:1", "v/1", "v\\1", "*", "v?", "v[0]", "v 1", "v\t1", "v\n1", "v\x00", strings.Repeat("v", 257)},
		"release": {"", "../r", "r-1", "r:1", "r/1", "r\\1", "*", "r?", "r[0]", "r 1", "r\t1", "r\n1", "r\x00", strings.Repeat("r", 257)},
		"arch":    {"", "amd64", "arm64", "i686", "src", "X86_64", "x86_64*", "x86_64\n"},
	} {
		for _, value := range values {
			t.Run(fmt.Sprintf("%s=%q", field, value), func(t *testing.T) {
				m := codexRPMMetadata{"lib-test", "0", "1.0", "1", "x86_64"}
				switch field {
				case "name":
					m.Name = value
				case "epoch":
					m.Epoch = value
				case "version":
					m.Version = value
				case "release":
					m.Release = value
				case "arch":
					m.Arch = value
				}
				if err := validateRPMDependencyPackage(m, "amd64"); err == nil {
					t.Fatal("invalid metadata accepted")
				}
				if err := parseRPMDependencyMetadata(fmt.Sprintf("%s\t%s\t%s\t%s\t%s\n", m.Name, m.Epoch, m.Version, m.Release, m.Arch), m); err == nil {
					t.Fatal("invalid expected metadata accepted")
				}
			})
		}
	}
	for _, goarch := range []string{"", "386", "x86_64", "arm64"} {
		if err := validateRPMDependencyPackage(codexRPMMetadata{"lib", "0", "1", "1", "x86_64"}, goarch); err == nil {
			t.Fatalf("accepted incompatible architecture %q", goarch)
		}
	}
	for _, target := range []codexRPMMetadata{{}, {"other", "0", "1.0", "1", "x86_64"}, {"chatgpt", "00", "1.0", "1", "x86_64"}, {"chatgpt", "0", "1.0", "1", "noarch"}, {"chatgpt", "0", "../1", "1", "x86_64"}} {
		if got, err := validateCodexRPMDependencyPlan([]byte(codexRPMDependencyPlanFixture), target); err == nil || got != nil {
			t.Fatalf("invalid target accepted: %#v, %v", got, err)
		}
	}
}
