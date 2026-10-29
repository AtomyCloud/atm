//go:build linux

package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type codexRPMDependency struct {
	Package codexRPMMetadata
	Path    string
	Repo    string
}

var (
	codexRPMDependencyNameRE  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)
	codexRPMDependencyNEVRARE = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9._+-]*)-(?:([1-9][0-9]*):)?([A-Za-z0-9][A-Za-z0-9._+~^]*)-([A-Za-z0-9][A-Za-z0-9._+~^]*)\.(x86_64|aarch64|noarch)$`)
	codexRPMDependencyFileRE  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+~^-]*\.rpm$`)
	codexRPMDependencyRepoRE  = regexp.MustCompile(`^[A-Za-z0-9_.:-]+$`)
)

func validateRPMDependencyIdentity(m codexRPMMetadata) error {
	if m.Name == "chatgpt" || len(m.Name) > 256 || !codexRPMDependencyNameRE.MatchString(m.Name) {
		return fmt.Errorf("invalid or oversized RPM dependency name")
	}
	epoch, err := strconv.ParseUint(m.Epoch, 10, 32)
	if err != nil || strconv.FormatUint(epoch, 10) != m.Epoch {
		return fmt.Errorf("canonical nonnegative RPM dependency epoch required")
	}
	for _, value := range []string{m.Version, m.Release} {
		if len(value) > 256 || !codexRPMComponentRE.MatchString(value) {
			return fmt.Errorf("invalid or oversized RPM dependency version/release")
		}
	}
	if m.Arch != "x86_64" && m.Arch != "aarch64" && m.Arch != "noarch" {
		return fmt.Errorf("unsupported RPM dependency architecture")
	}
	return nil
}

// validateRPMDependencyPackage also validates dependency identities read from a
// journal. It authorizes neither a package action nor ownership of the package.
func validateRPMDependencyPackage(m codexRPMMetadata, goarch string) error {
	arch, err := codexRPMArch(goarch)
	if err != nil {
		return err
	}
	if m.Arch != arch && m.Arch != "noarch" {
		return fmt.Errorf("RPM dependency architecture differs from the target")
	}
	return validateRPMDependencyIdentity(m)
}

// The caller supplies diagnostic-free query output and separately establishes
// absence. Empty, multi-record and noncanonical output never proves identity.
func parseRPMDependencyMetadata(output string, want codexRPMMetadata) error {
	if len(output) > 1024 {
		return fmt.Errorf("one bounded RPM dependency metadata record required")
	}
	if err := validateRPMDependencyIdentity(want); err != nil {
		return err
	}
	record := fmt.Sprintf("%s\t%s\t%s\t%s\t%s\n", want.Name, want.Epoch, want.Version, want.Release, want.Arch)
	if output != record {
		return fmt.Errorf("RPM dependency metadata differs from the exact expected identity")
	}
	return nil
}

// DNF5 serializes get_nevra() (zero epoch omitted), and inbound package_path
// uses the location basename, not necessarily the NEVRA. This validates a plan,
// not the files: the caller must pin/recheck RPMs, absence and stored-plan bytes.
func validateCodexRPMDependencyPlan(data []byte, target codexRPMMetadata) ([]codexRPMDependency, error) {
	if len(data) == 0 || len(data) > 64<<10 {
		return nil, fmt.Errorf("invalid stored transaction size")
	}
	// This existing identity formatter validates the exact ChatGPT metadata.
	targetNEVRA, err := codexRPMRemovalTarget(target)
	if err != nil {
		return nil, err
	}
	goarch := "amd64"
	if target.Arch == "aarch64" {
		goarch = "arm64"
	}
	var p codexRPMPlan
	if err := decode(data, &p, true); err != nil {
		return nil, err
	}
	if p.Version != "1.0" || len(p.RPMs) < 1 || len(p.RPMs) > 65 {
		return nil, fmt.Errorf("DNF5 dependency plan requires ChatGPT and at most 64 dependencies")
	}
	// decode rejects duplicate/unknown keys; encoding/json still permits case
	// aliases and null strings. Require the exact schema and string types too.
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		return nil, err
	}
	if len(object) != 2 || object["version"] != "1.0" {
		return nil, fmt.Errorf("noncanonical DNF5 dependency plan fields")
	}
	items, ok := object["rpms"].([]any)
	if !ok {
		return nil, fmt.Errorf("DNF5 dependency RPM array required")
	}
	for _, item := range items {
		fields, ok := item.(map[string]any)
		if !ok || len(fields) != 5 {
			return nil, fmt.Errorf("DNF5 dependency RPM requires exactly five fields")
		}
		for _, key := range []string{"nevra", "action", "reason", "repo_id", "package_path"} {
			if _, ok := fields[key].(string); !ok {
				return nil, fmt.Errorf("DNF5 dependency RPM fields must be canonical strings")
			}
		}
	}
	deps := make([]codexRPMDependency, 0, len(p.RPMs)-1)
	names := map[string]bool{"chatgpt": true}
	paths := map[string]bool{"./packages/chatgpt.rpm": true}
	foundTarget := false
	for _, r := range p.RPMs {
		if r.Action != "Install" {
			return nil, fmt.Errorf("DNF5 dependency plan permits only initial installs")
		}
		if r.NEVRA == targetNEVRA {
			localRepo := r.Repo == "@commandline" || r.Repo == "@stored_transaction(@commandline)"
			if foundTarget || r.Reason != "User" || !localRepo || r.Path != "./packages/chatgpt.rpm" {
				return nil, fmt.Errorf("DNF5 plan differs from the exact initial ChatGPT RPM installation")
			}
			foundTarget = true
			continue
		}
		if r.Reason != "Dependency" || len(r.NEVRA) > 1024 {
			return nil, fmt.Errorf("DNF5 plan permits only bounded required dependencies")
		}
		fields := codexRPMDependencyNEVRARE.FindStringSubmatch(r.NEVRA)
		if fields == nil {
			return nil, fmt.Errorf("canonical RPM dependency NEVRA required")
		}
		epoch := fields[2]
		if epoch == "" {
			epoch = "0"
		}
		m := codexRPMMetadata{fields[1], epoch, fields[3], fields[4], fields[5]}
		if err := validateRPMDependencyPackage(m, goarch); err != nil {
			return nil, err
		}
		if names[m.Name] {
			return nil, fmt.Errorf("duplicate RPM dependency name")
		}
		filename, ok := strings.CutPrefix(r.Path, "./packages/")
		if !ok || len(filename) > 255 || !codexRPMDependencyFileRE.MatchString(filename) || paths[r.Path] {
			return nil, fmt.Errorf("unique safe flat RPM dependency path required")
		}
		// Older raw IDs can reuse configured repositories during replay, even
		// when disabled. Require the stored-repository namespace for dependencies.
		inner, wrapped := strings.CutPrefix(r.Repo, "@stored_transaction(")
		repo, closed := strings.CutSuffix(inner, ")")
		if !wrapped || !closed {
			return nil, fmt.Errorf("stored RPM dependency repository wrapper required")
		}
		if len(repo) > 256 || !codexRPMDependencyRepoRE.MatchString(repo) {
			return nil, fmt.Errorf("normal RPM dependency repository ID required")
		}
		names[m.Name], paths[r.Path] = true, true
		deps = append(deps, codexRPMDependency{Package: m, Path: r.Path, Repo: r.Repo})
	}
	if !foundTarget {
		return nil, fmt.Errorf("DNF5 dependency plan requires exactly one ChatGPT installation")
	}
	return deps, nil
}
