//go:build linux

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/AtomyCloud/atm/internal/protocol"
	"golang.org/x/sys/unix"
)

const codexFixtureStage = "/run/atm-ai-codex-desktop-0123456789"

type codexDesktopFixture struct {
	*fixture
	pkg                    codexPackageRecord
	version, digest, fault string
	rootFiles              map[string][]byte
	calls                  []systemCall
	reads, policies        int
	cancel                 context.CancelFunc
	guardRecord            func() (codexPackageRecord, error)
}

func newCodexDesktopFixture(t *testing.T, arch string) *codexDesktopFixture {
	t.Helper()
	f := &codexDesktopFixture{fixture: newFixture(t, arch), version: "1:2026.1028.1-1", rootFiles: map[string][]byte{}}
	self := filepath.Join(filepath.Dir(f.b.req.AppsDir), "synthetic-backend")
	writeFixture(t, self, []byte("synthetic native guard image; not executed"), 0600)
	payload := []byte("synthetic ChatGPT DEB fixture; never executable")
	f.digest = fmt.Sprintf("%x", sha256.Sum256(payload))
	url, err := codexDesktopURL(arch)
	if err != nil {
		t.Fatal(err)
	}
	f.responses[url] = payload
	s := &systemTools{
		resolve: func(n string) (string, error) { return "/trusted/" + n, nil },
		uid:     func() int { return 1000 },
		stage: func(path string) error {
			if path != codexFixtureStage || f.fault == "stage" {
				return errors.New("unsafe stage")
			}
			return nil
		},
		policy: func() error {
			f.policies++
			if f.fault == "pins" || f.fault == "pins-changed" && f.policies == 2 {
				return errors.New("custom preferences")
			}
			return nil
		},
	}
	f.b.codexDesktop = &codexDesktopTools{commands: s, configFiles: func() error { return nil }, executable: func() (*os.File, error) { return os.Open(self) }, packageRecord: func() (codexPackageRecord, error) {
		f.reads++
		if f.fault == "db" {
			return codexPackageRecord{}, os.ErrNotExist
		}
		if f.fault == "concurrent" && f.reads == 2 {
			return codexPackageRecord{Present: true, Status: "hold ok installed"}, nil
		}
		if f.fault == "postcondition" && f.reads == 3 {
			return codexPackageRecord{}, nil
		}
		return f.pkg, nil
	}}
	s.run = func(ctx context.Context, ex string, args []string) (string, error) {
		elevated := ex == "/trusted/sudo"
		if elevated {
			prefix := append([]string{"-n", "--", "/trusted/env", "-i"}, codexSystemEnvironment()...)
			if len(args) <= len(prefix) || !reflect.DeepEqual(args[:len(prefix)], prefix) {
				t.Fatal("unsafe sudo", args)
			}
			ex, args = args[len(prefix)], args[len(prefix)+1:]
		}
		name := strings.TrimPrefix(ex, "/trusted/")
		if strings.Contains(name, "/") {
			t.Fatal("unexpected executable", ex)
		}
		f.calls = append(f.calls, systemCall{name, slices.Clone(args), elevated})
		phase := name
		if name == "dpkg-deb" && elevated {
			phase = "root-metadata"
		}
		if f.fault == phase || f.fault == "cancel-"+phase {
			if f.cancel != nil && strings.HasPrefix(f.fault, "cancel-") {
				f.cancel()
				return "", ctx.Err()
			}
			return "", errors.New("synthetic command failure")
		}
		target := codexFixtureStage + "/chatgpt.deb"
		switch name {
		case "apt-config":
			if reflect.DeepEqual(args, codexConfigDumpArgs) {
				if elevated {
					t.Fatal("unexpected elevated preflight")
				}
				data := "Dir=/\nDir::State=var/lib/apt\nDir::State::status=/var/lib/dpkg/status\nDir::Bin::dpkg=/trusted/dpkg\n"
				if f.fault == "redirect-config" {
					data += "DPkg::Options::=--root%3d/tmp/foreign\n"
				}
				if f.fault == "preinvoke" {
					data += "DPkg::Pre-Invoke::=touch /tmp/not-run\n"
				}
				return data, nil
			}
			if elevated || !reflect.DeepEqual(args, []string{"shell", "ATM_PREF", "Dir::Etc::preferences/f", "ATM_PARTS", "Dir::Etc::preferencesparts/d"}) {
				t.Fatal("unsafe apt-config", args)
			}
			if f.fault == "preferences-path" {
				return "ATM_PREF='/other'\nATM_PARTS='/etc/apt/preferences.d/'\n", nil
			}
			return "ATM_PREF='/etc/apt/preferences'\nATM_PARTS='/etc/apt/preferences.d/'\n", nil
		case "dpkg-deb":
			if len(args) != 5 || args[0] != "--field" || !reflect.DeepEqual(args[2:], []string{"Package", "Version", "Architecture"}) {
				t.Fatal("unsafe metadata", args)
			}
			if elevated && args[1] != target {
				t.Fatal("wrong root metadata target")
			}
			out := "Package: chatgpt\nVersion: " + f.version + "\nArchitecture: " + arch + "\n"
			if f.fault == "metadata" || f.fault == "root-fields" && elevated {
				out = strings.Replace(out, "chatgpt", "codex", 1)
			}
			if f.fault == "root-version" && elevated {
				out = strings.Replace(out, f.version, "2.0.0", 1)
			}
			return out, nil
		case "mktemp":
			if !elevated || !reflect.DeepEqual(args, []string{"-d", "/run/atm-ai-codex-desktop-XXXXXXXXXX"}) {
				t.Fatal("unsafe mktemp", args)
			}
			if f.fault == "stage-path" {
				return "/tmp/untrusted\n", nil
			}
			return codexFixtureStage + "\n", nil
		case "install":
			if !elevated || len(args) != 6 {
				t.Fatal("unsafe copy", args)
			}
			name := filepath.Base(args[5])
			mode := "0600"
			if name == codexGuardName {
				mode = "0500"
			}
			if !reflect.DeepEqual(args[:4], []string{"-T", "-m", mode, "--"}) || args[5] != codexFixtureStage+"/"+name || !slices.Contains([]string{"chatgpt.deb", codexGuardName, "guard.json"}, name) {
				t.Fatal("unsafe copy", args)
			}
			info, err := os.Stat(args[4])
			if err != nil || info.Mode().Perm() != 0600 {
				t.Fatal("unsafe local file", err)
			}
			data, err := os.ReadFile(args[4])
			if err != nil {
				t.Fatal(err)
			}
			f.rootFiles[args[5]] = data
			if f.fault == "guard-copy-changed" && name == codexGuardName {
				f.rootFiles[args[5]] = []byte("changed guard")
			}
			return "", nil
		case "sha256sum":
			if !elevated || len(args) != 2 || args[0] != "--" || filepath.Dir(args[1]) != codexFixtureStage {
				t.Fatal("unsafe hash", args)
			}
			data, ok := f.rootFiles[args[1]]
			if !ok {
				return "", os.ErrNotExist
			}
			h := fmt.Sprintf("%x", sha256.Sum256(data))
			if f.fault == "copy-changed" {
				h = strings.Repeat("0", 64)
			}
			return h + "  " + args[1] + "\n", nil
		case "apt-get":
			if !elevated || !reflect.DeepEqual(args, codexTransactionAPTArgs(codexFixtureStage, f.b.req.Operation)) {
				t.Fatal("unsafe apt-get", args)
			}
			guard := codexAPTGuardTools{
				stage: s.stage, lock: func() error {
					if f.fault == "guard-lock" {
						return errors.New("no APT frontend lock")
					}
					return nil
				},
				policy: func(context.Context) error { return nil },
				packageRecord: func() (codexPackageRecord, error) {
					if f.guardRecord != nil {
						return f.guardRecord()
					}
					return f.pkg, nil
				},
				read: func(path string, _ os.FileMode, _ int64) ([]byte, error) {
					data, ok := f.rootFiles[path]
					if !ok {
						return nil, os.ErrNotExist
					}
					return slices.Clone(data), nil
				},
				hash: func(path string, _ os.FileMode, _ int64) (int64, string, error) {
					data, ok := f.rootFiles[path]
					if !ok {
						return 0, "", os.ErrNotExist
					}
					return int64(len(data)), fmt.Sprintf("%x", sha256.Sum256(data)), nil
				},
				fields: func(context.Context, string) (string, error) {
					return "Package: chatgpt\nVersion: " + f.version + "\nArchitecture: " + arch + "\n", nil
				},
				marker: func(path string, data []byte) error {
					if _, ok := f.rootFiles[path]; ok {
						return os.ErrExist
					}
					f.rootFiles[path] = slices.Clone(data)
					return nil
				},
			}
			if f.fault == "late-package" {
				f.pkg = codexPackageRecord{Present: true, Status: "install ok unpacked"}
			}
			if f.fault != "apt-noop" {
				if f.fault != "skip-early" {
					if err := runCodexAPTGuardPhase(ctx, codexFixtureStage, strings.NewReader(""), guard, true); err != nil {
						return "", err
					}
				}
				if f.fault == "guard-repeat" {
					if err := runCodexAPTGuardPhase(ctx, codexFixtureStage, strings.NewReader(""), guard, true); err != nil {
						return "", err
					}
				}
				if f.fault != "skip-final" {
					if err := runCodexAPTGuard(ctx, codexFixtureStage, strings.NewReader(target+"\n"), guard); err != nil {
						return "", err
					}
				}
			}
			f.pkg = codexPackageRecord{true, "install ok installed", f.version, arch}
			if f.fault == "missing-early-ack" {
				delete(f.rootFiles, codexFixtureStage+"/guard-early")
			}
			if f.fault == "missing-final-ack" {
				delete(f.rootFiles, codexFixtureStage+"/guard-ok")
			}
			return "synthetic package installation\n", nil
		case "rm", "rmdir":
			want := ""
			if len(args) == 2 && slices.Contains([]string{"chatgpt.deb", codexGuardName, "guard.json", "guard-early", "guard-ok"}, filepath.Base(args[1])) {
				want = codexFixtureStage + "/" + filepath.Base(args[1])
			}
			if name == "rmdir" {
				want = codexFixtureStage
			}
			if !elevated || !reflect.DeepEqual(args, []string{"--", want}) {
				t.Fatal("unsafe cleanup", args)
			}
			if name == "rm" {
				delete(f.rootFiles, args[1])
			}
			if name == "rmdir" && f.fault == "final-journal" {
				writeFixture(t, filepath.Join(f.b.root(codexDesktopSystem), ".journal-next"), []byte("interrupted"), 0600)
			}
			return "", nil
		}
		t.Fatalf("unexpected command %s", name)
		return "", nil
	}
	return f
}

