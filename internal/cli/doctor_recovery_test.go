package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/AtomyCloud/atm/internal/coreupdate"
)

func TestDoctorBrokenPluginPreservesCoreDiagnostics(t *testing.T) {
	root, home := recoveryCLIFixture(t)
	broken := filepath.Join(root, "plugins", "broken")
	if err := os.Mkdir(broken, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(broken, "plugin.json"), []byte("invalid JSON"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(home+coreupdate.BundleTransactionSuffix, 0700); err != nil {
		t.Fatal(err)
	}
	before := recoveryCLISnapshot(t, root)
	var out, stderr bytes.Buffer
	if code := Run([]string{"--home", home, "--json", "doctor"}, nil, &out, &stderr); code == 0 || stderr.Len() == 0 {
		t.Fatal("broken discovery reported success", code, stderr.String())
	}
	var report map[string]any
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err, out.String())
	}
	if report["plugin_count"] != nil || report["plugin_discovery_error"] == nil || report["compiled_recovery_pending"] != true || report["compiled_core_updater"] != coreupdate.ProductionStatus {
		t.Fatal(report)
	}
	trust, ok := report["release_trust"].(map[string]any)
	if !ok || trust["fingerprint"] != coreupdate.OfficialKeyFingerprint || trust["replacement_enabled"] != coreupdate.ProductionEnabled {
		t.Fatal(report)
	}
	if !reflect.DeepEqual(before, recoveryCLISnapshot(t, root)) {
		t.Fatal("doctor wrote to broken installation")
	}
}
