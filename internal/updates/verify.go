package updates

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"

	"github.com/AtomyCloud/atm/internal/catalog"
	"github.com/AtomyCloud/atm/internal/data"
	"github.com/AtomyCloud/atm/internal/plugin"
)

// PrepareAll resolves one main snapshot before consent. Results describes
// installed plugins for which no replacement plan is available.
func (s *Service) PrepareAll(ctx context.Context) ([]*Plan, []Result, error) {
	ctx, cancel := context.WithTimeout(ctx, catalog.Timeout)
	defer cancel()
	ids, err := s.Installed()
	if err != nil {
		return nil, nil, err
	}
	plans := []*Plan{}
	results := []Result{}
	if s.options.DryRun {
		for _, id := range ids {
			results = append(results, Result{Component: "plugin:" + id, Status: "success", Detail: "dry-run: no update performed"})
		}
		return plans, results, nil
	}
	main, err := s.client.Snapshot(ctx)
	if err != nil {
		return nil, nil, err
	}
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return plans, results, err
		}
		row, p, _ := s.inspect(ctx, id, main)
		if p != nil {
			plans = append(plans, p)
		} else {
			results = append(results, Result{Component: row.Component, Status: row.Status, Detail: row.Detail})
		}
	}
	return plans, results, nil
}

// Verify is explicit baseline enrollment, never automatic adoption during Check.
// It writes only an origin receipt, never component code or application state.
func (s *Service) Verify(ctx context.Context, id, commit string, consent bool) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, catalog.Timeout)
	defer cancel()
	result := Result{Component: "plugin:" + id, Status: "failed"}
	if err := validID(id); err != nil {
		return result, err
	}
	if !commitPattern.MatchString(commit) {
		return result, fmt.Errorf("full official commit required")
	}
	if s.options.DryRun {
		result.Status = "success"
		result.Detail = "dry-run: no baseline verification performed"
		return result, nil
	}
	if !consent {
		return result, fmt.Errorf("explicit baseline verification consent required")
	}
	if err := s.guard(); err != nil {
		return result, err
	}
	main, err := s.client.Snapshot(ctx)
	if err != nil {
		return result, err
	}
	p, err := s.client.Baseline(ctx, id, commit, main)
	if err != nil {
		return result, err
	}
	required, err := s.client.RequiredAPI(ctx, p)
	if err != nil {
		return result, err
	}
	if required > plugin.API {
		return result, fmt.Errorf("incompatible plugin CORE API")
	}
	unlock, err := s.lock(false)
	if err != nil {
		return result, err
	}
	defer unlock()
	if err = s.noPending(); err != nil {
		return result, err
	}
	inv, err := scan(s.root(id))
	if err != nil {
		return result, err
	}
	owned := p.Fingerprints()
	if !matches(inv, owned) {
		return result, fmt.Errorf("installed files do not match official baseline")
	}
	if err = p.VerifyDirectory(ctx, s.root(id)); err != nil {
		return result, err
	}
	for n, f := range inv {
		if n == ".atm-origin.json" || f.Mode == "dir" {
			continue
		}
		if _, ok := owned[n]; !ok {
			return result, fmt.Errorf("unowned local files require review before enrollment")
		}
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	readback, err := scan(s.root(id))
	if err != nil {
		return result, err
	}
	if !reflect.DeepEqual(inv, readback) {
		return result, fmt.Errorf("installed files changed during baseline verification")
	}
	o := origin{Schema: 1, Source: catalog.Source, Branch: "main", Commit: commit, Plugin: id, Component: result.Component, Files: owned}
	if err = writeJSON(s.receiptPath(id), o); err != nil {
		return result, err
	}
	result.Status = "success"
	result.Detail = "Official baseline verified; only the receipt was written"
	return result, nil
}

func checkLocalAPI(dir, id string) error {
	b, err := readOptional(filepath.Join(dir, "plugin.json"))
	if err != nil {
		return err
	}
	var m plugin.Manifest
	if b != nil {
		if err = json.Unmarshal(b, &m); err != nil {
			return err
		}
		if err = m.Validate(id); err != nil {
			return err
		}
	}
	b, err = readOptional(filepath.Join(dir, "plugin.metadata"))
	if err != nil {
		return err
	}
	if b != nil {
		values, err := data.ParseAssignments(b, "plugin.metadata")
		if err != nil {
			return err
		}
		legacy, err := plugin.LegacyManifest(values)
		if err != nil {
			return err
		}
		if err = legacy.Validate(id); err != nil {
			return err
		}
		if legacy.MinAPI > plugin.API {
			return fmt.Errorf("incompatible legacy API")
		}
		if m.ID == "" {
			m = legacy
		}
	}
	if m.ID == "" || m.MinAPI > plugin.API {
		return fmt.Errorf("missing or incompatible plugin manifest")
	}
	return nil
}
