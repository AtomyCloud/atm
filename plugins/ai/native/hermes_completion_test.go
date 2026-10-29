//go:build linux

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// seedHermesPMCompletion simulates PM outputs only. The caller owns the source
// fixture, source.json, bootstrap interpreter, and subsequent launcher outputs.
func seedHermesPMCompletion(t *testing.T, versionDir, arch string, startedAt time.Time) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(versionDir, "source", "pm", "lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	var pins struct {
		Packages struct {
			Python struct {
				Version   string               `json:"version"`
				Artifacts map[string]hermesPin `json:"artifacts"`
			} `json:"python"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(data, &pins); err != nil {
		t.Fatal(err)
	}
	target := "linux-x64"
	if arch == "arm64" {
		target = "linux-arm64"
	} else if arch != "amd64" {
		t.Fatalf("unsupported fixture architecture %q", arch)
	}
	if !validSHA(pins.Packages.Python.Artifacts[target].SHA256) {
		t.Fatal("fixture source must include Python artifact SHA256 for " + target)
	}
	home := filepath.Join(versionDir, "home", ".hermes")
	key := sha256.Sum256([]byte(filepath.Join(versionDir, "source")))
	state := filepath.Join(home, "installs", hex.EncodeToString(key[:])[:16])
	generation := filepath.Join(state, "environments", strings.Repeat("1", 32))
	venv := filepath.Join(generation, "venv")
	entry := "python-" + pins.Packages.Python.Version + "-" + target
	pythonHome := filepath.Join(home, "tools", entry, "bin")
	write := func(path string, value any) {
		t.Helper()
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		writeFixture(t, path, data, 0600)
	}
	write(filepath.Join(home, "logs", "update_receipts", "latest.json"), map[string]any{
		"schema": 1, "kind": "sync", "outcome": "ok", "exit_code": 0,
		"started_at":   startedAt.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano),
		"finished_at":  time.Now().UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano),
		"feature_list": []string{"all"}, "venv_rebuild": map[string]any{"ok": true, "reason": ""},
		"refusal": nil, "steps": []any{}, "warnings": []string{}, "update_id": nil,
	})
	write(filepath.Join(state, "facts.json"), map[string]any{"schema": 1, "packages": map[string]any{
		"venv": map[string]any{"stamp": strings.Repeat("a", 64), "extras": []string{"all"}, "environment": venv, "resolved_lock": filepath.Join(generation, "workspace", "uv.lock")},
	}})
	write(filepath.Join(state, "declined-packages.json"), map[string]any{"schema": 1, "declined": []string{"agent-browser", "cua-driver"}})
	write(filepath.Join(home, "tools", "facts.json"), map[string]any{"schema": 1, "packages": map[string]any{
		"python": map[string]any{"entry": entry, "version": pins.Packages.Python.Version, "target": target, "artifacts": []string{pins.Packages.Python.Artifacts[target].SHA256}, "digest": strings.Repeat("b", 64), "env": map[string]string{}},
	}})
	writeFixture(t, filepath.Join(pythonHome, "python3.14"), []byte("synthetic interpreter; never execute\n"), 0755)
	if err := os.Symlink("python3.14", filepath.Join(pythonHome, "python3")); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(venv, "pyvenv.cfg"), []byte(fmt.Sprintf("home = %s\nimplementation = CPython\nuv = 0.13.5\nversion_info = 3.14.9\ninclude-system-site-packages = false\nrelocatable = true\n", pythonHome)), 0644)
	writeFixture(t, filepath.Join(generation, "workspace", "uv.lock"), []byte("version = 1\n"), 0644)
	if err := os.MkdirAll(filepath.Join(venv, "lib", "python3.14", "site-packages"), 0700); err != nil {
		t.Fatal(err)
	}
}

type hermesCompletionFixture struct {
	dir, bootstrap string
	started        time.Time
	source         hermesSource
	b              backend
	calls          int
}

func newHermesCompletionFixture(t *testing.T, arch string) *hermesCompletionFixture {
	t.Helper()
	f := &hermesCompletionFixture{dir: t.TempDir(), started: time.Now().UTC().Add(-time.Second)}
	if err := os.Chmod(f.dir, 0700); err != nil {
		t.Fatal(err)
	}
	f.source = hermesSource{Version: hermesPMVersion, Commit: hermesPMCommit, Tree: strings.Repeat("0", 40), Files: map[string]hermesSourceFile{}}
	lock, err := json.Marshal(map[string]any{"packages": map[string]any{"python": map[string]any{
		"version": "3.14.9+20261021", "artifacts": map[string]hermesPin{
			"linux-x64": {SHA256: strings.Repeat("c", 64)}, "linux-arm64": {SHA256: strings.Repeat("d", 64)},
		},
	}, "empty-array-entry": []any{}}})
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"pm/lock.json": lock, "pyproject.toml": []byte("[project]\nname = 'synthetic'\n"), "uv.lock": []byte("version = 1\n"),
		"hermes_cli/venv_sync.py": []byte("# synthetic; never execute\n"), "hermes_cli/_launchers.py": []byte("# synthetic; never execute\n"),
	}
	for name, data := range files {
		writeFixture(t, filepath.Join(f.dir, "source", filepath.FromSlash(name)), data, 0644)
		f.source.Files[name] = hermesSourceFile{Mode: 100644, Size: int64(len(data)), SHA: hermesObjectHash("blob", data)}
	}
	trees, err := hermesSourceTrees(context.Background(), f.source)
	if err != nil {
		t.Fatal(err)
	}
	f.source.Tree = trees["."]
	record, err := json.Marshal(hermesRecord{Schema: 1, Plugin: "ai", Provider: hermesAgent, Arch: arch, Source: f.source})
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(f.dir, "source.json"), record, 0600)
	f.bootstrap = filepath.Join(f.dir, "python", "bin", "python3")
	writeFixture(t, f.bootstrap, []byte("synthetic bootstrap; never execute\n"), 0755)
	seedHermesPMCompletion(t, f.dir, arch, f.started)
	f.b.hermes = &hermesTools{run: func(ctx context.Context, executable string, args []string, dir string, env []string) (string, error) {
		f.calls++
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if executable != f.bootstrap || dir != filepath.Join(f.dir, "source") || !reflect.DeepEqual(env, hermesEnvironment(f.dir)) {
			t.Fatal("wrong completion process boundary")
		}
		switch f.calls {
		case 1:
			want := []string{"-s", "-B", "-m", "hermes_cli.venv_sync", "--project-root", dir, "--check", "--json"}
			if !reflect.DeepEqual(args, want) {
				t.Fatalf("currency argv: %q", args)
			}
			return `{"state":"current","ok":true}`, nil
		case 2:
			bin := filepath.Join(dir, ".hermes", "bin")
			want := []string{"-I", "-B", filepath.Join(dir, "hermes_cli", "_launchers.py"), bin}
			if !reflect.DeepEqual(args, want) {
				t.Fatalf("launcher argv: %q", args)
			}
			for _, name := range []string{"hermes", "hermes-acp"} {
				writeFixture(t, filepath.Join(bin, name), []byte("#!/bin/sh\n# synthetic only\n"), 0755)
			}
			return filepath.Join(bin, "hermes") + "\n" + filepath.Join(bin, "hermes-acp") + "\n", nil
		default:
			t.Fatal("unexpected provider execution")
			return "", fmt.Errorf("unexpected provider execution")
		}
	}}
	return f
}

func (f *hermesCompletionFixture) receipt() string {
	return filepath.Join(f.dir, "home", ".hermes", "logs", "update_receipts", "latest.json")
}

func mutateHermesCompletionJSON(t *testing.T, path string, mutate func(map[string]any)) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	mutate(value)
	data, err = json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, path, data, 0600)
}

func TestHermesCompletionRoundTrip(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		t.Run(arch, func(t *testing.T) {
			f := newHermesCompletionFixture(t, arch)
			evidence, err := f.b.hermesCompletePM(context.Background(), f.dir, f.bootstrap, f.started)
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(evidence)
			if err != nil {
				t.Fatal(err)
			}
			var restored hermesCompletionEvidence
			if err := json.Unmarshal(data, &restored); err != nil {
				t.Fatal(err)
			}
			if err := verifyHermesCompletion(context.Background(), f.dir, restored); err != nil {
				t.Fatal(err)
			}
			if err := verifyHermesGeneratedSource(context.Background(), f.source, f.dir); err != nil {
				t.Fatal(err)
			}
			if f.calls != 2 {
				t.Fatalf("got %d subprocesses", f.calls)
			}
			if evidence.StorePython == f.bootstrap || len(evidence.Files) != 15 {
				t.Fatalf("incomplete evidence: %+v", evidence)
			}
		})
	}
}

func TestHermesCompletionReceiptContract(t *testing.T) {
	for _, tc := range []struct {
		name     string
		accepted bool
		change   func(map[string]any, *hermesCompletionFixture)
	}{
		{"no-rebuild", true, func(r map[string]any, _ *hermesCompletionFixture) {
			r["venv_rebuild"] = map[string]any{"ok": false, "reason": "already in sync"}
		}},
		{"microsecond-bound", true, func(r map[string]any, f *hermesCompletionFixture) {
			f.started = f.started.Truncate(time.Microsecond).Add(999 * time.Nanosecond)
			r["started_at"] = f.started.Truncate(time.Microsecond).Format(time.RFC3339Nano)
		}},
		{"stale", false, func(r map[string]any, f *hermesCompletionFixture) {
			r["started_at"] = f.started.Add(-time.Microsecond).Format(time.RFC3339Nano)
		}},
		{"failed", false, func(r map[string]any, _ *hermesCompletionFixture) { r["outcome"] = "failed" }},
		{"missing-exit", false, func(r map[string]any, _ *hermesCompletionFixture) { delete(r, "exit_code") }},
		{"bad-exit", false, func(r map[string]any, _ *hermesCompletionFixture) { r["exit_code"] = 1 }},
		{"missing-rebuild", false, func(r map[string]any, _ *hermesCompletionFixture) { r["venv_rebuild"] = nil }},
		{"missing-rebuild-ok", false, func(r map[string]any, _ *hermesCompletionFixture) { r["venv_rebuild"] = map[string]any{} }},
		{"future", false, func(r map[string]any, _ *hermesCompletionFixture) {
			r["finished_at"] = time.Now().Add(time.Hour).Format(time.RFC3339Nano)
		}},
		{"reversed", false, func(r map[string]any, f *hermesCompletionFixture) {
			r["finished_at"] = f.started.Add(-time.Second).Format(time.RFC3339Nano)
		}},
		{"extras", false, func(r map[string]any, _ *hermesCompletionFixture) { r["feature_list"] = []string{} }},
		{"refused", false, func(r map[string]any, _ *hermesCompletionFixture) { r["refusal"] = map[string]any{"reason": "fixture"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newHermesCompletionFixture(t, "amd64")
			mutateHermesCompletionJSON(t, f.receipt(), func(r map[string]any) { tc.change(r, f) })
			_, err := f.b.hermesCompletePM(context.Background(), f.dir, f.bootstrap, f.started)
			if (err == nil) != tc.accepted {
				t.Fatalf("accepted=%v, error=%v", tc.accepted, err)
			}
			if !tc.accepted && f.calls != 0 {
				t.Fatal("ran PM with invalid receipt")
			}
		})
	}
}

func TestHermesCompletionCurrencyRefusals(t *testing.T) {
	for _, out := range []string{`{"state":"would-sync","ok":true}`, `{"state":"sealed","ok":true}`, `{"state":"current","ok":false}`, `{"state":"current"}`, `{"state":"current","ok":null}`, `{"state":"current","ok":true,"ok":true}`, `{"state":"current","ok":true} {}`, strings.Repeat("x", 4097)} {
		t.Run(out[:min(len(out), 60)], func(t *testing.T) {
			f := newHermesCompletionFixture(t, "amd64")
			f.b.hermes.run = func(context.Context, string, []string, string, []string) (string, error) { f.calls++; return out, nil }
			if _, err := f.b.hermesCompletePM(context.Background(), f.dir, f.bootstrap, f.started); err == nil {
				t.Fatal("accepted invalid check")
			}
			if f.calls != 1 {
				t.Fatal("published after failed currency check")
			}
		})
	}
}

func TestHermesCompletionMutationRefusals(t *testing.T) {
	for _, phase := range []int{1, 2} {
		t.Run(fmt.Sprint(phase), func(t *testing.T) {
			f := newHermesCompletionFixture(t, "amd64")
			run := f.b.hermes.run
			f.b.hermes.run = func(ctx context.Context, exe string, args []string, dir string, env []string) (string, error) {
				out, err := run(ctx, exe, args, dir, env)
				if f.calls == phase {
					mutateHermesCompletionJSON(t, f.receipt(), func(r map[string]any) { r["warnings"] = []string{"changed"} })
				}
				return out, err
			}
			if _, err := f.b.hermesCompletePM(context.Background(), f.dir, f.bootstrap, f.started); err == nil {
				t.Fatal("accepted mutation during completion")
			}
		})
	}
}

func TestHermesCompletionReadOnlyTamper(t *testing.T) {
	f := newHermesCompletionFixture(t, "amd64")
	evidence, err := f.b.hermesCompletePM(context.Background(), f.dir, f.bootstrap, f.started)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range evidence.Files {
		t.Run(item.Path, func(t *testing.T) {
			path := filepath.Join(f.dir, filepath.FromSlash(item.Path))
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			writeFixture(t, path, append(append([]byte{}, original...), '\n'), os.FileMode(item.Mode))
			if err := verifyHermesCompletion(context.Background(), f.dir, evidence); err == nil {
				t.Fatal("accepted changed evidence")
			}
			writeFixture(t, path, original, os.FileMode(item.Mode))
		})
	}
	if err := verifyHermesCompletion(context.Background(), f.dir, evidence); err != nil {
		t.Fatal(err)
	}
	evidence.Files = evidence.Files[:len(evidence.Files)-1]
	if err := verifyHermesCompletion(context.Background(), f.dir, evidence); err == nil {
		t.Fatal("accepted omitted evidence")
	}
	if f.calls != 2 {
		t.Fatal("read-only verification started PM")
	}
}

func TestHermesGeneratedSourceRefusals(t *testing.T) {
	for _, name := range []string{".venv", "__pycache__/surprise.pyc", "node_modules/package.json", ".hermes/bin/extra", ".hermes/other", "unexpected.py"} {
		t.Run(name, func(t *testing.T) {
			f := newHermesCompletionFixture(t, "amd64")
			if err := verifyHermesGeneratedSource(context.Background(), f.source, f.dir); err != nil {
				t.Fatal(err)
			}
			writeFixture(t, filepath.Join(f.dir, "source", filepath.FromSlash(name)), []byte("unexpected\n"), 0600)
			if err := verifyHermesGeneratedSource(context.Background(), f.source, f.dir); err == nil {
				t.Fatal("accepted unapproved source addition")
			}
		})
	}
}

func TestHermesCompletionCancelled(t *testing.T) {
	f := newHermesCompletionFixture(t, "amd64")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.b.hermesCompletePM(ctx, f.dir, f.bootstrap, f.started); err == nil {
		t.Fatal("ignored cancellation")
	}
	if err := verifyHermesGeneratedSource(ctx, f.source, f.dir); err == nil {
		t.Fatal("ignored cancellation")
	}
	if f.calls != 0 {
		t.Fatal("executed after cancellation")
	}
}

func TestHermesCompletionEnvironmentRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*testing.T, *hermesCompletionFixture, hermesCompletionEvidence)
	}{
		{"external-environment", func(t *testing.T, f *hermesCompletionFixture, e hermesCompletionEvidence) {
			mutateHermesCompletionJSON(t, filepath.Join(f.dir, "home", ".hermes", "installs", e.InstallKey, "facts.json"), func(r map[string]any) {
				r["packages"].(map[string]any)["venv"].(map[string]any)["environment"] = "/tmp/unowned/venv"
			})
		}},
		{"external-lock", func(t *testing.T, f *hermesCompletionFixture, e hermesCompletionEvidence) {
			mutateHermesCompletionJSON(t, filepath.Join(f.dir, "home", ".hermes", "installs", e.InstallKey, "facts.json"), func(r map[string]any) {
				r["packages"].(map[string]any)["venv"].(map[string]any)["resolved_lock"] = "/tmp/unowned/uv.lock"
			})
		}},
		{"bad-stamp", func(t *testing.T, f *hermesCompletionFixture, e hermesCompletionEvidence) {
			mutateHermesCompletionJSON(t, filepath.Join(f.dir, "home", ".hermes", "installs", e.InstallKey, "facts.json"), func(r map[string]any) {
				r["packages"].(map[string]any)["venv"].(map[string]any)["stamp"] = "not-a-digest"
			})
		}},
		{"extras", func(t *testing.T, f *hermesCompletionFixture, e hermesCompletionEvidence) {
			mutateHermesCompletionJSON(t, filepath.Join(f.dir, "home", ".hermes", "installs", e.InstallKey, "facts.json"), func(r map[string]any) {
				r["packages"].(map[string]any)["venv"].(map[string]any)["extras"] = []string{"all", "browser"}
			})
		}},
		{"declines", func(t *testing.T, f *hermesCompletionFixture, e hermesCompletionEvidence) {
			mutateHermesCompletionJSON(t, filepath.Join(f.dir, "home", ".hermes", "installs", e.InstallKey, "declined-packages.json"), func(r map[string]any) { r["declined"] = []string{"agent-browser"} })
		}},
		{"python-pin", func(t *testing.T, f *hermesCompletionFixture, e hermesCompletionEvidence) {
			mutateHermesCompletionJSON(t, filepath.Join(f.dir, "home", ".hermes", "tools", "facts.json"), func(r map[string]any) {
				r["packages"].(map[string]any)["python"].(map[string]any)["artifacts"] = []string{strings.Repeat("0", 64)}
			})
		}},
		{"python-target", func(t *testing.T, f *hermesCompletionFixture, e hermesCompletionEvidence) {
			mutateHermesCompletionJSON(t, filepath.Join(f.dir, "home", ".hermes", "tools", "facts.json"), func(r map[string]any) {
				r["packages"].(map[string]any)["python"].(map[string]any)["target"] = "linux-arm64"
			})
		}},
		{"cfg-duplicate", func(t *testing.T, _ *hermesCompletionFixture, e hermesCompletionEvidence) {
			path := filepath.Join(e.Environment, "pyvenv.cfg")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			writeFixture(t, path, append(data, []byte("home = /tmp/external\n")...), 0644)
		}},
		{"cfg-system-site", func(t *testing.T, _ *hermesCompletionFixture, e hermesCompletionEvidence) {
			path := filepath.Join(e.Environment, "pyvenv.cfg")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			writeFixture(t, path, []byte(strings.ReplaceAll(string(data), "include-system-site-packages = false", "include-system-site-packages = true")), 0644)
		}},
		{"cfg-symlink", func(t *testing.T, _ *hermesCompletionFixture, e hermesCompletionEvidence) {
			path := filepath.Join(e.Environment, "pyvenv.cfg")
			if err := os.Rename(path, path+".original"); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("pyvenv.cfg.original", path); err != nil {
				t.Fatal(err)
			}
		}},
		{"cfg-hardlink", func(t *testing.T, _ *hermesCompletionFixture, e hermesCompletionEvidence) {
			path := filepath.Join(e.Environment, "pyvenv.cfg")
			if err := os.Link(path, path+".hardlink"); err != nil {
				t.Fatal(err)
			}
		}},
		{"cfg-writable", func(t *testing.T, _ *hermesCompletionFixture, e hermesCompletionEvidence) {
			if err := os.Chmod(filepath.Join(e.Environment, "pyvenv.cfg"), 0664); err != nil {
				t.Fatal(err)
			}
		}},
		{"missing-site-packages", func(t *testing.T, _ *hermesCompletionFixture, e hermesCompletionEvidence) {
			if err := os.Remove(filepath.Join(e.Environment, "lib", "python3.14", "site-packages")); err != nil {
				t.Fatal(err)
			}
		}},
		{"source-script", func(t *testing.T, f *hermesCompletionFixture, _ hermesCompletionEvidence) {
			writeFixture(t, filepath.Join(f.dir, "source", "hermes_cli", "venv_sync.py"), []byte("changed\n"), 0644)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newHermesCompletionFixture(t, "amd64")
			e, err := hermesCompletionSnapshot(context.Background(), f.dir, f.started, time.Now().UTC(), false)
			if err != nil {
				t.Fatal(err)
			}
			tc.change(t, f, e)
			if _, err := f.b.hermesCompletePM(context.Background(), f.dir, f.bootstrap, f.started); err == nil {
				t.Fatal("accepted invalid environment")
			}
			if f.calls != 0 {
				t.Fatal("executed PM with invalid environment")
			}
		})
	}
}

func TestHermesCompletionProcessFailures(t *testing.T) {
	for _, kind := range []string{"check-error", "publisher-error", "publisher-output", "missing-launcher", "launcher-symlink"} {
		t.Run(kind, func(t *testing.T) {
			f := newHermesCompletionFixture(t, "amd64")
			run := f.b.hermes.run
			f.b.hermes.run = func(ctx context.Context, exe string, args []string, dir string, env []string) (string, error) {
				out, err := run(ctx, exe, args, dir, env)
				if f.calls == 1 && kind == "check-error" {
					return "", fmt.Errorf("synthetic check failure")
				}
				if f.calls == 2 {
					path := filepath.Join(dir, ".hermes", "bin", "hermes")
					switch kind {
					case "publisher-error":
						return "", fmt.Errorf("synthetic publisher failure")
					case "publisher-output":
						return "unexpected", nil
					case "missing-launcher":
						if err := os.Remove(path); err != nil {
							t.Fatal(err)
						}
					case "launcher-symlink":
						if err := os.Remove(path); err != nil {
							t.Fatal(err)
						}
						if err := os.Symlink("hermes-acp", path); err != nil {
							t.Fatal(err)
						}
					}
				}
				return out, err
			}
			if _, err := f.b.hermesCompletePM(context.Background(), f.dir, f.bootstrap, f.started); err == nil {
				t.Fatal("accepted publication failure")
			}
		})
	}
}

func TestHermesGeneratedSourceSymlink(t *testing.T) {
	f := newHermesCompletionFixture(t, "amd64")
	if err := os.Symlink(filepath.Join(f.dir, "home"), filepath.Join(f.dir, "source", ".hermes")); err != nil {
		t.Fatal(err)
	}
	if err := verifyHermesGeneratedSource(context.Background(), f.source, f.dir); err == nil {
		t.Fatal("accepted generated directory symlink")
	}
}
