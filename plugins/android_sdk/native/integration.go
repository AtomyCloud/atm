package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

func (b *backend) integrate(ctx context.Context, remove bool) error {
	return b.integrateExcluding(ctx, remove, "")
}

func (b *backend) integrateExcluding(ctx context.Context, remove bool, exclude string) error {
	file := filepath.Join(b.root, markerName)
	before, mode, exists, err := readProfile(file)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("native SDK ownership required")
	}
	var marker rootMarker
	if err = decodeStrict(before, &marker); err != nil {
		return err
	}
	if remove && !marker.Integrated {
		return nil
	}
	if exclude != "" && !marker.Integrated {
		return nil
	}
	if !remove {
		if _, err = b.verifyOwned(ctx, toolsID); err != nil {
			return err
		}
		if err = b.checkLatest(true); err != nil {
			return err
		}
		records, err := b.receipts()
		if err != nil {
			return err
		}
		var best revision
		b.cmakePath = ""
		for _, r := range records {
			if r.Spec.ID != exclude && strings.HasPrefix(r.Spec.ID, "cmake;") && compare(r.Spec.Revision, best) > 0 {
				if _, err = b.verifyOwned(ctx, r.Spec.ID); err != nil {
					return err
				}
				best = r.Spec.Revision
				b.cmakePath = filepath.Join(b.root, r.Spec.Directory, "bin")
			}
		}
	}
	if err = realDirs(b.home, false); err != nil {
		return err
	}
	for _, name := range []string{".profile", ".bashrc", ".zshrc"} {
		data, _, _, err := readProfile(filepath.Join(b.home, name))
		if err != nil {
			return err
		}
		if bytes.Contains(data, []byte(profileStart)) && (!marker.Integrated || marker.Block == "" || !bytes.Contains(data, []byte(marker.Block))) {
			return fmt.Errorf("unowned/modified Android SDK profile block; preserving %s", name)
		}
	}
	plans, err := b.profilePlans(remove)
	if err != nil {
		return err
	}
	if exclude != "" {
		// A package removal refreshes existing owned blocks only. Do not recreate
		// a profile/block the user removed or integrate a newly created profile.
		kept := plans[:0]
		for _, p := range plans {
			if bytes.Contains(p.before, []byte(profileStart)) {
				kept = append(kept, p)
			}
		}
		plans = kept
	}
	marker.Integrated = !remove
	marker.Block = ""
	if !remove {
		marker.Block = profileStart + "\n" + b.shellEnvironment("\n") + "\n" + profileEnd
	}
	after, err := json.Marshal(marker)
	if err != nil {
		return err
	}
	plans = append(plans, profilePlan{file, before, after, mode, true})
	_, err = applyProfiles(ctx, plans)
	if err == nil && !remove {
		b.log("Android SDK user environment configured. Refresh this shell: " + b.shellRefresh())
	}
	return err
}
