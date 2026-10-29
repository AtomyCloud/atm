//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func hermesUseArgs() []string {
	return []string{"--provider", hermesAgent, "--version", hermesPMVersion}
}

func newHermesSelectionFixture(t *testing.T, arch string) *hermesAgentFixture {
	t.Helper()
	f := newHermesAgentFixture(t, arch)
	mustRun(t, f.fixture, "install", hermesAgentArgs()...)
	f.calls, f.commands = 0, nil
	f.b.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("selection attempted network")
		return nil, errors.New("network forbidden")
	})}
	f.b.hermes.run = func(context.Context, string, []string, string, []string) (string, error) {
		t.Fatal("selection attempted subprocess/PM")
		return "", errors.New("execution forbidden")
	}
	return f
}

func TestHermesSelectionExactOptions(t *testing.T) {
	for _, args := range [][]string{
		{"--provider", hermesAgent},
		{"--provider", hermesAgent, "--version", "latest"},
		{"--provider", hermesAgent, "--version", "v0.22.5"},
		{"--provider", hermesAgent, "--version", "0.22.4"},
		{"--provider", hermesAgent, "--version", "0.22.6"},
		append(hermesUseArgs(), "--trust-download"),
		append(hermesUseArgs(), "--accept-dependency-execution"),
		append(hermesUseArgs(), "--accept-system-packages"),
		append(hermesUseArgs(), "--version", hermesPMVersion),
	} {
		if _, err := parseOptions("use", args); err == nil {
			t.Fatalf("accepted non-exact selection or extraneous consent: %v", args)
		}
	}
	for _, args := range [][]string{hermesUseArgs(), {"--provider=hermes-agent", "--version=0.22.5"}} {
		if _, err := parseOptions("use", args); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHermesSelectionCompletedOffline(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		t.Run(arch, func(t *testing.T) {
			f := newHermesSelectionFixture(t, arch)
			root := f.b.root(hermesAgent)
			dir := filepath.Join(root, "versions", hermesPMVersion)
			paths := []string{filepath.Join(dir, "source.json"), filepath.Join(dir, "complete.json"), filepath.Join(dir, "source", ".hermes", "bin", "hermes")}
			for _, path := range []string{filepath.Join(os.Getenv("HOME"), ".bashrc"), filepath.Join(os.Getenv("HOME"), ".hermes", "config.yaml"), filepath.Join(dir, "home", ".hermes", "config.yaml")} {
				writeFixture(t, path, []byte("preserve private/user data\n"), 0600)
				paths = append(paths, path)
			}
			before := map[string]pathState{}
			for _, path := range paths {
				state, err := pathSnapshot(path, false)
				if err != nil {
					t.Fatal(err)
				}
				before[path] = state
			}
			if err := os.Remove(filepath.Join(root, "current.json")); err != nil {
				t.Fatal(err)
			}
			// An unselected completed preparation and an already-selected one
			// use exactly the same local validation and lock policy.
			for i := 0; i < 2; i++ {
				previous, err := pathSnapshot(filepath.Join(root, "current.json"), false)
				if err != nil {
					t.Fatal(err)
				}
				mustRun(t, f.fixture, "use", hermesUseArgs()...)
				vs, active, err := f.b.hermesInspect(context.Background())
				if err != nil || !reflect.DeepEqual(vs, []string{hermesPMVersion}) || active != hermesPMVersion {
					t.Fatal(vs, active, err)
				}
				integrationMissing(t, root+".lock")
				if i == 1 {
					now, err := pathSnapshot(filepath.Join(root, "current.json"), false)
					if err != nil || !pathUnchanged(now, previous) || !now.info.ModTime().Equal(previous.info.ModTime()) {
						t.Fatal("no-op rewrote selection", err)
					}
				}
			}
			for path, old := range before {
				now, err := pathSnapshot(path, false)
				if err != nil || !pathUnchanged(now, old) {
					t.Fatal("selection changed preparation or profile", path, err)
				}
			}
			integrationMissing(t, filepath.Join(root, "path.json"))
		})
	}
}

func TestHermesSelectionMenuCompletedOnly(t *testing.T) {
	for _, state := range []string{"completed", "unselected", "incomplete", "locked", "absent"} {
		t.Run(state, func(t *testing.T) {
			f := newHermesSelectionFixture(t, "amd64")
			root := f.b.root(hermesAgent)
			switch state {
			case "unselected", "incomplete":
				if err := os.Remove(filepath.Join(root, "current.json")); err != nil {
					t.Fatal(err)
				}
				if state == "incomplete" {
					if err := os.Remove(filepath.Join(root, "versions", hermesPMVersion, "complete.json")); err != nil {
						t.Fatal(err)
					}
				}
			case "locked":
				if err := os.Mkdir(root+".lock", 0700); err != nil {
					t.Fatal(err)
				}
			case "absent":
				if err := os.RemoveAll(root); err != nil {
					t.Fatal(err)
				}
			}
			choices, err := f.b.hermesMenu(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, c := range choices {
				if c.Operation == "use" {
					found = true
					if !reflect.DeepEqual(c.Arguments, hermesUseArgs()) || c.Prompt != "" {
						t.Fatal("menu did not select exact completed version", c)
					}
				}
			}
			if found != (state == "completed" || state == "unselected") {
				t.Fatal("incorrect selectable menu state", state, choices)
			}
		})
	}
}

func TestHermesSelectionRefusesUnsafeState(t *testing.T) {
	for _, kind := range []string{"incomplete", "foreign", "source", "completion", "inventory", "lock", "hardlink", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			f := newHermesSelectionFixture(t, "amd64")
			root := f.b.root(hermesAgent)
			dir := filepath.Join(root, "versions", hermesPMVersion)
			pointer := filepath.Join(root, "current.json")
			before := integrationRead(t, pointer)
			switch kind {
			case "incomplete":
				if err := os.Remove(filepath.Join(dir, "complete.json")); err != nil {
					t.Fatal(err)
				}
			case "foreign":
				writeFixture(t, filepath.Join(root, "owner.json"), []byte(`{"Schema":1,"Plugin":"other","Provider":"hermes-agent"}`), 0600)
			case "source":
				writeFixture(t, filepath.Join(dir, "source", "pm", "cli.py"), []byte("changed"), 0644)
			case "completion":
				writeFixture(t, filepath.Join(dir, "complete.json"), []byte("{}"), 0600)
			case "inventory":
				writeFixture(t, filepath.Join(root, "unknown"), []byte("keep"), 0600)
			case "lock":
				writeFixture(t, filepath.Join(root+".lock", "keep"), []byte("do not clear"), 0600)
			case "hardlink":
				if err := os.Link(filepath.Join(dir, "complete.json"), filepath.Join(t.TempDir(), "linked")); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Rename(filepath.Join(dir, "source"), filepath.Join(dir, "saved")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("saved", filepath.Join(dir, "source")); err != nil {
					t.Fatal(err)
				}
			}
			if err := f.run(context.Background(), "use", hermesUseArgs()...); err == nil {
				t.Fatal("unsafe selection accepted")
			}
			if !bytes.Equal(before, integrationRead(t, pointer)) {
				t.Fatal("refused selection changed pointer")
			}
			if kind == "lock" && string(integrationRead(t, filepath.Join(root+".lock", "keep"))) != "do not clear" {
				t.Fatal("existing lock changed")
			}
		})
	}
}

func TestHermesSelectionLateMutation(t *testing.T) {
	for _, kind := range []string{"source", "receipt", "completion", "owner", "owner-replaced", "inventory", "root-replaced", "version-replaced", "pointer", "staged-pointer", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			f := newHermesSelectionFixture(t, "amd64")
			root := f.b.root(hermesAgent)
			dir := filepath.Join(root, "versions", hermesPMVersion)
			pointer := filepath.Join(root, "current.json")
			if err := os.Remove(pointer); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			seen := false
			f.b.checkpoint = func(point string) error {
				if point != "hermes-before-select" {
					return nil
				}
				seen = true
				switch kind {
				case "source":
					writeFixture(t, filepath.Join(dir, "source", "pm", "cli.py"), []byte("changed"), 0644)
				case "receipt", "completion", "owner":
					path := filepath.Join(dir, "source.json")
					if kind == "completion" {
						path = filepath.Join(dir, "complete.json")
					} else if kind == "owner" {
						path = filepath.Join(root, "owner.json")
					}
					writeFixture(t, path, []byte("{}"), 0600)
				case "owner-replaced":
					path := filepath.Join(root, "owner.json")
					data := integrationRead(t, path)
					if err := os.Rename(path, filepath.Join(t.TempDir(), "old-owner")); err != nil {
						t.Fatal(err)
					}
					writeFixture(t, path, data, 0600)
				case "inventory":
					writeFixture(t, filepath.Join(root, "versions", "0.22.6", "source.json"), []byte("{}"), 0600)
				case "root-replaced", "version-replaced":
					path := root
					if kind == "version-replaced" {
						path = dir
					}
					old := path + "-old"
					if err := os.Rename(path, old); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(path, 0700); err != nil {
						t.Fatal(err)
					}
					items, err := os.ReadDir(old)
					if err != nil {
						t.Fatal(err)
					}
					for _, item := range items {
						if err := os.Rename(filepath.Join(old, item.Name()), filepath.Join(path, item.Name())); err != nil {
							t.Fatal(err)
						}
					}
				case "pointer":
					writeFixture(t, pointer, []byte("manual pointer"), 0600)
				case "staged-pointer":
					matches, err := filepath.Glob(filepath.Join(root+".lock", ".hermes-select-*", "current.json"))
					if err != nil || len(matches) != 1 {
						t.Fatal(matches, err)
					}
					writeFixture(t, matches[0], []byte("changed staged pointer"), 0600)
				case "cancel":
					cancel()
				}
				return nil
			}
			err := f.run(ctx, "use", hermesUseArgs()...)
			if err == nil || !seen || !strings.Contains(err.Error(), "retained") {
				t.Fatal("late mutation not refused with retained state", seen, err)
			}
			if kind == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation lost", err)
			}
			if kind == "pointer" {
				if string(integrationRead(t, pointer)) != "manual pointer" {
					t.Fatal("late mutation overwrote pointer")
				}
			} else {
				integrationMissing(t, pointer)
			}
			integrationRead(t, filepath.Join(root+".lock", "selection.json"))
		})
	}
}

