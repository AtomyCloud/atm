//go:build linux

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/AtomyCloud/atm/internal/protocol"
)

type systemCall struct {
	name     string
	args     []string
	elevated bool
}
type systemFixture struct {
	*fixture
	a               artifact
	calls           []systemCall
	version, arch   string
	fault           string
	cancel          context.CancelFunc
	reads, policies int
}

func newSystemFixture(t *testing.T) *systemFixture {
	t.Helper()
	f := &systemFixture{fixture: newFixture(t, "amd64")}
	f.a = f.release(t, hermesSystem, "0.7.7", []byte("synthetic DEB bytes: never executable"))
	f.b.system = &systemTools{
		resolve: func(n string) (string, error) { return "/trusted/" + n, nil },
		uid:     func() int { return 1000 },
		stage: func(p string) error {
			if p != "/run/atm-ai-hermes-0123456789" || f.fault == "stage" {
				return errors.New("unsafe staging")
			}
			return nil
		},
		installed: func() (string, string, error) {
			f.reads++
			if f.fault == "postcondition" && f.reads == 3 {
				return "", "", nil
			}
			if f.fault == "concurrent" && f.reads == 2 {
				return "0.7.6", "amd64", nil
			}
			return f.version, f.arch, nil
		},
		policy: func() error {
			f.policies++
			if f.fault == "pins" || f.fault == "pins-changed" && f.policies == 2 {
				return errors.New("unsupported custom preferences")
			}
			return nil
		},
	}
	f.b.system.run = func(ctx context.Context, ex string, args []string) (string, error) {
		t.Helper()
		elevated := ex == "/trusted/sudo"
		if elevated {
			if len(args) < 4 || !reflect.DeepEqual(args[:3], []string{"-n", "--preserve-env=DEBIAN_FRONTEND", "--"}) {
				t.Fatal("unsafe sudo", args)
			}
			ex, args = args[3], args[4:]
		}
		name := strings.TrimPrefix(ex, "/trusted/")
		if f.b.system.uid() == 0 && name != "apt-config" && !(name == "dpkg-deb" && len(args) > 1 && !strings.HasPrefix(args[1], "/run/")) {
			elevated = true
		}
		if strings.Contains(name, "/") {
			t.Fatal("untrusted executable", ex)
		}
		f.calls = append(f.calls, systemCall{name, append([]string{}, args...), elevated})
		phase := name
		if name == "dpkg-deb" && elevated {
			phase = "root-metadata"
		}
		if f.fault == phase || f.fault == "cancel-"+phase {
			if f.cancel != nil && strings.HasPrefix(f.fault, "cancel-") {
				f.cancel()
				return "", ctx.Err()
			}
			return "", errors.New("fixture command failure")
		}
		switch name {
		case "apt-config":
			if elevated || !reflect.DeepEqual(args, []string{"shell", "ATM_PREF", "Dir::Etc::preferences/f", "ATM_PARTS", "Dir::Etc::preferencesparts/d"}) {
				t.Fatal("unexpected policy command", args)
			}
			if f.fault == "preferences-path" {
				return "ATM_PREF='/other/preferences'\nATM_PARTS='/etc/apt/preferences.d/'\n", nil
			}
			return "ATM_PREF='/etc/apt/preferences'\nATM_PARTS='/etc/apt/preferences.d/'\n", nil
		case "dpkg-deb":
			if len(args) != 5 || args[0] != "--field" || !reflect.DeepEqual(args[2:], []string{"Package", "Version", "Architecture"}) {
				t.Fatal("unsafe metadata argv", args)
			}
			metadata := "Package: hermes-desktop\nVersion: " + f.a.Version + "\nArchitecture: amd64\n"
			if f.fault == "metadata" || f.fault == "root-fields" && elevated {
				metadata = strings.ReplaceAll(metadata, "hermes-desktop", "wrong-package")
			}
			return metadata, nil
		case "mktemp":
			if !elevated || !reflect.DeepEqual(args, []string{"-d", "/run/atm-ai-hermes-XXXXXXXXXX"}) {
				t.Fatal("unsafe mktemp", args)
			}
			if f.fault == "stage-path" {
				return "/tmp/user-stage\n", nil
			}
			return "/run/atm-ai-hermes-0123456789\n", nil
		case "install":
			if !elevated || len(args) != 6 || !reflect.DeepEqual(args[:4], []string{"-T", "-m", "0600", "--"}) || args[5] != "/run/atm-ai-hermes-0123456789/hermes-desktop.deb" {
				t.Fatal("unsafe copy", args)
			}
			info, err := os.Stat(args[4])
			if err != nil || info.Mode().Perm() != 0600 {
				t.Fatal("unsafe input", err)
			}
			return "", nil
		case "sha256sum":
			if !elevated || !reflect.DeepEqual(args, []string{"--", "/run/atm-ai-hermes-0123456789/hermes-desktop.deb"}) {
				t.Fatal("unsafe checksum", args)
			}
			h := f.a.SHA256
			if f.fault == "copy-changed" {
				h = strings.Repeat("0", 64)
			}
			return h + "  " + args[1] + "\n", nil
		case "apt-get":
			if !elevated || !reflect.DeepEqual(args, systemAPTArgs("/run/atm-ai-hermes-0123456789/hermes-desktop.deb")) {
				t.Fatal("unsafe APT", args)
			}
			f.version, f.arch = f.a.Version, "amd64"
			return "fixture installed\n", nil
		case "rm", "rmdir":
			want := "/run/atm-ai-hermes-0123456789"
			if name == "rm" {
				want += "/hermes-desktop.deb"
			}
			if !elevated || !reflect.DeepEqual(args, []string{"--", want}) {
				t.Fatal("unsafe cleanup", args)
			}
			if name == "rmdir" && f.fault == "final-journal" {
				writeFixture(t, filepath.Join(f.b.root(hermesSystem), ".journal-next"), []byte("interrupted journal write"), 0600)
			}
			return "", nil
		}
		t.Fatalf("unexpected tool %s", name)
		return "", nil
	}
	return f
}

