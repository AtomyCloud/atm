package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/AtomyCloud/atm/internal/core"
	"github.com/AtomyCloud/atm/internal/coreupdate"
	"github.com/AtomyCloud/atm/internal/updates"
)

func recoveryCLIFixture(t *testing.T) (root, home string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"runtime", "config", "state", "apps", "inputs", "user", "tmp", "plugins"} {
		dir := filepath.Join(root, name)
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "sentinel"), []byte("preserve fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	home = filepath.Join(root, "runtime")
	if err := os.WriteFile(filepath.Join(home, "core-api.json"), []byte(`{"schema":1,"plugin_api":1,"update_protocol":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"ATM_LANG", "ATM_PLUGIN_SORT_ORDER", "ATM_DRY_RUN"} {
		t.Setenv(key, "")
	}
	for key, value := range map[string]string{
		"HOME": filepath.Join(root, "user"), "USERPROFILE": filepath.Join(root, "user"),
		"XDG_CONFIG_HOME": filepath.Join(root, "config"), "APPDATA": filepath.Join(root, "config"),
		"ATM_HOME": home, "ATM_CONFIG_DIR": filepath.Join(root, "config"), "ATM_STATE_DIR": filepath.Join(root, "state"),
		"ATM_APPS_DIR": filepath.Join(root, "apps"), "ATM_PLUGIN_DIR": filepath.Join(root, "plugins"),
		"ATM_CONFIG_FILE": filepath.Join(root, "config", "atm.conf"), "ATM_PLUGINS_CONFIG_FILE": filepath.Join(root, "config", "plugins.conf"),
		"TMPDIR": filepath.Join(root, "tmp"), "TMP": filepath.Join(root, "tmp"), "TEMP": filepath.Join(root, "tmp"),
		"PATH": "", "GITHUB_TOKEN": "invalid\nfixture-token", "GH_TOKEN": "invalid\nfixture-token",
	} {
		t.Setenv(key, value)
	}
	return root, home
}

func recoveryCLISnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	snapshot := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		var content []byte
		if info.Mode().IsRegular() {
			content, err = os.ReadFile(path)
			if err != nil {
				return err
			}
		}
		snapshot[path] = fmt.Sprintf("%s:%s:%x", info.Mode(), info.ModTime().UTC(), sha256.Sum256(content))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestRecoverCoreMissingRootBypassesConfigurationAndConsentInDryRun(t *testing.T) {
	root, home := recoveryCLIFixture(t)
	missing := filepath.Join(root, "missing-installation")
	if err := os.WriteFile(filepath.Join(root, "config", "atm-go.json"), []byte("invalid JSON"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ATM_HOME", missing)
	t.Setenv("ATM_STATE_DIR", "invalid-relative-state")
	before := recoveryCLISnapshot(t, root)
	for _, prefix := range [][]string{{"--dry-run"}, {"--dry-run", "--json"}, {"--dry-run", "--yes", "--home", missing}} {
		var out, stderr bytes.Buffer
		args := append(append([]string{}, prefix...), "recover-core", "--root", missing)
		if code := Run(args, nil, &out, &stderr); code != 0 {
			t.Fatal(args, code, stderr.String())
		}
		var got struct {
			Executor  string `json:"executor"`
			Operation string `json:"operation"`
			Root      string `json:"root"`
			DryRun    bool   `json:"dry_run"`
		}
		if err := json.Unmarshal(out.Bytes(), &got); err != nil || got.Executor != "native-core" || got.Operation != "recover-core" || got.Root != missing || !got.DryRun || stderr.Len() != 0 {
			t.Fatal(out.String(), stderr.String(), err)
		}
	}
	var out, stderr bytes.Buffer
	if code := Run([]string{"--home", home, "updates", "trust"}, nil, &out, &stderr); code == 0 {
		t.Fatal("fixture's normal configuration unexpectedly valid")
	}
	if !reflect.DeepEqual(before, recoveryCLISnapshot(t, root)) {
		t.Fatal("recovery dry-run accessed or changed installation/configuration state")
	}
}

func TestRecoverCoreConsentBeforeMissingRootAndBrokenConfig(t *testing.T) {
	root, _ := recoveryCLIFixture(t)
	missing := filepath.Join(root, "missing-installation")
	t.Setenv("ATM_HOME", missing)
	t.Setenv("ATM_STATE_DIR", "not-absolute")
	before := recoveryCLISnapshot(t, root)
	for _, prefix := range [][]string{{}, {"--json"}, {"--home", missing}} {
		var out, stderr bytes.Buffer
		args := append(append([]string{}, prefix...), "recover-core", "--root", missing)
		err := run(args, nil, &out, &stderr)
		if err == nil || !strings.Contains(err.Error(), "requires --yes or interactive confirmation") || out.Len() != 0 {
			t.Fatal(args, out.String(), err)
		}
	}
	if !reflect.DeepEqual(before, recoveryCLISnapshot(t, root)) {
		t.Fatal("unconfirmed missing-root recovery wrote files")
	}
	// A redirected input file containing yes is not interactive consent.
	input := filepath.Join(root, "inputs", "answer")
	if err := os.WriteFile(input, []byte("y\n"), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(input)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	before = recoveryCLISnapshot(t, root)
	var out, stderr bytes.Buffer
	err = run([]string{"recover-core", "--root", missing}, f, &out, &stderr)
	if err == nil || !strings.Contains(err.Error(), "requires --yes") {
		t.Fatal("redirected input bypassed consent", err)
	}
	if !reflect.DeepEqual(before, recoveryCLISnapshot(t, root)) {
		t.Fatal("unconfirmed recovery wrote files")
	}
}

func TestRecoverCoreProductionDisabledBeforeFilesystemAccess(t *testing.T) {
	if coreupdate.ProductionEnabled {
		t.Skip("disabled-production gate is not active")
	}
	root, _ := recoveryCLIFixture(t)
	missing := filepath.Join(root, "missing-installation")
	t.Setenv("ATM_HOME", missing)
	t.Setenv("ATM_STATE_DIR", "invalid-state")
	before := recoveryCLISnapshot(t, root)
	var out, stderr bytes.Buffer
	err := run([]string{"--yes", "--json", "recover-core", "--root", missing}, nil, &out, &stderr)
	if !errors.Is(err, coreupdate.ErrProductionDisabled) || out.Len() != 0 {
		t.Fatal("recovery gate bypassed", out.String(), err)
	}
	// An invalid policy proves the disabled boundary precedes library validation.
	err = executeRecovery(context.Background(), coreupdate.BundlePolicy{Root: missing}, &out)
	if !errors.Is(err, coreupdate.ErrProductionDisabled) || out.Len() != 0 {
		t.Fatal(err, out.String())
	}
	if !reflect.DeepEqual(before, recoveryCLISnapshot(t, root)) {
		t.Fatal("disabled recovery modified files")
	}
}

func TestRecoverCoreRejectsInvalidArgumentsWithoutWriting(t *testing.T) {
	root, _ := recoveryCLIFixture(t)
	missing := filepath.Join(root, "missing")
	before := recoveryCLISnapshot(t, root)
	volumeRoot := filepath.VolumeName(root) + string(filepath.Separator)
	for _, args := range [][]string{
		{"--dry-run", "recover-core"}, {"--dry-run", "recover-core", "--root", "relative"},
		{"--dry-run", "recover-core", "--root", volumeRoot},
		{"--dry-run", "recover-core", "--root", missing + string(filepath.Separator) + ".."},
		{"--dry-run", "recover-core", "--root", missing + "\n"}, {"--dry-run", "recover-core", "--root", missing + "\r"},
		{"--dry-run", "recover-core", "--root", missing + "\x00"},
		{"--dry-run", "recover-core", "--root", missing, "extra"}, {"--dry-run", "recover-core", "--unknown"},
		{"--demo", "recover-core", "--root", missing},
		{"--timeout", "0s", "recover-core", "--root", missing}, {"--timeout", "-1s", "recover-core", "--root", missing},
	} {
		var out, stderr bytes.Buffer
		if code := Run(args, nil, &out, &stderr); code == 0 || stderr.Len() == 0 || out.Len() != 0 {
			t.Fatal("invalid recovery arguments accepted", args, code, out.String(), stderr.String())
		}
	}
	if !reflect.DeepEqual(before, recoveryCLISnapshot(t, root)) {
		t.Fatal("invalid recovery request modified files")
	}
}

type recoveryFailWriter struct{ err error }

func (w recoveryFailWriter) Write([]byte) (int, error) { return 0, w.err }

func TestRecoverCoreDryRunPropagatesOutputFailure(t *testing.T) {
	root, _ := recoveryCLIFixture(t)
	before := recoveryCLISnapshot(t, root)
	want := errors.New("fixture output failure")
	var stderr bytes.Buffer
	err := run([]string{"--dry-run", "recover-core", "--root", filepath.Join(root, "missing")}, nil, recoveryFailWriter{want}, &stderr)
	if !errors.Is(err, want) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, recoveryCLISnapshot(t, root)) {
		t.Fatal("failed dry-run output wrote files")
	}
}

func TestReleaseCLIDryRunAndConsentBeforeLocalInputs(t *testing.T) {
	root, home := recoveryCLIFixture(t)
	before := recoveryCLISnapshot(t, root)
	for _, args := range [][]string{
		{"updates", "core"}, {"self-update"},
		{"updates", "core", "missing.json", "missing.sig", "missing.archive"},
		{"updates", "enroll-core", "missing.json", "missing.sig", "missing.archive"},
	} {
		var out, stderr bytes.Buffer
		dry := append([]string{"--home", home, "--dry-run", "--json"}, args...)
		if code := Run(dry, nil, &out, &stderr); code != 0 {
			t.Fatal(args, code, stderr.String())
		}
		var plan core.Plan
		if err := json.Unmarshal(out.Bytes(), &plan); err != nil || !plan.DryRun || !plan.Mutating || plan.Executor != "native-core" {
			t.Fatal(out.String(), err)
		}
		out.Reset()
		stderr.Reset()
		unconfirmed := append([]string{"--home", home, "--json"}, args...)
		if code := Run(unconfirmed, nil, &out, &stderr); code == 0 || !strings.Contains(stderr.String(), "require --yes") || out.Len() != 0 {
			t.Fatal("consent did not precede release reads", args, code, out.String(), stderr.String())
		}
	}
	if !reflect.DeepEqual(before, recoveryCLISnapshot(t, root)) {
		t.Fatal("dry-run/unconfirmed release changed fixture roots")
	}
}

func TestReleaseCLICheckUnenrolledIgnoresBrokenPlugins(t *testing.T) {
	root, home := recoveryCLIFixture(t)
	broken := filepath.Join(root, "plugins", "broken")
	if err := os.Mkdir(broken, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(broken, "plugin.json"), []byte("invalid JSON"), 0600); err != nil {
		t.Fatal(err)
	}
	before := recoveryCLISnapshot(t, root)
	var out, stderr bytes.Buffer
	if code := Run([]string{"--home", home, "--json", "updates", "check-core"}, nil, &out, &stderr); code != 0 {
		t.Fatal(code, stderr.String())
	}
	var result struct {
		Operation string      `json:"operation"`
		Value     updates.Row `json:"result"`
		Restart   bool        `json:"restart_required"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil || result.Operation != "updates check-core" || result.Value.Status != "unmanaged" || result.Value.Component != "core" || result.Value.Commit != "" || result.Restart {
		t.Fatal(out.String(), err)
	}
	out.Reset()
	stderr.Reset()
	if code := Run([]string{"--home", home, "--yes", "updates", "core"}, nil, &out, &stderr); code == 0 || !strings.Contains(stderr.String(), "signed installed baseline required") || out.Len() != 0 {
		t.Fatal(code, out.String(), stderr.String())
	}
	if !reflect.DeepEqual(before, recoveryCLISnapshot(t, root)) {
		t.Fatal("unenrolled release inspection changed files")
	}
}

func TestReleaseCLIInvalidSignatureIsOfflineAndReadOnly(t *testing.T) {
	root, home := recoveryCLIFixture(t)
	manifest, signature := filepath.Join(root, "inputs", "manifest"), filepath.Join(root, "inputs", "signature")
	if err := os.WriteFile(manifest, []byte(`{"schema":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(signature, make([]byte, 64), 0600); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "inputs", "missing-archive")
	before := recoveryCLISnapshot(t, root)
	for _, operation := range []string{"core", "enroll-core", "verify-package"} {
		var out, stderr bytes.Buffer
		args := []string{"--home", home, "--json"}
		if operation != "verify-package" {
			args = append(args, "--yes")
		}
		args = append(args, "updates", operation, manifest, signature, archive)
		if code := Run(args, nil, &out, &stderr); code == 0 || !strings.Contains(stderr.String(), "invalid signed package manifest") {
			t.Fatal(operation, code, out.String(), stderr.String())
		}
	}
	if !reflect.DeepEqual(before, recoveryCLISnapshot(t, root)) {
		t.Fatal("invalid signature changed fixture files")
	}
}
