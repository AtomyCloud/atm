package catalog

import (
	"context"
	"fmt"
	"github.com/AtomyCloud/atm/internal/data"
	"os"
	"strconv"
)

// Fingerprint is compatible with legacy component receipts.
type Fingerprint struct {
	SHA  string `json:"sha"`
	Mode string `json:"mode"`
}

func (p *DownloadPlan) Fingerprints() map[string]Fingerprint {
	out := map[string]Fingerprint{}
	for name, e := range p.files {
		out[name] = Fingerprint{e.SHA, e.Mode}
	}
	return out
}

// VerifyDirectory reuses download integrity, syntax and executable checks for an
// existing payload. It does not write, execute code, or infer ownership.
func (p *DownloadPlan) VerifyDirectory(ctx context.Context, directory string) error {
	if p == nil || len(p.files) == 0 {
		return fmt.Errorf("unverified plan")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer root.Close()
	for name, e := range p.files {
		if err = ctx.Err(); err != nil {
			return err
		}
		info, err := root.Lstat(name)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("nonregular payload")
		}
		if err = verifyStaged(root, name, e); err != nil {
			return err
		}
		b, err := root.ReadFile(name)
		if err != nil {
			return err
		}
		if err = checkShell(name, b); err != nil {
			return err
		}
	}
	return validatePayload(root, p)
}

// Snapshot fetches only the official main tree; malformed unrelated metadata
// cannot prevent inspection of an installed component.
func (c *Client) Snapshot(ctx context.Context) (Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	return c.snapshot(ctx)
}

func (s Snapshot) HasPlugin(id string) bool { return len(s.files[id]) != 0 }

// Relationship describes target relative to base, using GitHub's compare status.
func (c *Client) Relationship(ctx context.Context, base, target string) (string, error) {
	if !shaPattern.MatchString(base) || !shaPattern.MatchString(target) {
		return "", fmt.Errorf("invalid commit")
	}
	if base == target {
		return "identical", nil
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	var result struct {
		Status string `json:"status"`
	}
	if err := c.get(ctx, "/compare/"+base+"..."+target, data.MaxFileSize, &result); err != nil {
		return "", err
	}
	switch result.Status {
	case "ahead", "behind", "identical", "diverged":
		return result.Status, nil
	}
	return "", fmt.Errorf("unknown official commit relationship")
}

// Baseline reconstructs old commit-only receipts only after proving main ancestry.
func (c *Client) Baseline(ctx context.Context, id, commit string, main Snapshot) (*DownloadPlan, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	relation, err := c.Relationship(ctx, commit, main.revision)
	if err != nil {
		return nil, err
	}
	if relation != "ahead" && relation != "identical" {
		return nil, fmt.Errorf("baseline is not an ancestor of official main")
	}
	if commit == main.revision {
		return c.Prepare(ctx, id, &main)
	}
	snapshot, err := c.snapshotAt(ctx, commit)
	if err != nil {
		return nil, err
	}
	return c.Prepare(ctx, id, &snapshot)
}

func (c *Client) RequiredAPI(ctx context.Context, p *DownloadPlan) (int, error) {
	if p == nil {
		return 0, fmt.Errorf("missing plan")
	}
	required := p.manifest.MinAPI
	if e, ok := p.files["plugin.metadata"]; ok {
		b, err := c.blob(ctx, e)
		if err != nil {
			return 0, err
		}
		values, err := data.ParseAssignments(b, "plugin.metadata")
		if err != nil {
			return 0, err
		}
		if raw, ok := values["ATM_PLUGIN_MIN_CORE_API_VALUE"]; ok {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 || n > 999999 {
				return 0, fmt.Errorf("invalid minimum CORE API")
			}
			if n > required {
				required = n
			}
		}
	}
	return required, nil
}

// ValidPayloadPath uses the same portable boundaries as official downloads.
func ValidPayloadPath(name string) bool { return portablePath(name) }

// PublishDirectory never replaces an existing destination, including empty dirs.
func PublishDirectory(from, to *os.File, source, destination string) error {
	return publishNoReplace(from, to, source, destination)
}
