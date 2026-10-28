//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/AtomyCloud/atm/internal/protocol"
)

type invocation struct {
	path string
	args []string
}

func writeFixture(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
func testBackend(t *testing.T) (*backend, *[]invocation) {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]string{"HOME": home, "XDG_CONFIG_HOME": filepath.Join(home, ".config"), "ATM_CONFIG_DIR": filepath.Join(home, ".config/atm"), "ATM_CONFIG_FILE": filepath.Join(home, ".config/atm/atm.conf"), "ATM_PLUGINS_CONFIG_FILE": filepath.Join(home, ".config/atm/plugins.conf"), "ATM_STATE_DIR": filepath.Join(home, "state"), "ATM_PLUGIN_DIR": "", "ATM_OS_PACKAGES_PACKAGE_FILE": "", "ATM_DRY_RUN": "0"} {
		t.Setenv(k, v)
	}
	req := protocol.Request{Protocol: 1, RequestID: strings.Repeat("a", 32), PluginID: "os_packages", Home: filepath.Join(root, "runtime"), AppsDir: filepath.Join(home, "Apps"), Language: "en-us"}
	b, err := newBackend(req, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("../packages.txt")
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, b.packageFile, data)
	b.system = func() (systemInfo, error) {
		return detectSystem([]byte("ID=ubuntu\nNAME=Ubuntu\nVERSION_CODENAME=noble\n"), false)
	}
	b.uid = func() int { return 1000 }
	b.resolve = func(name string) (string, error) { return "/trusted/" + name, nil }
	calls := []invocation{}
	b.run = func(ctx context.Context, path string, args []string) (string, error) {
		calls = append(calls, invocation{path, append([]string(nil), args...)})
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if reflect.DeepEqual(args, []string{"--version"}) {
			return "apt 2.8.3 (amd64)\n", nil
		}
		return "synthetic success", nil
	}
	return b, &calls
}
func act(b *backend, operation string, args ...string) ([]protocol.Choice, error) {
	b.req.Operation = operation
	b.req.Arguments = args
	return b.execute(context.Background())
}
func absent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("unexpected path %s: %v", path, err)
	}
}
func TestReadOnlyAndConsentGuards(t *testing.T) {
	b, calls := testBackend(t)
	for _, args := range [][]string{nil, {"install"}, {"remove"}, {"commands"}, {"install", "--ubuntu-sources"}} {
		if _, err := act(b, "menu", args...); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := act(b, "status"); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{nil, {"--accept-removal"}, {"--unknown"}, {"--plan"}} {
		if _, err := act(b, "install", args...); err == nil {
			t.Fatal("unapproved/invalid install")
		}
	}
	if _, err := act(b, "remove", "--accept-system-packages"); err == nil {
		t.Fatal("removal acknowledgement skipped")
	}
	if len(*calls) != 0 {
		t.Fatal(*calls)
	}
	absent(t, filepath.Dir(b.state))
}
func TestManagersLifecycleAndRecords(t *testing.T) {
	for _, id := range []string{"ubuntu", "debian", "rocky", "centos", "arch", "alpine"} {
		t.Run(id, func(t *testing.T) {
			b, calls := testBackend(t)
			b.system = func() (systemInfo, error) {
				return detectSystem([]byte("ID="+id+"\nNAME=Fixture\nVERSION_CODENAME=noble\n"), false)
			}
			if _, err := act(b, "install", "--accept-system-packages"); err != nil {
				t.Fatal(err)
			}
			r, _, err := readRecord(b.state)
			if err != nil || r.Status != "completed" || r.Action != "install" {
				t.Fatal(r, err)
			}
			if len(*calls) == 0 {
				t.Fatal(*calls)
			}
			for _, call := range *calls {
				if reflect.DeepEqual(call.args, []string{"--version"}) {
					continue
				}
				prefix := []string{"-n", "--preserve-env=DEBIAN_FRONTEND", "--", "/trusted/" + r.Manager}
				if call.path != "/trusted/sudo" || len(call.args) <= len(prefix) || !reflect.DeepEqual(call.args[:len(prefix)], prefix) {
					t.Fatal("expected direct noninteractive sudo command", call)
				}
				for _, arg := range call.args {
					if arg == r.Manager {
						t.Fatal("executable repeated as a package-manager argument", call)
					}
				}
			}
			*calls = nil
			if _, err = act(b, "uninstall", "--accept-system-packages", "--accept-removal"); err != nil {
				t.Fatal(err)
			}
			if len(*calls) != 1 {
				t.Fatal(*calls)
			}
			r, _, err = readRecord(b.state)
			if err != nil || r.Status != "completed" || r.Action != "remove" {
				t.Fatal(r, err)
			}
			absent(t, filepath.Join(filepath.Dir(b.state), ".native-lock"))
		})
	}
}
func TestTemporarySourcesBoundToBothCommands(t *testing.T) {
	b, _ := testBackend(t)
	b.uid = func() int { return 0 }
	var paths []string
	b.run = func(_ context.Context, _ string, args []string) (string, error) {
		if reflect.DeepEqual(args, []string{"--version"}) {
			return "apt 2.8.3", nil
		}
		for _, arg := range args {
			if p, ok := strings.CutPrefix(arg, "Dir::Etc::sourcelist="); ok {
				data, err := os.ReadFile(p)
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(data), "noble") || strings.Contains(string(data), "trusted=yes") {
					t.Fatal(string(data))
				}
				paths = append(paths, p)
			}
		}
		return "ok", nil
	}
	if _, err := act(b, "install", "--accept-system-packages", "--ubuntu-sources"); err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 || paths[0] != paths[1] {
		t.Fatal(paths)
	}
	absent(t, paths[0])
}
func TestStaleReviewAndUnderLockRevalidation(t *testing.T) {
	for _, when := range []string{"before-confirmation", "before-lock"} {
		t.Run(when, func(t *testing.T) {
			b, calls := testBackend(t)
			b.uid = func() int { return 0 }
			choices, err := act(b, "menu", "install")
			if err != nil {
				t.Fatal(err)
			}
			change := func() {
				data, _ := os.ReadFile(b.packageFile)
				writeFixture(t, b.packageFile, append(data, []byte("\n# changed review bytes\n")...))
			}
			if when == "before-confirmation" {
				change()
			} else {
				b.resolve = func(name string) (string, error) { change(); return "/trusted/" + name, nil }
			}
			if _, err = act(b, "install", choices[0].Arguments...); err == nil {
				t.Fatal("stale approval accepted")
			}
			if len(*calls) != 0 {
				t.Fatal(*calls)
			}
			absent(t, b.state)
		})
	}
}
func TestFailuresStopAndDoNotClaimSuccess(t *testing.T) {
	for _, mode := range []string{"sudo", "update", "old-apt"} {
		t.Run(mode, func(t *testing.T) {
			b, calls := testBackend(t)
			original := b.run
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			b.run = func(ctx context.Context, path string, args []string) (string, error) {
				output, err := original(ctx, path, args)
				if err != nil {
					return output, err
				}
				if reflect.DeepEqual(args, []string{"--version"}) {
					if mode == "old-apt" {
						return "apt 1.8.2", nil
					}
					return output, nil
				}
				if mode == "sudo" {
					return "", errors.New("denied")
				}
				return "refresh failed", errors.New("synthetic update error")
			}
			b.req.Operation = "install"
			b.req.Arguments = []string{"--accept-system-packages"}
			_, executeErr := b.execute(ctx)
			if executeErr == nil {
				t.Fatal("failure accepted")
			}
			if mode == "sudo" && (!strings.Contains(executeErr.Error(), "sudo policy may deny") || !strings.Contains(executeErr.Error(), "ATM never reads a password")) {
				t.Fatal("missing authorization guidance", executeErr)
			}
			for _, call := range *calls {
				for _, a := range call.args {
					if a == "install" {
						t.Fatal("continued after error")
					}
				}
			}
			r, _, err := readRecord(b.state)
			if err != nil {
				t.Fatal(err)
			}
			if r != nil && r.Status != "failed" {
				t.Fatal(r)
			}
			if mode == "old-apt" {
				absent(t, b.state)
			} else if r == nil {
				t.Fatal("missing failed attempt")
			}
			absent(t, filepath.Join(filepath.Dir(b.state), ".native-lock"))
		})
	}
}
func TestCooperativeCancellationPreservesUnresolvedAttempt(t *testing.T) {
	for _, uid := range []int{0, 1000} {
		for _, mode := range []string{"before-launch", "refresh-error", "refresh-success", "between-commands", "final-success"} {
			t.Run(fmt.Sprintf("uid%d/%s", uid, mode), func(t *testing.T) {
				b, _ := testBackend(t)
				b.uid = func() int { return uid }
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				calls := 0
				var source string
				var running []byte
				b.log = func(s string) {
					if mode == "between-commands" && s == "Running: Install selected packages" {
						cancel()
					}
				}
				b.run = func(_ context.Context, _ string, args []string) (string, error) {
					if reflect.DeepEqual(args, []string{"--version"}) {
						if mode == "before-launch" {
							cancel()
						}
						return "apt 2.8.3", nil
					}
					calls++
					for _, arg := range args {
						if path, ok := strings.CutPrefix(arg, "Dir::Etc::sourcelist="); ok {
							source = path
						}
					}
					var err error
					running, err = os.ReadFile(b.state)
					if err != nil {
						t.Fatal(err)
					}
					if mode == "refresh-error" || mode == "refresh-success" || mode == "final-success" && calls == 2 {
						cancel()
					}
					if mode == "refresh-error" {
						return "", ctx.Err()
					}
					return "synthetic success", nil
				}
				b.req.Operation = "install"
				b.req.Arguments = []string{"--accept-system-packages", "--ubuntu-sources"}
				_, err := b.execute(ctx)
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				r, contents, readErr := readRecord(b.state)
				if readErr != nil || r == nil {
					t.Fatal(r, readErr)
				}
				lock := filepath.Join(filepath.Dir(b.state), ".native-lock")
				if mode == "before-launch" {
					if calls != 0 || r.Status != "failed" {
						t.Fatal(calls, r)
					}
					absent(t, lock)
					return
				}
				if source == "" {
					t.Fatal("source argument missing")
				}
				t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(source)) })
				if r.Status != "running" || !strings.Contains(err.Error(), "running journal and lock retained") {
					t.Fatal(r, err)
				}
				if mode != "between-commands" && !bytes.Equal(contents, running) {
					t.Fatal("interrupted running journal was rewritten")
				}
				if _, err := os.Stat(lock); err != nil {
					t.Fatal("interrupted lock missing", err)
				}
				if data, err := os.ReadFile(source); err != nil || !strings.Contains(string(data), "noble") {
					t.Fatal("interrupted sources removed", err)
				}
				wantCalls := 1
				if mode == "final-success" {
					wantCalls = 2
				}
				if calls != wantCalls {
					t.Fatal("continued after cancellation", calls)
				}
				if _, err = act(b, "install", b.req.Arguments...); err == nil || calls != wantCalls {
					t.Fatal("interrupted attempt retried", err, calls)
				}
				if err = os.Remove(lock); err != nil {
					t.Fatal(err)
				}
				if _, release, err := openJournal(b.state); err == nil {
					release()
					t.Fatal("running journal allowed retry without lock")
				}
			})
		}
	}
}

