package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AtomyCloud/atm/internal/data"
)

func TestDesktopFileValidator(t *testing.T) {
	validator, err := exec.LookPath("desktop-file-validate")
	if err != nil {
		t.Skip("optional desktop-file-utils validation tool unavailable")
	}
	root := t.TempDir()
	b := &backend{root: filepath.Join(root, "Apps with $literal `text` %F and \\\"quote", "AndroidStudio")}
	content, err := b.desktopContent(identity{Launcher: "bin/studio.sh", Icon: "bin/studio.svg"})
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, desktopFileName)
	if err = os.WriteFile(file, content, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, validator, file)
	cmd.Env = []string{"HOME=" + root, "XDG_CONFIG_HOME=" + filepath.Join(root, "config"), "PATH=", "LC_ALL=C"}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("desktop validator rejected generated entry: %v\n%s", err, output)
	}
}

func TestDesktopExecLiteralRoundTrip(t *testing.T) {
	for _, path := range []string{
		"/tmp/Apps/AndroidStudio/current/bin/studio.sh",
		"/tmp/Apps with spaces/current/bin/studio.sh",
		"/tmp/quotes\"and'single/$literal/`literal`/a\\b/100%/%F/current/bin/studio.sh",
		"/tmp/\u00c1rvore/(one);two&three/current/bin/studio.sh",
	} {
		t.Run(path, func(t *testing.T) {
			encoded, err := desktopExec(path)
			if err != nil {
				t.Fatal(err)
			}
			// Desktop string unescaping precedes quoted Exec token parsing.
			value := strings.NewReplacer(`\\`, `\`, `\s`, " ", `\n`, "\n", `\r`, "\r", `\t`, "\t").Replace(encoded)
			parsed, err := data.ParseAssignments([]byte("EXEC="+value), "desktop-exec-fixture")
			if err != nil {
				t.Fatalf("literal Exec token was not safely quoted: %q: %v", encoded, err)
			}
			if got := strings.ReplaceAll(parsed["EXEC"], "%%", "%"); got != path {
				t.Fatalf("Exec changed path: got %q want %q (encoded %q)", got, path, encoded)
			}
		})
	}
}

func TestDesktopExecRefusesUnsupportedPaths(t *testing.T) {
	for _, path := range []string{"", "/tmp/a=b/bin/studio", "/tmp/line\nbin/studio", "/tmp/null\x00bin/studio", "/tmp/tab\tbin/studio"} {
		if _, err := desktopExec(path); err == nil {
			t.Fatalf("unsafe Exec path accepted: %q", path)
		}
	}
}

func TestDesktopContentUsesCurrentAndNoFieldInjection(t *testing.T) {
	b := &backend{root: "/tmp/Apps with $literal and %F/AndroidStudio"}
	id := identity{Launcher: "bin/studio.sh", Icon: "bin/studio.svg"}
	content, err := b.desktopContent(id)
	if err != nil {
		t.Fatal(err)
	}
	command, _ := desktopExec(filepath.Join(b.root, "current", id.Launcher))
	if !strings.Contains(string(content), "\nExec="+command+"\n") ||
		!strings.Contains(string(content), "\nIcon="+desktopScalar(filepath.Join(b.root, "current", id.Icon))+"\n") {
		t.Fatalf("desktop does not target stable current: %s", content)
	}
	id.Launcher = "bin/studio.sh\nExec=unexpected"
	if _, err := b.desktopContent(id); err == nil {
		t.Fatal("injected Exec accepted")
	}
}

func TestPinnedCatalogPreservesLegacySelections(t *testing.T) {
	contents, err := os.ReadFile("../plugin.conf")
	if err != nil {
		t.Fatal(err)
	}
	lookup := func(key string) string {
		t.Helper()
		for _, line := range strings.Split(string(contents), "\n") {
			if !strings.HasPrefix(line, key+"=") {
				continue
			}
			values, err := data.ParseAssignments([]byte(line), "legacy-selector")
			if err != nil {
				t.Fatal(err)
			}
			return values[key]
		}
		t.Fatalf("missing literal selector %s", key)
		return ""
	}
	ids := strings.Fields(lookup("ATM_ANDROID_STUDIO_OPTION_IDS"))
	if len(ids) != len(catalog) {
		t.Fatal("native catalog differs from legacy selector count")
	}
	for i, a := range catalog {
		prefix := "ATM_ANDROID_STUDIO_" + strings.ToUpper(ids[i])
		if a.ID != ids[i] || a.Name != lookup(prefix+"_NAME") || a.Version != lookup(prefix+"_VERSION") || a.Package != lookup(prefix+"_PACKAGE") {
			t.Fatalf("legacy selector changed: %+v", a)
		}
	}
	defaultArtifact, err := parseOptions(nil)
	if err != nil || defaultArtifact.ID != lookup("ATM_ANDROID_STUDIO_DEFAULT_ID") {
		t.Fatalf("default selector changed: %+v %v", defaultArtifact, err)
	}
}
