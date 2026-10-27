package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/AtomyCloud/atm/internal/config"
	"github.com/AtomyCloud/atm/internal/coreupdate"
)

func TestRecoveryGuardRecheckedAfterConfirmation(t *testing.T) {
	c := config.Config{Home: t.TempDir(), AppsDir: filepath.Join(t.TempDir(), "Apps")}
	p, err := Prepare(c, []string{"path", "apply"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	e, err := Resolve(context.Background(), c, p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(c.Home, ".atm-coreupdate"), 0700); err != nil {
		t.Fatal(err)
	}
	_, err = Execute(context.Background(), c, e, true)
	if !errors.Is(err, coreupdate.ErrRecoveryRequired) {
		t.Fatal(err)
	}
}

func TestChangesPropagatesInspectionFailures(t *testing.T) {
	root := t.TempDir()
	c := config.Config{Home: root, PluginsDir: filepath.Join(root, "plugins"), StateDir: filepath.Join(root, "state")}
	if err := os.MkdirAll(filepath.Join(c.PluginsDir, "unmanaged"), 0700); err != nil {
		t.Fatal(err)
	}
	p, err := Prepare(c, []string{"updates", "changes"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	e, err := Resolve(context.Background(), c, p)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Execute(context.Background(), c, e, false)
	if err == nil || r.Value == nil {
		t.Fatal("inspection failure reported as success", r, err)
	}
}
