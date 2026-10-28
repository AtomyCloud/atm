package main

import (
	"context"
	"path/filepath"
)

// VERSION and executable metadata suffice. Status never runs Flutter (which can
// otherwise download a toolchain), touches profiles, or contacts the network.
func (b *backend) status(ctx context.Context) error {
	vs, err := b.versionDirs(false)
	if err != nil {
		return err
	}
	current, err := b.current()
	if err != nil {
		return err
	}
	if current == "" {
		b.log("ATM Flutter: not selected")
	} else {
		b.log("ATM Flutter current: " + current + " (" + filepath.Join(b.root, "current") + ")")
	}
	for _, v := range vs {
		if err := ctx.Err(); err != nil {
			return err
		}
		origin := "legacy/unowned (preserved)"
		if b.hasReceipt(v) {
			origin = "native receipt (not verified by status)"
		}
		if err := b.validInstalled(v); err != nil {
			origin += "; SDK identity unavailable/changed: " + err.Error()
		}
		b.log("ATM Flutter version: " + v + " - " + origin)
	}
	b.log("Versions are labels verified from SDK metadata, not a runtime health check. Flutter was not executed.")
	return ctx.Err()
}
