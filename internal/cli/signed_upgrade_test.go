package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/AtomyCloud/atm/internal/coreupdate"
)

// Opt-in acceptance uses a trusted external updater and two reviewed official
// signatures. It never signs, downloads, overrides trust, or uses a real install.
func TestSignedReleaseCLIUpgrade(t *testing.T) {
	candidate := os.Getenv("ATM_TEST_SIGNED_UPGRADE_DIR")
	if candidate == "" {
		t.Skip("set ATM_TEST_SIGNED_UPGRADE_DIR to the private signed 0.0.3 candidate")
	}
	if runtime.GOOS != "linux" || (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64") {
		t.Skip("requires a supported native Linux host")
	}
	baseline := os.Getenv("ATM_TEST_SIGNED_RELEASE_DIR")
	driver := os.Getenv("ATM_TEST_UPDATER_BIN")
	if baseline == "" || !filepath.IsAbs(driver) {
		t.Fatal("require ATM_TEST_SIGNED_RELEASE_DIR and absolute ATM_TEST_UPDATER_BIN")
	}
	info, err := os.Lstat(driver)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
		t.Fatal("updater must be a locally trusted regular executable", err)
	}
	driverBefore := recoveryCLISnapshot(t, driver)
	t.Cleanup(func() {
		if !reflect.DeepEqual(driverBefore, recoveryCLISnapshot(t, driver)) {
			t.Error("trusted external updater changed")
		}
	})
	root, _ := recoveryCLIFixture(t)
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "signed runtime")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{
		"ATM_HOME": home, "ATM_PLUGIN_DIR": filepath.Join(home, "plugins"),
		"XDG_DATA_HOME": filepath.Join(root, "state"), "XDG_CACHE_HOME": filepath.Join(root, "tmp"),
		"XDG_STATE_HOME": filepath.Join(root, "state"), "XDG_RUNTIME_DIR": filepath.Join(root, "tmp"),
		"SSH_AUTH_SOCK": "", "SSH_AGENT_PID": "",
	} {
		t.Setenv(key, value)
	}
	ctx := context.Background()
	load := func(dir, version, metadata string) ([]string, coreupdate.VerifiedPackage) {
		t.Helper()
		stem := "atm-" + version + "-linux-" + runtime.GOARCH
		sources := []string{filepath.Join(dir, metadata, stem+".manifest.json"), filepath.Join(dir, metadata, stem+".manifest.sig"), filepath.Join(dir, stem+".tar.gz")}
		inputs := make([]string, len(sources))
		for i, source := range sources {
			before := recoveryCLISnapshot(t, source)
			t.Cleanup(func() {
				if !reflect.DeepEqual(before, recoveryCLISnapshot(t, source)) {
					t.Errorf("signed input changed: %s", source)
				}
			})
			inputs[i] = filepath.Join(root, "inputs", filepath.Base(source))
			signedLifecycleCopy(t, source, inputs[i], []int64{coreupdate.MaxManifestBytes, 64, coreupdate.MaxPackageBytes}[i])
		}
		verified, err := coreupdate.VerifyOfficialPackageFiles(ctx, inputs[0], inputs[1], inputs[2], runtime.GOOS, runtime.GOARCH)
		if err != nil || verified.Manifest.Version != version || !verified.ReplacementEnabled {
			t.Fatalf("verify %s: %+v (%v)", version, verified, err)
		}
		return inputs, verified
	}
	old, oldInfo := load(baseline, "0.0.2", "signatures-linux-"+runtime.GOARCH)
	next, nextInfo := load(candidate, "0.0.3", "signatures")
	signedLifecycleExtract(t, old[2], home, "atm-0.0.2-linux-"+runtime.GOARCH, oldInfo.Files)
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(name, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	sentinel := filepath.Join(home, "user-data.txt")
	write(sentinel, "preserve user data\n")
	if err := os.Mkdir(filepath.Join(home, "empty-user-directory"), 0750); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(root, "config", "atm-go.json"), `{"schema":1,"language":"en-us","sort":"za"}`)
	run := func(binary string, args []string, wantError string) []byte {
		t.Helper()
		ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Dir = root
		var out, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &stderr
		err := cmd.Run()
		if ctx.Err() != nil {
			t.Fatalf("command timed out: %v", args)
		}
		if wantError == "" {
			if err != nil || stderr.Len() != 0 {
				t.Fatalf("%v: %v stdout=%s stderr=%s", args, err, &out, &stderr)
			}
		} else if err == nil || !strings.Contains(stderr.String(), wantError) {
			t.Fatalf("%v: want %q, got %v stdout=%s stderr=%s", args, wantError, err, &out, &stderr)
		}
		return out.Bytes()
	}
	command := func(operation string, inputs []string, flags ...string) []string {
		args := append([]string{"--home", home, "--json"}, flags...)
		return append(append(args, "updates", operation), inputs...)
	}
	readOnly := func(args []string, wantError string) {
		t.Helper()
		before := recoveryCLISnapshot(t, root)
		run(driver, args, wantError)
		if !reflect.DeepEqual(before, recoveryCLISnapshot(t, root)) {
			t.Fatalf("refused/read-only command wrote files: %v", args)
		}
	}
	checkVersion := func(want coreupdate.VerifiedPackage) {
		t.Helper()
		out := run(filepath.Join(home, "atm"), []string{"--version"}, "")
		expected := "atm " + want.Manifest.Version + " (" + want.Manifest.Commit + ")\n"
		if string(out) != expected {
			t.Fatalf("installed executable: got %q, want %q", out, expected)
		}
		t.Logf("executed installed binary: %s", strings.TrimSpace(string(out)))
	}
	checkVersion(oldInfo)
	run(driver, command("enroll-core", old, "--yes"), "")
	beforeUpgrade := recoveryCLISnapshot(t, home)
	readOnly(command("core", next), "require --yes")
	readOnly(command("core", next, "--dry-run", "--yes"), "")
	bad := append([]string{}, next...)
	bad[1] = filepath.Join(root, "inputs", "corrupt.sig")
	sig := append([]byte{}, signedLifecycleContents(t, next[1])...)
	sig[0] ^= 1
	if err := os.WriteFile(bad[1], sig, 0600); err != nil {
		t.Fatal(err)
	}
	readOnly(command("core", bad, "--yes"), "invalid signed package manifest")
	outside := func() map[string]map[string]string {
		result := map[string]map[string]string{}
		for _, name := range []string{"runtime", "config", "state", "apps", "inputs", "user", "tmp", "plugins"} {
			result[name] = recoveryCLISnapshot(t, filepath.Join(root, name))
		}
		return result
	}
	outsideBefore := outside()
	out := run(driver, command("core", next, "--yes"), "")
	var applied struct {
		Result  coreupdate.BundleResult `json:"result"`
		Restart bool                    `json:"restart_required"`
	}
	if err := json.Unmarshal(out, &applied); err != nil || !applied.Result.Changed || applied.Result.State != "applied" || !applied.Restart || applied.Result.Info.CurrentVersion != "0.0.2" || applied.Result.Info.TargetVersion != "0.0.3" {
		t.Fatalf("invalid upgrade result: %s (%v)", out, err)
	}
	key, err := coreupdate.OfficialPublicKey()
	if err != nil {
		t.Fatal(err)
	}
	policy := coreupdate.BundlePolicy{Root: home, PublicKey: key, OS: runtime.GOOS, Arch: runtime.GOARCH}
	installed, err := coreupdate.InspectBundle(ctx, policy)
	if err != nil || installed.Manifest != nextInfo.Manifest {
		t.Fatalf("candidate identity mismatch: %+v (%v)", installed, err)
	}
	checkVersion(nextInfo)
	if string(signedLifecycleContents(t, sentinel)) != "preserve user data\n" || !reflect.DeepEqual(outsideBefore, outside()) {
		t.Fatal("upgrade changed user data or external configuration")
	}
	readOnly(command("core", next, "--yes"), "package version must strictly increase")
	readOnly(command("core", old, "--yes"), "package version must strictly increase")
	for _, args := range [][]string{{"plugins", "list", "--json"}, {"doctor"}, {"--dry-run", "updates", "core"}} {
		run(filepath.Join(home, "atm"), append([]string{"--home", home}, args...), "")
	}
	// Preserve post-upgrade edits in quarantine; do not silently discard them.
	write(sentinel, "post-upgrade edit\n")
	write(filepath.Join(root, "config", "atm-go.json"), "broken JSON")
	t.Setenv("ATM_HOME", filepath.Join(root, "missing runtime"))
	t.Setenv("ATM_PLUGIN_DIR", filepath.Join(root, "missing plugins"))
	outsideBefore = outside()
	readOnly([]string{"--json", "recover-core", "--root", home}, "requires --yes")
	out = run(driver, []string{"--yes", "--json", "recover-core", "--root", home}, "")
	var recovered coreupdate.BundleResult
	if err := json.Unmarshal(out, &recovered); err != nil || !recovered.Changed || recovered.State != "recovered" {
		t.Fatalf("invalid recovery: %s (%v)", out, err)
	}
	if !reflect.DeepEqual(beforeUpgrade, recoveryCLISnapshot(t, home)) || !reflect.DeepEqual(outsideBefore, outside()) {
		t.Fatal("recovery failed to restore exact baseline or changed external configuration")
	}
	checkVersion(oldInfo)
	installed, err = coreupdate.InspectBundle(ctx, policy)
	if err != nil || installed.Manifest != oldInfo.Manifest {
		t.Fatalf("recovered baseline mismatch: %+v (%v)", installed, err)
	}
	rejected := filepath.Join(home+coreupdate.BundleTransactionSuffix, "rejected", "user-data.txt")
	if string(signedLifecycleContents(t, rejected)) != "post-upgrade edit\n" {
		t.Fatal("post-upgrade edit was lost during recovery")
	}
	readOnly([]string{"--yes", "--json", "recover-core", "--root", home}, "")
	t.Logf("official signed CLI 0.0.2 -> 0.0.3 -> 0.0.2 passed on linux/%s; candidate commit=%s; isolated roots only", runtime.GOARCH, nextInfo.Manifest.Commit)
}
