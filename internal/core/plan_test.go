package core

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"testing"

	"github.com/AtomyCloud/atm/internal/config"
	"github.com/AtomyCloud/atm/internal/plugin"
	"github.com/AtomyCloud/atm/internal/runner"
)

func TestNativeCoreRoutesWithoutShell(t *testing.T) {
	t.Setenv("PATH", "")
	c := config.Config{DryRun: true}
	for _, args := range [][]string{{"updates", "check"}, {"updates", "all"}, {"updates", "plugin", "node"}, {"updates", "changes"}, {"updates", "backups"}, {"updates", "restore", "backup-1"}, {"updates", "recover", "node"}, {"setup", "portable"}, {"path", "apply"}, {"install"}, {"self-update", "--check"}, {"plugins", "update", "--all"}} {
		p, err := Prepare(c, args, nil)
		if err != nil || p.Executor != "native-core" || !p.DryRun {
			t.Fatal(args, p, err)
		}
	}
	for _, args := range [][]string{{"self-update", "--git"}, {"updates", "plugin", "../bad"}, {"updates", "restore", "../bad"}, {"updates", "check", "extra"}, {"setup", "system"}, {"plugins", "update"}} {
		if _, err := Prepare(c, args, nil); err == nil {
			t.Fatal("accepted", args)
		}
	}
}

func TestFullSetupOrderAndEligibility(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("legacy plans are Linux-only")
	}
	makePlugin := func(id string, order int, enabled, full bool) plugin.Plugin {
		return plugin.Plugin{Manifest: plugin.Manifest{ID: id, Order: order, FullSetup: full, Executor: "legacy-bash", Operations: []string{"install"}}, Enabled: enabled, Available: true}
	}
	items := []plugin.Plugin{makePlugin("z", 2, true, true), makePlugin("a", 1, true, true), makePlugin("disabled", 0, false, true), makePlugin("optout", 0, true, false)}
	p, err := Prepare(config.Config{Sort: "za"}, []string{"install"}, items)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	result, err := RunBatch(context.Background(), p, func(_ context.Context, p runner.Plan) error { got = append(got, p.PluginID); return nil })
	if err != nil || !reflect.DeepEqual(got, []string{"a", "z"}) || len(result.Items) != 2 {
		t.Fatal(got, result, err)
	}
}

func TestBatchFailureCancelAndDryRun(t *testing.T) {
	p := Plan{Plugins: []runner.Plan{{PluginID: "a"}, {PluginID: "b"}}}
	calls := 0
	run := func(context.Context, runner.Plan) error { calls++; return errors.New("fixture failure") }
	r, err := RunBatch(context.Background(), p, run)
	if err == nil || calls != 1 || r.Items[1].Status != "not_run" {
		t.Fatal(r, err, calls)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls = 0
	_, err = RunBatch(ctx, p, run)
	if !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatal(err, calls)
	}
	p.DryRun = true
	if _, err = RunBatch(context.Background(), p, run); err == nil || calls != 0 {
		t.Fatal("dry-run executed")
	}
}