func codexDesktopArgs() []string {
	return []string{"--provider", codexDesktopSystem, "--trust-download", "--accept-system-packages"}
}
func (f *codexDesktopFixture) hasCommand(name string) bool {
	for _, c := range f.calls {
		if c.name == name {
			return true
		}
	}
	return false
}

func TestCodexDesktopSyntheticInstall(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		t.Run(arch, func(t *testing.T) {
			f := newCodexDesktopFixture(t, arch)
			mustRun(t, f.fixture, "install", codexDesktopArgs()...)
			var names []string
			for _, c := range f.calls {
				names = append(names, c.name)
			}
			if !reflect.DeepEqual(names, []string{"apt-config", "apt-config", "dpkg-deb", "mktemp", "install", "sha256sum", "dpkg-deb", "install", "sha256sum", "install", "sha256sum", "apt-config", "apt-config", "apt-get", "sha256sum", "sha256sum", "rm", "rm", "rm", "rm", "rm", "rmdir"}) {
				t.Fatal(names)
			}
			rec, err := f.b.codexDesktopRecord()
			if err != nil || rec == nil || rec.Phase != "complete" || rec.LastPhase != "complete" || rec.SHA256 != f.digest || rec.Version != f.version || rec.Arch != arch || rec.Trust != codexDesktopTrust || rec.Size == 0 {
				t.Fatal(rec, err)
			}
			for _, p := range []string{f.b.root(codexDesktopSystem) + ".lock", rec.Input, filepath.Join(f.b.root(codexDesktopSystem), "current")} {
				if _, err := os.Lstat(p); !os.IsNotExist(err) {
					t.Fatal("unexpected retained path", p, err)
				}
			}
			calls, downloads := len(f.calls), f.fixture.calls
			if err := f.run(context.Background(), "install", codexDesktopArgs()...); err == nil {
				t.Fatal("existing install adopted")
			}
			if len(f.calls) != calls || f.fixture.calls != downloads {
				t.Fatal("existing package caused work")
			}
		})
	}
}

