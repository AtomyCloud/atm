package setup

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/AtomyCloud/atm/internal/coreupdate"
)

func TestIntegrationRefusesPendingSiblingPackageWithoutProfileWrites(t *testing.T) {
	for _, state := range []string{"pending", "partial", "corrupt"} {
		t.Run(state, func(t *testing.T) {
			o := installed(t)
			// Also prevent registry access on Windows if the guard regresses:
			// Unix-style integration is rejected before opening the real HKCU store.
			o.Shell, o.LinkCLI = "bash", true
			destination, _, err := o.paths()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := verifyInstalled(context.Background(), destination, ""); err != nil {
				t.Fatal("fixture must verify before its transaction is introduced", err)
			}
			for _, name := range []string{".profile", ".bashrc", ".zprofile", ".zshrc", ".config/fish/conf.d/atm.fish"} {
				put(t, filepath.Join(o.UserHome, filepath.FromSlash(name)), []byte("# personal fixture settings\n"), 0640)
			}
			journalDir := destination + coreupdate.BundleTransactionSuffix
			if err := os.Mkdir(journalDir, 0700); err != nil {
				t.Fatal(err)
			}
			switch state {
			case "pending":
				// Startup guarding needs only a structurally readable journal, not
				// trusted signatures. Recovery authentication is a separate boundary.
				raw, err := json.Marshal(map[string]any{
					"schema": 1, "root": destination,
					"before": map[string]any{"files": map[string]any{"fixture": map[string]any{}}, "dirs": map[string]any{}},
				})
				if err != nil {
					t.Fatal(err)
				}
				put(t, filepath.Join(journalDir, "journal.json"), raw, 0600)
			case "corrupt":
				put(t, filepath.Join(journalDir, "journal.json"), []byte("incomplete JSON"), 0600)
			}
			beforeHome, beforeSource := tree(t, o.UserHome), tree(t, o.Source)
			for _, dry := range []bool{true, false} {
				o.DryRun, o.Consent = dry, !dry
				result, err := Integrate(context.Background(), o)
				if !errors.Is(err, coreupdate.ErrRecoveryRequired) {
					t.Errorf("dry-run=%v: pending destination must require recovery, got %+v, %v", dry, result, err)
				}
				if result.Verified || len(result.Changed) != 0 || len(result.Backups) != 0 || len(result.Planned) != 0 {
					t.Errorf("pending destination reached profile planning/writes: %+v", result)
				}
				if !reflect.DeepEqual(beforeHome, tree(t, o.UserHome)) || !reflect.DeepEqual(beforeSource, tree(t, o.Source)) {
					t.Fatal("pending integration changed profiles, CLI links, backups, locks, package files or journal")
				}
			}
		})
	}
}

func TestInstalledVerificationGuardsPendingSiblingBeforeOpeningMissingRoot(t *testing.T) {
	parent := t.TempDir()
	source := filepath.Join(parent, "missing-installation")
	if err := os.Mkdir(source+coreupdate.BundleTransactionSuffix, 0700); err != nil {
		t.Fatal(err)
	}
	before := tree(t, parent)
	if _, err := verifyInstalled(context.Background(), source, ""); !errors.Is(err, coreupdate.ErrRecoveryRequired) {
		t.Fatal("pending sibling must be detected before opening the absent installation", err)
	}
	if !reflect.DeepEqual(before, tree(t, parent)) {
		t.Fatal("pending-root verification wrote files")
	}
}
