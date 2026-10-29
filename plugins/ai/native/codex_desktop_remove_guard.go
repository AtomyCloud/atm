//go:build linux

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
)

// Removal has no artifact, source URL or download trust claim.
type codexRemoveGuardMetadata struct {
	Schema                     int
	Operation                  string
	Baseline                   codexPackageRecord
	Stage, HelperSHA256, Nonce string
}

func (m *codexRemoveGuardMetadata) UnmarshalJSON(data []byte) error {
	type plain codexRemoveGuardMetadata
	var v plain
	if err := decode(data, &v, true); err != nil {
		return err
	}
	if err := codexCanonicalJSON(data, v); err != nil {
		return err
	}
	*m = codexRemoveGuardMetadata(v)
	return nil
}

func (m codexRemoveGuardMetadata) validate(stage string) error {
	if m.Schema != 3 || m.Operation != "remove" || !codexDesktopStageRE.MatchString(stage) || m.Stage != stage || (m.Baseline.Arch != "amd64" && m.Baseline.Arch != "arm64") || !codexUpdateEligible(m.Baseline, m.Baseline.Arch) || !validSHA(m.HelperSHA256) || !validSHA(m.Nonce) {
		return fmt.Errorf("invalid removal-only APT guard metadata")
	}
	return nil
}

func codexRemoveMarker(m codexRemoveGuardMetadata, early bool) []byte {
	data, _ := json.Marshal(m)
	prefix := "atm-codex-apt-remove-v3\n"
	if early {
		prefix = "early-baseline\n" + prefix
	}
	return append([]byte(prefix), data...)
}

// These options also exclude anonymous pending operations, which APT does not
// serialize as package rows in SendPkgsInfo. Dpkg scripts/triggers still run.
var codexRemoveFalseOptions = []string{
	"APT::Get::Purge", "APT::Get::AutomaticRemove", "APT::Get::Fix-Broken", "APT::Get::Fix-Missing",
	"APT::Get::AutomaticRemove::Kernels", "APT::Get::Fix-Policy-Broken",
	"APT::Get::ReInstall", "APT::Ignore-Hold", "APT::Get::Download", "APT::Get::Download-Only",
	"APT::Get::allow-Downgrades", "APT::Get::allow-Change-Held-Packages", "APT::Get::allow-Remove-Essential",
	"APT::Get::AllowUnauthenticated", "Acquire::AllowInsecureRepositories", "Acquire::AllowDowngradeToInsecureRepositories",
	"DPkg::ConfigurePending", "DPkg::TriggersPending", "DPkg::NoTriggers", "DPkg::Selection::Remove::Approved",
	"DPkg::Selection::Current::SaveAndRestore",
}

func codexRemoveAPTArgs(stage string) []string {
	args := []string{
		"-o", "DPkg::Pre-Invoke::=exec " + stage + "/" + codexGuardName + " " + codexGuardEarlyFlag,
		"-o", "DPkg::Pre-Install-Pkgs::=exec " + stage + "/" + codexGuardName + " " + codexGuardFlag,
		"-o", "DPkg::Tools::Options::exec::Version=3", "-o", "DPkg::Tools::Options::exec::InfoFD=0",
	}
	for _, key := range codexRemoveFalseOptions {
		args = append(args, "-o", key+"=false")
	}
	return append(args, "--no-download", "-y", "remove", "--", "chatgpt")
}

func checkCodexRemovePolicy(ctx context.Context, t *codexDesktopTools) error {
	if err := checkCodexSystemPolicy(ctx, t); err != nil {
		return err
	}
	ex, err := t.commands.resolve("apt-config")
	if err != nil {
		return err
	}
	out, err := t.commands.run(ctx, ex, codexConfigDumpArgs)
	if err != nil {
		return err
	}
	return validateCodexRemoveConfig(out, "", false)
}