func TestCodexDesktopRefusalsBeforeEffects(t *testing.T) {
	for _, args := range [][]string{
		{"--provider", codexDesktopSystem}, {"--provider", codexDesktopSystem, "--trust-download"}, {"--provider", codexDesktopSystem, "--accept-system-packages"},
		append(codexDesktopArgs(), "--version", "1.2.3"), append(codexDesktopArgs(), "--version", "https://evil.test/app.deb"),
		append(codexDesktopArgs(), "--accept-system-packages=true"), append(codexDesktopArgs(), "--rocm"), append(codexDesktopArgs(), "--accept-dependency-execution"),
	} {
		f := newCodexDesktopFixture(t, "amd64")
		if err := f.run(context.Background(), "install", args...); err == nil {
			t.Fatal("accepted", args)
		}
		if len(f.calls) != 0 || f.fixture.calls != 0 || f.reads != 0 {
			t.Fatal("refusal side effects")
		}
		if _, err := os.Lstat(f.b.req.AppsDir); !os.IsNotExist(err) {
			t.Fatal("refusal wrote files")
		}
	}
	for _, op := range []string{"update", "remove", "uninstall", "use", "integrate"} {
		f := newCodexDesktopFixture(t, "amd64")
		if err := f.run(context.Background(), op, "--provider", codexDesktopSystem); err == nil || len(f.calls) != 0 || f.fixture.calls != 0 {
			t.Fatal("unsupported operation", op, err)
		}
	}
	f := newCodexDesktopFixture(t, "amd64")
	f.b.arch = "386"
	if err := f.run(context.Background(), "install", codexDesktopArgs()...); err == nil || len(f.calls) != 0 || f.reads != 0 {
		t.Fatal("unsupported arch", err)
	}
}

