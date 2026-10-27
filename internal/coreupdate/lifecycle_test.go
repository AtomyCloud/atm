package coreupdate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/AtomyCloud/atm/internal/install"
)

func completedFixture(t *testing.T) *fixture {
	t.Helper()
	f := hostFixture(t)
	f.stage(t)
	if _, err := f.s.Replace(context.Background()); err != nil {
		t.Fatal(err)
	}
	return f
}

func transactionBytes(t *testing.T, path string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatal(err)
	}
	contents := map[string]string{}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(path, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		info, err := e.Info()
		if err != nil {
			t.Fatal(err)
		}
		contents[e.Name()] = fmt.Sprintf("%o:%s", info.Mode(), b)
	}
	return contents
}

func TestFinalizeRepeatedVersionsAndRecoveredRetry(t *testing.T) {
	ctx := context.Background()
	f := hostFixture(t)
	history := map[string]map[string]string{}
	for i := 0; i < 4; i++ {
		f.stage(t)
		if _, err := f.s.Replace(ctx); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			if _, err := f.s.Recover(ctx); err != nil {
				t.Fatal(err)
			}
		}
		before := transactionBytes(t, filepath.Join(f.s.p.Root, transaction))
		r, err := f.s.Finalize(ctx, true)
		if err != nil || !r.Finalized || r.HistoryPath == "" {
			t.Fatal(r, err)
		}
		history[r.HistoryPath] = before
		if err := StartupGuard(f.s.p.Root); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			f.assertActive(t, f.old)
			continue // Retrying the recovered version must not collide with history.
		}
		f.assertActive(t, f.next)
		p := f.s.p
		p.CurrentVersion, p.TargetVersion = p.TargetVersion, fmt.Sprintf("0.0.%d", i+3)
		p.BaselineSHA256 = sum(f.next)
		f.old = f.next
		f.next = binaryFixture(p.OS, p.Arch, byte(i+2))
		f.s, err = New(p)
		if err != nil {
			t.Fatal(err)
		}
		f.m.Version = p.TargetVersion
		f.m.Source = OfficialSource + "/releases/download/v" + p.TargetVersion + "/atm-" + p.TargetVersion + "-" + p.OS + "-" + p.Arch
		f.m.Files[f.s.name()] = install.File{SHA256: sum(f.next), Size: int64(len(f.next)), Mode: 0755}
		f.sign(t)
	}
	if len(history) != 4 {
		t.Fatal("history reused")
	}
	for path, want := range history {
		if !reflect.DeepEqual(transactionBytes(t, path), want) {
			t.Fatal("history changed", path)
		}
	}
}