// System policy is checked before elevation and again in both locked guards.
// The protocol's effective config independently proves the pinned APT options.
func validateCodexRemoveConfig(data, stage string, effective bool) error {
	if len(data) > 1<<20 {
		return fmt.Errorf("removal configuration exceeds bound")
	}
	seen := map[string]bool{}
	want := map[string]string{}
	for _, key := range codexRemoveFalseOptions {
		want[strings.ToLower(key)] = "false"
	}
	if effective {
		want["dpkg::tools::options::exec::version"] = "3"
		want["dpkg::tools::options::exec::infofd"] = "0"
	}
	var fixed strings.Builder
	pre, final := 0, 0
	for _, line := range strings.Split(strings.TrimSuffix(data, "\n"), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("malformed removal configuration")
		}
		key, err := url.PathUnescape(key)
		if err != nil {
			return err
		}
		value, err = url.PathUnescape(value)
		if err != nil {
			return err
		}
		key = strings.ToLower(key)
		if strings.ContainsAny(key, "\x00\r\n=\t ") || strings.ContainsAny(value, "\x00\r\n") {
			return fmt.Errorf("unsafe removal configuration")
		}
		if seen[key] && !strings.HasSuffix(key, "::") {
			return fmt.Errorf("duplicate removal configuration")
		}
		seen[key] = true
		if required, ok := want[key]; ok {
			valid := value == required
			if required == "false" {
				valid = value == "false" || value == "0" || value == "no" || !effective && value == ""
			}
			if !valid {
				return fmt.Errorf("incompatible removal configuration %s", key)
			}
		}
		if effective && key == "dpkg::pre-invoke::" {
			if value != "exec "+stage+"/"+codexGuardName+" "+codexGuardEarlyFlag {
				return fmt.Errorf("foreign early removal hook")
			}
			pre++
			continue
		}
		if effective && key == "dpkg::pre-install-pkgs::" && value == "exec "+stage+"/"+codexGuardName+" "+codexGuardFlag {
			final++
		}
		fixed.WriteString(line + "\n")
	}
	if effective {
		for key := range want {
			if !seen[key] {
				return fmt.Errorf("missing effective removal option %s", key)
			}
		}
		if pre != 1 || final != 1 {
			return fmt.Errorf("missing/duplicate removal hooks")
		}
		if err := validateCodexAPTConfig(fixed.String(), "/usr/bin/dpkg"); err != nil {
			return err
		}
	}
	return nil
}

// Debian/apt apt-pkg/deb/dpkgpm.cc SendPkgsInfo emits VERSION 3, escaped
// configuration, a blank line, then nine space-separated fields per action.
// Both purge and remove serialize as **REMOVE**; effective policy is mandatory.
func validateCodexAPTRemoveInput(input string, baseline codexPackageRecord, stage string) error {
	if len(input) > 1<<20 || !strings.HasPrefix(input, "VERSION 3\n") || !strings.HasSuffix(input, "\n") || strings.ContainsAny(input, "\x00\r") {
		return fmt.Errorf("removal requires bounded APT protocol-v3 input")
	}
	config, actions, ok := strings.Cut(strings.TrimPrefix(input, "VERSION 3\n"), "\n\n")
	if !ok || actions == "" {
		return fmt.Errorf("missing removal configuration/action boundary")
	}
	if err := validateCodexRemoveConfig(config+"\n", stage, true); err != nil {
		return err
	}
	row := strings.TrimSuffix(actions, "\n")
	f := strings.Split(row, " ")
	if len(f) != 9 || !codexUpdateEligible(baseline, baseline.Arch) || (baseline.Arch != "amd64" && baseline.Arch != "arm64") || f[0] != "chatgpt" || f[1] != baseline.Version || f[2] != baseline.Arch || f[4] != ">" || f[5] != "-" || f[6] != "-" || f[7] != "none" || f[8] != "**REMOVE**" {
		return fmt.Errorf("APT plan must contain exactly one baseline-matching chatgpt REMOVE action")
	}
	switch f[3] {
	case "none", "no", "same", "foreign", "allowed":
	default:
		return fmt.Errorf("invalid APT old multiarch field")
	}
	return nil
}

func runCodexRemoveGuard(ctx context.Context, stage string, input io.Reader, t codexAPTGuardTools, early bool, data []byte) error {
	var m codexRemoveGuardMetadata
	if err := decode(data, &m, true); err != nil {
		return err
	}
	if err := m.validate(stage); err != nil {
		return err
	}
	if !early {
		ack, err := t.read(stage+"/remove-early", 0600, 4096)
		if err != nil {
			return err
		}
		if string(ack) != string(codexRemoveMarker(m, true)) {
			return fmt.Errorf("missing/mismatched early removal acknowledgement")
		}
	}
	_, hash, err := t.hash(stage+"/"+codexGuardName, 0500, codexGuardMaxImage)
	if err != nil {
		return err
	}
	if hash != m.HelperSHA256 {
		return fmt.Errorf("removal guard image mismatch")
	}
	if t.removePolicy == nil {
		return fmt.Errorf("removal policy inspection unavailable")
	}
	if err := t.removePolicy(ctx); err != nil {
		return err
	}
	if !early {
		list, err := io.ReadAll(io.LimitReader(input, (1<<20)+1))
		if err != nil {
			return err
		}
		if err := validateCodexAPTRemoveInput(string(list), m.Baseline, stage); err != nil {
			return err
		}
	}
	pkg, err := t.packageRecord()
	if err != nil {
		return err
	}
	if pkg != m.Baseline {
		return fmt.Errorf("chatgpt original removal baseline changed under APT lock")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := t.lock(); err != nil {
		return err
	}
	if err := t.stage(stage); err != nil {
		return err
	}
	name := "remove-ok"
	if early {
		name = "remove-early"
	}
	return t.marker(stage+"/"+name, codexRemoveMarker(m, early))
}