func TestCodexDesktopExistingAndUnknownPreserved(t *testing.T) {
	for _, status := range []string{"install ok installed", "hold ok installed", "install ok unpacked", "deinstall ok config-files", "install reinstreq half-installed", "purge ok not-installed", ""} {
		f := newCodexDesktopFixture(t, "amd64")
		f.pkg = codexPackageRecord{Present: true, Status: status}
		if err := f.run(context.Background(), "install", codexDesktopArgs()...); err == nil {
			t.Fatal("existing state accepted", status)
		}
		if len(f.calls) != 0 || f.fixture.calls != 0 {
			t.Fatal("existing state caused work")
		}
	}
	f := newCodexDesktopFixture(t, "amd64")
	f.fault = "db"
	if err := f.run(context.Background(), "install", codexDesktopArgs()...); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatal(err)
	}
	if len(f.calls) != 0 || f.fixture.calls != 0 {
		t.Fatal("missing DB treated as absent")
	}
}

func TestCodexDesktopOfflineMenusAndStatus(t *testing.T) {
	for _, fault := range []string{"", "db"} {
		f := newCodexDesktopFixture(t, "amd64")
		f.fault = fault
		var logs []string
		f.b.log = func(s string) { logs = append(logs, s) }
		choices, err := f.b.menu(context.Background(), codexDesktopSystem)
		if err != nil {
			t.Fatal(err)
		}
		if err := protocol.ValidateChoices(choices); err != nil {
			t.Fatal(err)
		}
		if fault == "db" && len(choices) != 1 || fault == "" && len(choices) != 2 {
			t.Fatal(choices)
		}
		for _, c := range choices {
			if _, err := parseOptions(c.Operation, c.Arguments); err != nil {
				t.Fatal(err)
			}
		}
		if err := f.run(context.Background(), "status"); err != nil {
			t.Fatal("unknown broke aggregate status", err)
		}
		if len(f.calls) != 0 || f.fixture.calls != 0 {
			t.Fatal("offline view ran commands/network")
		}
		if _, err := os.Lstat(f.b.req.AppsDir); !os.IsNotExist(err) {
			t.Fatal("offline view wrote files")
		}
		joined := strings.Join(logs, "\n")
		for _, want := range []string{"continuity", "sudo -n", "Pending"} {
			if !strings.Contains(joined, want) {
				t.Fatal("missing disclosure", want)
			}
		}
	}
}

