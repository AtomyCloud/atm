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

const codexRPMUpdateDependencyPlanFixture = `{"version":"1.0","rpms":[
{"nevra":"chatgpt-1.0-1.x86_64","action":"Upgrade","reason":"User","repo_id":"@commandline","package_path":"./packages/chatgpt.rpm"},
{"nevra":"chatgpt-0.9-1.x86_64","action":"Replaced","reason":"User","repo_id":"@System"},
{"nevra":"lib-test++-2:1.0~rc1-3.fc43.x86_64","action":"Install","reason":"Dependency","repo_id":"@stored_transaction(updates)","package_path":"./packages/payload.rpm"},
{"nevra":"shared-data-1.0-1.noarch","action":"Install","reason":"Dependency","repo_id":"@stored_transaction(copr:example.repo)","package_path":"./packages/shared-data.rpm"}
]}`

func updateDependencyPlanMetadata() (codexRPMMetadata, codexRPMMetadata) {
	candidate := dependencyPlanTarget()
	baseline := candidate
	baseline.Version = "0.9"
	return candidate, baseline
}

func updateDependencyPlanMutation(t *testing.T, change func(map[string]any, []any)) []byte {
	t.Helper()
	var p map[string]any
	if err := json.Unmarshal([]byte(codexRPMUpdateDependencyPlanFixture), &p); err != nil {
		t.Fatal(err)
	}
	change(p, p["rpms"].([]any))
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestCodexRPMUpdateDependencyPlanAccepted(t *testing.T) {
	reasons := []string{"None", "Dependency", "User", "Clean", "Weak Dependency", "Group", "External User"}
	for _, arch := range []string{"x86_64", "aarch64"} {
		for _, repo := range []string{"@commandline", "@stored_transaction(@commandline)"} {
			for _, reason := range reasons {
				for _, oldReason := range reasons {
					t.Run(arch+repo+reason+"/"+oldReason, func(t *testing.T) {
						candidate, baseline := updateDependencyPlanMetadata()
						candidate.Arch, baseline.Arch = arch, arch
						data := updateDependencyPlanMutation(t, func(p map[string]any, r []any) {
							for _, item := range r {
								entry := item.(map[string]any)
								entry["nevra"] = strings.ReplaceAll(entry["nevra"].(string), "x86_64", arch)
							}
							r[0].(map[string]any)["repo_id"] = repo
							r[0].(map[string]any)["reason"] = reason
							r[1].(map[string]any)["reason"] = oldReason
							// Explicit empty outgoing paths and interleaved/reversed targets are valid.
							r[1].(map[string]any)["package_path"] = ""
							p["rpms"] = []any{r[2], r[1], r[3], r[0]}
						})
						want := []codexRPMDependency{
							{codexRPMMetadata{"lib-test++", "2", "1.0~rc1", "3.fc43", arch}, "./packages/payload.rpm", "@stored_transaction(updates)"},
							{codexRPMMetadata{"shared-data", "0", "1.0", "1", "noarch"}, "./packages/shared-data.rpm", "@stored_transaction(copr:example.repo)"},
						}
						got, err := validateCodexRPMUpdateDependencyPlan(data, candidate, baseline)
						if err != nil || !reflect.DeepEqual(got, want) {
							t.Fatalf("got %#v, %v; want %#v", got, err, want)
						}
					})
				}
			}
		}
	}
	for _, epoch := range []string{"0", "1", "4294967295"} {
		for _, oldEpoch := range []string{"0", "2", "4294967295"} {
			t.Run("epochs-"+epoch+"/"+oldEpoch, func(t *testing.T) {
				candidate, baseline := updateDependencyPlanMetadata()
				candidate.Epoch, baseline.Epoch = epoch, oldEpoch
				candidate.Version, baseline.Version = "1.0^git1", "1.0~rc1"
				candidate.Release, baseline.Release = "2.fc43", "1.fc43"
				data := updateDependencyPlanMutation(t, func(_ map[string]any, r []any) {
					for i, m := range []codexRPMMetadata{candidate, baseline} {
						nevra, err := codexRPMRemovalTarget(m)
						if err != nil {
							t.Fatal(err)
						}
						r[i].(map[string]any)["nevra"] = nevra
					}
					r[2].(map[string]any)["nevra"] = "lib-test++-4294967295:1.0~rc1-3.fc43.x86_64"
				})
				got, err := validateCodexRPMUpdateDependencyPlan(data, candidate, baseline)
				if err != nil || len(got) != 2 || got[0].Package.Epoch != "4294967295" {
					t.Fatalf("epoch/version update: %#v, %v", got, err)
				}
			})
		}
	}
}

func TestCodexRPMUpdateDependencyPlanLimits(t *testing.T) {
	candidate, baseline := updateDependencyPlanMetadata()
	for _, count := range []int{0, 1, 64, 65} {
		t.Run(fmt.Sprintf("dependencies-%d", count), func(t *testing.T) {
			data := updateDependencyPlanMutation(t, func(p map[string]any, r []any) {
				items := r[:2]
				for i := 0; i < count; i++ {
					items = append(items, map[string]any{
						"nevra": fmt.Sprintf("dep-%d-1-1.noarch", i), "action": "Install", "reason": "Dependency",
						"repo_id": "@stored_transaction(fedora)", "package_path": fmt.Sprintf("./packages/dep-%d.rpm", i),
					})
				}
				p["rpms"] = items
			})
			got, err := validateCodexRPMUpdateDependencyPlan(data, candidate, baseline)
			if count == 65 {
				if err == nil || got != nil {
					t.Fatalf("65 dependencies accepted: %#v, %v", got, err)
				}
			} else if err != nil || len(got) != count {
				t.Fatalf("got %d dependencies, %v", len(got), err)
			}
		})
	}
	data := codexRPMUpdateDependencyPlanFixture + strings.Repeat(" ", (64<<10)-len(codexRPMUpdateDependencyPlanFixture))
	if _, err := validateCodexRPMUpdateDependencyPlan([]byte(data), candidate, baseline); err != nil {
		t.Fatalf("64 KiB boundary: %v", err)
	}
	if got, err := validateCodexRPMUpdateDependencyPlan([]byte(data+" "), candidate, baseline); err == nil || got != nil {
		t.Fatal("oversized plan accepted")
	}
}

func TestCodexRPMUpdateDependencyPlanRawRefusals(t *testing.T) {
	candidate, baseline := updateDependencyPlanMetadata()
	check := func(t *testing.T, data []byte) {
		t.Helper()
		if got, err := validateCodexRPMUpdateDependencyPlan(data, candidate, baseline); err == nil || got != nil {
			t.Fatalf("unsafe update plan returned %#v, %v", got, err)
		}
	}
	for name, data := range map[string]string{
		"empty": "", "null": "null", "array": "[]", "object": "{}", "boolean": "true",
		"truncated": codexRPMUpdateDependencyPlanFixture[:100], "trailing": codexRPMUpdateDependencyPlanFixture + `{}`,
		"garbage": codexRPMUpdateDependencyPlanFixture + "!", "bom": "\xef\xbb\xbf" + codexRPMUpdateDependencyPlanFixture,
		"deep":              strings.Repeat("[", 34) + "0" + strings.Repeat("]", 34),
		"duplicate-version": strings.Replace(codexRPMUpdateDependencyPlanFixture, `"version":`, `"version":"1.0","version":`, 1),
		"duplicate-rpms":    strings.Replace(codexRPMUpdateDependencyPlanFixture, `"rpms":`, `"rpms":[],"rpms":`, 1),
	} {
		t.Run(name, func(t *testing.T) { check(t, []byte(data)) })
	}
	mutate := func(name string, change func(map[string]any, []any)) {
		t.Helper()
		t.Run(name, func(t *testing.T) { check(t, updateDependencyPlanMutation(t, change)) })
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
	for index := range 4 {
		for _, value := range []any{nil, "", 1, false, []any{}, map[string]any{}} {
			mutate(fmt.Sprintf("entry-%d-%T", index, value), func(_ map[string]any, r []any) { r[index] = value })
		}
		for _, key := range []string{"nevra", "action", "reason", "repo_id", "package_path"} {
			if index != 1 || key != "package_path" {
				mutate(fmt.Sprintf("missing-%d-%s", index, key), func(_ map[string]any, r []any) { delete(r[index].(map[string]any), key) })
			}
			mutate(fmt.Sprintf("alias-%d-%s", index, key), func(_ map[string]any, r []any) {
				entry := r[index].(map[string]any)
				value := entry[key]
				if index == 1 && key == "package_path" {
					value = ""
				}
				entry[strings.ToUpper(key)] = value
				delete(entry, key)
			})
			mutate(fmt.Sprintf("case-duplicate-%d-%s", index, key), func(_ map[string]any, r []any) {
				entry := r[index].(map[string]any)
				if index == 1 && key == "package_path" {
					entry[key] = ""
				}
				entry[strings.ToUpper(key)] = entry[key]
			})
			for _, value := range []any{nil, 1, false, []any{}, map[string]any{}} {
				mutate(fmt.Sprintf("field-%d-%s-%T", index, key, value), func(_ map[string]any, r []any) { r[index].(map[string]any)[key] = value })
			}
			t.Run(fmt.Sprintf("duplicate-%d-%s", index, key), func(t *testing.T) {
				// Inject duplicates into the original entry, including optional outgoing paths.
				data := updateDependencyPlanMutation(t, func(_ map[string]any, r []any) { r[index].(map[string]any)[key] = "duplicate-marker" })
				needle := fmt.Sprintf(`%q:"duplicate-marker"`, key)
				value := ""
				var original map[string]any
				if err := json.Unmarshal([]byte(codexRPMUpdateDependencyPlanFixture), &original); err != nil {
					t.Fatal(err)
				}
				if v, ok := original["rpms"].([]any)[index].(map[string]any)[key].(string); ok {
					value = v
				}
				check(t, []byte(strings.Replace(string(data), needle, fmt.Sprintf(`%q:%q,%q:%q`, key, value, key, value), 1)))
				// Escaped keys must still count as duplicates before reencoding.
				escapedKey := fmt.Sprintf(`"\u%04x%s"`, key[0], key[1:])
				check(t, []byte(strings.Replace(string(data), needle, fmt.Sprintf(`%s:%q,%q:%q`, escapedKey, value, key, value), 1)))
			})
		}
		for _, key := range []string{"group_id", "unknown"} {
			mutate(fmt.Sprintf("unknown-entry-%d-%s", index, key), func(_ map[string]any, r []any) { r[index].(map[string]any)[key] = "" })
		}
	}
}

func TestCodexRPMUpdateDependencyPlanRefusals(t *testing.T) {
	candidate, baseline := updateDependencyPlanMetadata()
	mutate := func(name string, change func(map[string]any, []any)) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			got, err := validateCodexRPMUpdateDependencyPlan(updateDependencyPlanMutation(t, change), candidate, baseline)
			if err == nil || got != nil {
				t.Fatalf("unsafe update plan returned %#v, %v", got, err)
			}
		})
	}
	mutate("missing-candidate", func(p map[string]any, r []any) { p["rpms"] = r[1:] })
	mutate("missing-baseline", func(p map[string]any, r []any) { p["rpms"] = append(r[:1], r[2:]...) })
	mutate("dependencies-only", func(p map[string]any, r []any) { p["rpms"] = r[2:] })
	mutate("only-candidate", func(p map[string]any, r []any) { p["rpms"] = r[:1] })
	mutate("empty-rpms", func(p map[string]any, _ []any) { p["rpms"] = []any{} })
	for index := range 4 {
		mutate(fmt.Sprintf("duplicate-entry-%d", index), func(p map[string]any, r []any) { p["rpms"] = append(r, r[index]) })
		for _, action := range []string{"Upgrade", "Replaced", "Install", "Remove", "Downgrade", "Reinstall", "Reason Change", "install", "Install "} {
			if action == []string{"Upgrade", "Replaced", "Install", "Install"}[index] {
				continue
			}
			mutate(fmt.Sprintf("action-%d-%s", index, action), func(_ map[string]any, r []any) { r[index].(map[string]any)["action"] = action })
		}
	}
	mutate("swapped-identities", func(_ map[string]any, r []any) {
		a, b := r[0].(map[string]any), r[1].(map[string]any)
		a["nevra"], b["nevra"] = b["nevra"], a["nevra"]
	})
	for _, index := range []int{0, 1} {
		for key, values := range map[string][]string{
			"nevra":        {"", "other-1.0-1.x86_64", "chatgpt-2.0-1.x86_64", "chatgpt-0:1.0-1.x86_64", "chatgpt-1.0-1.noarch", "chatgpt-1.0-1.aarch64"},
			"reason":       {"", "user", "Unknown", "Dependency "},
			"repo_id":      {"", "updates", "@stored_transaction(updates)", "@stored_transaction(@System)", "@system"},
			"package_path": {"./packages/other.rpm", "packages/chatgpt.rpm", "./packages/../chatgpt.rpm"},
		} {
			for _, value := range values {
				mutate(fmt.Sprintf("target-%d-%s=%s", index, key, value), func(_ map[string]any, r []any) { r[index].(map[string]any)[key] = value })
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
	mutate("same-name-different-version", func(_ map[string]any, r []any) { r[3].(map[string]any)["nevra"] = "lib-test++-9-9.noarch" })
	mutate("duplicate-path", func(_ map[string]any, r []any) { r[3].(map[string]any)["package_path"] = "./packages/payload.rpm" })
	for _, index := range []int{2, 3} {
		for key, values := range map[string][]string{
			"nevra":        {"chatgpt-1.0-1.x86_64", "chatgpt-0.9-1.x86_64", "chatgpt-2-1.x86_64", "lib-0:1-1.x86_64", "lib-01:1-1.x86_64", "lib-4294967296:1-1.x86_64", "lib--1.x86_64", "lib-1-.x86_64", "lib-1-1.aarch64", "lib-1-1.i686", "lib-1-1.src", "lib-1-1.X86_64", "../lib-1-1.x86_64", "lib*-1-1.x86_64", "--lib-1-1.x86_64", "lib-1-1.x86_64\n", "lib-1-1.x86_64\x00", strings.Repeat("n", 257) + "-1-1.noarch", "lib-" + strings.Repeat("v", 257) + "-1.noarch", "lib-1-" + strings.Repeat("r", 257) + ".noarch", strings.Repeat("x", 1025)},
			"reason":       {"", "User", "Weak Dependency", "Group", "None", "Clean", "External User", "dependency", "Dependency "},
			"repo_id":      {"", "fedora", "updates", "copr:example.repo", "@System", "@commandline", "@stored_transaction(@System)", "@stored_transaction(@commandline)", "@stored_transaction(@stored_transaction(updates))", "@stored_transaction(updates", "@stored_transaction()", "@stored_transaction(updates))", "@Stored_transaction(updates)", "@stored_transaction(updates)extra", "@stored_transaction(updates) ", " @stored_transaction(updates)", "@stored_transaction(updates\n)", "@stored_transaction(updates\x00)", "@stored_transaction(../updates)", "@stored_transaction(https://repo.invalid)", "@stored_transaction(update*)", "@stored_transaction(" + strings.Repeat("r", 257) + ")"},
			"package_path": {"", "./packages/chatgpt.rpm", "packages/dep.rpm", "/packages/dep.rpm", "../packages/dep.rpm", "./packages/../dep.rpm", "./packages/sub/dep.rpm", "./packages//dep.rpm", "./packages/.hidden.rpm", "./packages/-option.rpm", "./packages/dep\\evil.rpm", "./packages/dep.rpm\n", "./packages/dep\x00.rpm", "./packages/dep*.rpm", "./packages/dep?.rpm", "./packages/dep.rpm/", "./packages/dep.RPM", "./packages/dep.rpm?x", "https://repo.invalid/dep.rpm", "./packages/" + strings.Repeat("f", 252) + ".rpm"},
		} {
			for _, value := range values {
				mutate(fmt.Sprintf("dependency-%d-%s=%s", index, key, value), func(_ map[string]any, r []any) { r[index].(map[string]any)[key] = value })
			}
		}
	}
}

func TestCodexRPMUpdateDependencyPlanMetadataRefusals(t *testing.T) {
	for _, target := range []string{"candidate", "baseline", "both"} {
		for name, change := range map[string]func(*codexRPMMetadata){
			"name":              func(m *codexRPMMetadata) { m.Name = "other" },
			"arch":              func(m *codexRPMMetadata) { m.Arch = "aarch64" },
			"unsupported-arch":  func(m *codexRPMMetadata) { m.Arch = "noarch" },
			"epoch":             func(m *codexRPMMetadata) { m.Epoch = "1" },
			"empty-epoch":       func(m *codexRPMMetadata) { m.Epoch = "" },
			"bad-epoch":         func(m *codexRPMMetadata) { m.Epoch = "01" },
			"overflow-epoch":    func(m *codexRPMMetadata) { m.Epoch = "4294967296" },
			"version":           func(m *codexRPMMetadata) { m.Version = "9" },
			"empty-version":     func(m *codexRPMMetadata) { m.Version = "" },
			"invalid-version":   func(m *codexRPMMetadata) { m.Version = "1\t2" },
			"release":           func(m *codexRPMMetadata) { m.Release = "2" },
			"oversized-release": func(m *codexRPMMetadata) { m.Release = strings.Repeat("a", 257) },
		} {
			t.Run(target+"/"+name, func(t *testing.T) {
				candidate, baseline := updateDependencyPlanMetadata()
				if target != "baseline" {
					change(&candidate)
				}
				if target != "candidate" {
					change(&baseline)
				}
				if got, err := validateCodexRPMUpdateDependencyPlan([]byte(codexRPMUpdateDependencyPlanFixture), candidate, baseline); err == nil || got != nil {
					t.Fatalf("invalid metadata accepted: %#v, %v", got, err)
				}
			})
		}
	}
	candidate, _ := updateDependencyPlanMetadata()
	data := strings.ReplaceAll(codexRPMUpdateDependencyPlanFixture, "chatgpt-0.9", "chatgpt-1.0")
	if got, err := validateCodexRPMUpdateDependencyPlan([]byte(data), candidate, candidate); err == nil || got != nil {
		t.Fatal("identical-identity update accepted")
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