func TestCommandSpecificNOPASSWDDoesNotRequireValidation(t *testing.T) {
	b, calls := testBackend(t)
	original := b.run
	b.run = func(ctx context.Context, path string, args []string) (string, error) {
		if path == "/trusted/sudo" && (len(args) < 5 || args[0] != "-n" || args[2] != "--" || args[3] != "/trusted/apt-get") {
			return "", errors.New("sudo validation denied; only package-manager commands allowed")
		}
		return original(ctx, path, args)
	}
	if _, err := act(b, "install", "--accept-system-packages"); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 3 {
		t.Fatal(*calls)
	}
}

func TestCompleteReviewUsesBoundedProtocolMessages(t *testing.T) {
	for _, view := range []string{"install", "remove", "commands"} {
		t.Run(view, func(t *testing.T) {
			b, calls := testBackend(t)
			var packages []string
			for i := 0; i < maxModelPackages; i++ {
				packages = append(packages, fmt.Sprintf("pkg%03d%s", i, strings.Repeat("a", maxPackageNameBytes-6)))
			}
			writeFixture(t, b.packageFile, []byte("[apt-get]\n"+strings.Join(packages, "\n")+"\n"))
			b.req.Operation = "menu"
			b.req.Arguments = []string{view}
			source := ""
			if view != "remove" {
				b.req.Arguments = append(b.req.Arguments, "--ubuntu-sources")
				source = "/temporary/atm-ubuntu.list"
			}
			input, _ := json.Marshal(b.req)
			var output bytes.Buffer
			if err := serveWith(context.Background(), bytes.NewReader(input), &output, func(_ protocol.Request, log func(string)) (*backend, error) {
				b.log = log
				return b, nil
			}); err != nil {
				t.Fatal(err)
			}
			stream := protocol.Stream{RequestID: b.req.RequestID}
			var reviewedPackages, reviewedCommands []string
			events := bytes.Split(bytes.TrimSpace(output.Bytes()), []byte{'\n'})
			if len(events) > 64 {
				t.Fatalf("review exceeds TUI queue capacity: %d", len(events))
			}
			for _, line := range events {
				event, err := stream.Accept(line)
				if err != nil || len([]rune(event.Message)) > 1800 || strings.Contains(event.Message, "[truncated]") || protocol.SafeText(event.Message) != event.Message {
					t.Fatal("invalid or truncated review event", err, event)
				}
				if text, ok := strings.CutPrefix(event.Message, "Packages: "); ok {
					reviewedPackages = append(reviewedPackages, strings.Split(text, ", ")...)
				}
				if text, ok := strings.CutPrefix(event.Message, "Command argv: (continued) "); ok {
					if len(reviewedCommands) == 0 {
						t.Fatal("orphan command continuation")
					}
					reviewedCommands[len(reviewedCommands)-1] += text
				} else if text, ok := strings.CutPrefix(event.Message, "Command argv: "); ok {
					reviewedCommands = append(reviewedCommands, text)
				}
			}
			if stream.Result == nil || stream.Result.Status != "success" {
				t.Fatal(stream.Result)
			}
			system, _ := b.system()
			action := view
			if view == "commands" {
				action = "install"
			}
			commands, err := planCommands(system, packages, action, source)
			if err != nil {
				t.Fatal(err)
			}
			wantPackages := append([]string(nil), packages...)
			if view == "commands" {
				remove, err := planCommands(system, packages, "remove", "")
				if err != nil {
					t.Fatal(err)
				}
				commands = append(commands, remove...)
				wantPackages = nil // Every name is still checked in both complete argv lists.
			}
			if !reflect.DeepEqual(reviewedPackages, wantPackages) || len(reviewedCommands) != len(commands) {
				t.Fatal("incomplete package or command review", len(reviewedPackages), len(reviewedCommands))
			}
			for i, cmd := range commands {
				var args []string
				if err := json.Unmarshal([]byte(reviewedCommands[i]), &args); err != nil || !reflect.DeepEqual(args, cmd.Args) {
					t.Fatal("incomplete command argv", i, err)
				}
			}
			if len(*calls) != 0 {
				t.Fatal("review executed commands", *calls)
			}
			absent(t, filepath.Dir(b.state))
		})
	}
}

