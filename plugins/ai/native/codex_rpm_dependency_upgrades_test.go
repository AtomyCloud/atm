//go:build linux

package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

const codexRPMDependencyUpgradeFixture = `{"version":"1.0","rpms":[
{"nevra":"chatgpt-1.0-1.x86_64","action":"Install","reason":"User","repo_id":"@commandline","package_path":"./packages/chatgpt.rpm"},
{"nevra":"new-data-1-1.noarch","action":"Install","reason":"Dependency","repo_id":"@stored_transaction(fedora)","package_path":"./packages/data.rpm"},
{"nevra":"lib-test++-2:1.0^git1-3.fc43.x86_64","action":"Upgrade","reason":"User","repo_id":"@stored_transaction(updates)","package_path":"./packages/payload.rpm"},
{"nevra":"lib-test++-1:1.0~rc1-2.fc43.x86_64","action":"Replaced","reason":"User","repo_id":"@System"},
{"nevra":"shared-old-2-1.noarch","action":"Upgrade","reason":"Group","repo_id":"@stored_transaction(copr:example.repo)","package_path":"./packages/shared.rpm"},
{"nevra":"shared-old-1-1.noarch","action":"Replaced","reason":"Group","repo_id":"@System","package_path":""}
]}`

func dependencyUpgradeFixture(t *testing.T, updating bool) (map[string]any, codexRPMMetadata, codexRPMMetadata) {
	t.Helper()
	var p map[string]any
	if err := json.Unmarshal([]byte(codexRPMDependencyUpgradeFixture), &p); err != nil {
		t.Fatal(err)
	}
	candidate := codexRPMMetadata{"chatgpt", "0", "1.0", "1", "x86_64"}
	baseline := codexRPMMetadata{"chatgpt", "0", "0.9", "1", "x86_64"}
	if updating {
		r := p["rpms"].([]any)
		r[0].(map[string]any)["action"] = "Upgrade"
		p["rpms"] = append(r, map[string]any{
			"nevra": "chatgpt-0.9-1.x86_64", "action": "Replaced", "reason": "User", "repo_id": "@System",
		})
	}
	return p, candidate, baseline
}

