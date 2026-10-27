package coreupdate

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/AtomyCloud/atm/internal/install"
)

type fixture struct {
	s                   *Service
	private             ed25519.PrivateKey
	m                   Manifest
	old, next, raw, sig []byte
}

func sum(b []byte) string { return fmt.Sprintf("%x", sha256.Sum256(b)) }

func binaryFixture(goos, arch string, marker byte) []byte {
	b := make([]byte, 128)
	b[100] = marker
	switch goos {
	case "linux":
		copy(b, "\x7fELF")
		b[4], b[5] = 2, 1
		machine := uint16(62)
		if arch == "arm64" {
			machine = 183
		}
		binary.LittleEndian.PutUint16(b[16:], 2)
		binary.LittleEndian.PutUint16(b[18:], machine)
	case "darwin":
		binary.LittleEndian.PutUint32(b, 0xfeedfacf)
		cpu := uint32(0x01000007)
		if arch == "arm64" {
			cpu = 0x0100000c
		}
		binary.LittleEndian.PutUint32(b[4:], cpu)
		binary.LittleEndian.PutUint32(b[12:], 2)
	case "windows":
		copy(b, "MZ")
		binary.LittleEndian.PutUint32(b[60:], 64)
		copy(b[64:], "PE\x00\x00")
		binary.LittleEndian.PutUint16(b[68:], 0x8664)
	}
	return b
}

func tempRoot(t *testing.T) string {
	t.Helper()
	// Explicit /tmp isolation, even when the surrounding process has a real HOME.
	base := "/tmp"
	if runtime.GOOS == "windows" {
		base = os.TempDir()
	}
	p, err := os.MkdirTemp(base, "atm-coreupdate-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(p) })
	p, err = filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func newFixture(t *testing.T, goos, arch string) *fixture {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{private: priv, old: binaryFixture(goos, arch, 1), next: binaryFixture(goos, arch, 2)}
	p := Policy{Root: tempRoot(t), PublicKey: pub, OS: goos, Arch: arch, CurrentVersion: "0.0.2", TargetVersion: "0.0.3", BaselineSHA256: sum(f.old), MaxBytes: 1024}
	f.s, err = New(p)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(p.Root, f.s.name()), f.old, 0755); err != nil {
		t.Fatal(err)
	}
	f.m = Manifest{1, Scope, OfficialSource + "/releases/download/v0.0.3/atm-0.0.3-" + goos + "-" + arch, "0.0.3", strings.Repeat("a", 40), goos, arch, true,
		map[string]install.File{f.s.name(): {SHA256: sum(f.next), Size: int64(len(f.next)), Mode: 0755}}}
	if goos == "windows" {
		f.m.Source += ".exe"
	}
	f.sign(t)
	return f
}

func (f *fixture) sign(t *testing.T) {
	t.Helper()
	var err error
	f.raw, err = json.Marshal(f.m)
	if err != nil {
		t.Fatal(err)
	}
	f.sig = ed25519.Sign(f.private, append([]byte(SignatureDomain), f.raw...))
}

func (f *fixture) stage(t *testing.T) {
	t.Helper()
	r, err := f.s.Stage(context.Background(), f.raw, f.sig, bytes.NewReader(f.next))
	if err != nil || r.State != "staged" {
		t.Fatalf("stage: %+v %v", r, err)
	}
}

func (f *fixture) assertActive(t *testing.T, want []byte) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(f.s.p.Root, f.s.name()))
	if err != nil || !bytes.Equal(b, want) {
		t.Fatalf("active binary mismatch: %v", err)
	}
}

func hostFixture(t *testing.T) *fixture {
	t.Helper()
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("replacement intentionally unsupported")
	}
	return newFixture(t, runtime.GOOS, runtime.GOARCH)
}