func TestSystemFinalJournalFailureRetainsLock(t *testing.T) {
	f := newSystemFixture(t)
	f.fault = "final-journal"
	err := f.run(context.Background(), "install", systemArgs()...)
	if err == nil || !strings.Contains(err.Error(), "no rollback claimed") || !strings.Contains(err.Error(), "journal save error=") {
		t.Fatal("journal failure lost", err)
	}
	if !f.hasCommand("apt-get") || !f.hasCommand("rmdir") || f.version != f.a.Version {
		t.Fatal("fixture never reached successful APT and cleanup")
	}
	if _, err := os.Lstat(f.b.root(hermesSystem) + ".lock"); err != nil {
		t.Fatal("uncertain lock lost", err)
	}
	var rec systemJournal
	if err := readJSON(filepath.Join(f.b.root(hermesSystem), "journal.json"), &rec); err != nil || rec.Phase != "root-cleanup" {
		t.Fatal("last durable in-flight phase lost", rec, err)
	}
	calls := len(f.calls)
	f.fault = ""
	if err := f.run(context.Background(), "install", systemArgs()...); err == nil {
		t.Fatal("blocked journal automatically retried")
	}
	for _, c := range f.calls[calls:] {
		if c.elevated {
			t.Fatal("automatic privileged recovery")
		}
	}
}

func TestSystemRootCallerUsesLiteralToolsWithoutSudo(t *testing.T) {
	f := newSystemFixture(t)
	f.b.system.uid = func() int { return 0 }
	f.b.system.resolve = func(name string) (string, error) {
		if name == "sudo" {
			t.Fatal("root caller requested sudo")
		}
		return "/trusted/" + name, nil
	}
	mustRun(t, f.fixture, "install", systemArgs()...)
	if !f.hasCommand("apt-get") || !f.hasCommand("rmdir") {
		t.Fatal("root fixture lifecycle incomplete")
	}
}

func systemArgs() []string {
	return []string{"--provider", hermesSystem, "--trust-download", "--accept-system-packages"}
}
func (f *systemFixture) hasCommand(name string) bool {
	for _, c := range f.calls {
		if c.name == name {
			return true
		}
	}
	return false
}

