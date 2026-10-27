package core

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/AtomyCloud/atm/internal/config"
	"github.com/AtomyCloud/atm/internal/data"
	"github.com/AtomyCloud/atm/internal/plugin"
	"github.com/AtomyCloud/atm/internal/protocol"
	"github.com/AtomyCloud/atm/internal/runner"
)

// A copy of this test executable is a native, shell-free JSONL fixture. It only
// records requests in the temporary AppsDir supplied by the parent test.
func TestMain(m *testing.M) {
	if os.Getenv("ATM_TEST_CORE_INTEGRATION_HELPER") == "1" {
		os.Exit(integrationHelper())
	}
	os.Exit(m.Run())
}

func integrationHelper() int {
	r, err := protocol.ReadRequest(os.Stdin)
	if err != nil || r.Operation != "integrate" || len(r.Arguments) != 0 {
		return 2
	}
	f, err := os.OpenFile(filepath.Join(r.AppsDir, "requests.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return 3
	}
	err = json.NewEncoder(f).Encode(r)
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return 3
	}
	emit := func(kind, status string) {
		_ = json.NewEncoder(os.Stdout).Encode(protocol.Event{Protocol: 1, RequestID: r.RequestID, Type: kind, Status: status})
	}
	emit("started", "")
	switch r.PluginID {
	case "fail":
		emit("result", "error")
		return 7
	case "cancelled":
		emit("result", "cancelled")
		return 0
	case "hang":
		time.Sleep(time.Hour)
	}
	emit("result", "success")
	return 0
}

func integrationFixture(t *testing.T, ids ...string) (config.Config, []plugin.Plugin) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "ATM space \u00e7")
	c := config.Config{Home: root, PluginsDir: filepath.Join(root, "plugins"), AppsDir: filepath.Join(root, "apps"), ConfigDir: filepath.Join(root, "config"), StateDir: filepath.Join(root, "state"), Language: "en-us", Sort: "za", NativeTimeout: 5 * time.Second}
	for _, path := range []string{c.PluginsDir, c.AppsDir, c.ConfigDir, c.StateDir} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	name := "fixture"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	for i, id := range ids {
		dir := filepath.Join(c.PluginsDir, id)
		manifest := plugin.Manifest{Schema: 2, ID: id, Name: id, Version: "0.0.1", Order: i, MinAPI: 2, Platforms: []string{runtime.GOOS}, Executor: "native", Protocol: "jsonl-v1", Operations: []string{"integrate"}, Executables: map[string]string{runtime.GOOS + "/" + runtime.GOARCH: name}}
		if err := data.WriteJSON(filepath.Join(dir, "plugin.json"), manifest); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), binary, 0700); err != nil {
			t.Fatal(err)
		}
	}
	items, err := plugin.Discover(c, runtime.GOOS)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("ATM_TEST_CORE_INTEGRATION_HELPER", "1")
	t.Setenv("PATH", "")
	return c, items
}

func integrationRequests(t *testing.T, c config.Config) []protocol.Request {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(c.AppsDir, "requests.jsonl"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var requests []protocol.Request
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var r protocol.Request
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatal(err)
		}
		requests = append(requests, r)
	}
	return requests
}

func integrationSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		b := []byte{}
		if !entry.IsDir() {
			b, err = os.ReadFile(path)
			if err != nil {
				return err
			}
		}
		result[path] = fmt.Sprintf("%v:%x", info.Mode(), sha256.Sum256(b))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestIntegrationMetadataOrderAndNativeRequest(t *testing.T) {
	c, items := integrationFixture(t, "alpha", "zulu")
	before := integrationSnapshot(t, c.Home)
	p, err := IntegrationPlan(c, items)
	if err != nil || p.Executor != "native-core" || !p.Mutating || !reflect.DeepEqual(p.Arguments, []string{"path", "apply", "--plugins"}) {
		t.Fatal(p, err)
	}
	if p.Plugins[0].PluginID != "alpha" || p.Plugins[1].PluginID != "zulu" || items[0].ID != "zulu" {
		t.Fatal("metadata order or input slice changed", p, items)
	}
	if !reflect.DeepEqual(before, integrationSnapshot(t, c.Home)) {
		t.Fatal("planning changed files")
	}
	r, err := ExecuteIntegration(context.Background(), c, p, true)
	if err != nil || len(r.Items) != 2 || r.Items[0].Status != "success" || r.Items[1].Status != "success" {
		t.Fatal(r, err)
	}
	requests := integrationRequests(t, c)
	if len(requests) != 2 || requests[0].PluginID != "alpha" || requests[1].PluginID != "zulu" {
		t.Fatal(requests)
	}
	for _, req := range requests {
		if req.Operation != "integrate" || len(req.Arguments) != 0 || req.Home != c.Home || req.AppsDir != c.AppsDir || req.Language != c.Language {
			t.Fatal(req)
		}
	}
}

