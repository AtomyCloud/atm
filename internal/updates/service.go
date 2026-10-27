// Package updates inspects and replaces owned plugin code without executing it.
// Compiled CORE updates require a separate authenticated release model.
package updates

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/AtomyCloud/atm/internal/catalog"
	"github.com/AtomyCloud/atm/internal/plugin"
)

type Options struct {
	Home, PluginsDir, StateDir string
	DryRun                     bool
	Client                     *catalog.Client
}

type Service struct {
	options Options
	state   string
	client  *catalog.Client
}
type Row struct {
	Component string `json:"component"`
	Status    string `json:"status"`
	Detail    string `json:"detail"`
	Commit    string `json:"commit,omitempty"`
}
type Report struct {
	Schema   int    `json:"schema"`
	Source   string `json:"source"`
	Revision string `json:"revision,omitempty"`
	Rows     []Row  `json:"rows"`
}
type Change struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
}
type Plan struct {
	owner    *Service
	download *catalog.DownloadPlan
	before   inventory
	receipt  []byte
	changes  []Change
	owned    map[string]catalog.Fingerprint
}

func (p *Plan) Info() catalog.DownloadInfo { return p.download.Info() }
func (p *Plan) Changes() []Change          { return append([]Change(nil), p.changes...) }

type Result struct {
	Component       string `json:"component"`
	Status          string `json:"status"`
	BackupID        string `json:"backup_id,omitempty"`
	Detail          string `json:"detail,omitempty"`
	RestartRequired bool   `json:"restart_required"`
}
type origin struct {
	Schema    int                            `json:"schema"`
	Source    string                         `json:"source"`
	Branch    string                         `json:"branch"`
	Commit    string                         `json:"commit"`
	Plugin    string                         `json:"plugin,omitempty"`
	Component string                         `json:"component,omitempty"`
	Files     map[string]catalog.Fingerprint `json:"files"`
}

var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