func TestTrustAndPolicy(t *testing.T) {
	if _, err := New(Policy{}); !errors.Is(err, ErrNoTrustAnchor) {
		t.Fatal(err)
	}
	var zero Service
	if _, err := zero.Verify(nil, nil); !errors.Is(err, ErrNoTrustAnchor) {
		t.Fatal(err)
	}
	if _, err := zero.Stage(context.Background(), nil, nil, nil); !errors.Is(err, ErrNoTrustAnchor) {
		t.Fatal(err)
	}
	if _, err := zero.Replace(context.Background()); !errors.Is(err, ErrNoTrustAnchor) {
		t.Fatal(err)
	}
	if _, err := zero.Recover(context.Background()); !errors.Is(err, ErrNoTrustAnchor) {
		t.Fatal(err)
	}
	if err := zero.CheckStartup(context.Background()); !errors.Is(err, ErrNoTrustAnchor) {
		t.Fatal(err)
	}
	f := newFixture(t, "linux", "amd64")
	for _, mutate := range []func(*Policy){
		func(p *Policy) { p.PublicKey = []byte{1} }, func(p *Policy) { p.OS = "freebsd" },
		func(p *Policy) { p.TargetVersion = p.CurrentVersion }, func(p *Policy) { p.TargetVersion = "0.0.1" },
		func(p *Policy) { p.TargetVersion = "0.0.3-rc1" }, func(p *Policy) { p.TargetVersion = "01.0.3" },
		func(p *Policy) { p.Root = "/" }, func(p *Policy) { p.Root = "relative" },
		func(p *Policy) { p.BaselineSHA256 = "" }, func(p *Policy) { p.MaxBytes = MaxBinaryBytes + 1 },
	} {
		p := f.s.p
		mutate(&p)
		if _, err := New(p); err == nil {
			t.Fatal("unsafe policy accepted")
		}
	}
	p := f.s.p
	p.PublicKey = append([]byte(nil), p.PublicKey...)
	s, err := New(p)
	if err != nil {
		t.Fatal(err)
	}
	p.PublicKey[0] ^= 1
	if _, err := s.Verify(f.raw, f.sig); err != nil {
		t.Fatal("key not defensively copied", err)
	}
}

func TestManifestRejections(t *testing.T) {
	for name, mutate := range map[string]func(*fixture){
		"source":           func(f *fixture) { f.m.Source = "https://example.com/atm" },
		"source-query":     func(f *fixture) { f.m.Source += "?key=x" },
		"source-traversal": func(f *fixture) { f.m.Source += "/../atm" },
		"version":          func(f *fixture) { f.m.Version = "0.0.1" },
		"platform":         func(f *fixture) { f.m.OS = "windows" },
		"arch":             func(f *fixture) { f.m.Arch = "arm64" },
		"commit":           func(f *fixture) { f.m.Commit += "-dirty" },
		"scope":            func(f *fixture) { f.m.Scope = "runtime" },
		"schema":           func(f *fixture) { f.m.Schema = 2 },
		"migration":        func(f *fixture) { f.m.Migration = false },
		"plugins":          func(f *fixture) { f.m.Files["plugins/x/plugin.sh"] = f.m.Files["atm"] },
		"user-data":        func(f *fixture) { f.m.Files["config.json"] = f.m.Files["atm"] },
		"traversal":        func(f *fixture) { f.m.Files["../atm"] = f.m.Files["atm"]; delete(f.m.Files, "atm") },
		"absolute":         func(f *fixture) { f.m.Files["/atm"] = f.m.Files["atm"]; delete(f.m.Files, "atm") },
		"case":             func(f *fixture) { f.m.Files["ATM"] = f.m.Files["atm"]; delete(f.m.Files, "atm") },
		"backslash":        func(f *fixture) { f.m.Files[`..\atm`] = f.m.Files["atm"]; delete(f.m.Files, "atm") },
		"size":             func(f *fixture) { x := f.m.Files["atm"]; x.Size = 2048; f.m.Files["atm"] = x },
		"mode":             func(f *fixture) { x := f.m.Files["atm"]; x.Mode = 04755; f.m.Files["atm"] = x },
		"digest":           func(f *fixture) { x := f.m.Files["atm"]; x.SHA256 = strings.Repeat("A", 64); f.m.Files["atm"] = x },
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, "linux", "amd64")
			mutate(f)
			f.sign(t)
			if _, err := f.s.Verify(f.raw, f.sig); err == nil {
				t.Fatal("invalid manifest accepted")
			}
		})
	}
	f := newFixture(t, "linux", "amd64")
	if _, err := f.s.Verify(f.raw, f.sig); err != nil {
		t.Fatal(err)
	}
	for _, raw := range [][]byte{
		append(f.raw, []byte(" {}")...), bytes.Replace(f.raw, []byte(`"schema":1`), []byte(`"schema":1,"schema":1`), 1),
		bytes.Replace(f.raw, []byte(`"schema":1`), []byte(`"unknown":1,"schema":1`), 1),
		bytes.Repeat([]byte(" "), MaxManifestBytes+1),
	} {
		sig := ed25519.Sign(f.private, append([]byte(SignatureDomain), raw...))
		if _, err := f.s.Verify(raw, sig); err == nil {
			t.Fatal("ambiguous/oversized JSON accepted")
		}
	}
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	for _, sig := range [][]byte{nil, f.sig[:63], ed25519.Sign(other, append([]byte(SignatureDomain), f.raw...)), ed25519.Sign(f.private, f.raw)} {
		if _, err := f.s.Verify(f.raw, sig); err == nil {
			t.Fatal("unauthenticated manifest accepted")
		}
	}
}

