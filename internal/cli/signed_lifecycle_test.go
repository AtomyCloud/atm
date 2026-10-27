package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/AtomyCloud/atm/internal/core"
	"github.com/AtomyCloud/atm/internal/coreupdate"
)

// This opt-in acceptance test uses existing official signatures, never a test
// key or a production-gate override. All mutations target private fixture roots.
func TestSignedReleaseCLILifecycle(t *testing.T) {
	dir := os.Getenv("ATM_TEST_SIGNED_RELEASE_DIR")
	if dir == "" {
		t.Skip("set ATM_TEST_SIGNED_RELEASE_DIR to reviewed Linux 0.0.2 artifacts")
	}
	if runtime.GOOS != "linux" || (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64") {
		t.Skip("signed lifecycle acceptance requires a supported native Linux host")
	}
	if !coreupdate.ProductionEnabled {
		t.Fatal("signed lifecycle acceptance requires the approved production gate; no test bypass")
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	root, _ := recoveryCLIFixture(t)
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "signed runtime")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ATM_HOME", home)
	t.Setenv("ATM_PLUGIN_DIR", filepath.Join(home, "plugins"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "tmp"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(root, "tmp"))
	t.Setenv("SSH_AUTH_SOCK", "")
	oldTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = oldTransport })
	http.DefaultTransport = catalogTransport(func(*http.Request) (*http.Response, error) {
		t.Error("signed local lifecycle attempted network access")
		return nil, errors.New("network forbidden in signed lifecycle acceptance")
	})

	stem := "atm-0.0.2-linux-" + runtime.GOARCH
	meta := filepath.Join(dir, "signatures-linux-"+runtime.GOARCH)
	sources := []string{filepath.Join(meta, stem+".manifest.json"), filepath.Join(meta, stem+".manifest.sig"), filepath.Join(dir, stem+".tar.gz")}
	inputs := make([]string, len(sources))
	for i, source := range sources {
		before := recoveryCLISnapshot(t, source)
		t.Cleanup(func() {
			if !reflect.DeepEqual(before, recoveryCLISnapshot(t, source)) {
				t.Errorf("release artifact changed: %s", source)
			}
		})
		inputs[i] = filepath.Join(root, "inputs", filepath.Base(source))
		signedLifecycleCopy(t, source, inputs[i], []int64{coreupdate.MaxManifestBytes, 64, coreupdate.MaxPackageBytes}[i])
	}
	manifest, signature, archive := inputs[0], inputs[1], inputs[2]
	key, err := coreupdate.OfficialPublicKey()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	verified, err := coreupdate.VerifyOfficialPackageFiles(ctx, manifest, signature, archive, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	if verified.Fingerprint != coreupdate.OfficialKeyFingerprint || verified.Manifest.Version != "0.0.2" || verified.Files == 0 || !verified.ReplacementEnabled {
		t.Fatalf("unexpected official package verification: %+v", verified)
	}
	// Extraction happens only after signature and complete archive verification.
	signedLifecycleExtract(t, archive, home, stem, verified.Files)
	policy := coreupdate.BundlePolicy{Root: home, PublicKey: key, OS: runtime.GOOS, Arch: runtime.GOARCH}
	if _, err := coreupdate.InspectBundle(ctx, policy); !errors.Is(err, coreupdate.ErrBundleUnenrolled) {
		t.Fatalf("fixture is not an unenrolled package: %v", err)
	}
	write := func(name string, data []byte) {
		t.Helper()
		if err := os.WriteFile(name, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	sentinel := filepath.Join(home, "unrelated-user-data")
	write(sentinel, []byte("preserve unrelated data\x00\xff\n"))
	if err := os.Mkdir(filepath.Join(home, "unrelated-empty-directory"), 0750); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(root, "config", "atm-go.json"), []byte(`{"schema":1,"language":"en-us","sort":"za"}`))
	write(filepath.Join(root, "config", "atm.conf"), []byte("ATM_LANG=en-us\n"))
	write(filepath.Join(root, "config", "plugins.conf"), []byte("ATM_PLUGIN_NODE_ENABLED=0\n"))
	sigBytes, err := os.ReadFile(signature)
	if err != nil {
		t.Fatal(err)
	}
	sigBytes[0] ^= 1
	badSignature := filepath.Join(root, "inputs", "corrupt.sig")
	write(badSignature, sigBytes)
	invoke := func(args []string, wantError string) []byte {
		t.Helper()
		var out, stderr bytes.Buffer
		code := Run(args, nil, &out, &stderr)
		if wantError == "" {
			if code != 0 || stderr.Len() != 0 || !json.Valid(out.Bytes()) {
				t.Fatalf("%v: code=%d stdout=%s stderr=%s", args, code, &out, &stderr)
			}
		} else if code == 0 || !strings.Contains(stderr.String(), wantError) {
			t.Fatalf("%v: want refusal %q; code=%d stdout=%s stderr=%s", args, wantError, code, &out, &stderr)
		}
		return out.Bytes()
	}
	command := func(operation string, flags ...string) []string {
		args := append([]string{"--home", home, "--json"}, flags...)
		return append(args, "updates", operation, manifest, signature, archive)
	}
	readOnly := func(args []string, wantError string) []byte {
		t.Helper()
		before := recoveryCLISnapshot(t, root)
		out := invoke(args, wantError)
		if !reflect.DeepEqual(before, recoveryCLISnapshot(t, root)) {
			t.Fatalf("read-only/refused command changed fixture files or directories: %v", args)
		}
		return out
	}
	for _, operation := range []string{"enroll-core", "core"} {
		readOnly(command(operation), "require --yes")
		for _, flags := range [][]string{{"--dry-run"}, {"--dry-run", "--yes"}} {
			out := readOnly(command(operation, flags...), "")
			var plan core.Plan
			if err := json.Unmarshal(out, &plan); err != nil || !plan.DryRun || !plan.Mutating || plan.Executor != "native-core" {
				t.Fatalf("invalid native dry-run plan: %s (%v)", out, err)
			}
		}
	}
	for _, operation := range []string{"verify-package", "enroll-core", "core"} {
		args := command(operation, "--yes")
		args[len(args)-2] = badSignature
		readOnly(args, "invalid signed package manifest")
	}
	t.Log("real-signature consent, dry-run and corrupt-signature no-write checks passed")

	oldTree := recoveryCLISnapshot(t, home)
	outside := func() map[string]map[string]string {
		result := map[string]map[string]string{}
		for _, name := range []string{"runtime", "config", "state", "apps", "inputs", "user", "tmp", "plugins"} {
			result[name] = recoveryCLISnapshot(t, filepath.Join(root, name))
		}
		return result
	}
	outsideBefore := outside()
	sentinelBefore := signedLifecycleContents(t, sentinel)
	out := invoke(command("enroll-core", "--yes"), "")
	var enrollment struct {
		Operation string                  `json:"operation"`
		Result    coreupdate.BundleResult `json:"result"`
		Restart   bool                    `json:"restart_required"`
	}
	wantOperation := strings.Join(append([]string{"updates", "enroll-core"}, inputs...), " ")
	if err := json.Unmarshal(out, &enrollment); err != nil || enrollment.Operation != wantOperation || enrollment.Result.State != "enrolled" || !enrollment.Result.Changed || !enrollment.Restart || enrollment.Result.Root != home {
		t.Fatalf("invalid enrollment result: %s (%v)", out, err)
	}
	installed, err := coreupdate.InspectBundle(ctx, policy)
	if err != nil || installed.Manifest != verified.Manifest || installed.Files != verified.Files || installed.PreservedFiles != 1 {
		t.Fatalf("installed signed baseline mismatch: %+v (%v)", installed, err)
	}
	for i, receipt := range []string{coreupdate.BundleManifestName, coreupdate.BundleSignatureName} {
		if !bytes.Equal(signedLifecycleContents(t, filepath.Join(home, receipt)), signedLifecycleContents(t, inputs[i])) {
			t.Fatalf("installed receipt differs from official input: %s", receipt)
		}
	}
	if !bytes.Equal(sentinelBefore, signedLifecycleContents(t, sentinel)) || !reflect.DeepEqual(outsideBefore, outside()) {
		t.Fatal("enrollment changed unrelated data or configuration")
	}
	readOnly(command("core", "--yes"), "package version must strictly increase")
	owned := filepath.Join(home, "README.md")
	ownedBytes := signedLifecycleContents(t, owned)
	// Leave this edit in the active candidate: recovery must retain it in quarantine.
	if err := os.WriteFile(owned, append(ownedBytes, []byte("\nlocal owned edit\n")...), 0644); err != nil {
		t.Fatal(err)
	}
	readOnly(command("core", "--yes"), "modified owned package file: README.md")
	t.Log("official CLI enrollment, same-version refusal and owned-edit refusal passed")

	write(filepath.Join(root, "config", "atm-go.json"), []byte("malformed JSON"))
	write(filepath.Join(root, "config", "atm.conf"), []byte("ATM_LANG=$(invalid)\n"))
	write(filepath.Join(root, "plugins", "plugin.json"), []byte("malformed plugin JSON"))
	t.Setenv("ATM_HOME", filepath.Join(root, "missing ATM home"))
	t.Setenv("ATM_PLUGIN_DIR", filepath.Join(root, "plugins", "plugin.json"))
	t.Setenv("ATM_PLUGINS_CONFIG_FILE", filepath.Join(root, "plugins"))
	t.Setenv("ATM_STATE_DIR", "invalid-relative-state")
	readOnly([]string{"--json", "updates", "trust"}, "not an ATM directory")
	readOnly([]string{"--home", home, "--json", "updates", "trust"}, "shell expansion is not supported")
	outsideBefore = outside()
	readOnly([]string{"--json", "recover-core", "--root", home}, "requires --yes")
	readOnly([]string{"--dry-run", "--json", "recover-core", "--root", home}, "")
	candidateBefore := recoveryCLISnapshot(t, home)
	out = invoke([]string{"--yes", "--json", "recover-core", "--root", home}, "")
	var recovery coreupdate.BundleResult
	if err := json.Unmarshal(out, &recovery); err != nil || recovery.State != "recovered" || !recovery.Changed || recovery.Root != home {
		t.Fatalf("invalid early recovery result: %s (%v)", out, err)
	}
	if !reflect.DeepEqual(oldTree, recoveryCLISnapshot(t, home)) {
		t.Fatal("recovery did not restore the exact original unenrolled tree, modes and timestamps")
	}
	if !reflect.DeepEqual(outsideBefore, outside()) {
		t.Fatal("early recovery changed unrelated data or malformed configuration")
	}
	for _, receipt := range []string{coreupdate.BundleManifestName, coreupdate.BundleSignatureName} {
		if _, err := os.Lstat(filepath.Join(home, receipt)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("recovery retained an enrollment receipt: %s (%v)", receipt, err)
		}
	}
	if _, err := coreupdate.InspectBundle(ctx, policy); !errors.Is(err, coreupdate.ErrBundleUnenrolled) {
		t.Fatalf("recovery did not undo enrollment: %v", err)
	}
	rejected := filepath.Join(home+coreupdate.BundleTransactionSuffix, "rejected")
	rejectedTree := recoveryCLISnapshot(t, rejected)
	for name, value := range candidateBefore {
		rel, err := filepath.Rel(home, name)
		if err != nil || rejectedTree[filepath.Join(rejected, rel)] != value {
			t.Fatalf("recovery did not preserve displaced candidate: %s (%v)", name, err)
		}
	}
	if len(rejectedTree) != len(candidateBefore) {
		t.Fatal("unexpected files in recovery quarantine")
	}
	readOnly([]string{"--yes", "--json", "recover-core", "--root", home}, "")
	t.Logf("linux/%s signed 0.0.2 CLI lifecycle passed: %d files, commit=%s; recovery undid enrollment", runtime.GOARCH, verified.Files, verified.Manifest.Commit)
}

func signedLifecycleContents(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func signedLifecycleCopy(t *testing.T, source, destination string, limit int64) {
	t.Helper()
	info, err := os.Lstat(source)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > limit {
		t.Fatalf("invalid artifact size/type: %s", source)
	}
	f, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(b)) != info.Size() {
		t.Fatalf("artifact changed or unreadable: %s (%v)", source, err)
	}
	if err := os.WriteFile(destination, b, 0600); err != nil {
		t.Fatal(err)
	}
}

// The reviewed Linux packages contain only regular files below one prefix.
// Reject other entry types, bound both decompression and payload, and confine
// exclusive writes with os.Root rather than relying on joined paths alone.
func signedLifecycleExtract(t *testing.T, archive, destination, prefix string, wantFiles int) {
	t.Helper()
	f, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(io.LimitReader(f, coreupdate.MaxPackageBytes+1))
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	expanded := &io.LimitedReader{R: gz, N: 512 << 20}
	tr := tar.NewReader(expanded)
	dest, err := os.OpenRoot(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer dest.Close()
	var total int64
	files := 0
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		name, ok := strings.CutPrefix(h.Name, prefix+"/")
		if !ok || !fs.ValidPath(name) || name == "." || strings.ContainsAny(name, "\\:\x00") || len(name) > 1024 || strings.Count(name, "/") > 32 {
			t.Fatalf("unsafe fixture archive path: %q", h.Name)
		}
		if h.Typeflag != tar.TypeReg || h.Linkname != "" || (h.Mode != 0644 && h.Mode != 0755) || h.Size < 0 || h.Size > 64<<20 || h.Size > (256<<20)-total || files >= 4096 {
			t.Fatalf("unsafe or oversized fixture archive entry: %q", h.Name)
		}
		if err := dest.MkdirAll(path.Dir(name), 0700); err != nil {
			t.Fatal(err)
		}
		out, err := dest.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, os.FileMode(h.Mode))
		if err != nil {
			t.Fatal(err)
		}
		n, copyErr := io.CopyN(out, tr, h.Size)
		modeErr := out.Chmod(os.FileMode(h.Mode))
		closeErr := out.Close()
		if err := errors.Join(copyErr, modeErr, closeErr); err != nil || n != h.Size {
			t.Fatal(fmt.Errorf("extract %s: copied %d of %d bytes: %v", name, n, h.Size, err))
		}
		total += n
		files++
	}
	if _, err := io.Copy(io.Discard, expanded); err != nil || expanded.N == 0 || files != wantFiles {
		t.Fatalf("fixture archive incomplete/oversized: files=%d want=%d (%v)", files, wantFiles, err)
	}
}
