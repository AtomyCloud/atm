//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type ollamaServiceFixture struct {
	f     *fixture
	home  string
	state ollamaManagerState
	calls []string
	paths string
	fault func(string) error
}

func serviceFixture(t *testing.T) *ollamaServiceFixture {
	t.Helper()
	f := newFixture(t, "amd64")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("SYSTEMD_UNIT_PATH", "")
	s := &ollamaServiceFixture{f: f, home: os.Getenv("HOME")}
	s.paths = "UnitPath=" + ollamaManagerWord(filepath.Dir(ollamaServiceUnitPath(s.home))) + "\n"
	s.absent()
	f.b.ollamaService = &ollamaServiceTools{
		uid: func() int { return 1234 }, bus: func(int) error { return nil },
		resolve: func(name string) (string, error) {
			if name != "env" && name != "systemctl" {
				return "", fmt.Errorf("unexpected executable")
			}
			return "/usr/bin/" + name, nil
		},
		run: func(ctx context.Context, p string, args, env []string) (string, error) {
			if p != "/usr/bin/systemctl" || len(args) < 4 || !reflect.DeepEqual(args[:3], []string{"--user", "--no-pager", "--no-ask-password"}) {
				t.Fatalf("unbounded manager argv: %q %q", p, args)
			}
			if !reflect.DeepEqual(env, ollamaServiceEnvironment(s.home, 1234)) {
				t.Fatalf("manager env: %q", env)
			}
			action := args[3]
			if action == "show" && len(args) == 5 && args[4] == "--property=UnitPath" {
				action = "paths"
			}
			s.calls = append(s.calls, action)
			if s.fault != nil {
				if err := s.fault(action); err != nil {
					return "", err
				}
			}
			switch action {
			case "paths":
				return s.paths, nil
			case "show":
				if !reflect.DeepEqual(args[4:], []string{"--all", "--property=" + ollamaServiceProperties, ollamaServiceName}) {
					t.Fatalf("show argv %q", args)
				}
				var out strings.Builder
				for _, key := range strings.Split(ollamaServiceProperties, ",") {
					if (key == "EnvironmentFiles" || key == "ExecStart") && s.state[key] == "" {
						continue
					}
					fmt.Fprintf(&out, "%s=%s\n", key, s.state[key])
				}
				return out.String(), nil
			case "daemon-reload":
				if len(args) != 4 {
					t.Fatalf("reload argv %q", args)
				}
				if _, err := os.Lstat(ollamaServiceUnitPath(s.home)); os.IsNotExist(err) {
					s.absent()
					return "", nil
				}
				var r ollamaServiceReceipt
				if err := readJSON(filepath.Join(f.b.ollamaServiceRoot(), "receipt.json"), &r); err != nil {
					return "", err
				}
				s.loaded(r)
			case "start", "stop":
				if !reflect.DeepEqual(args[4:], []string{ollamaServiceName}) {
					t.Fatalf("mutation argv %q", args)
				}
				if action == "start" {
					s.state["ActiveState"], s.state["SubState"] = "active", "running"
				} else {
					s.state["ActiveState"], s.state["SubState"] = "inactive", "dead"
				}
			default:
				t.Fatalf("unexpected manager action %q", args)
			}
			return "", nil
		},
	}
	ollamaFixtureRelease(t, f, "1.2.3")
	mustRun(t, f, "install", "--provider", "ollama", "--trust-download", "--version", "1.2.3")
	return s
}