func TestStageTargetsAndWindowsRefusal(t *testing.T) {
	for _, target := range []string{"linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64", "windows/amd64"} {
		t.Run(target, func(t *testing.T) {
			goos, arch, _ := strings.Cut(target, "/")
			f := newFixture(t, goos, arch)
			f.stage(t)
			f.assertActive(t, f.old)
			if err := f.s.CheckStartup(context.Background()); !errors.Is(err, ErrRecoveryRequired) {
				t.Fatal(err)
			}
			if err := StartupGuard(f.s.p.Root); !errors.Is(err, ErrRecoveryRequired) {
				t.Fatal(err)
			}
			if goos == "windows" || goos != runtime.GOOS || arch != runtime.GOARCH {
				if _, err := f.s.Replace(context.Background()); !errors.Is(err, ErrUnsupported) {
					t.Fatal(err)
				}
				if _, err := f.s.Recover(context.Background()); !errors.Is(err, ErrUnsupported) {
					t.Fatal(err)
				}
				f.assertActive(t, f.old)
			}
		})
	}
}

func TestReplaceRecoverPreservesOtherContent(t *testing.T) {
	f := hostFixture(t)
	for _, name := range []string{"plugins/x/data", "config.json", "lang/en-us", "release.json"} {
		path := filepath.Join(f.s.p.Root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.s.CheckStartup(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.stage(t)
	r, err := f.s.Replace(context.Background())
	if err != nil || r.State != "replaced" || !r.Changed {
		t.Fatalf("%+v %v", r, err)
	}
	f.assertActive(t, f.next)
	if err := f.s.CheckStartup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Replace(context.Background()); err != nil {
		t.Fatal(err)
	}
	r, err = f.s.Recover(context.Background())
	if err != nil || r.State != "recovered" || !r.Changed {
		t.Fatalf("%+v %v", r, err)
	}
	f.assertActive(t, f.old)
	if err := f.s.CheckStartup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Replace(context.Background()); err == nil {
		t.Fatal("reused recovered transaction")
	}
	if _, err := f.s.Stage(context.Background(), f.raw, f.sig, bytes.NewReader(f.next)); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatal("discarded retained backup", err)
	}
	for _, name := range []string{"plugins/x/data", "config.json", "lang/en-us", "release.json"} {
		b, err := os.ReadFile(filepath.Join(f.s.p.Root, name))
		if err != nil || string(b) != "keep" {
			t.Fatal("changed unrelated content", name, err)
		}
	}
}

type panicReader struct{}

func (panicReader) Read([]byte) (int, error) { panic("dry-run consumed payload") }

func TestDryRunAndCancellation(t *testing.T) {
	f := hostFixture(t)
	p := f.s.p
	p.DryRun = true
	p.Root = filepath.Join(p.Root, "absent")
	s, err := New(p)
	if err != nil {
		t.Fatal(err)
	}
	if r, err := s.Stage(context.Background(), f.raw, f.sig, panicReader{}); err != nil || r.State != "planned" {
		t.Fatal(r, err)
	}
	if _, err := s.Replace(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(p.Root); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("dry-run wrote root")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.s.Stage(ctx, f.raw, f.sig, panicReader{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(f.s.p.Root, lockName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("canceled stage wrote lock")
	}
	ctx, cancel = context.WithCancel(context.Background())
	f.s.checkpoint = func(at string) error {
		if at == "created" {
			cancel()
		}
		return nil
	}
	if _, err := f.s.Stage(ctx, f.raw, f.sig, bytes.NewReader(f.next)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	f.assertActive(t, f.old)
	if err := StartupGuard(f.s.p.Root); err != nil {
		t.Fatal("canceled stage leaked", err)
	}
}

func TestStagePayloadFailures(t *testing.T) {
	for _, kind := range []string{"truncated", "oversize", "checksum", "header", "wrong-arch", "local-edit", "reader-error"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t, "linux", "amd64")
			payload := append([]byte(nil), f.next...)
			switch kind {
			case "truncated":
				payload = payload[:len(payload)-1]
			case "oversize":
				payload = append(payload, 0)
			case "checksum":
				payload[90]++
			case "header":
				payload[0] = 0
				f.next = payload
				x := f.m.Files["atm"]
				x.SHA256 = sum(payload)
				f.m.Files["atm"] = x
				f.sign(t)
			case "wrong-arch":
				payload = binaryFixture("linux", "arm64", 2)
				x := f.m.Files["atm"]
				x.SHA256 = sum(payload)
				f.m.Files["atm"] = x
				f.sign(t)
			case "local-edit":
				if err := os.WriteFile(filepath.Join(f.s.p.Root, "atm"), []byte("local"), 0755); err != nil {
					t.Fatal(err)
				}
			}
			var reader io.Reader = bytes.NewReader(payload)
			if kind == "reader-error" {
				reader = errorReader{}
			}
			if _, err := f.s.Stage(context.Background(), f.raw, f.sig, reader); err == nil {
				t.Fatal("unsafe payload staged")
			}
			if err := StartupGuard(f.s.p.Root); err != nil {
				t.Fatal("failed stage leaked", err)
			}
			if kind != "local-edit" {
				f.assertActive(t, f.old)
			}
		})
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestTamperingAndLocks(t *testing.T) {
	for _, name := range []string{"candidate", "backup", "journal.json", "unknown", "../atm"} {
		t.Run(name, func(t *testing.T) {
			f := hostFixture(t)
			f.stage(t)
			path := filepath.Join(f.s.p.Root, transaction, name)
			if err := os.WriteFile(path, []byte("edited"), 0700); err != nil {
				t.Fatal(err)
			}
			if _, err := f.s.Replace(context.Background()); err == nil {
				t.Fatal("tampered replacement accepted")
			}
			if _, err := f.s.Recover(context.Background()); err == nil {
				t.Fatal("tampered recovery accepted")
			}
			if err := f.s.CheckStartup(context.Background()); err == nil {
				t.Fatal("tampered startup accepted")
			}
		})
	}
	f := hostFixture(t)
	f.stage(t)
	st, err := f.s.open(false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Replace(context.Background()); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	st.close()
	if _, err := f.s.Replace(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestLinksAndCheckoutRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privilege not assumed")
	}
	for _, kind := range []string{"symlink", "hardlink", "directory", "checkout", "stage-link", "backup-link", "lock-link"} {
		t.Run(kind, func(t *testing.T) {
			f := hostFixture(t)
			external := filepath.Join(tempRoot(t), "external")
			if err := os.WriteFile(external, f.old, 0755); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "symlink", "hardlink", "directory":
				path := filepath.Join(f.s.p.Root, f.s.name())
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				var err error
				if kind == "symlink" {
					err = os.Symlink(external, path)
				} else if kind == "hardlink" {
					err = os.Link(external, path)
				} else {
					err = os.Mkdir(path, 0700)
				}
				if err != nil {
					t.Fatal(err)
				}
			case "checkout":
				if err := os.Mkdir(filepath.Join(f.s.p.Root, ".git"), 0700); err != nil {
					t.Fatal(err)
				}
			case "stage-link":
				if err := os.Symlink(filepath.Dir(external), filepath.Join(f.s.p.Root, transaction)); err != nil {
					t.Fatal(err)
				}
			case "lock-link":
				if err := os.Symlink(external, filepath.Join(f.s.p.Root, lockName)); err != nil {
					t.Fatal(err)
				}
			case "backup-link":
				f.stage(t)
				path := filepath.Join(f.s.p.Root, transaction, "backup")
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(external, path); err != nil {
					t.Fatal(err)
				}
				if _, err := f.s.Recover(context.Background()); err == nil {
					t.Fatal("linked backup recovered")
				}
				return
			}
			if _, err := f.s.Stage(context.Background(), f.raw, f.sig, bytes.NewReader(f.next)); err == nil {
				t.Fatal("unsafe path accepted")
			}
			b, err := os.ReadFile(external)
			if err != nil || !bytes.Equal(b, f.old) {
				t.Fatal("external file changed", err)
			}
		})
	}
}

func TestFailureBoundaries(t *testing.T) {
	for _, point := range []string{"before-replace", "after-replace", "before-recover", "after-recover"} {
		t.Run(point, func(t *testing.T) {
			f := hostFixture(t)
			f.stage(t)
			f.s.checkpoint = func(at string) error {
				if at == point {
					return fmt.Errorf("injected")
				}
				return nil
			}
			r, err := f.s.Replace(context.Background())
			if strings.Contains(point, "replace") {
				if err == nil {
					t.Fatal("failure not injected")
				}
				if r.Changed != (point == "after-replace") {
					t.Fatal("incorrect commit report", r)
				}
				if e := f.s.CheckStartup(context.Background()); !errors.Is(e, ErrRecoveryRequired) {
					t.Fatal(e)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if _, err = f.s.Recover(context.Background()); err == nil {
					t.Fatal("recovery failure not injected")
				}
			}
			f.s.checkpoint = nil
			if _, err := f.s.Recover(context.Background()); err != nil {
				t.Fatal(err)
			}
			f.assertActive(t, f.old)
			if err := f.s.CheckStartup(context.Background()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCrashHelper(t *testing.T) {
	root := os.Getenv("ATM_COREUPDATE_TEST_ROOT")
	if root == "" {
		return
	}
	b, err := os.ReadFile(filepath.Join(root, "policy-test.json"))
	if err != nil {
		t.Fatal(err)
	}
	var p Policy
	if err = json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	s, err := New(p)
	if err != nil {
		t.Fatal(err)
	}
	s.checkpoint = func(at string) error {
		if at == os.Getenv("ATM_COREUPDATE_TEST_POINT") {
			os.Exit(71)
		}
		return nil
	}
	switch os.Getenv("ATM_COREUPDATE_TEST_ACTION") {
	case "stage":
		raw, err := os.ReadFile(filepath.Join(root, "manifest-test.json"))
		if err != nil {
			t.Fatal(err)
		}
		sig, err := os.ReadFile(filepath.Join(root, "signature-test"))
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.Stage(context.Background(), raw, sig, bytes.NewReader(binaryFixture(p.OS, p.Arch, 2)))
		if err != nil {
			t.Fatal(err)
		}
	case "recover":
		_, err = s.Recover(context.Background())
	default:
		_, err = s.Replace(context.Background())
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Fatal("crash checkpoint not reached")
}

func TestProcessCrashRecovery(t *testing.T) {
	for _, point := range []string{"created", "prepared", "before-replace", "after-replace", "before-recover", "after-recover"} {
		t.Run(point, func(t *testing.T) {
			f := hostFixture(t)
			action := "replace"
			if point == "created" || point == "prepared" {
				action = "stage"
			} else {
				f.stage(t)
			}
			if strings.Contains(point, "recover") {
				action = "recover"
				if _, err := f.s.Replace(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			b, err := json.Marshal(f.s.p)
			if err != nil {
				t.Fatal(err)
			}
			for name, data := range map[string][]byte{"policy-test.json": b, "manifest-test.json": f.raw, "signature-test": f.sig} {
				if err := os.WriteFile(filepath.Join(f.s.p.Root, name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestCrashHelper$")
			cmd.Env = append(os.Environ(), "ATM_COREUPDATE_TEST_ROOT="+f.s.p.Root, "ATM_COREUPDATE_TEST_POINT="+point, "ATM_COREUPDATE_TEST_ACTION="+action, "PATH=/nonexistent")
			out, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 71 {
				t.Fatalf("child crash: %v %s", err, out)
			}
			if err := StartupGuard(f.s.p.Root); !errors.Is(err, ErrRecoveryRequired) {
				t.Fatal(err)
			}
			if point == "created" {
				if _, err := f.s.Recover(context.Background()); err == nil {
					t.Fatal("adopted incomplete stage")
				}
				f.assertActive(t, f.old)
				return
			}
			if _, err := f.s.Recover(context.Background()); err != nil {
				t.Fatal("OS lock not released or recovery failed", err)
			}
			f.assertActive(t, f.old)
			if err := f.s.CheckStartup(context.Background()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestStartupGuardAllowsOrdinaryCheckoutOnlyWithoutTransaction(t *testing.T) {
	f := newFixture(t, "linux", "amd64")
	if err := os.Chmod(f.s.p.Root, 0775); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(f.s.p.Root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := StartupGuard(f.s.p.Root); err != nil {
		t.Fatal("ordinary checkout blocked", err)
	}
	if err := f.s.CheckStartup(context.Background()); err != nil {
		t.Fatal("ordinary checkout blocked", err)
	}
	if _, err := f.s.Stage(context.Background(), f.raw, f.sig, bytes.NewReader(f.next)); err == nil {
		t.Fatal("checkout mutation allowed")
	}
	entries, err := os.ReadDir(f.s.p.Root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatal("read-only guard created state")
	}
	if err := os.Mkdir(filepath.Join(f.s.p.Root, transaction), 0700); err != nil {
		t.Fatal(err)
	}
	if err := StartupGuard(f.s.p.Root); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatal("pending transaction ignored", err)
	}
	if err := f.s.CheckStartup(context.Background()); err == nil {
		t.Fatal("unsafe pending state accepted")
	}
}

func TestCancellationAtCommitBoundary(t *testing.T) {
	for _, point := range []string{"before-replace", "after-replace", "before-recover", "after-recover"} {
		t.Run(point, func(t *testing.T) {
			f := hostFixture(t)
			f.stage(t)
			if strings.Contains(point, "recover") {
				if _, err := f.s.Replace(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f.s.checkpoint = func(at string) error {
				if at == point {
					cancel()
				}
				return nil
			}
			var r Result
			var err error
			if strings.Contains(point, "recover") {
				r, err = f.s.Recover(ctx)
			} else {
				r, err = f.s.Replace(ctx)
			}
			if strings.HasPrefix(point, "before") {
				if !errors.Is(err, context.Canceled) || r.Changed {
					t.Fatal("canceled before commit", r, err)
				}
			} else {
				if err != nil || !r.Changed || r.State == "recovery-required" {
					t.Fatal("late cancellation hid committed result", r, err)
				}
				if err := f.s.CheckStartup(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestConcurrentLocalEditsAndChangedRecoveryKey(t *testing.T) {
	for _, point := range []string{"before-replace", "before-recover"} {
		t.Run(point, func(t *testing.T) {
			f := hostFixture(t)
			f.stage(t)
			if point == "before-recover" {
				if _, err := f.s.Replace(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			f.s.checkpoint = func(at string) error {
				if at == point {
					return os.WriteFile(filepath.Join(f.s.p.Root, f.s.name()), []byte("local edit"), 0755)
				}
				return nil
			}
			var err error
			if point == "before-recover" {
				_, err = f.s.Recover(context.Background())
			} else {
				_, err = f.s.Replace(context.Background())
			}
			if err == nil {
				t.Fatal("concurrent edit overwritten")
			}
			f.assertActive(t, []byte("local edit"))
		})
	}
	f := hostFixture(t)
	f.stage(t)
	p := f.s.p
	p.PublicKey, _, _ = ed25519.GenerateKey(rand.Reader)
	s, err := New(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Replace(context.Background()); err == nil {
		t.Fatal("changed key accepted staged signature")
	}
	if _, err := s.Recover(context.Background()); err == nil {
		t.Fatal("changed key accepted recovery signature")
	}
	if err := s.CheckStartup(context.Background()); err == nil {
		t.Fatal("changed key accepted startup")
	}
	f.assertActive(t, f.old)
}

func TestStableVersionSemantics(t *testing.T) {
	for _, tc := range []struct {
		target, current string
		want            bool
	}{
		{"0.0.3", "0.0.2", true}, {"0.10.0", "0.9.99", true},
		{"1.0.0", "0.999999999.999999999", true}, {"12.0.0", "9.99.99", true},
		{"0.0.2", "0.0.2", false}, {"0.0.1", "0.0.2", false},
		{"0.9.99", "0.10.0", false}, {"v0.0.3", "0.0.2", false},
		{"0.0.3-rc.1", "0.0.2", false}, {"0.0.3+build.1", "0.0.2", false},
		{"0.0.3", "0.0.2+build.1", false}, {"0.0.3", "development", false},
		{"0.00.3", "0.0.2", false}, {"0.0.3\n", "0.0.2", false},
		{"1000000000.0.0", "0.0.2", false}, {"0.0", "0.0.2", false},
	} {
		t.Run(tc.target+"-from-"+tc.current, func(t *testing.T) {
			if got := newer(tc.target, tc.current); got != tc.want {
				t.Fatalf("newer = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestManifestCanonicalJSONKeys(t *testing.T) {
	f := newFixture(t, "linux", "amd64")
	for _, field := range []string{`"Schema":1`, `"\u017fchema":1`, `"sche\u0000ma":1`, `"schema":1`} {
		raw := bytes.Replace(f.raw, []byte(`"schema":1`), []byte(`"schema":1,`+field), 1)
		sig := ed25519.Sign(f.private, append([]byte(SignatureDomain), raw...))
		if _, err := f.s.Verify(raw, sig); err == nil {
			t.Fatalf("noncanonical/duplicate key accepted: %s", field)
		}
	}
	var v any
	if err := strictJSON([]byte{'"', 0xff, '"'}, &v); err == nil {
		t.Fatal("invalid UTF-8 accepted")
	}
}

func TestWindowsReplacementAndRecoveryAreNoIO(t *testing.T) {
	f := newFixture(t, "windows", "amd64")
	for _, dry := range []bool{false, true} {
		p := f.s.p
		p.Root = filepath.Join(p.Root, "absent")
		p.DryRun = dry
		s, err := New(p)
		if err != nil {
			t.Fatal(err)
		}
		for _, op := range []func(context.Context) (Result, error){s.Replace, s.Recover} {
			r, err := op(context.Background())
			if !errors.Is(err, ErrUnsupported) || r.Changed {
				t.Fatal("Windows operation not refused before I/O", r, err)
			}
		}
		if _, err := os.Lstat(p.Root); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("unsupported operation created state", err)
		}
	}
	f.stage(t)
	// An open handle is a fixture, not Windows runtime acceptance on Unix.
	h, err := os.Open(filepath.Join(f.s.p.Root, "atm.exe"))
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	before, err := os.ReadFile(filepath.Join(f.s.p.Root, transaction, "journal.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range []func(context.Context) (Result, error){f.s.Replace, f.s.Recover} {
		if _, err := op(context.Background()); !errors.Is(err, ErrUnsupported) {
			t.Fatal(err)
		}
	}
	after, err := os.ReadFile(filepath.Join(f.s.p.Root, transaction, "journal.json"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("journal changed", err)
	}
	f.assertActive(t, f.old)
	if err := StartupGuard(f.s.p.Root); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatal("staged Windows transaction did not block startup", err)
	}
}