func dependencyUpgradeJSON(t *testing.T, p any) []byte {
	t.Helper()
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestCodexRPMDependencyUpgradePlanAccepted(t *testing.T) {
	for _, updating := range []bool{false, true} {
		for _, arch := range []string{"x86_64", "aarch64"} {
			for _, repo := range []string{"@commandline", "@stored_transaction(@commandline)"} {
				t.Run(fmt.Sprintf("update-%t/%s/%s", updating, arch, repo), func(t *testing.T) {
					p, candidate, baseline := dependencyUpgradeFixture(t, updating)
					candidate.Arch, baseline.Arch = arch, arch
					p["rpms"].([]any)[0].(map[string]any)["repo_id"] = repo
					data := []byte(strings.ReplaceAll(string(dependencyUpgradeJSON(t, p)), "x86_64", arch))
					original := string(data)
					got, err := validateCodexRPMDependencyUpgradePlan(data, candidate, baseline, updating)
					want := []codexRPMDependencyChange{
						{codexRPMDependency{codexRPMMetadata{"new-data", "0", "1", "1", "noarch"}, "./packages/data.rpm", "@stored_transaction(fedora)"}, codexRPMMetadata{}},
						{codexRPMDependency{codexRPMMetadata{"lib-test++", "2", "1.0^git1", "3.fc43", arch}, "./packages/payload.rpm", "@stored_transaction(updates)"}, codexRPMMetadata{"lib-test++", "1", "1.0~rc1", "2.fc43", arch}},
						{codexRPMDependency{codexRPMMetadata{"shared-old", "0", "2", "1", "noarch"}, "./packages/shared.rpm", "@stored_transaction(copr:example.repo)"}, codexRPMMetadata{"shared-old", "0", "1", "1", "noarch"}},
					}
					if err != nil || !reflect.DeepEqual(got, want) {
						t.Fatalf("got %#v, %v; want %#v", got, err, want)
					}
					if string(data) != original {
						t.Fatal("original plan bytes changed")
					}
				})
			}
		}
	}
}

func TestCodexRPMDependencyUpgradePlanOrderAndReasons(t *testing.T) {
	reasons := []string{"None", "Dependency", "User", "Clean", "Weak Dependency", "Group", "External User"}
	for _, updating := range []bool{false, true} {
		for _, incomingReason := range reasons {
			for _, outgoingReason := range reasons {
				t.Run(fmt.Sprintf("update-%t/%s/%s", updating, incomingReason, outgoingReason), func(t *testing.T) {
					p, candidate, baseline := dependencyUpgradeFixture(t, updating)
					r := p["rpms"].([]any)
					for _, index := range []int{2, 4} {
						r[index].(map[string]any)["reason"] = incomingReason
						r[index+1].(map[string]any)["reason"] = outgoingReason
					}
					if updating {
						r[0].(map[string]any)["reason"] = incomingReason
						r[6].(map[string]any)["reason"] = outgoingReason
					}
					// Rotate both forward and reversed orders so every record can
					// precede the target and replacements can precede their upgrades.
					for reverse := range 2 {
						if reverse == 1 {
							for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
								r[i], r[j] = r[j], r[i]
							}
						}
						for shift := range len(r) {
							p["rpms"] = append(append([]any{}, r[shift:]...), r[:shift]...)
							got, err := validateCodexRPMDependencyUpgradePlan(dependencyUpgradeJSON(t, p), candidate, baseline, updating)
							if err != nil || len(got) != 3 {
								t.Fatalf("reverse %d shift %d: %#v, %v", reverse, shift, got, err)
							}
							for _, change := range got {
								if change.Package.Name == "new-data" {
									if change.Baseline != (codexRPMMetadata{}) {
										t.Fatal("new dependency acquired a baseline")
									}
								} else if change.Baseline.Name != change.Package.Name || change.Baseline.Arch != change.Package.Arch || change.Baseline == change.Package {
									t.Fatalf("wrong baseline: %#v", change)
								}
							}
						}
					}
				})
			}
		}
	}
}

func TestCodexRPMDependencyUpgradePlanEpochs(t *testing.T) {
	for _, updating := range []bool{false, true} {
		for _, epoch := range []string{"0", "1", "4294967295"} {
			for _, oldEpoch := range []string{"0", "2", "4294967295"} {
				t.Run(fmt.Sprintf("update-%t/%s/%s", updating, epoch, oldEpoch), func(t *testing.T) {
					p, candidate, baseline := dependencyUpgradeFixture(t, updating)
					candidate.Epoch, baseline.Epoch = epoch, oldEpoch
					r := p["rpms"].([]any)
					nevra, err := codexRPMRemovalTarget(candidate)
					if err != nil {
						t.Fatal(err)
					}
					r[0].(map[string]any)["nevra"] = nevra
					if updating {
						nevra, err = codexRPMRemovalTarget(baseline)
						if err != nil {
							t.Fatal(err)
						}
						r[6].(map[string]any)["nevra"] = nevra
					}
					for i, e := range []string{epoch, oldEpoch} {
						prefix := e + ":"
						if e == "0" {
							prefix = ""
						}
						r[i+2].(map[string]any)["nevra"] = fmt.Sprintf("lib-test++-%s%d-1.x86_64", prefix, 2-i)
					}
					got, err := validateCodexRPMDependencyUpgradePlan(dependencyUpgradeJSON(t, p), candidate, baseline, updating)
					if err != nil || len(got) != 3 || got[1].Package.Epoch != epoch || got[1].Baseline.Epoch != oldEpoch {
						t.Fatalf("epochs: %#v, %v", got, err)
					}
				})
			}
		}
	}
}

