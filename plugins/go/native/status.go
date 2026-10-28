package main

import (
	"context"
	"path/filepath"
)

// VERSION and executable metadata suffice. Status never runs Go (which can
// otherwise download a toolchain), touches profiles, or contacts the network.
func (b *backend) status(ctx context.Context) error {
	vs, err := b.installed()
	if err != nil {
		return err
	}
	current, err := b.current()
	if err != nil {
		return err
	}
	if current == "" {
		b.log("ATM Go: not selected")
	} else {
		b.log("ATM Go current: " + current + " (" + filepath.Join(b.root, "current") + ")")
	}
	for _, v := range vs {
		if err := ctx.Err(); err != nil {
			return err
		}
		origin := "legacy/unowned (preserved)"
		if b.hasReceipt(v) {
			origin = "native receipt (not verified by status)"
		}
		b.log("ATM Go version: " + v + " - " + origin)
	}
	b.log("Workspace (always preserved): " + filepath.Join(b.root, "workspace"))
	return ctx.Err()
}
