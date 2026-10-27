package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/AtomyCloud/atm/internal/catalog"
	"github.com/AtomyCloud/atm/internal/config"
	"github.com/AtomyCloud/atm/internal/coreupdate"
	"github.com/AtomyCloud/atm/internal/setup"
	"github.com/AtomyCloud/atm/internal/updates"
)

type Preview struct {
	Plan        Plan                   `json:"plan"`
	Updates     []catalog.DownloadInfo `json:"updates,omitempty"`
	States      []updates.Result       `json:"states,omitempty"`
	Destination string                 `json:"destination,omitempty"`
}

// Execution retains pinned update plans through the confirmation boundary.
type Execution struct {
	Preview Preview
	service *updates.Service
	plans   []*updates.Plan
	setup   setup.Options
}

type Result struct {
	Operation      string   `json:"operation"`
	Value          any      `json:"result"`
	ChangedPlugins []string `json:"changed_plugins,omitempty"`
}

func IsCommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "updates", "path", "setup", "self-update":
		return true
	case "install":
		return len(args) == 1
	case "plugins":
		return len(args) > 1 && (args[1] == "check-updates" || args[1] == "update")
	}
	return false
}

func Resolve(ctx context.Context, c config.Config, p Plan) (*Execution, error) {
	if len(p.Arguments) == 0 {
		return nil, fmt.Errorf("missing CORE operation")
	}
	e := &Execution{Preview: Preview{Plan: p}}
	if p.DryRun || c.DryRun {
		return e, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p.Mutating {
		if err := coreupdate.StartupGuard(c.Home); err != nil {
			return nil, err
		}
	}
	a := p.Arguments
	if a[0] == "updates" {
		if a[1] == "core" {
			return nil, fmt.Errorf("%s", coreupdate.ProductionStatus)
		}
		if a[1] == "trust" || a[1] == "verify-package" {
			return e, nil
		}
		state := c.StateDir
		if state == "" {
			state = c.ConfigDir
		}
		s, err := updates.New(updates.Options{Home: c.Home, PluginsDir: c.PluginsDir, StateDir: state, Client: catalog.New(os.Getenv("GITHUB_TOKEN"))})
		if err != nil {
			return nil, err
		}
		e.service = s
		switch a[1] {
		case "plugin":
			p, err := s.Prepare(ctx, a[2])
			if err != nil {
				return nil, err
			}
			e.plans = []*updates.Plan{p}
		case "all":
			var err error
			e.plans, e.Preview.States, err = s.PrepareAll(ctx)
			if err != nil {
				return nil, err
			}
		}
		for _, plan := range e.plans {
			e.Preview.Updates = append(e.Preview.Updates, plan.Info())
		}
	}
	if a[0] == "path" || a[0] == "setup" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		e.setup = setup.DefaultOptions(c, home)
		if len(a) == 4 {
			e.setup.TrustedManifestSHA256 = a[3]
		}
		e.Preview.Destination = filepath.Join(e.setup.AppsDir, "atm")
	}
	return e, nil
}

func Execute(ctx context.Context, c config.Config, e *Execution, consent bool) (Result, error) {
	r := Result{}
	if e == nil {
		return r, fmt.Errorf("missing prepared CORE operation")
	}
	p := e.Preview.Plan
	a := p.Arguments
	if len(a) == 0 {
		return r, fmt.Errorf("missing CORE operation")
	}
	r.Operation = strings.Join(a, " ")
	if c.DryRun || p.DryRun {
		return r, fmt.Errorf("dry-run cannot execute CORE operations")
	}
	if p.Mutating && !consent {
		return r, fmt.Errorf("explicit CORE consent required")
	}
	if p.Mutating {
		if err := coreupdate.StartupGuard(c.Home); err != nil {
			return r, err
		}
	}
	if err := ctx.Err(); err != nil {
		return r, err
	}
	var err error
	switch a[0] {
	case "path":
		o := e.setup
		o.Consent = consent
		if a[1] == "show" {
			r.Value, err = setup.PathBlock(o)
		} else {
			r.Value, err = setup.Integrate(ctx, o)
		}
	case "setup":
		o := e.setup
		o.Consent = consent
		r.Value, err = setup.Install(ctx, o)
	case "updates":
		if a[1] == "trust" {
			r.Value, err = coreupdate.OfficialTrust()
			return r, err
		}
		if a[1] == "verify-package" {
			r.Value, err = coreupdate.VerifyOfficialPackageFiles(ctx, a[2], a[3], a[4], runtime.GOOS, runtime.GOARCH)
			return r, err
		}
		s := e.service
		if s == nil {
			return r, fmt.Errorf("missing update service")
		}
		switch a[1] {
		case "check":
			r.Value, err = s.Check(ctx)
		case "changes":
			ids, e := s.Installed()
			if e != nil {
				return r, e
			}
			type item struct {
				ID      string           `json:"id"`
				Changes []updates.Change `json:"changes"`
				Error   string           `json:"error,omitempty"`
			}
			items := []item{}
			for _, id := range ids {
				if e := ctx.Err(); e != nil {
					return r, e
				}
				changes, e := s.Changes(id)
				v := item{ID: id, Changes: changes}
				if e != nil {
					v.Error = e.Error()
					err = errors.Join(err, fmt.Errorf("%s: %w", id, e))
				}
				items = append(items, v)
			}
			r.Value = items
		case "backups":
			r.Value, err = s.Backups()
		case "restore", "recover", "verify":
			var result updates.Result
			switch a[1] {
			case "restore":
				result, err = s.Restore(ctx, a[2], consent)
			case "recover":
				if a[2] != "plugins" {
					return r, fmt.Errorf("usage: updates recover plugins")
				}
				result, err = s.Recover(ctx, consent)
			case "verify":
				result, err = s.Verify(ctx, strings.TrimPrefix(a[2], "plugin:"), a[3], consent)
			}
			r.Value = result
			if result.RestartRequired {
				r.ChangedPlugins = append(r.ChangedPlugins, strings.TrimPrefix(result.Component, "plugin:"))
			}
		case "plugin", "all":
			results := append([]updates.Result{}, e.Preview.States...)
			for _, p := range e.plans {
				if ctxErr := ctx.Err(); ctxErr != nil {
					err = errors.Join(err, ctxErr)
					break
				}
				result, applyErr := s.Apply(ctx, p, consent)
				if applyErr != nil {
					err = errors.Join(err, applyErr)
					result.Component = "plugin:" + p.Info().ID
					result.Status = "error"
					result.Detail = applyErr.Error()
				}
				results = append(results, result)
				if result.RestartRequired {
					r.ChangedPlugins = append(r.ChangedPlugins, strings.TrimPrefix(result.Component, "plugin:"))
				}
			}
			r.Value = results
			for _, result := range results {
				if result.Status != "current" && result.Status != "updated" && result.Status != "success" {
					if err == nil {
						err = fmt.Errorf("one or more plugin updates were not applied; see per-plugin results")
					}
				}
			}
		}
	default:
		err = fmt.Errorf("operation requires the plugin orchestrator")
	}
	return r, err
}