func TestCodexRPMDependencyUpgradePlanBounds(t *testing.T) {
	for _, updating := range []bool{false, true} {
		for _, count := range []int{0, 1, 64, 65} {
			for _, mode := range []string{"installs", "upgrades", "mixed"} {
				t.Run(fmt.Sprintf("update-%t/%s/%d", updating, mode, count), func(t *testing.T) {
					p, candidate, baseline := dependencyUpgradeFixture(t, updating)
					r := p["rpms"].([]any)
					items := []any{r[0]}
					if updating {
						items = append(items, r[6])
					}
					for i := range count {
						action := "Install"
						if mode == "upgrades" || (mode == "mixed" && i%2 == 0) {
							action = "Upgrade"
							items = append(items, map[string]any{
								"nevra": fmt.Sprintf("dep-%d-1-1.noarch", i), "action": "Replaced", "reason": "Dependency", "repo_id": "@System",
							})
						}
						items = append(items, map[string]any{
							"nevra": fmt.Sprintf("dep-%d-2-1.noarch", i), "action": action, "reason": "Dependency",
							"repo_id": "@stored_transaction(updates)", "package_path": fmt.Sprintf("./packages/dep-%d.rpm", i),
						})
					}
					p["rpms"] = items
					got, err := validateCodexRPMDependencyUpgradePlan(dependencyUpgradeJSON(t, p), candidate, baseline, updating)
					if count > 64 {
						if err == nil || got != nil {
							t.Fatalf("accepted 65 dependency names: %#v, %v", got, err)
						}
					} else if err != nil || len(got) != count {
						t.Fatalf("got %d dependencies: %v", len(got), err)
					}
				})
			}
		}
		p, candidate, baseline := dependencyUpgradeFixture(t, updating)
		data := dependencyUpgradeJSON(t, p)
		data = append(data, []byte(strings.Repeat(" ", (64<<10)-len(data)))...)
		if _, err := validateCodexRPMDependencyUpgradePlan(data, candidate, baseline, updating); err != nil {
			t.Fatalf("64 KiB boundary update=%t: %v", updating, err)
		}
		if got, err := validateCodexRPMDependencyUpgradePlan(append(data, ' '), candidate, baseline, updating); err == nil || got != nil {
			t.Fatal("accepted more than 64 KiB")
		}
	}
}