func TestFinalizeRejectsUnverifiedState(t *testing.T) {
	for _, name := range []string{"consent", "incomplete", "unmarked", "backup", "candidate", "journal.json", "committed", "rolledback", "replacement", "restore", "unknown", "current", "key", "lock"} {
		t.Run(name, func(t *testing.T) {
			f := completedFixture(t)
			root := f.s.p.Root
			switch name {
			case "consent":
			case "incomplete", "unmarked":
				path := "journal.json"
				if name == "unmarked" {
					path = "committed"
				}
				if err := os.Remove(filepath.Join(root, transaction, path)); err != nil {
					t.Fatal(err)
				}
			case "current":
				if err := os.WriteFile(filepath.Join(root, f.s.name()), []byte("local edit"), 0755); err != nil {
					t.Fatal(err)
				}
			case "key":
				f.s.p.PublicKey[0] ^= 1
			case "lock":
				st, err := f.s.open(false)
				if err != nil {
					t.Fatal(err)
				}
				defer st.close()
			default:
				if err := os.WriteFile(filepath.Join(root, transaction, name), []byte("conflict"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			before := transactionBytes(t, filepath.Join(root, transaction))
			active, err := os.ReadFile(filepath.Join(root, f.s.name()))
			if err != nil {
				t.Fatal(err)
			}
			r, err := f.s.Finalize(context.Background(), name != "consent")
			if err == nil || r.Finalized {
				t.Fatal("unsafe finalization", r, err)
			}
			if name == "consent" && !errors.Is(err, ErrConsentRequired) || name == "lock" && !errors.Is(err, ErrBusy) {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, transactionBytes(t, filepath.Join(root, transaction))) {
				t.Fatal("changed rejected state")
			}
			f.assertActive(t, active)
		})
	}
}

func TestFinalizeBoundaryRechecksAndCancellation(t *testing.T) {
	for _, point := range []string{"before-finalize", "finalize-reserved", "after-finalize"} {
		for _, kind := range []string{"cancel", "failure", "edit"} {
			t.Run(point+"/"+kind, func(t *testing.T) {
				f := completedFixture(t)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				f.s.checkpoint = func(at string) error {
					if at != point {
						return nil
					}
					switch kind {
					case "cancel":
						cancel()
					case "failure":
						return fmt.Errorf("injected failure")
					case "edit":
						return os.WriteFile(filepath.Join(f.s.p.Root, f.s.name()), []byte("local edit"), 0755)
					}
					return nil
				}
				r, err := f.s.Finalize(ctx, true)
				committed := point == "after-finalize"
				if r.Finalized != committed || (err != nil) != (!committed || kind == "failure") {
					t.Fatal(r, err)
				}
				if kind == "cancel" && !committed && !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				if kind == "edit" {
					f.assertActive(t, []byte("local edit"))
				} else {
					f.assertActive(t, f.next)
				}
				if committed {
					transactionBytes(t, r.HistoryPath)
				} else if err := StartupGuard(f.s.p.Root); !errors.Is(err, ErrRecoveryRequired) {
					t.Fatal(err)
				}
				f.s.checkpoint = nil
				if !committed && kind != "edit" {
					if r, err := f.s.Finalize(context.Background(), true); err != nil || !r.Finalized {
						t.Fatal("retry", r, err)
					}
				}
			})
		}
	}
}

func TestFinalizeNoIOGates(t *testing.T) {
	ctx := context.Background()
	var zero Service
	if _, err := zero.Finalize(ctx, true); !errors.Is(err, ErrNoTrustAnchor) {
		t.Fatal(err)
	}
	f := hostFixture(t)
	f.s.p.Root = filepath.Join(f.s.p.Root, "absent")
	if _, err := f.s.Finalize(ctx, false); !errors.Is(err, ErrConsentRequired) {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := f.s.Finalize(canceled, true); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	f.s.p.DryRun = true
	if r, err := f.s.Finalize(ctx, false); err != nil || r.Finalized {
		t.Fatal(r, err)
	}
	if _, err := os.Stat(f.s.p.Root); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("dry run created root", err)
	}
	f.s.p.OS = "windows"
	if _, err := f.s.Finalize(ctx, true); !errors.Is(err, ErrUnsupported) {
		t.Fatal(err)
	}
}

func TestFinalizeCrashHelper(t *testing.T) {
	root := os.Getenv("ATM_FINALIZE_TEST_ROOT")
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
		if at == os.Getenv("ATM_FINALIZE_TEST_POINT") {
			os.Exit(71)
		}
		return nil
	}
	_, err = s.Finalize(context.Background(), true)
	t.Fatal("expected abrupt exit", err)
}

func TestFinalizeProcessInterruption(t *testing.T) {
	for _, point := range []string{"before-finalize", "finalize-reserved", "after-finalize"} {
		t.Run(point, func(t *testing.T) {
			f := completedFixture(t)
			want := transactionBytes(t, filepath.Join(f.s.p.Root, transaction))
			b, err := json.Marshal(f.s.p)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(f.s.p.Root, "policy-test.json"), b, 0600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestFinalizeCrashHelper$")
			cmd.Env = append(os.Environ(), "ATM_FINALIZE_TEST_ROOT="+f.s.p.Root, "ATM_FINALIZE_TEST_POINT="+point)
			out, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 71 {
				t.Fatalf("%v: %s", err, out)
			}
			if point != "after-finalize" {
				if !reflect.DeepEqual(want, transactionBytes(t, filepath.Join(f.s.p.Root, transaction))) {
					t.Fatal("interruption changed active transaction")
				}
				if _, err := f.s.Finalize(context.Background(), true); err != nil {
					t.Fatal(err)
				}
			}
			if err := StartupGuard(f.s.p.Root); err != nil {
				t.Fatal(err)
			}
			if r, err := f.s.Finalize(context.Background(), true); err == nil || r.Finalized {
				t.Fatal("inferred completion without active transaction", r, err)
			}
			paths, err := filepath.Glob(filepath.Join(f.s.p.Root, historyPrefix+"*", transaction))
			if err != nil || len(paths) != 1 || !reflect.DeepEqual(want, transactionBytes(t, paths[0])) {
				t.Fatal("history not retained", paths, err)
			}
			f.assertActive(t, f.next)
		})
	}
}

func TestFinalizeRechecksStateBeforeMove(t *testing.T) {
	for _, kind := range []string{"backup", "journal.json", "committed", "destination", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			if kind == "symlink" && runtime.GOOS == "windows" {
				t.Skip("symlink privilege not assumed")
			}
			f := completedFixture(t)
			f.s.checkpoint = func(at string) error {
				if at != "finalize-reserved" {
					return nil
				}
				if kind == "destination" {
					entries, err := os.ReadDir(f.s.p.Root)
					if err != nil {
						return err
					}
					for _, e := range entries {
						if strings.HasPrefix(e.Name(), historyPrefix) {
							return os.Mkdir(filepath.Join(f.s.p.Root, e.Name(), transaction), 0700)
						}
					}
					return fmt.Errorf("missing reservation")
				}
				path := filepath.Join(f.s.p.Root, transaction, kind)
				if kind == "symlink" {
					return os.Symlink(f.s.p.Root, path)
				}
				return os.WriteFile(path, []byte("edited"), 0600)
			}
			if r, err := f.s.Finalize(context.Background(), true); err == nil || r.Finalized {
				t.Fatal(r, err)
			}
			if _, err := os.Stat(filepath.Join(f.s.p.Root, transaction)); err != nil {
				t.Fatal("lost active transaction", err)
			}
			f.assertActive(t, f.next)
		})
	}
}

func TestFinalizeDoesNotAdoptNextBaseline(t *testing.T) {
	f := completedFixture(t)
	if _, err := f.s.Finalize(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Stage(context.Background(), f.raw, f.sig, bytes.NewReader(f.next)); err == nil {
		t.Fatal("old policy adopted new baseline")
	}
}
