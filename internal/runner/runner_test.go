package runner

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/AtomyCloud/atm/internal/config"
	"github.com/AtomyCloud/atm/internal/plugin"
)

func TestPlansAndGuards(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux backend")
	}
	c := config.Config{Home: t.TempDir(), DryRun: true}
	p := plugin.Plugin{Manifest: plugin.Manifest{ID: "node", Operations: []string{"install", "use"}}, Available: true}
	plan, err := Plugin(c, p, "install", []string{"--version", "x; touch sentinel"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Command[len(plan.Command)-1] != "x; touch sentinel" {
		t.Fatal("argument was reinterpreted")
	}
	if _, err = Command(c, plan); err == nil {
		t.Fatal("dry-run command created")
	}
	if _, err = Plugin(c, p, "use", nil); err == nil {
		t.Fatal("missing version accepted")
	}
	for _, args := range [][]string{{"updates", "check"}, {"updates", "all"}, {"path", "apply"}, {"install"}, {"plugins", "update", "node"}, {"updates", "core"}, {"self-update", "--git"}, {"updates", "restore", "id"}, {"setup", "portable"}, {"plugins", "download", "../bad"}, {"plugins", "search"}, {"plugins", "download", "brave"}} {
		if _, err := Core(c, args); err == nil {
			t.Fatal("unsafe legacy operation accepted", args)
		}
	}
}

func TestLegacyArgumentsExitAndEnvironment(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux backend")
	}
	c := config.Config{Home: t.TempDir(), Language: "en-us", Sort: "az"}
	os.MkdirAll(filepath.Join(c.Home, "libexec"), 0700)
	script := []byte("printf '%s\\n' \"$@\"\nprintf 'ENV=%s|%s\\n' \"$BASH_ENV\" \"$ATM_LANG\"\nexit 23\n")
	os.WriteFile(filepath.Join(c.Home, "libexec", "atm-legacy.sh"), script, 0600)
	p := plugin.Plugin{Manifest: plugin.Manifest{ID: "node", Operations: []string{"install"}}, Available: true}
	plan, err := Plugin(c, p, "install", []string{"with spaces", "$(touch sentinel)"})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("BASH_ENV", "/nonexistent/do-not-source")
	cmd, err := Command(c, plan)
	if err != nil {
		t.Fatal(err)
	}
	output, err := cmd.CombinedOutput()
	if ExitCode(err) != 23 || !strings.Contains(string(output), "$(touch sentinel)\nENV=|en-us") {
		t.Fatalf("%s %v", output, err)
	}
	os.WriteFile(filepath.Join(c.Home, ".atm-core-update-pending"), []byte("pending"), 0600)
	if _, err := Command(c, plan); err == nil {
		t.Fatal("pending recovery ignored")
	}
}
