//go:build linux

package main

import (
	"encoding/json"
	"fmt"
)

type codexRPMDependencyChange struct {
	codexRPMDependency
	Baseline codexRPMMetadata
}

// The caller obtains separate dependency-upgrade consent and pins/rechecks the
// original plan, incoming RPMs and installed baselines. Upgrade ordering and
// dependency selection come from the trusted DNF solver, not provenance or a
// version comparison inferred here. Installed User/Group reasons are permitted.
func validateCodexRPMDependencyUpgradePlan(data []byte, candidate, targetBaseline codexRPMMetadata, updating bool) ([]codexRPMDependencyChange, error) {
	if len(data) == 0 || len(data) > 64<<10 {
		return nil, fmt.Errorf("invalid stored transaction size")
	}
	var p codexRPMPlan
	if err := decode(data, &p, true); err != nil {
		return nil, err
	}
	if p.Version != "1.0" || len(p.RPMs) < 1 || len(p.RPMs) > 130 {
		return nil, fmt.Errorf("DNF5 dependency upgrade plan requires at most 130 RPM records")
	}
	// Validate original bytes before projection can erase aliases, nulls,
	// missing fields or duplicates (including escaped duplicate field names).
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		return nil, err
	}
	if len(object) != 2 || object["version"] != "1.0" {
		return nil, fmt.Errorf("noncanonical DNF5 dependency upgrade plan fields")
	}
	items, ok := object["rpms"].([]any)
	if !ok {
		return nil, fmt.Errorf("DNF5 dependency upgrade RPM array required")
	}
	for _, item := range items {
		fields, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("DNF5 dependency upgrade RPM object required")
		}
		for key, value := range fields {
			switch key {
			case "nevra", "action", "reason", "repo_id", "package_path":
			default:
				return nil, fmt.Errorf("noncanonical DNF5 dependency upgrade RPM field")
			}
			if _, ok := value.(string); !ok {
				return nil, fmt.Errorf("DNF5 dependency upgrade RPM fields must be strings")
			}
		}
		for _, key := range []string{"nevra", "action", "reason", "repo_id", "package_path"} {
			if _, ok := fields[key]; !ok && !(key == "package_path" && fields["action"] == "Replaced") {
				return nil, fmt.Errorf("DNF5 dependency upgrade RPM field missing")
			}
		}
	}
	targetNEVRA, err := codexRPMRemovalTarget(candidate)
	if err != nil {
		return nil, err
	}
	var baselineNEVRA string
	if updating {
		baselineNEVRA, err = codexRPMRemovalTarget(targetBaseline)
		if err != nil {
			return nil, err
		}
	}
	goarch := "amd64"
	if candidate.Arch == "aarch64" {
		goarch = "arm64"
	}
	incoming := map[string]codexRPMMetadata{}
	outgoing := map[string]codexRPMMetadata{}
	upgrades := map[string]bool{}
	projected := make([]any, 0, len(items))
	for i, r := range p.RPMs {
		if r.NEVRA == targetNEVRA || (updating && r.NEVRA == baselineNEVRA) {
			projected = append(projected, items[i])
			continue
		}
		m, err := parseCodexRPMDependencyUpgradeNEVRA(r.NEVRA, goarch)
		if err != nil {
			return nil, err
		}
		switch r.Action {
		case "Upgrade", "Replaced":
			// Same upstream enum spellings accepted by the target update validator.
			switch r.Reason {
			case "None", "Dependency", "User", "Clean", "Weak Dependency", "Group", "External User":
			default:
				return nil, fmt.Errorf("unknown DNF5 dependency upgrade reason")
			}
		case "Install":
			if r.Reason != "Dependency" {
				return nil, fmt.Errorf("new RPM dependencies require the Dependency reason")
			}
		default:
			return nil, fmt.Errorf("unsupported DNF5 dependency upgrade action")
		}
		if r.Action == "Replaced" {
			if _, exists := outgoing[m.Name]; exists || r.Repo != "@System" || r.Path != "" {
				return nil, fmt.Errorf("unique installed RPM dependency replacement without a path required")
			}
			outgoing[m.Name] = m
			continue
		}
		if _, exists := incoming[m.Name]; exists {
			return nil, fmt.Errorf("duplicate incoming RPM dependency name")
		}
		incoming[m.Name] = m
		upgrades[m.Name] = r.Action == "Upgrade"
		// Only validated upgrade actions/reasons are normalized. Keep all
		// identity, path and repo fields for the existing strict validators.
		fields := items[i].(map[string]any)
		fields["action"], fields["reason"] = "Install", "Dependency"
		projected = append(projected, fields)
	}
	if len(incoming) > 64 {
		return nil, fmt.Errorf("DNF5 dependency upgrade plan permits at most 64 dependency names")
	}
	for name, m := range incoming {
		old, exists := outgoing[name]
		if upgrades[name] != exists || (exists && (old.Arch != m.Arch || old == m)) {
			return nil, fmt.Errorf("RPM dependency upgrade requires a distinct same-name same-architecture replacement")
		}
	}
	for name := range outgoing {
		if !upgrades[name] {
			return nil, fmt.Errorf("unpaired outgoing RPM dependency")
		}
	}
	object["rpms"] = projected
	projectedData, err := json.Marshal(object)
	if err != nil {
		return nil, err
	}
	var deps []codexRPMDependency
	if updating {
		deps, err = validateCodexRPMUpdateDependencyPlan(projectedData, candidate, targetBaseline)
	} else {
		deps, err = validateCodexRPMDependencyPlan(projectedData, candidate)
	}
	if err != nil {
		return nil, err
	}
	changes := make([]codexRPMDependencyChange, 0, len(deps))
	for _, dep := range deps {
		changes = append(changes, codexRPMDependencyChange{codexRPMDependency: dep, Baseline: outgoing[dep.Package.Name]})
	}
	return changes, nil
}

func parseCodexRPMDependencyUpgradeNEVRA(nevra, goarch string) (codexRPMMetadata, error) {
	if len(nevra) > 1024 {
		return codexRPMMetadata{}, fmt.Errorf("bounded RPM dependency NEVRA required")
	}
	fields := codexRPMDependencyNEVRARE.FindStringSubmatch(nevra)
	if fields == nil {
		return codexRPMMetadata{}, fmt.Errorf("canonical RPM dependency NEVRA required")
	}
	epoch := fields[2]
	if epoch == "" {
		epoch = "0"
	}
	m := codexRPMMetadata{fields[1], epoch, fields[3], fields[4], fields[5]}
	if err := validateRPMDependencyPackage(m, goarch); err != nil {
		return codexRPMMetadata{}, err
	}
	return m, nil
}