func TestCodexDesktopFailureRetention(t *testing.T) {
	for _, fault := range []string{"apt-config", "pins", "preferences-path", "dpkg-deb", "metadata", "mktemp", "stage-path", "stage", "install", "sha256sum", "copy-changed", "root-metadata", "root-fields", "root-version", "pins-changed", "concurrent", "apt-get", "postcondition", "rm", "rmdir", "final-journal"} {
		t.Run(fault, func(t *testing.T) {
			f := newCodexDesktopFixture(t, "amd64")
			f.fault = fault
			if err := f.run(context.Background(), "install", codexDesktopArgs()...); err == nil {
				t.Fatal("fault accepted")
			}
			if slices.Contains([]string{"apt-config", "pins", "preferences-path"}, fault) {
				if f.fixture.calls != 0 {
					t.Fatal("network before policy")
				}
				return
			}
			privileged := f.hasCommand("mktemp")
			_, lockErr := os.Lstat(f.b.root(codexDesktopSystem) + ".lock")
			if privileged && lockErr != nil || !privileged && !os.IsNotExist(lockErr) {
				t.Fatal("wrong lock retention", lockErr)
			}
			rec, err := f.b.codexDesktopRecord()
			if fault == "final-journal" {
				if err == nil {
					t.Fatal("interrupted journal accepted")
				}
			} else if err != nil || rec == nil || privileged && rec.Phase != "unknown" || !privileged && rec.Phase != "failed" {
				t.Fatal(rec, err)
			}
			if !slices.Contains([]string{"apt-get", "postcondition", "rm", "rmdir", "final-journal"}, fault) && f.hasCommand("apt-get") {
				t.Fatal("APT after guard failure")
			}
			calls := len(f.calls)
			f.fault = ""
			if err := f.run(context.Background(), "install", codexDesktopArgs()...); err == nil {
				t.Fatal("automatic retry")
			}
			for _, c := range f.calls[calls:] {
				if c.elevated {
					t.Fatal("replay cleanup/mutation")
				}
			}
		})
	}
}