func (s *ollamaServiceFixture) absent() {
	s.state = ollamaManagerState{"Id": ollamaServiceName, "Names": ollamaServiceName, "LoadState": "not-found", "ActiveState": "inactive", "SubState": "dead", "NeedDaemonReload": "no"}
}
func (s *ollamaServiceFixture) loaded(r ollamaServiceReceipt) {
	s.state = ollamaManagerState{"Id": ollamaServiceName, "Names": ollamaServiceName, "LoadState": "loaded", "ActiveState": "inactive", "SubState": "dead", "NeedDaemonReload": "no", "FragmentPath": r.UnitPath, "UnitFileState": "static", "Type": "exec", "Restart": "no", "WorkingDirectory": r.DataHome, "Description": r.Description, "ExecStart": "{ path=" + r.Env + " ; argv[]=" + strings.Join(r.Argv, " ") + " ; ignore_errors=no ; start_time=[n/a] ; stop_time=[n/a] ; pid=0 ; code=(null) ; status=0/0 }"}
}
func (s *ollamaServiceFixture) run(action string) error {
	args := []string{"--provider", "ollama", "--service-action", action}
	if action != "status" {
		args = append(args, "--accept-service-execution")
	}
	return s.f.run(context.Background(), "integrate", args...)
}
func (s *ollamaServiceFixture) must(t *testing.T, action string) {
	t.Helper()
	if err := s.run(action); err != nil {
		t.Fatal(action, err)
	}
}
func (s *ollamaServiceFixture) mutations() []string {
	var out []string
	for _, a := range s.calls {
		if a != "show" && a != "paths" {
			out = append(out, a)
		}
	}
	return out
}

func TestOllamaServiceLifecycleRetainsData(t *testing.T) {
	s := serviceFixture(t)
	unitRoot := filepath.Dir(ollamaServiceUnitPath(s.home))
	if err := os.MkdirAll(filepath.Join(unitRoot, "default.target.wants"), 0700); err != nil {
		t.Fatal(err)
	}
	// Both lookup and dependency directories may be empty (ReadDir returns EOF).
	s.must(t, "status")
	s.must(t, "register")
	root := s.f.b.ollamaServiceRoot()
	var rec ollamaServiceReceipt
	if err := readJSON(filepath.Join(root, "receipt.json"), &rec); err != nil {
		t.Fatal(err)
	}
	unit := string(integrationRead(t, rec.UnitPath))
	for _, want := range []string{"Type=exec\n", "Restart=no\n", "ExecStart=:\"/usr/bin/env\" \"-i\"", "versions/1.2.3/tree/bin/ollama", "OLLAMA_HOST=127.0.0.1:11434", "UnsetEnvironment=LD_PRELOAD LD_LIBRARY_PATH LD_AUDIT"} {
		if !strings.Contains(unit, want) {
			t.Fatalf("unit lacks %s: %s", want, unit)
		}
	}
	for _, bad := range []string{"/current/", "[Install]", "WantedBy=", "User=", "ollama.service.d"} {
		if strings.Contains(unit, bad) {
			t.Fatalf("unexpected unit content %s", bad)
		}
	}
	if !reflect.DeepEqual(s.mutations(), []string{"daemon-reload"}) {
		t.Fatal(s.calls)
	}
	model := filepath.Join(root, "data/models", "retained-model")
	writeFixture(t, model, []byte("model data"), 0600)
	s.must(t, "start")
	if err := s.run("remove"); err == nil {
		t.Fatal("removed active service")
	}
	s.must(t, "status")
	s.must(t, "stop")
	if err := s.f.b.ollamaActivate(context.Background(), "1.2.3"); err == nil {
		t.Fatal("stopped service released pin")
	}
	s.must(t, "remove")
	integrationMissing(t, rec.UnitPath)
	integrationMissing(t, ollamaServiceAnchor(s.home))
	if got := string(integrationRead(t, model)); got != "model data" {
		t.Fatal(got)
	}
	s.must(t, "register")
	s.must(t, "remove")
	mustRun(t, s.f, "uninstall", "--provider", "ollama")
	if got := string(integrationRead(t, model)); got != "model data" {
		t.Fatal(got)
	}
	if _, err := os.Stat(filepath.Join(root, "owner.json")); err != nil {
		t.Fatal(err)
	}
}