func TestSystemInstallTransactionAndExistingVersion(t *testing.T) {
	for _, existing := range []string{"", "0.7.6", "0.7.7", "0.8.0"} {
		t.Run("existing-"+existing, func(t *testing.T) {
			f := newSystemFixture(t)
			f.version = existing
			if existing != "" {
				f.arch = "amd64"
			}
			err := f.run(context.Background(), "install", systemArgs()...)
			if existing == "0.8.0" {
				if err == nil || f.hasCommand("dpkg-deb") {
					t.Fatal("downgrade accepted", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if existing == "0.7.7" {
				if f.hasCommand("dpkg-deb") || f.hasCommand("apt-get") {
					t.Fatal("existing package changed")
				}
				return
			}
			var names []string
			for _, c := range f.calls {
				names = append(names, c.name)
			}
			want := []string{"apt-config", "dpkg-deb", "mktemp", "install", "sha256sum", "dpkg-deb", "apt-config", "apt-get", "rm", "rmdir"}
			if !reflect.DeepEqual(names, want) {
				t.Fatal(names)
			}
			rec, err := f.b.systemRecord()
			if err != nil || rec == nil || rec.Phase != "complete" {
				t.Fatal(rec, err)
			}
			if _, err = os.Lstat(f.b.root(hermesSystem) + ".lock"); !os.IsNotExist(err) {
				t.Fatal("lock retained", err)
			}
			if _, err = os.Lstat(rec.Input); !os.IsNotExist(err) {
				t.Fatal("input retained on success", err)
			}
			if _, err = os.Lstat(filepath.Join(f.b.root(hermesSystem), "current")); !os.IsNotExist(err) {
				t.Fatal("system adopted as portable")
			}
			calls := len(f.calls)
			f.b.req.Operation = "status"
			f.b.req.Arguments = nil
			if _, err = f.b.execute(context.Background()); err != nil {
				t.Fatal(err)
			}
			if len(f.calls) != calls {
				t.Fatal("status probes system")
			}
		})
	}
}

func TestSystemConsentAndRoutesHaveNoEffects(t *testing.T) {
	cases := [][]string{
		{"--provider", hermesSystem}, {"--provider", hermesSystem, "--trust-download"}, {"--provider", hermesSystem, "--accept-system-packages"},
		{"--provider", hermesSystem, "--trust-download", "--accept-system-packages=true"},
		append(systemArgs(), "--accept-system-packages"), append(systemArgs(), "--version", "../invalid"),
		{"--provider", "picoclaw", "--trust-download", "--accept-system-packages"},
	}
	for i, args := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			f := newSystemFixture(t)
			if err := f.run(context.Background(), "install", args...); err == nil {
				t.Fatal("accepted", args)
			}
			if len(f.calls) != 0 || f.fixture.calls != 0 || f.reads != 0 {
				t.Fatal("no-consent effects")
			}
			if _, err := os.Lstat(f.b.req.AppsDir); !os.IsNotExist(err) {
				t.Fatal("no-consent writes")
			}
		})
	}
	for _, op := range []string{"update", "use", "remove", "uninstall"} {
		f := newSystemFixture(t)
		if err := f.run(context.Background(), op, "--provider", hermesSystem, "--version", "0.7.7"); err == nil {
			t.Fatal("unsupported route accepted", op)
		}
		if len(f.calls) != 0 || f.fixture.calls != 0 {
			t.Fatal("unsupported route effects")
		}
	}
	f := newSystemFixture(t)
	f.b.arch = "arm64"
	if err := f.run(context.Background(), "install", systemArgs()...); err == nil || len(f.calls) != 0 || f.fixture.calls != 0 {
		t.Fatal("arm64 system fallback", err)
	}
}

func TestSystemMenusOfflineAndSeparate(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		f := newSystemFixture(t)
		f.b.arch = arch
		for _, id := range []string{"", hermesSystem} {
			choices, err := f.b.menu(context.Background(), id)
			if err != nil {
				t.Fatal(err)
			}
			if err = protocol.ValidateChoices(choices); err != nil {
				t.Fatal(err)
			}
			for _, c := range choices {
				args := append([]string{}, c.Arguments...)
				if c.Prompt != "" {
					args = append(args, "0.7.7")
				}
				if _, err = parseOptions(c.Operation, args); err != nil {
					t.Fatal(err, c)
				}
				if id == hermesSystem && c.Operation == "install" && (!slices.Contains(args, "--trust-download") || !slices.Contains(args, "--accept-system-packages")) {
					t.Fatal("missing menu consent")
				}
			}
			if id == hermesSystem && arch == "arm64" && len(choices) != 1 {
				t.Fatal("unsupported install choice")
			}
		}
		if err := f.run(context.Background(), "status"); err != nil {
			t.Fatal(err)
		}
		if len(f.calls) != 0 || f.fixture.calls != 0 || f.reads != 0 || f.policies != 0 {
			t.Fatal("offline views run checks")
		}
		if _, err := os.Lstat(f.b.req.AppsDir); !os.IsNotExist(err) {
			t.Fatal("offline writes")
		}
	}
}