func TestCodexDesktopCancellation(t *testing.T) {
	for _, phase := range []string{"dpkg-deb", "mktemp", "install", "sha256sum", "root-metadata", "apt-get", "rm", "rmdir"} {
		t.Run(phase, func(t *testing.T) {
			f := newCodexDesktopFixture(t, "amd64")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f.cancel, f.fault = cancel, "cancel-"+phase
			if err := f.run(ctx, "install", codexDesktopArgs()...); !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			rec, err := f.b.codexDesktopRecord()
			want := "unknown"
			if phase == "dpkg-deb" {
				want = "failed"
			}
			if err != nil || rec == nil || rec.Phase != want {
				t.Fatal(rec, err)
			}
			if f.calls[len(f.calls)-1].name != map[string]string{"dpkg-deb": "dpkg-deb", "root-metadata": "dpkg-deb", "mktemp": "mktemp", "install": "install", "sha256sum": "sha256sum", "apt-get": "apt-get", "rm": "rm", "rmdir": "rmdir"}[phase] {
				t.Fatal("work after cancellation")
			}
		})
	}
}

func TestCodexDesktopFixedURLAndLengths(t *testing.T) {
	f := newCodexDesktopFixture(t, "amd64")
	url, _ := codexDesktopURL("amd64")
	for _, bad := range []string{strings.Replace(url, "https:", "http:", 1), url + "?x=1", url + "#x", strings.Replace(url, "persistent.oaistatic.com", "evil.test", 1), strings.Replace(url, "latest/", "1.2.3/", 1), strings.Replace(url, "amd64", "arm64", 1), strings.Replace(url, "persistent.oaistatic.com", "persistent.oaistatic.com:443", 1)} {
		if _, _, err := f.b.downloadCodexDesktop(context.Background(), bad, io.Discard); err == nil {
			t.Fatal("URL accepted", bad)
		}
	}
	if f.fixture.calls != 0 {
		t.Fatal("invalid URL caused network")
	}
	for _, length := range []int64{-1, 0, 2, 4, codexDesktopMaxDownload + 1} {
		f.b.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, ContentLength: length, Body: io.NopCloser(strings.NewReader("abc")), Header: make(http.Header), Request: r}, nil
		})}
		if _, _, err := f.b.downloadCodexDesktop(context.Background(), url, io.Discard); err == nil {
			t.Fatal("invalid length", length)
		}
	}
	for _, destination := range []string{url, "https://evil.test/payload", "/other.deb"} {
		calls := 0
		f.b.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: 302, ContentLength: 0, Header: http.Header{"Location": {destination}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
		})}
		if _, _, err := f.b.downloadCodexDesktop(context.Background(), url, io.Discard); err == nil || calls != 1 {
			t.Fatal("redirect followed", err, calls)
		}
	}
	f = newCodexDesktopFixture(t, "amd64")
	var out bytes.Buffer
	n, h, err := f.b.downloadCodexDesktop(context.Background(), url, &out)
	if err != nil || n != int64(out.Len()) || h != f.digest {
		t.Fatal(n, h, err)
	}
}

