//go:build linux

package main

import (
	"encoding/json"
	"fmt"
)

// Match DNF5's NAME-[EPOCH:]VERSION-RELEASE.ARCH syntax and get_nevra's
// zero-epoch omission. Validate before constructing a package specification.
func codexRPMRemovalTarget(baseline codexRPMMetadata) (string, error) {
	var goarch string
	switch baseline.Arch {
	case "x86_64":
		goarch = "amd64"
	case "aarch64":
		goarch = "arm64"
	default:
		return "", fmt.Errorf("unsupported ChatGPT RPM removal architecture")
	}
	record := fmt.Sprintf("%s\t%s\t%s\t%s\t%s\n", baseline.Name, baseline.Epoch, baseline.Version, baseline.Release, baseline.Arch)
	if _, err := parseCodexRPMMetadata(record, goarch); err != nil {
		return "", err
	}
	version := baseline.Version
	if baseline.Epoch != "0" {
		version = baseline.Epoch + ":" + version
	}
	return baseline.Name + "-" + version + "-" + baseline.Release + "." + baseline.Arch, nil
}

// DNF5 base/transaction.cpp::serialize leaves @System unwrapped and assigns
// package_path only to inbound actions. transaction_sr.cpp omits empty paths.
// The caller must use --no-autoremove and pin/recheck the original baseline and
// stored plan; this validator authorizes no other package or dependency action.
func validateCodexRPMRemovePlan(data []byte, baseline codexRPMMetadata) error {
	if len(data) == 0 || len(data) > 64<<10 {
		return fmt.Errorf("invalid stored transaction size")
	}
	target, err := codexRPMRemovalTarget(baseline)
	if err != nil {
		return err
	}
	var p codexRPMPlan
	if err := decode(data, &p, true); err != nil {
		return err
	}
	if p.Version != "1.0" || len(p.RPMs) != 1 {
		return fmt.Errorf("DNF5 removal plan requires exactly one removed RPM")
	}
	// decode rejects duplicate/unknown keys. Check canonical names and string
	// types too: encoding/json otherwise accepts case aliases and null strings.
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	for key := range object {
		if key != "version" && key != "rpms" {
			return fmt.Errorf("noncanonical DNF5 removal plan field")
		}
	}
	items, ok := object["rpms"].([]any)
	if !ok {
		return fmt.Errorf("DNF5 removal RPM array required")
	}
	for _, item := range items {
		fields, ok := item.(map[string]any)
		if !ok {
			return fmt.Errorf("DNF5 removal RPM object required")
		}
		for key, value := range fields {
			switch key {
			case "nevra", "action", "reason", "repo_id", "package_path":
			default:
				return fmt.Errorf("noncanonical DNF5 removal RPM field")
			}
			if _, ok := value.(string); !ok {
				return fmt.Errorf("DNF5 removal RPM fields must be strings")
			}
		}
	}
	r := p.RPMs[0]
	// These spellings come from upstream transaction_item_reason.cpp. A reason
	// of Dependency describes this package; it never permits another removal.
	switch r.Reason {
	case "None", "Dependency", "User", "Clean", "Weak Dependency", "Group", "External User":
	default:
		return fmt.Errorf("unknown DNF5 removal RPM reason")
	}
	if r.Action != "Remove" || r.NEVRA != target || r.Repo != "@System" || r.Path != "" {
		return fmt.Errorf("DNF5 removal differs from the original installed ChatGPT RPM")
	}
	return nil
}
