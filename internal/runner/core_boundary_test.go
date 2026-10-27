package runner

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/AtomyCloud/atm/internal/config"
)

func TestCoreAdministrationCannotEnterLegacyRunner(t *testing.T) {
	t.Setenv("PATH", "")
	c := config.Config{Home: t.TempDir()}
	for _, args := range [][]string{
		{"doctor"}, {"updates", "check"}, {"updates", "check-core"},
		{"updates", "plugin", "fixture"}, {"updates", "all"}, {"updates", "changes"},
		{"updates", "backups"}, {"updates", "restore", "backup-1"}, {"updates", "recover", "plugins"},
		{"updates", "verify", "fixture", strings.Repeat("a", 40)}, {"updates", "trust"},
		{"updates", "verify-package", "manifest", "signature", "archive"},
		{"updates", "enroll-core", "manifest", "signature", "archive"}, {"updates", "core"},
		{"recover-core", "--root", c.Home}, {"plugins", "check-updates"},
		{"plugins", "update", "fixture"}, {"plugins", "update", "--all"},
		{"plugins", "search"}, {"plugins", "download", "fixture"},
		{"self-update"}, {"self-update", "--check"}, {"self-update", "--git"},
		{"path", "show"}, {"path", "apply"}, {"path", "apply", "--plugins"},
		{"setup", "portable"}, {"setup", "system"}, {"install"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			if _, err := Core(c, args); err == nil || !strings.Contains(err.Error(), "legacy CORE delegation disabled") {
				t.Fatalf("CORE delegation accepted: %v", err)
			}
			for _, entry := range []string{"bin/atm", "libexec/atm-legacy.sh"} {
				command := append([]string{"bash", filepath.Join(c.Home, filepath.FromSlash(entry))}, args...)
				if _, err := Command(c, Plan{Command: command}); err == nil || !strings.Contains(err.Error(), "invalid execution plan") {
					t.Fatalf("CORE command reached legacy execution: %v: %v", command, err)
				}
			}
		})
	}
}
