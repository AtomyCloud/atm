//go:build linux

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverySymlinkDirectoryRequirements(t *testing.T) {
	for _, suffix := range []string{"/", "/.", "//", "/../payload"} {
		t.Run(suffix, func(t *testing.T) {
			root := t.TempDir()
			payload := filepath.Join(root, "payload")
			if err := os.WriteFile(payload, []byte("never execute"), 0755); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(root, "link")
			if err := os.Symlink(payload+suffix, link); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(link); err == nil {
				t.Fatal("fixture must violate kernel directory traversal requirements")
			}
			_, _, state, err := discoveryStat(context.Background(), link)
			if err != nil || state != "not-regular" {
				t.Fatalf("invalid directory traversal accepted: state=%q err=%v", state, err)
			}
		})
	}
}

func TestDiscoveryInactivePreviewHardlinkIsNotExternal(t *testing.T) {
	f := installedPair(t)
	alias := filepath.Join(f.b.discovery.home, ".local/bin/picoclaw")
	if err := os.MkdirAll(filepath.Dir(alias), 0700); err != nil {
		t.Fatal(err)
	}
	inactive := filepath.Join(f.b.root("picoclaw"), "versions/1.2.3/tool")
	if err := os.Link(inactive, alias); err != nil {
		t.Fatal(err)
	}
	report, err := f.b.discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range report.Evidence {
		if e.Path == alias {
			if e.State != "native-preview-alias" && e.State != "ambiguous-hardlink" {
				t.Fatalf("inactive preview hardlink misclassified: %+v", e)
			}
			return
		}
	}
	t.Fatal("missing hardlink evidence")
}
