package updates

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

// Construct the exact on-disk prepared transaction before testing process exits
// at each publication boundary. No application installer is ever executed.
func prepareTransaction(t *testing.T, f *fixture) transaction {
	t.Helper()
	before, err := scan(f.s.root("alpha"))
	if err != nil {
		t.Fatal(err)
	}
	f.current = newCommit
	p, err := f.s.client.Prepare(context.Background(), "alpha", nil)
	if err != nil {
		t.Fatal(err)
	}
	id, err := newID()
	if err != nil {
		t.Fatal(err)
	}
	stage := f.s.stageDir(id)
	if err = os.Mkdir(stage, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.client.Download(context.Background(), p, stage); err != nil {
		t.Fatal(err)
	}
	after, err := scan(filepath.Join(stage, "alpha"))
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := readOptional(filepath.Join(stage, "alpha/.atm-origin.json"))
	if err != nil {
		t.Fatal(err)
	}
	tx := transaction{Backup: Backup{ID: id, Component: "plugin:alpha", Commit: newCommit, Phase: "prepared"}, Schema: 1, Home: f.s.options.Home, Plugins: f.s.options.PluginsDir, Plugin: "alpha", Before: before, After: after, ReceiptAfter: receipt}
	if err = os.MkdirAll(f.s.backupDir(id), 0700); err != nil {
		t.Fatal(err)
	}
	if err = copyTree(context.Background().Err, f.s.root("alpha"), filepath.Join(f.s.backupDir(id), "before"), before); err != nil {
		t.Fatal(err)
	}
	if err = writeJSON(filepath.Join(f.s.backupDir(id), "metadata.json"), tx); err != nil {
		t.Fatal(err)
	}
	return tx
}
func TestCrashHelper(t *testing.T) {
	if os.Getenv("ATM_UPDATE_CRASH_HELPER") != "1" {
		return
	}
	var o Options
	if err := json.Unmarshal([]byte(os.Getenv("ATM_UPDATE_OPTIONS")), &o); err != nil {
		t.Fatal(err)
	}
	s, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := s.readBackup(os.Getenv("ATM_UPDATE_BACKUP"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.lock(false)
	if err != nil {
		t.Fatal(err)
	} // Deliberately bypass release on exit.
	if err = writeJSON(s.pendingPath(), map[string]string{"backup": tx.ID, "state": s.state}); err != nil {
		t.Fatal(err)
	}
	phase := os.Getenv("ATM_UPDATE_PHASE")
	if phase == "prepared" {
		os.Exit(23)
	}
	if err = noReplace(s.root(tx.Plugin), filepath.Join(s.stageDir(tx.ID), "old")); err != nil {
		t.Fatal(err)
	}
	if phase == "displaced" {
		os.Exit(23)
	}
	if err = noReplace(filepath.Join(s.stageDir(tx.ID), tx.Plugin), s.root(tx.Plugin)); err != nil {
		t.Fatal(err)
	}
	if phase == "published" {
		os.Exit(23)
	}
	if err = s.setReceipt(tx.Plugin, tx.ReceiptAfter); err != nil {
		t.Fatal(err)
	}
	if phase == "receipt" {
		os.Exit(23)
	}
	tx.Phase = "applied"
	if err = writeJSON(filepath.Join(s.backupDir(tx.ID), "metadata.json"), tx); err != nil {
		t.Fatal(err)
	}
	os.Exit(23)
}
func TestRecoveryAfterActualProcessExit(t *testing.T) {
	for _, phase := range []string{"prepared", "displaced", "published", "receipt", "committed"} {
		t.Run(phase, func(t *testing.T) {
			f := fixtureService(t)
			f.install(t, "alpha")
			tx := prepareTransaction(t, f)
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			o := f.s.options
			o.Client = nil
			b, _ := json.Marshal(o)
			cmd := exec.Command(exe, "-test.run=^TestCrashHelper$")
			cmd.Env = append(os.Environ(), "ATM_UPDATE_CRASH_HELPER=1", "ATM_UPDATE_OPTIONS="+string(b), "ATM_UPDATE_BACKUP="+tx.ID, "ATM_UPDATE_PHASE="+phase, "PATH="+t.TempDir())
			output, err := cmd.CombinedOutput()
			exit, ok := err.(*exec.ExitError)
			if !ok || exit.ExitCode() != 23 {
				t.Fatalf("helper: %s %v", output, err)
			}
			if _, err = f.s.Recover(context.Background(), false); err == nil {
				t.Fatal("recovery without consent")
			}
			result, err := f.s.Recover(context.Background(), true)
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != "success" {
				t.Fatal(result)
			}
			restored, err := scan(f.s.root("alpha"))
			if err != nil || !reflect.DeepEqual(tx.Before, restored) {
				t.Fatal("crash recovery mismatch", err)
			}
			if b, err := readOptional(f.s.receiptPath("alpha")); err != nil || b != nil {
				t.Fatal("receipt recovery mismatch", err)
			}
			if _, err = os.Stat(f.s.pendingPath()); !os.IsNotExist(err) {
				t.Fatal("pending journal retained", err)
			}
			if result, err = f.s.Recover(context.Background(), true); err != nil || result.Status != "current" {
				t.Fatal("recovery is not idempotent", result, err)
			}
		})
	}
}
func TestRecoveryRefusesChangedFilesAndBackups(t *testing.T) {
	for _, name := range []string{"current", "old", "backup", "receipt", "state", "traversal"} {
		t.Run(name, func(t *testing.T) {
			f := fixtureService(t)
			f.install(t, "alpha")
			tx := prepareTransaction(t, f)
			noReplace(f.s.root("alpha"), filepath.Join(f.s.stageDir(tx.ID), "old"))
			noReplace(filepath.Join(f.s.stageDir(tx.ID), "alpha"), f.s.root("alpha"))
			pointer := map[string]string{"backup": tx.ID, "state": f.s.state}
			switch name {
			case "current":
				write(t, filepath.Join(f.s.root("alpha"), "plugin.conf"), "user edit")
			case "old":
				write(t, filepath.Join(f.s.stageDir(tx.ID), "old/plugin.conf"), "user edit")
			case "backup":
				write(t, filepath.Join(f.s.backupDir(tx.ID), "before/plugin.conf"), "user edit")
			case "receipt":
				write(t, f.s.receiptPath("alpha"), "user edit")
			case "state":
				pointer["state"] = "wrong"
			case "traversal":
				pointer["backup"] = "../outside"
			}
			writeJSON(f.s.pendingPath(), pointer)
			before, _ := scan(f.s.root("alpha"))
			if _, err := f.s.Recover(context.Background(), true); err == nil {
				t.Fatal("unsafe recovery succeeded")
			}
			after, _ := scan(f.s.root("alpha"))
			if !reflect.DeepEqual(before, after) {
				t.Fatal("recovery overwrote edits")
			}
			if _, err := os.Stat(f.s.pendingPath()); err != nil {
				t.Fatal("recovery journal lost")
			}
		})
	}
}
func TestCanceledDownloadAndConcurrentEdit(t *testing.T) {
	for _, cancelDownload := range []bool{true, false} {
		t.Run(fmt.Sprint(cancelDownload), func(t *testing.T) {
			f := fixtureService(t)
			f.install(t, "alpha")
			p := prepare(t, f)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			f.onBlob = func() {
				calls++
				if calls == 2 {
					if cancelDownload {
						cancel()
					} else {
						write(t, filepath.Join(f.s.root("alpha"), "unowned.txt"), "new edit")
					}
				}
			}
			if _, err := f.s.Apply(ctx, p, true); err == nil {
				t.Fatal("interrupted operation succeeded")
			}
			b, _ := os.ReadFile(filepath.Join(f.s.root("alpha"), "plugin.sh"))
			if string(b) != "#!/usr/bin/env bash\n# old\n" {
				t.Fatal("installed files changed")
			}
			entries, _ := os.ReadDir(f.s.options.PluginsDir)
			for _, e := range entries {
				if len(e.Name()) >= 12 && e.Name()[:12] == ".atm-update-" {
					t.Fatal("staging retained after pre-publication failure", e.Name())
				}
			}
		})
	}
}
func TestAPIRequirementBlocksUpdate(t *testing.T) {
	f := fixtureService(t)
	f.install(t, "alpha")
	f.trees[newCommit] = nil
	f.addPlugin(newCommit, "alpha", "new")
	// Prefer a valid JSON manifest while the legacy declaration requires a newer API.
	f.add(newCommit, "alpha", "plugin.json", `{"schema":1,"id":"alpha","name":"Fixture","version":"0.0.1","min_core_api":1,"platforms":["linux"],"executor":"legacy-bash","protocol":"tty-v1","operations":[]}`)
	f.trees[newCommit] = f.trees[newCommit][1:]
	f.add(newCommit, "alpha", "plugin.metadata", "ATM_PLUGIN_ID=alpha\nATM_PLUGIN_NAME_VALUE=Fixture\nATM_PLUGIN_VERSION_VALUE=0.0.1\nATM_PLUGIN_MIN_CORE_API_VALUE=999999\n")
	f.current = newCommit
	status(t, f.s, "plugin:alpha", "blocked")
	if _, err := f.s.Prepare(context.Background(), "alpha"); err == nil {
		t.Fatal("incompatible API prepared")
	}
}
func TestRootsAndLinkGuards(t *testing.T) {
	f := fixtureService(t)
	for _, o := range []Options{
		{Home: f.s.options.Home, PluginsDir: f.s.options.Home, StateDir: f.s.options.StateDir},
		{Home: f.s.options.Home, PluginsDir: f.s.options.PluginsDir, StateDir: filepath.Join(f.s.options.PluginsDir, "alpha", "state")},
		{Home: f.s.options.Home, PluginsDir: filepath.Join(f.s.options.Home, "bin"), StateDir: f.s.options.StateDir},
	} {
		if _, err := New(o); err == nil {
			t.Fatal("overlap accepted", o)
		}
	}
	f.install(t, "alpha")
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(f.s.root("alpha"), "escape")); err != nil {
		t.Skip(err)
	}
	status(t, f.s, "plugin:alpha", "unknown")
	if _, err := f.s.Verify(context.Background(), "alpha", oldCommit, true); err == nil {
		t.Fatal("linked payload verified")
	}
}

func TestPublicationNeverReplacesExistingDirectory(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source, target := filepath.Join(root, "source"), filepath.Join(root, "target")
	write(t, filepath.Join(source, "payload"), "original")
	if err = os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err = noReplace(source, target); err == nil {
		t.Fatal("empty destination overwritten")
	}
	b, err := os.ReadFile(filepath.Join(source, "payload"))
	if err != nil || string(b) != "original" {
		t.Fatal("source lost", err)
	}
	entries, err := os.ReadDir(target)
	if err != nil || len(entries) != 0 {
		t.Fatal("destination changed", err)
	}
}
