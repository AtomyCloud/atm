package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/AtomyCloud/atm/internal/config"
	"github.com/AtomyCloud/atm/internal/coreupdate"
	"github.com/AtomyCloud/atm/internal/updates"
)

type releaseExecution struct {
	policy    coreupdate.BundlePolicy
	installed coreupdate.BundleInfo
	bundle    *coreupdate.BundlePlan
	remote    *coreupdate.ReleasePlan
}

func officialBundlePolicy(root string) (coreupdate.BundlePolicy, error) {
	key, err := coreupdate.OfficialPublicKey()
	return coreupdate.BundlePolicy{Root: root, PublicKey: key, OS: runtime.GOOS, Arch: runtime.GOARCH}, err
}

// Signed local inputs are pinned before confirmation. The archive is rechecked
// against those bytes during Apply, never silently replaced by another release.
func releaseInput(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > limit {
		return nil, fmt.Errorf("invalid signed release input")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, opened) {
		return nil, fmt.Errorf("signed input changed while opening")
	}
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("signed input exceeds limit")
	}
	return raw, err
}

func resolveRelease(ctx context.Context, c config.Config, e *Execution) error {
	p, err := officialBundlePolicy(c.Home)
	if err != nil {
		return err
	}
	x := &releaseExecution{policy: p}
	e.release = x
	a := e.Preview.Plan.Arguments
	if len(a) == 5 {
		raw, err := releaseInput(a[2], coreupdate.MaxManifestBytes)
		if err != nil {
			return err
		}
		sig, err := releaseInput(a[3], 64)
		if err != nil {
			return err
		}
		if a[1] == "enroll-core" {
			x.bundle, err = coreupdate.PrepareEnrollment(ctx, p, raw, sig, a[4])
		} else {
			x.bundle, err = coreupdate.PrepareBundle(ctx, p, raw, sig, a[4])
		}
		if err != nil {
			return err
		}
		e.Preview.Release = x.bundle.Info()
		return nil
	}
	// Do not contact GitHub for unsigned/unmanaged installations. An explicit
	// signed enrollment must first establish the complete installed baseline.
	x.installed, err = coreupdate.InspectBundle(ctx, p)
	if err != nil {
		return fmt.Errorf("signed installed baseline required (updates enroll-core): %w", err)
	}
	x.remote, err = coreupdate.DiscoverPackage(ctx, p.OS, p.Arch)
	if err != nil {
		return err
	}
	if !coreupdate.IsNewerRelease(x.remote.Manifest().Version, x.installed.CurrentVersion) {
		return fmt.Errorf("authenticated candidate is not newer than installed %s", x.installed.CurrentVersion)
	}
	e.Preview.Release = struct {
		Installed coreupdate.BundleInfo  `json:"installed"`
		Candidate coreupdate.ReleaseInfo `json:"candidate"`
	}{x.installed, x.remote.Info()}
	return nil
}

func executeRelease(ctx context.Context, e *Execution, consent bool) (coreupdate.BundleResult, error) {
	if !coreupdate.ProductionEnabled {
		return coreupdate.BundleResult{}, coreupdate.ErrProductionDisabled
	}
	if !consent {
		return coreupdate.BundleResult{}, fmt.Errorf("explicit CORE update consent required")
	}
	if e.release == nil {
		return coreupdate.BundleResult{}, fmt.Errorf("missing signed release plan")
	}
	x := e.release
	if x.bundle != nil {
		return coreupdate.ApplyBundle(ctx, x.policy, x.bundle, true)
	}
	if x.remote == nil {
		return coreupdate.BundleResult{}, fmt.Errorf("missing authenticated candidate")
	}
	current, err := coreupdate.InspectBundle(ctx, x.policy)
	if err != nil {
		return coreupdate.BundleResult{}, err
	}
	if current.Manifest != x.installed.Manifest {
		return coreupdate.BundleResult{}, fmt.Errorf("installed baseline changed after confirmation")
	}
	temp, err := os.MkdirTemp("", "atm-release-download-")
	if err != nil {
		return coreupdate.BundleResult{}, err
	}
	defer os.RemoveAll(temp)
	canonical, err := filepath.EvalSymlinks(temp)
	if err != nil {
		return coreupdate.BundleResult{}, err
	}
	archive := filepath.Join(canonical, "package")
	if err = x.remote.Download(ctx, archive); err != nil {
		return coreupdate.BundleResult{}, err
	}
	plan, err := coreupdate.PrepareBundle(ctx, x.policy, x.remote.Raw(), x.remote.Signature(), archive)
	if err != nil {
		return coreupdate.BundleResult{}, err
	}
	// The installed tree may have changed while downloading; the library pins
	// that tree, and this comparison also preserves the identity shown to the user.
	if plan.Info().Baseline != x.installed.Manifest {
		return coreupdate.BundleResult{}, fmt.Errorf("installed baseline changed during download")
	}
	return coreupdate.ApplyBundle(ctx, x.policy, plan, true)
}

func checkCoreRelease(ctx context.Context, c config.Config) (updates.Row, error) {
	row := updates.Row{Component: "core", Status: "unknown"}
	p, err := officialBundlePolicy(c.Home)
	if err != nil {
		return row, err
	}
	installed, err := coreupdate.InspectBundle(ctx, p)
	if err != nil {
		if errors.Is(err, coreupdate.ErrBundleUnenrolled) {
			row.Status = "unmanaged"
		}
		if errors.Is(err, coreupdate.ErrRecoveryRequired) {
			row.Status = "blocked"
		}
		row.Detail = "Signed installed baseline unavailable: " + err.Error()
		if ctx.Err() != nil {
			return row, ctx.Err()
		}
		if row.Status == "unmanaged" {
			return row, nil
		}
		return row, err
	}
	row.Commit = installed.Manifest.Commit
	plan, err := coreupdate.DiscoverPackage(ctx, p.OS, p.Arch)
	if err != nil {
		row.Detail = "Authenticated release discovery unavailable: " + err.Error()
		return row, err
	}
	m := plan.Manifest()
	switch {
	case coreupdate.IsNewerRelease(m.Version, installed.CurrentVersion):
		row.Status = "available"
		row.Detail = "Authenticated package " + m.Version + " available; freshness is not independently established"
	case m == installed.Manifest:
		row.Status = "current"
		row.Detail = "Installed package matches authenticated candidate; freshness is not independently established"
	case m.Version == installed.CurrentVersion:
		row.Status = "conflict"
		row.Detail = "Same version has different signed release identity; replacement refused"
	default:
		row.Status = "ahead"
		row.Detail = "Authenticated candidate is older; automatic downgrade refused"
	}
	return row, nil
}

func checkComponents(ctx context.Context, c config.Config, s *updates.Service) (updates.Report, error) {
	report, err := s.Check(ctx)
	row, coreErr := checkCoreRelease(ctx, c)
	if len(report.Rows) > 0 && report.Rows[0].Component == "core" {
		report.Rows[0] = row
	} else {
		report.Rows = append([]updates.Row{row}, report.Rows...)
	}
	return report, errors.Join(err, coreErr)
}
