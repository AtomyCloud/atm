package core

import (
	"context"
	"fmt"
	"slices"

	"github.com/AtomyCloud/atm/internal/config"
	"github.com/AtomyCloud/atm/internal/plugin"
	"github.com/AtomyCloud/atm/internal/runner"
)

// IntegrationPlan prepares plugin-owned PATH/CLI/desktop integration without
// executing code. Every supplied plugin is selected; unavailable selections are
// reported, including disabled plugins. FullSetup and display sorting do not
// affect this separate, explicitly requested operation.
func IntegrationPlan(c config.Config, plugins []plugin.Plugin) (Plan, error) {
	p := Plan{Executor: "native-core", Arguments: []string{"path", "apply", "--plugins"}, DryRun: c.DryRun, Mutating: true}
	ordered := append([]plugin.Plugin{}, plugins...)
	plugin.Sort(ordered, "default")
	seen := map[string]bool{}
	for _, item := range ordered {
		if !plugin.IDPattern.MatchString(item.ID) || seen[item.ID] {
			return p, fmt.Errorf("invalid or duplicate selected plugin ID: %s", item.ID)
		}
		seen[item.ID] = true
		reason := ""
		switch {
		case !item.Enabled:
			reason = "plugin disabled"
		case item.Schema != 2 || item.Executor != "native" || item.Protocol != "jsonl-v1":
			reason = "native integration backend unavailable; legacy plugins are not executed"
		case !item.Available:
			reason = item.Reason
			if reason == "" {
				reason = "native integration backend unavailable on this platform"
			}
		case !slices.Contains(item.Operations, "integrate"):
			reason = "integrate operation unavailable"
		}
		if reason == "" {
			plan, err := runner.Plugin(c, item, "integrate", nil)
			if err == nil {
				p.Plugins = append(p.Plugins, plan)
				continue
			}
			reason = err.Error()
		}
		p.Skipped = append(p.Skipped, Skipped{ID: item.ID, Reason: reason})
	}
	return p, nil
}

// ExecuteIntegration requires fresh explicit consent and uses the native runner's
// manifest/recovery rechecks, bounded JSONL and process-tree cancellation. There
// is deliberately no legacy fallback or application-specific implementation.
func ExecuteIntegration(ctx context.Context, c config.Config, p Plan, consent bool) (BatchResult, error) {
	empty := BatchResult{Items: []BatchItem{}}
	if c.DryRun || p.DryRun {
		return empty, fmt.Errorf("dry-run cannot execute plugin integration")
	}
	if !consent {
		return empty, fmt.Errorf("explicit plugin integration consent required")
	}
	if p.Executor != "native-core" || !p.Mutating {
		return empty, fmt.Errorf("invalid plugin integration plan")
	}
	// Validate the whole batch before any selected backend can start.
	for _, item := range p.Plugins {
		if item.Schema != 2 || item.Executor != "native" || item.Protocol != "jsonl-v1" || item.Operation != "integrate" || len(item.Arguments) != 0 || item.DryRun {
			return empty, fmt.Errorf("invalid native integration plan for %s", item.PluginID)
		}
	}
	// RunBatch owns stop-on-failure behavior; report integration-specific skips
	// here so its Full Setup diagnostic does not mislabel this separate action.
	skipped := p.Skipped
	p.Skipped = nil
	r, err := RunBatch(ctx, p, func(ctx context.Context, plan runner.Plan) error {
		return runner.RunNative(ctx, c, plan, runner.NativeOptions{Timeout: c.NativeTimeout}).Err()
	})
	items := make([]BatchItem, 0, len(skipped)+len(r.Items))
	for _, skip := range skipped {
		items = append(items, BatchItem{ID: skip.ID, Status: "unavailable", Error: skip.Reason})
	}
	r.Items = append(items, r.Items...)
	if err == nil {
		err = ctx.Err()
	}
	if err == nil && len(skipped) > 0 {
		err = fmt.Errorf("plugin integration incomplete: %d selected plugins unavailable", len(skipped))
	}
	if err == nil && len(r.Items) == 0 {
		err = fmt.Errorf("no plugins selected for integration")
	}
	return r, err
}