func TestCodexRPMDependencyUpgradePlanRawRefusals(t *testing.T) {
	for _, updating := range []bool{false, true} {
		t.Run(fmt.Sprintf("update-%t", updating), func(t *testing.T) {
			p, candidate, baseline := dependencyUpgradeFixture(t, updating)
			original := string(dependencyUpgradeJSON(t, p))
			check := func(t *testing.T, data []byte) {
				t.Helper()
				if got, err := validateCodexRPMDependencyUpgradePlan(data, candidate, baseline, updating); err == nil || got != nil {
					t.Fatalf("unsafe raw plan: %#v, %v", got, err)
				}
			}
			for name, data := range map[string]string{
				"empty": "", "null": "null", "array": "[]", "object": "{}", "bool": "true",
				"truncated": original[:100], "trailing": original + `{}`, "garbage": original + "!", "bom": "\xef\xbb\xbf" + original,
				"deep":              strings.Repeat("[", 34) + "0" + strings.Repeat("]", 34),
				"duplicate-version": strings.Replace(original, `"version":`, `"version":"1.0","version":`, 1),
				"duplicate-rpms":    strings.Replace(original, `"rpms":`, `"rpms":[],"rpms":`, 1),
			} {
				t.Run(name, func(t *testing.T) { check(t, []byte(data)) })
			}
			mutate := func(name string, change func(map[string]any, []any)) {
				t.Helper()
				t.Run(name, func(t *testing.T) {
					p, _, _ := dependencyUpgradeFixture(t, updating)
					change(p, p["rpms"].([]any))
					check(t, dependencyUpgradeJSON(t, p))
				})
			}
			for _, key := range []string{"version", "rpms"} {
				mutate("missing-"+key, func(p map[string]any, _ []any) { delete(p, key) })
				mutate("alias-"+key, func(p map[string]any, _ []any) { p[strings.ToUpper(key)] = p[key]; delete(p, key) })
				mutate("case-duplicate-"+key, func(p map[string]any, _ []any) { p[strings.ToUpper(key)] = p[key] })
				for _, value := range []any{nil, 1, true, "", "1.1", []any{}, map[string]any{}} {
					mutate(fmt.Sprintf("%s-type-%T-%v", key, value, value), func(p map[string]any, _ []any) { p[key] = value })
				}
			}
			for _, key := range []string{"groups", "environments", "modules", "packages", "unknown"} {
				mutate("unknown-"+key, func(p map[string]any, _ []any) { p[key] = []any{} })
			}
			for index := range len(p["rpms"].([]any)) {
				for _, value := range []any{nil, "", 1, false, []any{}, map[string]any{}} {
					mutate(fmt.Sprintf("entry-%d-%T", index, value), func(_ map[string]any, r []any) { r[index] = value })
				}
				for _, key := range []string{"nevra", "action", "reason", "repo_id", "package_path"} {
					if key != "package_path" || (index != 3 && index != 5 && index != 6) {
						mutate(fmt.Sprintf("missing-%d-%s", index, key), func(_ map[string]any, r []any) { delete(r[index].(map[string]any), key) })
					}
					mutate(fmt.Sprintf("alias-%d-%s", index, key), func(_ map[string]any, r []any) {
						fields := r[index].(map[string]any)
						value, _ := fields[key].(string)
						fields[strings.ToUpper(key)] = value
						delete(fields, key)
					})
					mutate(fmt.Sprintf("case-duplicate-%d-%s", index, key), func(_ map[string]any, r []any) {
						fields := r[index].(map[string]any)
						value, _ := fields[key].(string)
						fields[key], fields[strings.ToUpper(key)] = value, value
					})
					for _, value := range []any{nil, 1, false, []any{}, map[string]any{}} {
						mutate(fmt.Sprintf("field-%d-%s-%T", index, key, value), func(_ map[string]any, r []any) { r[index].(map[string]any)[key] = value })
					}
					t.Run(fmt.Sprintf("duplicate-%d-%s", index, key), func(t *testing.T) {
						p, _, _ := dependencyUpgradeFixture(t, updating)
						fields := p["rpms"].([]any)[index].(map[string]any)
						value, _ := fields[key].(string)
						fields[key] = "duplicate-marker"
						data := string(dependencyUpgradeJSON(t, p))
						needle := fmt.Sprintf(`%q:"duplicate-marker"`, key)
						for _, duplicateKey := range []string{fmt.Sprintf("%q", key), fmt.Sprintf(`"\u%04x%s"`, key[0], key[1:])} {
							check(t, []byte(strings.Replace(data, needle, fmt.Sprintf(`%s:%q,%q:%q`, duplicateKey, value, key, value), 1)))
						}
					})
				}
				mutate(fmt.Sprintf("unknown-entry-%d", index), func(_ map[string]any, r []any) { r[index].(map[string]any)["group_id"] = "" })
			}
		})
	}
}