func TestJournalAndPackagePathGuards(t *testing.T) {
	for _, mode := range []string{"journal", "journal-link", "parent-link", "package-link", "interrupted"} {
		t.Run(mode, func(t *testing.T) {
			b, calls := testBackend(t)
			b.uid = func() int { return 0 }
			sentinel := filepath.Join(t.TempDir(), "private")
			writeFixture(t, sentinel, []byte("private\n"))
			switch mode {
			case "journal":
				writeFixture(t, b.state, []byte(`{"schema":1,"schema":2}`))
			case "journal-link":
				os.MkdirAll(filepath.Dir(b.state), 0700)
				if err := os.Symlink(sentinel, b.state); err != nil {
					t.Fatal(err)
				}
			case "parent-link":
				os.MkdirAll(filepath.Dir(filepath.Dir(b.state)), 0700)
				if err := os.Symlink(filepath.Dir(sentinel), filepath.Dir(b.state)); err != nil {
					t.Fatal(err)
				}
			case "package-link":
				os.Remove(b.packageFile)
				if err := os.Symlink(sentinel, b.packageFile); err != nil {
					t.Fatal(err)
				}
			case "interrupted":
				j, release, err := openJournal(b.state)
				if err != nil {
					t.Fatal(err)
				}
				if err = j.save(operationRecord{Schema: 1, Plugin: "os_packages", Action: "install", Status: "running"}); err != nil {
					t.Fatal(err)
				}
				release()
			}
			if _, err := act(b, "install", "--accept-system-packages"); err == nil {
				t.Fatal("unsafe state/path accepted")
			}
			if len(*calls) != 0 {
				t.Fatal(*calls)
			}
			data, _ := os.ReadFile(sentinel)
			if string(data) != "private\n" {
				t.Fatal("private data changed")
			}
		})
	}
}
func TestRecordPublicationConflictAndModes(t *testing.T) {
	b, _ := testBackend(t)
	j, release, err := openJournal(b.state)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	r := operationRecord{Schema: 1, Plugin: "os_packages", Action: "install", Status: "running"}
	if err = j.save(r); err != nil {
		t.Fatal(err)
	}
	i, _ := os.Stat(b.state)
	if i.Mode().Perm() != 0600 {
		t.Fatal(i.Mode())
	}
	original, _ := os.ReadFile(b.state)
	changed := append(original, ' ')
	writeFixture(t, b.state, changed)
	r.Status = "completed"
	if err = j.save(r); err == nil {
		t.Fatal("concurrent edit overwritten")
	}
	now, _ := os.ReadFile(b.state)
	if !bytes.Equal(changed, now) {
		t.Fatal("state changed")
	}
}
func TestProtocolDryRunAndEnvironment(t *testing.T) {
	b, calls := testBackend(t)
	b.req.Operation = "install"
	input, _ := json.Marshal(b.req)
	var output bytes.Buffer
	t.Setenv("ATM_DRY_RUN", "1")
	if err := serveWith(context.Background(), bytes.NewReader(append(input, '\n')), &output, func(protocol.Request, func(string)) (*backend, error) {
		t.Fatal("dry-run constructed backend")
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"status":"success"`) || len(*calls) != 0 {
		t.Fatal(output.String())
	}
	for _, e := range commandEnvironment() {
		if strings.HasPrefix(e, "APT_CONFIG=") || strings.HasPrefix(e, "PYTHONPATH=") || strings.HasPrefix(e, "LD_PRELOAD=") {
			t.Fatal(e)
		}
	}
}
func TestModernAPT(t *testing.T) {
	for _, v := range []string{"apt 2.2.4 (amd64)", "apt 2.8.3", "apt 3.1.0"} {
		if !modernAPT(v) {
			t.Fatal(v)
		}
	}
	for _, v := range []string{"apt 1.8.2", "apt 2.1.99", "unknown", "apt x.y"} {
		if modernAPT(v) {
			t.Fatal(v)
		}
	}
}