func TestCodexDesktopParsersAndReplayPaths(t *testing.T) {
	good := "Package: chatgpt\nStatus: hold ok installed\nVersion: 2026.1028.1-1\nArchitecture: arm64\nDescription: example\n continuation\n"
	rec, err := parseCodexPackageRecord([]byte(good))
	if err != nil || !rec.Present || rec.Status != "hold ok installed" || rec.Arch != "arm64" {
		t.Fatal(rec, err)
	}
	for _, bad := range []string{good + "\n" + good, good + "Package: other\n", "Package: chatgpt\n", "Package: other\nPackage: chatgpt\nStatus: install ok installed\n", "Package: chatgpt\n continuation\nStatus: install ok installed\n", "bad", strings.Repeat("x", 64<<10)} {
		if _, err := parseCodexPackageRecord([]byte(bad)); err == nil {
			t.Fatal("invalid record accepted")
		}
	}
	metadata := "Package: chatgpt\nVersion: 1:2026.1028.1-1\nArchitecture: amd64\n"
	if _, err := codexDEBFields(metadata, "amd64"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{metadata + "Package: chatgpt\n", strings.Replace(metadata, "chatgpt", "codex", 1), strings.Replace(metadata, "amd64", "arm64", 1), strings.Replace(metadata, "1:2026.1028.1-1", "../x", 1), metadata + "Unknown: x\n"} {
		if _, err := codexDEBFields(bad, "amd64"); err == nil {
			t.Fatal("invalid metadata accepted")
		}
	}
	f := newCodexDesktopFixture(t, "amd64")
	mustRun(t, f.fixture, "install", codexDesktopArgs()...)
	j, err := f.b.codexDesktopRecord()
	if err != nil {
		t.Fatal(err)
	}
	j.Phase, j.Input, j.Staging = "unknown", "/home/another-user", "/etc"
	if err := f.b.saveCodexDesktopRecord(*j); err != nil {
		t.Fatal(err)
	}
	f.pkg = codexPackageRecord{}
	calls := len(f.calls)
	f.b.codexDesktopStatus()
	if err := f.run(context.Background(), "install", codexDesktopArgs()...); err == nil {
		t.Fatal("replayed journal")
	}
	for _, c := range f.calls[calls:] {
		if c.elevated {
			t.Fatal("journal path cleanup authority")
		}
	}
}

func TestCodexDesktopGuardTransactionFailures(t *testing.T) {
	for _, fault := range []string{"guard-copy-changed", "guard-lock", "late-package", "guard-repeat", "apt-noop", "missing-early-ack", "missing-final-ack"} {
		t.Run(fault, func(t *testing.T) {
			f := newCodexDesktopFixture(t, "amd64")
			f.fault = fault
			if err := f.run(context.Background(), "install", codexDesktopArgs()...); err == nil {
				t.Fatal("guard failure accepted")
			}
			if f.hasCommand("rm") || f.hasCommand("rmdir") {
				t.Fatal("uncertain guard state cleaned")
			}
			rec, err := f.b.codexDesktopRecord()
			if err != nil || rec == nil || rec.Phase != "unknown" {
				t.Fatal(rec, err)
			}
			if _, err := os.Lstat(f.b.root(codexDesktopSystem) + ".lock"); err != nil {
				t.Fatal("guard failure lost lock", err)
			}
			if fault == "guard-copy-changed" && f.hasCommand("apt-get") {
				t.Fatal("changed elevated helper reached APT")
			}
			if fault == "late-package" && f.pkg.Status != "install ok unpacked" {
				t.Fatal("concurrently unpacked package changed")
			}
			if fault == "apt-noop" && !f.pkg.Present {
				t.Fatal("fixture did not simulate matching installed package")
			}
		})
	}
	for _, fault := range []string{"redirect-config", "preinvoke"} {
		f := newCodexDesktopFixture(t, "amd64")
		f.fault = fault
		if err := f.run(context.Background(), "install", codexDesktopArgs()...); err == nil {
			t.Fatal("unsafe config accepted", fault)
		}
		if f.fixture.calls != 0 || f.hasCommand("mktemp") {
			t.Fatal("config refusal occurred after download/privilege")
		}
	}
}

func TestCodexDesktopGuardLockRangeAndAncestry(t *testing.T) {
	good := unix.Flock_t{Type: unix.F_WRLCK, Whence: 0, Start: 0, Len: 0, Pid: 123}
	if !codexWholeWriteLock(good) {
		t.Fatal("whole-file lock rejected")
	}
	for _, change := range []func(*unix.Flock_t){
		func(l *unix.Flock_t) { l.Type = unix.F_UNLCK }, func(l *unix.Flock_t) { l.Type = unix.F_RDLCK },
		func(l *unix.Flock_t) { l.Start = 1 }, func(l *unix.Flock_t) { l.Len = 1 },
		func(l *unix.Flock_t) { l.Whence = 1 }, func(l *unix.Flock_t) { l.Pid = 1 },
	} {
		lk := good
		change(&lk)
		if codexWholeWriteLock(lk) {
			t.Fatal("partial/invalid lock accepted", lk)
		}
	}
	if pid, err := parseCodexRootParent("Name:\tapt-get\nPPid:\t123\nUid:\t0\t0\t0\t0\n"); err != nil || pid != 123 {
		t.Fatal(pid, err)
	}
	for _, bad := range []string{"PPid: 123\n", "Uid: 0 0 0 0\n", "PPid: 123\nUid: 0 1000 0 0\n", "PPid: 123\nUid: 0 0 0 0\nPPid: 999\n", "PPid: 0\nUid: 0 0 0 0\n"} {
		if _, err := parseCodexRootParent(bad); err == nil {
			t.Fatal("invalid ancestry accepted", bad)
		}
	}
}

func TestCodexDesktopGuardPlanAndStockHook(t *testing.T) {
	target := codexFixtureStage + "/chatgpt.deb"
	checks := 0
	dependency := func(string) error { checks++; return nil }
	if err := validateCodexAPTInput(target+"\n/var/cache/apt/archives/dependency.deb\n", target, dependency); err != nil || checks != 1 {
		t.Fatal(err, checks)
	}
	for _, bad := range []string{"", target, target + "\n" + target + "\n", "/var/cache/apt/archives/dependency.deb\n", target + "\n/tmp/foreign.deb\n", target + "\n/var/cache/apt/archives/../foreign.deb\n", "VERSION 2\n" + target + "\n"} {
		if err := validateCodexAPTInput(bad, target, dependency); err == nil {
			t.Fatal("unsafe hook input accepted", bad)
		}
	}
	const base = "Dir=/\nDir::State=var/lib/apt\nDir::State::status=/var/lib/dpkg/status\nDir::Bin::dpkg=/usr/bin/dpkg\n"
	if err := validateCodexAPTConfig(base+"DPkg::Pre-Install-Pkgs::=/usr/sbin/dpkg-preconfigure --apt || true\n", "/usr/bin/dpkg"); err != nil {
		t.Fatal("stock Debian preconfiguration hook rejected", err)
	}
	if err := validateCodexAPTConfig(base+"DPkg::Pre-Invoke::=/usr/bin/custom-hook\n", "/usr/bin/dpkg"); err == nil {
		t.Fatal("pre-guard hook accepted")
	}
	args := codexGuardAPTArgs(codexFixtureStage)
	for _, required := range []string{"DPkg::Pre-Invoke::=exec " + codexFixtureStage + "/" + codexGuardName + " " + codexGuardEarlyFlag, "DPkg::Pre-Install-Pkgs::=exec " + codexFixtureStage + "/" + codexGuardName + " " + codexGuardFlag, "DPkg::Tools::Options::exec::Version=1", "DPkg::Tools::Options::exec::InfoFD=0"} {
		if !slices.Contains(args, required) {
			t.Fatal("missing fixed native hook", required)
		}
	}
}

func TestCodexDesktopPendingDpkgUpdatesUnknown(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "updates")
	uid := uint32(os.Geteuid())
	if err := codexUpdatesEmpty(path, uid); err == nil {
		t.Fatal("missing updates directory accepted")
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := codexUpdatesEmpty(path, uid); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"0000", "tmp.i", "unexpected"} {
		p := filepath.Join(path, name)
		writeFixture(t, p, []byte("pending update; not read"), 0600)
		if err := codexUpdatesEmpty(path, uid); err == nil {
			t.Fatal("pending dpkg update treated as stable", name)
		}
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(path, 0777); err != nil {
		t.Fatal(err)
	}
	if err := codexUpdatesEmpty(path, uid); err == nil {
		t.Fatal("writable updates directory accepted")
	}
	if err := os.Chmod(path, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "updates-link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if err := codexUpdatesEmpty(link, uid); err == nil {
		t.Fatal("linked updates directory accepted")
	}
}
