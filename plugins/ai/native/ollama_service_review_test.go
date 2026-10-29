//go:build linux

package main

import (
	"reflect"
	"strings"
	"testing"
)

func reviewOllamaManagerOutput(overrides map[string]string) string {
	fields := map[string]string{
		"Id": ollamaServiceName, "Names": ollamaServiceName,
		"LoadState": "not-found", "ActiveState": "inactive", "SubState": "dead",
		"NeedDaemonReload": "no",
	}
	for key, value := range overrides {
		fields[key] = value
	}
	var out strings.Builder
	for _, key := range strings.Split(ollamaServiceProperties, ",") {
		out.WriteString(key + "=" + fields[key] + "\n")
	}
	return out.String()
}

func TestOllamaServiceReviewRejectsAmbiguousManager(t *testing.T) {
	valid := reviewOllamaManagerOutput(nil)
	m, err := parseOllamaManager(valid)
	if err != nil || m.absent() != nil {
		t.Fatalf("valid absent fixture rejected: %v", err)
	}
	for name, output := range map[string]string{
		"missing":    strings.Replace(valid, "Job=\n", "", 1),
		"duplicate":  valid + "Job=\n",
		"unknown":    valid + "Unexpected=\n",
		"malformed":  valid + "Job\n",
		"alias":      reviewOllamaManagerOutput(map[string]string{"Names": ollamaServiceName + " external.service"}),
		"wrong-id":   reviewOllamaManagerOutput(map[string]string{"Id": "ollama.service"}),
		"dropin":     reviewOllamaManagerOutput(map[string]string{"DropInPaths": "/etc/systemd/user/service.d/extra.conf"}),
		"pending":    reviewOllamaManagerOutput(map[string]string{"Job": "42"}),
		"stale":      reviewOllamaManagerOutput(map[string]string{"NeedDaemonReload": "yes"}),
		"transition": reviewOllamaManagerOutput(map[string]string{"ActiveState": "activating", "SubState": "start"}),
		"env-file":   reviewOllamaManagerOutput(map[string]string{"EnvironmentFiles": "/tmp/foreign.env"}),
		"source":     reviewOllamaManagerOutput(map[string]string{"SourcePath": "/tmp/foreign.service"}),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseOllamaManager(output); err == nil {
				t.Fatal("ambiguous manager state accepted")
			}
		})
	}
	for _, fields := range []map[string]string{
		{"LoadState": "masked"}, {"FragmentPath": "/tmp/foreign.service"},
		{"UnitFileState": "enabled"}, {"ExecStart": "foreign"},
		{"ActiveState": "active", "SubState": "running"},
	} {
		m, err := parseOllamaManager(reviewOllamaManagerOutput(fields))
		if err == nil && m.absent() == nil {
			t.Fatalf("foreign unit accepted as absent: %v", fields)
		}
	}
}

func TestOllamaServiceReviewEnvironmentIgnoresCaller(t *testing.T) {
	for _, key := range []string{"DBUS_SESSION_BUS_ADDRESS", "XDG_RUNTIME_DIR", "SYSTEMD_UNIT_PATH", "LD_PRELOAD", "LD_LIBRARY_PATH", "LD_AUDIT", "OLLAMA_HOST", "OLLAMA_MODELS", "OPENAI_API_KEY"} {
		t.Setenv(key, "caller-must-not-leak")
	}
	got := ollamaServiceEnvironment("/tmp/private-home", 1234)
	want := []string{
		"PATH=/usr/bin:/bin", "HOME=/tmp/private-home", "LANG=C", "LC_ALL=C",
		"XDG_CONFIG_HOME=/tmp/private-home/.config", "XDG_RUNTIME_DIR=/run/user/1234",
		"DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1234/bus", "SYSTEMD_PAGER=cat", "SYSTEMD_COLORS=0",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected service client environment: %q", got)
	}
}

func TestOllamaServiceReviewSystemctlEmptyArrayOmission(t *testing.T) {
	output := reviewOllamaManagerOutput(nil)
	for _, key := range []string{"EnvironmentFiles", "ExecStart"} {
		output = strings.Replace(output, key+"=\n", "", 1)
	}
	m, err := parseOllamaManager(output)
	if err != nil || m.absent() != nil {
		t.Fatalf("systemctl omits empty custom-formatted arrays even with --all: %v", err)
	}
	output = strings.Replace(output, "FragmentPath=\n", "", 1)
	if _, err := parseOllamaManager(output); err == nil {
		t.Fatal("mandatory identity property omission accepted")
	}
}

func TestOllamaServiceReviewWorkingDirectoryIsRawPath(t *testing.T) {
	r := ollamaServiceReceipt{
		DataHome:    "/tmp/a b%h${HOME}\"quote'back\\slash",
		Description: "ATM fixture", Argv: []string{"/usr/bin/env", "-i", "/tmp/bin/ollama", "serve"},
	}
	unit := string(ollamaServiceUnit(r))
	if !strings.Contains(unit, "\nWorkingDirectory="+strings.ReplaceAll(r.DataHome, "%", "%%")+"\n") {
		t.Fatalf("WorkingDirectory must not be shell/ExecStart word quoted: %s", unit)
	}
	for _, required := range []string{"\nExecStart=:\"/usr/bin/env\" \"-i\"", "\nRestart=no\n", "\nUnsetEnvironment=LD_PRELOAD LD_LIBRARY_PATH LD_AUDIT\n"} {
		if !strings.Contains(unit, required) {
			t.Fatalf("unit missing invariant %q", required)
		}
	}
	if strings.Contains(unit, "[Install]") || strings.Contains(unit, "/current/") {
		t.Fatal("unit must not auto-enable or follow a mutable version link")
	}
}

func TestOllamaServiceReviewSystemdWord(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"/tmp/a b", `"/tmp/a b"`},
		{"/tmp/%h", `"/tmp/%%h"`},
		{"/tmp/${HOME}", `"/tmp/${HOME}"`},
		{"/tmp/a\"b\\c'd", `"/tmp/a\"b\\c'd"`},
	} {
		if got := ollamaSystemdWord(tc.input); got != tc.want {
			t.Errorf("quote %q: got %q want %q", tc.input, got, tc.want)
		}
	}
}