func TestSystemFailuresRetainJournalAndNeverRollback(t *testing.T) {
	for _, fault := range []string{"apt-config", "pins", "preferences-path", "dpkg-deb", "metadata", "mktemp", "stage-path", "stage", "install", "sha256sum", "copy-changed", "root-metadata", "root-fields", "pins-changed", "concurrent", "apt-get", "postcondition", "rm", "rmdir"} {
		t.Run(fault, func(t *testing.T) {
			f := newSystemFixture(t)
			f.fault = fault
			if err := f.run(context.Background(), "install", systemArgs()...); err == nil {
				t.Fatal("fault accepted")
			}
			if fault == "apt-config" || fault == "pins" || fault == "preferences-path" {
				if f.fixture.calls != 0 {
					t.Fatal("policy failure network")
				}
				return
			}
			rec, err := f.b.systemRecord()
			if err != nil || rec == nil || rec.Phase == "complete" {
				t.Fatal("false success", rec, err)
			}
			privileged := f.hasCommand("mktemp")
			if privileged && rec.Phase != "unknown" {
				t.Fatal("uncertain result not recorded", rec)
			}
			if privileged {
				if _, err = os.Lstat(f.b.root(hermesSystem) + ".lock"); err != nil {
					t.Fatal("uncertain lock lost")
				}
			}
			if !slices.Contains([]string{"apt-get", "postcondition", "rm", "rmdir"}, fault) && f.hasCommand("apt-get") {
				t.Fatal("APT after failed guard")
			}
			for _, c := range f.calls {
				if c.name == "apt-get" && slices.Contains(c.args, "remove") {
					t.Fatal("rollback mutation")
				}
			}
			calls := len(f.calls)
			f.fault = ""
			if err = f.run(context.Background(), "install", systemArgs()...); err == nil {
				t.Fatal("interrupted state retried")
			}
			for _, c := range f.calls[calls:] {
				if c.elevated {
					t.Fatal("automatic privileged recovery")
				}
			}
		})
	}
}

func TestSystemCancellationRetainsPrivilegedStaging(t *testing.T) {
	for _, phase := range []string{"mktemp", "install", "sha256sum", "root-metadata", "apt-get", "rm", "rmdir"} {
		t.Run(phase, func(t *testing.T) {
			f := newSystemFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f.cancel = cancel
			f.fault = "cancel-" + phase
			if err := f.run(ctx, "install", systemArgs()...); !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			rec, err := f.b.systemRecord()
			if err != nil || rec == nil || rec.Phase != "unknown" {
				t.Fatal(rec, err)
			}
			if _, err = os.Lstat(f.b.root(hermesSystem) + ".lock"); err != nil {
				t.Fatal("lock lost", err)
			}
			if f.calls[len(f.calls)-1].name != map[string]string{"root-metadata": "dpkg-deb", "mktemp": "mktemp", "install": "install", "sha256sum": "sha256sum", "apt-get": "apt-get", "rm": "rm", "rmdir": "rmdir"}[phase] {
				t.Fatal("cleanup after cancel")
			}
		})
	}
}