func TestOllamaServiceAllPrivateVersionRoutesBlocked(t *testing.T) {
	s := serviceFixture(t)
	s.must(t, "register")
	b := s.f.b
	ctx := context.Background()
	for name, fn := range map[string]func() error{
		"install":        func() error { return b.ollamaInstall(ctx, options{version: "latest"}) },
		"install-held":   func() error { return b.ollamaInstallHeld(ctx, options{version: "latest"}) },
		"use":            func() error { return b.ollamaActivate(ctx, "1.2.3") },
		"use-held":       func() error { return b.ollamaActivateHeld(ctx, "1.2.3") },
		"remove":         func() error { return b.ollamaRemove(ctx, "1.2.2") },
		"remove-held":    func() error { return b.ollamaRemoveHeld(ctx, "1.2.2") },
		"uninstall":      func() error { return b.ollamaUninstall(ctx) },
		"uninstall-held": func() error { return b.ollamaUninstallHeld(ctx) },
	} {
		t.Run(name, func(t *testing.T) {
			if err := fn(); err == nil || !strings.Contains(err.Error(), "service registered or uncertain") {
				t.Fatal(err)
			}
		})
	}
	other := *b
	other.req.AppsDir = filepath.Join(t.TempDir(), "Apps")
	if err := other.ollamaServiceMutationGuard(); err == nil {
		t.Fatal("cross-root registration ignored")
	}
}

func TestOllamaServiceRejectsManagerAndPayloadChanges(t *testing.T) {
	for name, change := range map[string]func(*ollamaServiceFixture){
		"stale":    func(s *ollamaServiceFixture) { s.state["NeedDaemonReload"] = "yes" },
		"job":      func(s *ollamaServiceFixture) { s.state["Job"] = "42" },
		"fragment": func(s *ollamaServiceFixture) { s.state["FragmentPath"] = "/tmp/foreign.service" },
		"dropins":  func(s *ollamaServiceFixture) { s.state["DropInPaths"] = "/etc/systemd/user/service.d/foreign.conf" },
		"aliases":  func(s *ollamaServiceFixture) { s.state["Names"] += " alias.service" },
		"exec":     func(s *ollamaServiceFixture) { s.state["ExecStart"] = "foreign" },
		"payload": func(s *ollamaServiceFixture) {
			writeFixture(t, filepath.Join(s.f.b.ollamaVersion("1.2.3"), "tree/lib/ollama/lib.so.1"), []byte("changed"), 0600)
		},
		"unit": func(s *ollamaServiceFixture) { writeFixture(t, ollamaServiceUnitPath(s.home), []byte("foreign"), 0600) },
	} {
		t.Run(name, func(t *testing.T) {
			s := serviceFixture(t)
			s.must(t, "register")
			change(s)
			before := len(s.mutations())
			if err := s.run("start"); err == nil {
				t.Fatal("accepted changed state")
			}
			if len(s.mutations()) != before {
				t.Fatal("manager mutation on failed guard")
			}
		})
	}
}

func TestOllamaServiceFailureRetainsJournalAndLocks(t *testing.T) {
	for _, action := range []string{"register", "start", "stop", "remove"} {
		t.Run(action, func(t *testing.T) {
			s := serviceFixture(t)
			if action != "register" {
				s.must(t, "register")
			}
			if action == "stop" {
				s.must(t, "start")
			}
			s.fault = func(a string) error {
				if a == "daemon-reload" || a == "start" || a == "stop" {
					return context.DeadlineExceeded
				}
				return nil
			}
			if err := s.run(action); err == nil || !strings.Contains(err.Error(), "retained") {
				t.Fatal(err)
			}
			for _, p := range []string{s.f.b.root("ollama") + ".lock", filepath.Join(s.home, ".atm-ai-path.lock"), filepath.Join(s.f.b.ollamaServiceRoot(), "journal.json")} {
				if _, err := os.Lstat(p); err != nil {
					t.Fatal(p, err)
				}
			}
			if err := s.f.b.ollamaServiceMutationGuard(); err == nil {
				t.Fatal("uncertainty did not block version mutation")
			}
		})
	}
}