func TestIntegrationUnavailableSelections(t *testing.T) {
	c, items := integrationFixture(t, "available", "disabled", "unsupported", "missing", "platform")
	for i := range items {
		switch items[i].ID {
		case "disabled":
			items[i].Enabled = false
		case "unsupported":
			items[i].Operations = []string{"status"}
		case "missing":
			for _, name := range items[i].Executables {
				if err := os.Remove(filepath.Join(items[i].Directory, name)); err != nil {
					t.Fatal(err)
				}
			}
		case "platform":
			items[i].Available, items[i].Reason = false, "target unavailable"
		}
	}
	legacy := plugin.Plugin{Manifest: plugin.Manifest{Schema: 1, ID: "legacy", Executor: "legacy-bash", Protocol: "tty-v1", Operations: []string{"install"}}, Enabled: true, Available: true}
	items = append(items, legacy)
	p, err := IntegrationPlan(c, items)
	if err != nil || len(p.Skipped) != 5 || len(p.Plugins) != 1 {
		t.Fatal(p, err)
	}
	r, err := ExecuteIntegration(context.Background(), c, p, true)
	if err == nil || !strings.Contains(err.Error(), "plugin integration incomplete") || len(r.Items) != 6 {
		t.Fatal(r, err)
	}
	for _, item := range r.Items[:5] {
		if item.Status != "unavailable" || item.Error == "" {
			t.Fatal(item)
		}
	}
	requests := integrationRequests(t, c)
	if len(requests) != 1 || requests[0].PluginID != "available" {
		t.Fatal(requests)
	}
	for _, selection := range [][]plugin.Plugin{{legacy}, nil} {
		p, err = IntegrationPlan(c, selection)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = ExecuteIntegration(context.Background(), c, p, true); err == nil {
			t.Fatal("empty/legacy-only integration reported success")
		}
	}
}

func TestIntegrationConsentDryRunAndPlanGuards(t *testing.T) {
	c, items := integrationFixture(t, "first", "second")
	for _, mode := range []string{"no-consent", "config-dry-run", "plan-dry-run", "legacy", "wrong-operation", "arguments", "child-dry-run"} {
		t.Run(mode, func(t *testing.T) {
			c := c
			p, err := IntegrationPlan(c, items)
			if err != nil {
				t.Fatal(err)
			}
			consent := true
			switch mode {
			case "no-consent":
				consent = false
				c.AssumeYes = true // Consent is an explicit call boundary, not ambient state.
			case "config-dry-run":
				c.DryRun = true
			case "plan-dry-run":
				p.DryRun = true
			case "legacy":
				p.Plugins[1].Executor = "legacy-bash"
			case "wrong-operation":
				p.Plugins[1].Operation = "install"
			case "arguments":
				p.Plugins[1].Arguments = []string{"unexpected"}
			case "child-dry-run":
				p.Plugins[1].DryRun = true
			}
			before := integrationSnapshot(t, c.Home)
			if _, err := ExecuteIntegration(context.Background(), c, p, consent); err == nil {
				t.Fatal("guard accepted", mode)
			}
			if !reflect.DeepEqual(before, integrationSnapshot(t, c.Home)) || len(integrationRequests(t, c)) != 0 {
				t.Fatal("guard wrote files or executed a plugin", mode)
			}
		})
	}
	c.DryRun = true
	p, err := IntegrationPlan(c, items)
	if err != nil || !p.DryRun || !p.Plugins[0].DryRun {
		t.Fatal(p, err)
	}
	for _, selection := range [][]plugin.Plugin{{items[0], items[0]}, {{Manifest: plugin.Manifest{ID: "../bad"}}}} {
		if _, err := IntegrationPlan(c, selection); err == nil {
			t.Fatal("invalid selected identities accepted")
		}
	}
}

func TestIntegrationFailureAndCancellation(t *testing.T) {
	for _, tc := range []struct {
		id   string
		exit int
	}{{"fail", 7}, {"cancelled", 130}, {"hang", 124}} {
		t.Run(tc.id, func(t *testing.T) {
			c, items := integrationFixture(t, tc.id, "later")
			if tc.id == "hang" {
				c.NativeTimeout = 200 * time.Millisecond
			}
			p, err := IntegrationPlan(c, items)
			if err != nil {
				t.Fatal(err)
			}
			r, err := ExecuteIntegration(context.Background(), c, p, true)
			if err == nil || runner.ExitCode(err) != tc.exit || len(r.Items) != 2 || r.Items[0].Status != "error" || r.Items[1].Status != "not_run" {
				t.Fatal(r, err)
			}
			for _, req := range integrationRequests(t, c) {
				if req.PluginID == "later" {
					t.Fatal("executed after failure")
				}
			}
		})
	}
	c, items := integrationFixture(t, "hang", "later")
	p, err := IntegrationPlan(c, items)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, err := ExecuteIntegration(ctx, c, p, true)
	if !errors.Is(err, context.Canceled) || len(integrationRequests(t, c)) != 0 || r.Items[0].Status != "not_run" {
		t.Fatal(r, err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		deadline := time.NewTimer(3 * time.Second)
		defer deadline.Stop()
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-tick.C:
				if _, err := os.Stat(filepath.Join(c.AppsDir, "requests.jsonl")); err == nil {
					cancel()
					return
				}
			case <-deadline.C:
				cancel()
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	r, err = ExecuteIntegration(ctx, c, p, true)
	<-done
	if runner.ExitCode(err) != 130 || r.Items[1].Status != "not_run" || len(integrationRequests(t, c)) != 1 {
		t.Fatal(r, err)
	}
}

func TestIntegrationManifestRecheck(t *testing.T) {
	c, items := integrationFixture(t, "changed", "later")
	p, err := IntegrationPlan(c, items)
	if err != nil {
		t.Fatal(err)
	}
	item, err := plugin.Find(items, "changed")
	if err != nil {
		t.Fatal(err)
	}
	item.Operations = []string{"status"}
	if err := data.WriteJSON(filepath.Join(item.Directory, "plugin.json"), item.Manifest); err != nil {
		t.Fatal(err)
	}
	r, err := ExecuteIntegration(context.Background(), c, p, true)
	if err == nil || r.Items[0].Status != "error" || r.Items[1].Status != "not_run" || len(integrationRequests(t, c)) != 0 {
		t.Fatal(r, err)
	}
}