func New(o Options) (*Service, error) {
	for _, p := range []*string{&o.Home, &o.PluginsDir, &o.StateDir} {
		if *p == "" {
			return nil, fmt.Errorf("home, plugin and state roots are required")
		}
		abs, err := filepath.Abs(*p)
		if err != nil {
			return nil, err
		}
		*p = abs
		if err := safeAncestors(abs); err != nil {
			return nil, err
		}
	}
	if within(o.PluginsDir, o.StateDir) || within(o.StateDir, o.PluginsDir) || within(o.PluginsDir, o.Home) {
		return nil, fmt.Errorf("overlapping update roots")
	}
	for _, unit := range []string{"bin", "lib", "lang"} {
		if within(filepath.Join(o.Home, unit), o.PluginsDir) || within(filepath.Join(o.Home, unit), o.StateDir) {
			return nil, fmt.Errorf("overlapping CORE scope")
		}
	}
	sum := sha256.Sum256([]byte(o.Home + "\x00" + o.PluginsDir))
	s := &Service{options: o, state: filepath.Join(o.StateDir, "component-updates", fmt.Sprintf("%x", sum)[:24]), client: o.Client}
	if s.client == nil {
		s.client = catalog.New("")
	}
	return s, nil
}
func within(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
func validID(id string) error {
	if !plugin.IDPattern.MatchString(id) || !catalog.ValidPayloadPath(id) {
		return fmt.Errorf("invalid plugin ID")
	}
	return nil
}
func (s *Service) root(id string) string { return filepath.Join(s.options.PluginsDir, id) }
func (s *Service) receiptPath(id string) string {
	return filepath.Join(s.state, "receipts", "plugin-"+id+".json")
}

// Installed includes disabled and malformed local manifests, not catalog entries.
func (s *Service) Installed() ([]string, error) {
	entries, err := os.ReadDir(s.options.PluginsDir)
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for _, e := range entries {
		if validID(e.Name()) != nil {
			continue
		}
		if e.IsDir() || e.Type()&os.ModeSymlink != 0 {
			ids = append(ids, e.Name())
		}
	}
	return ids, nil
}

func (s *Service) baseline(ctx context.Context, id string, main *catalog.Snapshot) (origin, []byte, error) {
	var o origin
	central, err := readOptional(s.receiptPath(id))
	if err != nil {
		return o, nil, err
	}
	raw := central
	if raw == nil {
		raw, err = readOptional(filepath.Join(s.root(id), ".atm-origin.json"))
		if err != nil {
			return o, nil, err
		}
	}
	if raw == nil {
		return o, central, fmt.Errorf("unmanaged: no verified installation baseline")
	}
	if err = json.Unmarshal(raw, &o); err != nil {
		return o, central, err
	}
	if o.Schema != 1 || o.Source != catalog.Source || o.Branch != "main" || !commitPattern.MatchString(o.Commit) || (o.Component != "plugin:"+id && !(o.Component == "" && o.Plugin == id)) {
		return o, central, fmt.Errorf("unverified component origin")
	}
	if o.Plugin != "" && o.Plugin != id {
		return o, central, fmt.Errorf("conflicting receipt plugin identity")
	}
	if o.Files == nil && main != nil {
		p, err := s.client.Baseline(ctx, id, o.Commit, *main)
		if err != nil {
			return o, central, err
		}
		o.Files = p.Fingerprints()
	}
	if len(o.Files) == 0 || len(o.Files) > 2000 {
		return o, central, fmt.Errorf("baseline requires official reconstruction")
	}
	for name, f := range o.Files {
		if !catalog.ValidPayloadPath(name) || !commitPattern.MatchString(f.SHA) || (f.Mode != "100644" && f.Mode != "100755") {
			return o, central, fmt.Errorf("invalid origin file")
		}
	}
	return o, central, nil
}

func (s *Service) Check(ctx context.Context) (Report, error) {
	ctx, cancel := context.WithTimeout(ctx, catalog.Timeout)
	defer cancel()
	report := Report{Schema: 1, Source: catalog.Source, Rows: []Row{{Component: "core", Status: "unknown", Detail: "Compiled CORE identity requires an approved trusted release model; source commits are not binary releases"}}}
	if err := ctx.Err(); err != nil {
		return report, err
	}
	ids, err := s.Installed()
	if err != nil {
		return report, err
	}
	var main catalog.Snapshot
	if s.options.DryRun {
		err = fmt.Errorf("dry-run: network inspection skipped")
	} else {
		main, err = s.client.Snapshot(ctx)
		report.Revision = main.Commit
	}
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		row := Row{Component: "plugin:" + id, Status: "unknown"}
		if err != nil {
			row.Detail = err.Error()
			if o, _, e := s.baseline(ctx, id, nil); e == nil {
				if inv, e := scan(s.root(id)); e == nil && !matches(inv, o.Files) {
					row.Status = "modified"
					row.Detail = "Local owned files differ from origin"
				}
			}
		} else {
			row, _, _ = s.inspect(ctx, id, main)
		}
		report.Rows = append(report.Rows, row)
	}
	return report, ctx.Err()
}