func TestHermesSelectionNoopGuards(t *testing.T) {
	for _, kind := range []string{"source", "completion", "pointer", "pointer-replaced", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			f := newHermesSelectionFixture(t, "amd64")
			root := f.b.root(hermesAgent)
			pointer := filepath.Join(root, "current.json")
			before := integrationRead(t, pointer)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			seen := false
			f.b.checkpoint = func(point string) error {
				if point != "hermes-before-use-noop" {
					t.Fatal("no-op reached publication", point)
				}
				seen = true
				integrationMissing(t, filepath.Join(root+".lock", "selection.json"))
				switch kind {
				case "source":
					writeFixture(t, filepath.Join(root, "versions", hermesPMVersion, "source", "pm", "cli.py"), []byte("changed"), 0644)
				case "completion":
					writeFixture(t, filepath.Join(root, "versions", hermesPMVersion, "complete.json"), []byte("{}"), 0600)
				case "pointer":
					before = []byte("manual pointer")
					writeFixture(t, pointer, before, 0600)
				case "pointer-replaced":
					if err := os.Rename(pointer, filepath.Join(t.TempDir(), "old-current")); err != nil {
						t.Fatal(err)
					}
					writeFixture(t, pointer, before, 0600)
				case "cancel":
					cancel()
				}
				return nil
			}
			err := f.run(ctx, "use", hermesUseArgs()...)
			if err == nil || !seen {
				t.Fatal("late no-op mutation accepted", seen, err)
			}
			if kind == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation lost", err)
			}
			if !bytes.Equal(before, integrationRead(t, pointer)) {
				t.Fatal("no-op changed pointer")
			}
			integrationMissing(t, root+".lock")
		})
	}
}