func TestOllamaServiceNoDefaultExecutionAndPreexistingRefusal(t *testing.T) {
	s := serviceFixture(t)
	if _, err := s.f.b.menu(context.Background(), "ollama"); err != nil {
		t.Fatal(err)
	}
	if err := s.f.b.ollamaStatus(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(s.calls) != 0 {
		t.Fatal("metadata menu/status executed manager", s.calls)
	}
	s.paths = "UnitPath=/etc/systemd/user\n"
	if err := s.run("register"); err == nil {
		t.Fatal("temporary HOME accepted by unrelated login manager")
	}
	integrationMissing(t, s.f.b.ollamaServiceRoot())
	s.paths = "UnitPath=" + filepath.Dir(ollamaServiceUnitPath(s.home)) + "\n"
	if err := os.Mkdir(s.f.b.ollamaServiceRoot(), 0700); err != nil {
		t.Fatal(err)
	}
	if err := s.run("register"); err == nil {
		t.Fatal("adopted preexisting data root")
	}
	if len(s.mutations()) != 0 {
		t.Fatal(s.calls)
	}
}

func TestOllamaServiceDropinsAliasesAndCustomXDG(t *testing.T) {
	for _, name := range []string{"service.d", "atm-.service.d", ollamaServiceName + ".d", "alias", "enabled", "custom-xdg"} {
		t.Run(name, func(t *testing.T) {
			s := serviceFixture(t)
			dir := filepath.Dir(ollamaServiceUnitPath(s.home))
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			switch name {
			case "alias":
				if err := os.Symlink(ollamaServiceName, filepath.Join(dir, "external.service")); err != nil {
					t.Fatal(err)
				}
			case "enabled":
				if err := os.Mkdir(filepath.Join(dir, "default.target.wants"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("../"+ollamaServiceName, filepath.Join(dir, "default.target.wants", ollamaServiceName)); err != nil {
					t.Fatal(err)
				}
			case "custom-xdg":
				t.Setenv("XDG_CONFIG_HOME", filepath.Join(s.home, "custom"))
			default:
				if err := os.Mkdir(filepath.Join(dir, name), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.run("register"); err == nil {
				t.Fatal("accepted foreign lookup state")
			}
			if len(s.mutations()) != 0 {
				t.Fatal(s.calls)
			}
			integrationMissing(t, s.f.b.ollamaServiceRoot())
		})
	}
}

func TestOllamaServiceLateMutationAndCancellation(t *testing.T) {
	for _, point := range []string{"ollama-service-preflight", "ollama-service-before-mutation"} {
		t.Run(point, func(t *testing.T) {
			s := serviceFixture(t)
			s.must(t, "register")
			s.f.b.checkpoint = func(p string) error {
				if p == point {
					s.state["NeedDaemonReload"] = "yes"
				}
				return nil
			}
			before := len(s.mutations())
			if err := s.run("start"); err == nil {
				t.Fatal("late manager mutation accepted")
			}
			if len(s.mutations()) != before {
				t.Fatal("start submitted after mutation")
			}
		})
	}
	s := serviceFixture(t)
	s.f.b.checkpoint = func(p string) error {
		if p == "ollama-service-before-mutation" {
			return context.Canceled
		}
		return nil
	}
	if err := s.run("register"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(s.mutations()) != 0 {
		t.Fatal(s.calls)
	}
}

func TestOllamaServiceManagerQuotedUnitPaths(t *testing.T) {
	for _, part := range []string{"user space", "user%h", "user$HOME", "user\"quote", "user\\slash", "user'quote"} {
		t.Run(part, func(t *testing.T) {
			s := serviceFixture(t)
			home := filepath.Join(s.home, part)
			if err := os.Mkdir(home, 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("HOME", home)
			s.home = home
			s.paths = "UnitPath=" + ollamaManagerWord(filepath.Dir(ollamaServiceUnitPath(home))) + " /etc/systemd/user\n"
			s.must(t, "register")
			s.must(t, "start")
			s.must(t, "stop")
			s.must(t, "remove")
		})
	}
}

func TestOllamaServiceUnitRejectsControls(t *testing.T) {
	r := ollamaServiceReceipt{DataHome: "/tmp/data\nhome", Argv: []string{"/usr/bin/env", "-i"}}
	if ollamaServiceUnit(r) != nil {
		t.Fatal("control accepted in WorkingDirectory")
	}
	r.DataHome = "/tmp/data/home"
	r.Argv = append(r.Argv, "bad\narg")
	if ollamaServiceUnit(r) != nil {
		t.Fatal("control accepted in ExecStart")
	}
}