func (s *Service) inspect(ctx context.Context, id string, main catalog.Snapshot) (Row, *Plan, error) {
	row := Row{Component: "plugin:" + id, Status: "unknown"}
	fail := func(err error) (Row, *Plan, error) { row.Detail = err.Error(); return row, nil, err }
	o, central, err := s.baseline(ctx, id, &main)
	if err != nil {
		if strings.HasPrefix(err.Error(), "unmanaged:") {
			row.Status = "unmanaged"
		}
		return fail(err)
	}
	inv, err := scan(s.root(id))
	if err != nil {
		return fail(err)
	}
	row.Commit = o.Commit
	if !matches(inv, o.Files) {
		row.Status = "modified"
		return fail(fmt.Errorf("local owned files differ from origin"))
	}
	// A locally editable receipt is not proof of official ownership. Authenticate
	// its commit and complete owned manifest, even when it claims current hashes.
	relation, err := s.client.Relationship(ctx, o.Commit, main.Commit)
	if err != nil {
		return fail(err)
	}
	if relation == "behind" || relation == "diverged" {
		row.Status = "ahead"
		if relation == "diverged" {
			row.Status = relation
		}
		return fail(fmt.Errorf("origin is not behind official main"))
	}
	basePlan, err := s.client.Baseline(ctx, id, o.Commit, main)
	if err != nil {
		return fail(err)
	}
	if !reflect.DeepEqual(o.Files, basePlan.Fingerprints()) {
		return fail(fmt.Errorf("receipt manifest does not match the claimed official baseline"))
	}
	if !main.HasPlugin(id) {
		row.Status = "removed"
		return fail(fmt.Errorf("removed upstream; local plugin retained"))
	}
	p, err := s.client.Prepare(ctx, id, &main)
	if err != nil {
		return fail(err)
	}
	required, err := s.client.RequiredAPI(ctx, p)
	if err != nil {
		return fail(err)
	}
	if required > plugin.API {
		row.Status = "blocked"
		return fail(fmt.Errorf("requires CORE API %d", required))
	}
	target := p.Fingerprints()
	if reflect.DeepEqual(o.Files, target) {
		row.Status = "current"
		row.Detail = "Owned files match official snapshot"
		return row, nil, nil
	}
	if err = collisions(inv, o.Files, target); err != nil {
		row.Status = "modified"
		return fail(err)
	}
	row.Status = "available"
	row.Detail = "Verified plugin code changes available"
	return row, &Plan{s, p, inv, central, diff(o.Files, target), o.Files}, nil
}

func (s *Service) Prepare(ctx context.Context, id string) (*Plan, error) {
	ctx, cancel := context.WithTimeout(ctx, catalog.Timeout)
	defer cancel()
	if err := validID(id); err != nil {
		return nil, err
	}
	if s.options.DryRun {
		return nil, fmt.Errorf("dry-run: update preparation skipped")
	}
	main, err := s.client.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	row, p, err := s.inspect(ctx, id, main)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, fmt.Errorf("plugin is %s", row.Status)
	}
	return p, nil
}
func (s *Service) Changes(id string) ([]Change, error) {
	if err := validID(id); err != nil {
		return nil, err
	}
	o, _, err := s.baseline(context.Background(), id, nil)
	if err != nil {
		return nil, err
	}
	inv, err := scan(s.root(id))
	if err != nil {
		return nil, err
	}
	actual := map[string]catalog.Fingerprint{}
	for name, f := range inv {
		if f.Mode != "dir" && name != ".atm-origin.json" {
			actual[name] = f
		}
	}
	return diff(o.Files, actual), nil
}
func diff(a, b map[string]catalog.Fingerprint) []Change {
	out := []Change{}
	for n, f := range a {
		g, ok := b[n]
		if !ok {
			out = append(out, Change{n, "removed"})
		} else if f != g {
			out = append(out, Change{n, "modified"})
		}
	}
	for n := range b {
		if _, ok := a[n]; !ok {
			out = append(out, Change{n, "added"})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// UpdateAll pins main once and reports independent transactions for every
// installed plugin. It never downloads a plugin that is absent locally.
func (s *Service) UpdateAll(ctx context.Context, consent bool) ([]Result, error) {
	if !consent && !s.options.DryRun {
		return nil, fmt.Errorf("explicit consent required")
	}
	plans, results, err := s.PrepareAll(ctx)
	if err != nil {
		return results, err
	}
	for _, p := range plans {
		if err := ctx.Err(); err != nil {
			return results, err
		}
		r, e := s.Apply(ctx, p, true)
		if e != nil {
			r.Detail = e.Error()
		}
		results = append(results, r)
	}
	return results, nil
}