func TestHermesSelectionUncertainPublication(t *testing.T) {
	for _, kind := range []string{"error", "cancel", "source", "completion", "pointer"} {
		t.Run(kind, func(t *testing.T) {
			f := newHermesSelectionFixture(t, "amd64")
			root := f.b.root(hermesAgent)
			pointer := filepath.Join(root, "current.json")
			if err := os.Remove(pointer); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f.b.checkpoint = func(point string) error {
				if point != "hermes-after-select" {
					return nil
				}
				switch kind {
				case "error":
					return errors.New("uncertain final state")
				case "cancel":
					cancel()
				case "source":
					writeFixture(t, filepath.Join(root, "versions", hermesPMVersion, "source", "pm", "cli.py"), []byte("changed"), 0644)
				case "completion":
					writeFixture(t, filepath.Join(root, "versions", hermesPMVersion, "complete.json"), []byte("{}"), 0600)
				case "pointer":
					writeFixture(t, pointer, []byte("{}"), 0600)
				}
				return nil
			}
			err := f.run(ctx, "use", hermesUseArgs()...)
			if err == nil || !strings.Contains(err.Error(), "selection may have changed") {
				t.Fatal("uncertain publication accepted", err)
			}
			integrationRead(t, pointer) // Published selection is not silently rolled back.
			var journal struct {
				Schema int
				Before pathState
				After  string
			}
			if err := json.Unmarshal(integrationRead(t, filepath.Join(root+".lock", "selection.json")), &journal); err != nil || journal.Before.Exists || journal.After != hermesPMVersion {
				t.Fatal("missing recovery evidence", journal, err)
			}
		})
	}
}