func TestCodexRPMDependencyUpgradePlanRefusals(t *testing.T) {
	for _, updating := range []bool{false, true} {
		t.Run(fmt.Sprintf("update-%t", updating), func(t *testing.T) {
			mutate := func(name string, change func(map[string]any, []any)) {
				t.Helper()
				t.Run(name, func(t *testing.T) {
					p, candidate, baseline := dependencyUpgradeFixture(t, updating)
					change(p, p["rpms"].([]any))
					if got, err := validateCodexRPMDependencyUpgradePlan(dependencyUpgradeJSON(t, p), candidate, baseline, updating); err == nil || got != nil {
						t.Fatalf("unsafe plan: %#v, %v", got, err)
					}
				})
			}
			mutate("no-target", func(p map[string]any, r []any) { p["rpms"] = r[1:] })
			mutate("empty", func(p map[string]any, _ []any) { p["rpms"] = []any{} })
			mutate("same-identity", func(_ map[string]any, r []any) { r[3].(map[string]any)["nevra"] = r[2].(map[string]any)["nevra"] })
			mutate("cross-name", func(_ map[string]any, r []any) { r[3].(map[string]any)["nevra"] = "other-1-1.x86_64" })
			mutate("cross-arch", func(_ map[string]any, r []any) { r[3].(map[string]any)["nevra"] = "lib-test++-1-1.noarch" })
			mutate("cross-arch-incoming", func(_ map[string]any, r []any) { r[2].(map[string]any)["nevra"] = "lib-test++-3-1.noarch" })
			mutate("duplicate-name", func(_ map[string]any, r []any) { r[1].(map[string]any)["nevra"] = "lib-test++-3-1.x86_64" })
			mutate("duplicate-path", func(_ map[string]any, r []any) {
				r[4].(map[string]any)["package_path"] = r[2].(map[string]any)["package_path"]
			})
			mutate("wrong-target-version", func(_ map[string]any, r []any) { r[0].(map[string]any)["nevra"] = "chatgpt-8-1.x86_64" })
			mutate("wrong-target-action", func(_ map[string]any, r []any) { r[0].(map[string]any)["action"] = "Replaced" })
			count := 6
			if updating {
				count = 7
				mutate("wrong-target-baseline", func(_ map[string]any, r []any) { r[6].(map[string]any)["nevra"] = "chatgpt-0.8-1.x86_64" })
			}
			for index := range count {
				mutate(fmt.Sprintf("duplicate-%d", index), func(p map[string]any, r []any) { p["rpms"] = append(r, r[index]) })
				if index != 1 {
					mutate(fmt.Sprintf("missing-pair-%d", index), func(p map[string]any, r []any) { p["rpms"] = append(r[:index], r[index+1:]...) })
				}
				for _, action := range []string{"Remove", "Downgrade", "Reinstall", "Obsolete", "Obsoleted", "Reason Change", "install", "Install ", ""} {
					mutate(fmt.Sprintf("action-%d-%s", index, action), func(_ map[string]any, r []any) { r[index].(map[string]any)["action"] = action })
				}
				for _, reason := range []string{"", "unknown", "user", "Dependency "} {
					mutate(fmt.Sprintf("reason-%d-%s", index, reason), func(_ map[string]any, r []any) { r[index].(map[string]any)["reason"] = reason })
				}
			}
			for _, reason := range []string{"User", "Group", "Weak Dependency", "None", "Clean", "External User"} {
				mutate("new-reason-"+reason, func(_ map[string]any, r []any) { r[1].(map[string]any)["reason"] = reason })
			}
			for _, index := range []int{2, 4} {
				mutate(fmt.Sprintf("paired-install-%d", index), func(_ map[string]any, r []any) {
					fields := r[index].(map[string]any)
					fields["action"], fields["reason"] = "Install", "Dependency"
				})
			}
			for _, index := range []int{1, 2, 3, 4, 5} {
				for _, nevra := range []string{
					"", "chatgpt-9-1.x86_64", "lib-0:1-1.x86_64", "lib-01:1-1.x86_64", "lib-4294967296:1-1.x86_64",
					"lib--1.x86_64", "lib-1-.x86_64", "lib-1-1.aarch64", "lib-1-1.i686", "lib-1-1.src", "lib-1-1.X86_64",
					"../lib-1-1.x86_64", "lib*-1-1.x86_64", "--lib-1-1.x86_64", "lib-1-1.x86_64\n", "lib-1-1.x86_64\x00",
					strings.Repeat("n", 257) + "-1-1.noarch", "lib-" + strings.Repeat("v", 257) + "-1.noarch",
					"lib-1-" + strings.Repeat("r", 257) + ".noarch", strings.Repeat("x", 1025),
				} {
					mutate(fmt.Sprintf("nevra-%d-%q", index, nevra), func(_ map[string]any, r []any) { r[index].(map[string]any)["nevra"] = nevra })
				}
			}
			for _, index := range []int{1, 2, 4} {
				for key, values := range map[string][]string{
					"repo_id": {"", "fedora", "updates", "copr:example.repo", "@System", "@commandline", "@stored_transaction(@commandline)",
						"@stored_transaction(@System)", "@stored_transaction(@stored_transaction(updates))", "@stored_transaction(updates", "@stored_transaction()",
						"@stored_transaction(updates))", "@Stored_transaction(updates)", "@stored_transaction(updates)extra", "@stored_transaction(updates) ",
						" @stored_transaction(updates)", "@stored_transaction(updates\n)", "@stored_transaction(updates\x00)", "@stored_transaction(../updates)",
						"@stored_transaction(https://repo.invalid)", "@stored_transaction(update*)", "@stored_transaction(" + strings.Repeat("r", 257) + ")"},
					"package_path": {"", "./packages/chatgpt.rpm", "packages/dep.rpm", "/packages/dep.rpm", "../packages/dep.rpm", "./packages/../dep.rpm",
						"./packages/sub/dep.rpm", "./packages//dep.rpm", "./packages/.hidden.rpm", "./packages/-option.rpm", "./packages/dep\\evil.rpm",
						"./packages/dep.rpm\n", "./packages/dep\x00.rpm", "./packages/dep*.rpm", "./packages/dep?.rpm", "./packages/dep.rpm/",
						"./packages/dep.RPM", "./packages/dep.rpm?x", "https://repo.invalid/dep.rpm", "./packages/" + strings.Repeat("f", 252) + ".rpm"},
				} {
					for _, value := range values {
						mutate(fmt.Sprintf("incoming-%d-%s-%q", index, key, value), func(_ map[string]any, r []any) { r[index].(map[string]any)[key] = value })
					}
				}
			}
			for _, index := range []int{3, 5} {
				for _, repo := range []string{"", "@system", "updates", "@stored_transaction(updates)", "@commandline", "@stored_transaction(@System)"} {
					mutate(fmt.Sprintf("outgoing-%d-%s", index, repo), func(_ map[string]any, r []any) { r[index].(map[string]any)["repo_id"] = repo })
				}
				mutate(fmt.Sprintf("outgoing-path-%d", index), func(_ map[string]any, r []any) { r[index].(map[string]any)["package_path"] = "./packages/old.rpm" })
			}
		})
	}
}