func TestSystemReleaseAndDownloadValidation(t *testing.T) {
	for _, fault := range []string{"hash", "short", "long", "asset", "digest", "duplicate", "tag", "arch"} {
		t.Run(fault, func(t *testing.T) {
			f := newSystemFixture(t)
			var r release
			if err := json.Unmarshal(f.responses[f.latest], &r); err != nil {
				t.Fatal(err)
			}
			switch fault {
			case "hash":
				f.responses[f.a.URL] = []byte(strings.Repeat("x", int(f.a.Size)))
			case "short":
				f.responses[f.a.URL] = []byte("short")
			case "long":
				f.responses[f.a.URL] = append(f.responses[f.a.URL], byte('x'))
			case "asset":
				r.Assets[0].Name = "wrong.deb"
			case "digest":
				r.Assets[0].Digest = ""
			case "duplicate":
				r.Assets = append(r.Assets, r.Assets[0])
			case "tag":
				r.Tag = "v0.7.8"
			case "arch":
				r.Assets[0].Name = "hermes-desktop_0.7.7_arm64.deb"
			}
			data, _ := json.Marshal(r)
			f.responses[f.latest] = data
			if err := f.run(context.Background(), "install", systemArgs()...); err == nil {
				t.Fatal("invalid release accepted")
			}
			if f.hasCommand("dpkg-deb") || f.hasCommand("mktemp") || f.hasCommand("apt-get") {
				t.Fatal("unverified bytes used")
			}
		})
	}
	for _, version := range []string{"latest", "0.7.7"} {
		f := newSystemFixture(t)
		mustRun(t, f.fixture, "install", append(systemArgs(), "--version", version)...)
	}
}

func TestSystemMetadataParsersAndAPTPolicy(t *testing.T) {
	good := "Package: hermes-desktop\nVersion: 0.7.7\nArchitecture: amd64\n"
	a := artifact{Version: "0.7.7", Arch: "amd64"}
	if err := verifyDEBFields(good, a); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{strings.Replace(good, "amd64", "arm64", 1), strings.Replace(good, "0.7.7", "0.7.7-1", 1), strings.Replace(good, "hermes-desktop", "unrelated", 1), good + "Package: hermes-desktop\n", good + "Unexpected: x\n", good + "\n"} {
		if err := verifyDEBFields(bad, a); err == nil {
			t.Fatal("bad DEB metadata accepted", bad)
		}
	}
	installed := good + "Status: install ok installed\n"
	for _, suffix := range []string{"", "\n"} {
		v, arch, err := parseInstalledHermes([]byte(installed + suffix))
		if err != nil || v != "0.7.7" || arch != "amd64" {
			t.Fatal(v, arch, err)
		}
	}
	for _, bad := range []string{strings.Replace(installed, "install ok installed", "hold ok installed", 1), strings.Replace(installed, "install ok installed", "install ok unpacked", 1), strings.Replace(installed, "0.7.7", "1:0.7.7", 1), installed + "\n" + installed, installed + "Version: 0.7.7\n"} {
		if _, _, err := parseInstalledHermes([]byte(bad)); err == nil {
			t.Fatal("unsupported installed state", bad)
		}
	}
	if !emptyAPTPreferences([]byte("# comment\n \n")) || emptyAPTPreferences([]byte("Package: *\nPin: version *\nPin-Priority: -1\n")) {
		t.Fatal("APT preferences bypass")
	}
	args := systemAPTArgs("/run/test.deb")
	for _, required := range []string{"--no-remove", "APT::Get::allow-Downgrades=false", "APT::Get::allow-Change-Held-Packages=false", "APT::Get::AllowUnauthenticated=false"} {
		if !slices.Contains(args, required) {
			t.Fatal("missing policy guard", required)
		}
	}
	for _, arg := range args {
		if strings.Contains(arg, "Dir::Etc") || strings.Contains(arg, "force") || strings.Contains(arg, "=true") {
			t.Fatal("unexpected policy override", arg)
		}
	}
}

func TestSystemJournalDoesNotAuthorizeCleanup(t *testing.T) {
	f := newSystemFixture(t)
	mustRun(t, f.fixture, "install", systemArgs()...)
	rec, err := f.b.systemRecord()
	if err != nil {
		t.Fatal(err)
	}
	rec.Phase = "unknown"
	rec.Staging = "/etc"
	rec.Input = "/home/someone"
	if err = f.b.saveSystemRecord(*rec); err != nil {
		t.Fatal(err)
	}
	calls := len(f.calls)
	if err = f.run(context.Background(), "status"); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != calls {
		t.Fatal("status execution")
	}
	if err = f.run(context.Background(), "install", systemArgs()...); err == nil {
		t.Fatal("unsafe recovery")
	}
	for _, c := range f.calls[calls:] {
		if c.elevated {
			t.Fatal("journal authorized privileged cleanup")
		}
	}
}