func TestHermesSelectionAlreadyCancelled(t *testing.T) {
	f := newHermesSelectionFixture(t, "amd64")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := f.run(ctx, "use", hermesUseArgs()...); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	integrationMissing(t, f.b.root(hermesAgent)+".lock")
}

func TestHermesSelectionPinsCoherentSource(t *testing.T) {
	f := newHermesSelectionFixture(t, "amd64")
	root := f.b.root(hermesAgent)
	dir := filepath.Join(root, "versions", hermesPMVersion)
	pointer := filepath.Join(root, "current.json")
	if err := os.Remove(pointer); err != nil {
		t.Fatal(err)
	}
	seen := false
	f.b.checkpoint = func(point string) error {
		if point != "hermes-before-select" {
			return nil
		}
		seen = true
		path := filepath.Join(dir, "source.json")
		rec, err := hermesReadRecord(path)
		if err != nil {
			t.Fatal(err)
		}
		body := []byte("# coherently replaced source, never executed\n")
		writeFixture(t, filepath.Join(dir, "source", "pm", "cli.py"), body, 0644)
		rec.Source.Files["pm/cli.py"] = hermesSourceFile{100644, int64(len(body)), hermesObjectHash("blob", body)}
		trees, err := hermesSourceTrees(context.Background(), rec.Source)
		if err != nil {
			t.Fatal(err)
		}
		rec.Source.Tree = trees["."]
		data, err := json.Marshal(rec)
		if err != nil {
			t.Fatal(err)
		}
		writeFixture(t, path, data, 0600)
		var complete hermesCompletion
		path = filepath.Join(dir, "complete.json")
		if err := readJSON(path, &complete); err != nil {
			t.Fatal(err)
		}
		// Only the hostile fixture regenerates evidence. The selection route
		// must continue to compare against its original receipts.
		complete.Evidence, err = hermesCompletionSnapshot(context.Background(), dir, complete.Evidence.StartedAt, complete.Evidence.VerifiedAt, true)
		if err != nil {
			t.Fatal(err)
		}
		data, err = json.Marshal(complete)
		if err != nil {
			t.Fatal(err)
		}
		writeFixture(t, path, data, 0600)
		if _, _, err := f.b.hermesInspect(context.Background()); err != nil {
			t.Fatal("replacement fixture must remain internally consistent", err)
		}
		return nil
	}
	if err := f.run(context.Background(), "use", hermesUseArgs()...); err == nil || !seen {
		t.Fatal("coherent source replacement accepted", seen, err)
	}
	integrationMissing(t, pointer)
	integrationRead(t, filepath.Join(root+".lock", "selection.json"))
}