func TestCodexRPMDependencyUpgradePlanTargetMetadata(t *testing.T) {
	for _, updating := range []bool{false, true} {
		for _, target := range []string{"candidate", "baseline"} {
			if !updating && target == "baseline" {
				continue
			}
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
				t.Run(fmt.Sprintf("update-%t/%s/%s", updating, target, name), func(t *testing.T) {
					p, candidate, baseline := dependencyUpgradeFixture(t, updating)
					if target == "candidate" {
						change(&candidate)
					} else {
						change(&baseline)
					}
					if got, err := validateCodexRPMDependencyUpgradePlan(dependencyUpgradeJSON(t, p), candidate, baseline, updating); err == nil || got != nil {
						t.Fatalf("invalid target metadata accepted: %#v, %v", got, err)
					}
				})
			}
		}
	}
	p, candidate, _ := dependencyUpgradeFixture(t, true)
	r := p["rpms"].([]any)
	r[6].(map[string]any)["nevra"] = r[0].(map[string]any)["nevra"]
	if got, err := validateCodexRPMDependencyUpgradePlan(dependencyUpgradeJSON(t, p), candidate, candidate, true); err == nil || got != nil {
		t.Fatal("identical target update accepted")
	}
	p, candidate, _ = dependencyUpgradeFixture(t, false)
	if _, err := validateCodexRPMDependencyUpgradePlan(dependencyUpgradeJSON(t, p), candidate, codexRPMMetadata{}, false); err != nil {
		t.Fatalf("initial install with no target baseline: %v", err)
	}
}
