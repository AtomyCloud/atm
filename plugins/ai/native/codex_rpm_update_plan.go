//go:build linux

package main

import (
	"encoding/json"
	"fmt"
)

// DNF5 base/transaction.cpp emits Upgrade plus Replaced, preserving package
// reasons. Its serializer gives only inbound actions a package_path and never
// wraps @System. RPM ordering is supplied by the trusted solver's Upgrade action,
// not inferred from version strings here. The caller must pin the plan and RPMs.
func validateCodexRPMUpdatePlan(data []byte, candidate, baseline codexRPMMetadata) error {
	if len(data) == 0 || len(data) > 64<<10 {
		return fmt.Errorf("invalid stored transaction size")
	}
	if candidate.Arch != baseline.Arch || candidate == baseline {
		return fmt.Errorf("ChatGPT RPM update requires distinct identities with the same architecture")
	}
	nevras := make([]string, 0, 2)
	for _, m := range []codexRPMMetadata{candidate, baseline} {
		var goarch string
		switch m.Arch {
		case "x86_64":
			goarch = "amd64"
		case "aarch64":
			goarch = "arm64"
		default:
			return fmt.Errorf("unsupported ChatGPT RPM update architecture")
		}
		record := fmt.Sprintf("%s\t%s\t%s\t%s\t%s\n", m.Name, m.Epoch, m.Version, m.Release, m.Arch)
		if _, err := parseCodexRPMMetadata(record, goarch); err != nil {
			return err
		}
		version := m.Version
		if m.Epoch != "0" {
			version = m.Epoch + ":" + version
		}
		nevras = append(nevras, m.Name+"-"+version+"-"+m.Release+"."+m.Arch)
	}
	var p codexRPMPlan
	if err := decode(data, &p, true); err != nil {
		return err
	}
	if p.Version != "1.0" || len(p.RPMs) != 2 {
		return fmt.Errorf("DNF5 update plan requires exactly one upgrade and one replaced RPM")
	}
	// encoding/json matches struct fields case-insensitively and accepts null
	// strings. Check literal field names/types as well, without changing decode.
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	for key := range object {
		if key != "version" && key != "rpms" {
			return fmt.Errorf("noncanonical DNF5 update plan field")
		}
	}
	items, ok := object["rpms"].([]any)
	if !ok {
		return fmt.Errorf("DNF5 update RPM array required")
	}
	for _, item := range items {
		fields, ok := item.(map[string]any)
		if !ok {
			return fmt.Errorf("DNF5 update RPM object required")
		}
		for key, value := range fields {
			switch key {
			case "nevra", "action", "reason", "repo_id", "package_path":
			default:
				return fmt.Errorf("noncanonical DNF5 update RPM field")
			}
			if _, ok := value.(string); !ok {
				return fmt.Errorf("DNF5 update RPM fields must be strings")
			}
		}
	}
	upgrade, replaced := false, false
	for _, r := range p.RPMs {
		// These are the upstream transaction_item_reason.cpp enum spellings;
		// a dependency reason does not authorize any additional package action.
		switch r.Reason {
		case "None", "Dependency", "User", "Clean", "Weak Dependency", "Group", "External User":
		default:
			return fmt.Errorf("unknown DNF5 update RPM reason")
		}
		switch r.Action {
		case "Upgrade":
			localRepo := r.Repo == "@commandline" || r.Repo == "@stored_transaction(@commandline)"
			if upgrade || r.NEVRA != nevras[0] || !localRepo || r.Path != "./packages/chatgpt.rpm" {
				return fmt.Errorf("DNF5 upgrade differs from the exact candidate ChatGPT RPM")
			}
			upgrade = true
		case "Replaced":
			if replaced || r.NEVRA != nevras[1] || r.Repo != "@System" || r.Path != "" {
				return fmt.Errorf("DNF5 replacement differs from the original installed ChatGPT RPM")
			}
			replaced = true
		default:
			return fmt.Errorf("DNF5 update plan contains an unsupported RPM action")
		}
	}
	if !upgrade || !replaced {
		return fmt.Errorf("DNF5 update plan requires an upgrade and its original replaced RPM")
	}
	return nil
}
