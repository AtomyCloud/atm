// Package core owns native administration and generic plugin orchestration.
package core

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/AtomyCloud/atm/internal/config"
	"github.com/AtomyCloud/atm/internal/plugin"
	"github.com/AtomyCloud/atm/internal/runner"
)

type Skipped struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

type Plan struct {
	Executor  string        `json:"executor"`
	Arguments []string      `json:"arguments"`
	DryRun    bool          `json:"dry_run"`
	Mutating  bool          `json:"mutating"`
	Plugins   []runner.Plan `json:"plugins,omitempty"`
	Skipped   []Skipped     `json:"skipped,omitempty"`
}

// Prepare is offline and read-only. It never falls back to the Bash CORE.
func Prepare(c config.Config, args []string, plugins []plugin.Plugin) (Plan, error) {
	p := Plan{Executor: "native-core", Arguments: append([]string{}, args...), DryRun: c.DryRun}
	if len(args) == 0 {
		return p, fmt.Errorf("missing CORE operation")
	}
	if args[0] == "self-update" && len(args) == 2 && args[1] == "--check" {
		p.Arguments = []string{"updates", "check"}
	}
	if args[0] == "self-update" && len(args) == 1 {
		p.Arguments = []string{"updates", "core"}
	}
	if args[0] == "plugins" && len(args) >= 2 {
		switch args[1] {
		case "check-updates":
			if len(args) == 2 {
				p.Arguments = []string{"updates", "check"}
			}
		case "update":
			if len(args) == 3 {
				p.Arguments = []string{"updates", "plugin", args[2]}
				if args[2] == "--all" {
					p.Arguments = []string{"updates", "all"}
				}
			}
		}
	}
	a := p.Arguments
	valid := false
	switch a[0] {
	case "updates":
		if len(a) == 2 {
			valid = slices.Contains([]string{"check", "check-core", "changes", "backups", "all", "core", "trust"}, a[1])
			p.Mutating = a[1] == "all" || a[1] == "core"
		}
		if len(a) == 3 {
			valid = (a[1] == "plugin" && plugin.IDPattern.MatchString(a[2])) ||
				((a[1] == "restore" || a[1] == "recover") && safeToken(a[2]))
			p.Mutating = true
		}
		if len(a) == 4 && a[1] == "verify" {
			valid = plugin.IDPattern.MatchString(strings.TrimPrefix(a[2], "plugin:")) && commitID(a[3])
			p.Mutating = true
		}
		if len(a) == 5 && slices.Contains([]string{"verify-package", "enroll-core", "core"}, a[1]) {
			valid = true
			p.Mutating = a[1] != "verify-package"
			for _, path := range a[2:] {
				valid = valid && path != "" && !strings.ContainsAny(path, "\x00\r\n")
			}
		}
	case "path":
		valid = len(a) == 2 && (a[1] == "apply" || a[1] == "show")
		p.Mutating = len(a) == 2 && a[1] == "apply"
		if len(a) == 3 && a[1] == "apply" && a[2] == "--plugins" {
			return IntegrationPlan(c, plugins)
		}
	case "setup":
		valid = len(a) == 2 && a[1] == "portable"
		if len(a) == 4 && a[1] == "portable" && a[2] == "--manifest-sha256" {
			valid = len(a[3]) == 64
			for _, ch := range a[3] {
				valid = valid && (ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f')
			}
		}
		p.Mutating = true
	case "install":
		valid = len(a) == 1
		p.Mutating = true
		if valid {
			ordered := append([]plugin.Plugin{}, plugins...)
			plugin.Sort(ordered, "default")
			for _, item := range ordered {
				if !item.Enabled || !item.FullSetup {
					continue
				}
				if !item.Available || !slices.Contains(item.Operations, "install") {
					reason := item.Reason
					if reason == "" {
						reason = "install operation unavailable"
					}
					p.Skipped = append(p.Skipped, Skipped{item.ID, reason})
					continue
				}
				plan, err := runner.Plugin(c, item, "install", nil)
				if err != nil {
					return p, err
				}
				plan.PluginID, plan.Operation = item.ID, "install"
				p.Plugins = append(p.Plugins, plan)
			}
		}
	}
	if !valid {
		return p, fmt.Errorf("unsupported native CORE operation: %s", strings.Join(args, " "))
	}
	return p, nil
}

func safeToken(s string) bool {
	if s == "" || len(s) > 150 || s == "." || s == ".." {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}

func commitID(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'f' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

type BatchItem struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}
type BatchResult struct {
	Items []BatchItem `json:"items"`
}

// RunBatch stops on failure; subsequent entries are reported, never silently run.
func RunBatch(ctx context.Context, p Plan, run func(context.Context, runner.Plan) error) (BatchResult, error) {
	r := BatchResult{Items: []BatchItem{}}
	if p.DryRun {
		return r, fmt.Errorf("dry-run cannot execute Full Setup")
	}
	for _, skip := range p.Skipped {
		r.Items = append(r.Items, BatchItem{skip.ID, "unavailable", skip.Reason})
	}
	var failure error
	for _, item := range p.Plugins {
		if failure == nil {
			failure = ctx.Err()
		}
		if failure != nil {
			r.Items = append(r.Items, BatchItem{item.PluginID, "not_run", "previous operation failed or cancelled"})
			continue
		}
		if failure = run(ctx, item); failure != nil {
			r.Items = append(r.Items, BatchItem{item.PluginID, "error", failure.Error()})
		} else {
			r.Items = append(r.Items, BatchItem{item.PluginID, "success", ""})
		}
	}
	if failure == nil && len(p.Skipped) != 0 {
		failure = fmt.Errorf("Full Setup incomplete: %d selected plugins unavailable", len(p.Skipped))
	}
	return r, failure
}
