//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
)

func integrationFixture(t *testing.T, ids ...string) *fixture {
	t.Helper()
	f := newFixture(t, "amd64")
	f.b.req.AppsDir = filepath.Join(filepath.Dir(f.b.req.AppsDir), "Apps 'quoted space $dollar `tick`")
	home := os.Getenv("HOME")
	writeFixture(t, filepath.Join(home, ".profile"), []byte("# original without newline"), 0640)
	writeFixture(t, filepath.Join(home, ".bashrc"), []byte("# original bash\n"), 0600)
	for _, id := range ids {
		integrationRelease(t, f, id, "1.2.3")
		mustRun(t, f, "install", "--provider", id, "--trust-download")
	}
	return f
}
func integrationRelease(t *testing.T, f *fixture, id, version string) {
	t.Helper()
	if id == "ollama" {
		ollamaFixtureRelease(t, f, version)
	} else {
		f.release(t, id, version, nil)
	}
}
func integrationRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func integrationMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("expected absent %s: %v", path, err)
	}
}

func TestIntegrationOptions(t *testing.T) {
	for _, args := range [][]string{nil, {"--remove-path"}, {"--provider", "picoclaw"}, {"--provider=codex-cli", "--remove-path"}, {"--provider", "ollama"}, {"--provider=ollama", "--remove-path"}} {
		if _, err := parseOptions("integrate", args); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	for _, args := range [][]string{
		{"--provider", "hermes-desktop"}, {"--provider", hermesSystem}, {"--provider="},
		{"--version", "latest"}, {"--version", "1.2.3"}, {"--trust-download"}, {"--accept-system-packages"},
		{"--remove-path=true"}, {"--remove-path", "--remove-path"}, {"--provider", "picoclaw", "--provider=picoclaw"},
		{"--provider"}, {"picoclaw"}, {"--yes"},
	} {
		if _, err := parseOptions("integrate", args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	for _, op := range []string{"install", "update", "use", "remove", "uninstall", "status", "menu"} {
		if _, err := parseOptions(op, []string{"--provider", "picoclaw", "--remove-path"}); err == nil {
			t.Fatal("accepted inapplicable remove-path", op)
		}
	}
	f := newFixture(t, "amd64")
	mustRun(t, f, "integrate")
	integrationMissing(t, f.b.req.AppsDir)
	if entries, err := os.ReadDir(os.Getenv("HOME")); err != nil || len(entries) != 0 {
		t.Fatalf("empty fixture HOME changed by no-op integration: %v %v", entries, err)
	}
}

func TestIntegrationLifecycleAndProfiles(t *testing.T) {
	// Hermes owns a native copy, not the portable providers' current symlink.
	for _, id := range []string{"picoclaw", "codex-cli", "ollama"} {
		t.Run(id, func(t *testing.T) {
			f := integrationFixture(t, id)
			home := os.Getenv("HOME")
			profile := filepath.Join(home, ".profile")
			original := integrationRead(t, profile)
			integrationMissing(t, filepath.Join(f.b.root(id), "path.json"))
			calls := f.calls
			mustRun(t, f, "integrate", "--provider", id)
			first := integrationRead(t, profile)
			mustRun(t, f, "integrate", "--provider", id)
			if !bytes.Equal(first, integrationRead(t, profile)) {
				t.Fatal("non-idempotent integration")
			}
			integrationMissing(t, filepath.Join(home, ".zshrc"))
			integrationMissing(t, f.b.req.Home)
			link := filepath.Join(f.b.root(id), "bin", cliCommand(id))
			wantLink := "../current"
			if id == "ollama" {
				wantLink = "../current/bin/ollama"
			}
			if target, err := os.Readlink(link); err != nil || target != wantLink {
				t.Fatal(target, err)
			}
			if data := integrationRead(t, link); !bytes.Equal(data, fakeELF("amd64", "1.2.3", false)) {
				t.Fatal("command does not resolve to selected file")
			}
			if info, err := os.Stat(profile); err != nil || info.Mode().Perm() != 0640 {
				t.Fatal("profile mode changed", err)
			}
			mustRun(t, f, "menu", id)
			mustRun(t, f, "status")
			if f.calls != calls || !bytes.Equal(first, integrationRead(t, profile)) {
				t.Fatal("menu/status mutated state or used network")
			}
			integrationRelease(t, f, id, "1.2.4")
			mustRun(t, f, "update", "--provider", id, "--trust-download")
			if !bytes.Equal(integrationRead(t, link), fakeELF("amd64", "1.2.4", false)) {
				t.Fatal("stable command did not follow update")
			}
			mustRun(t, f, "use", "--provider", id, "--version", "1.2.3")
			if !bytes.Equal(first, integrationRead(t, profile)) {
				t.Fatal("update/use edited profiles")
			}
			mustRun(t, f, "remove", "--provider", id, "--version", "1.2.4")
			mustRun(t, f, "integrate", "--provider", id, "--remove-path")
			if !bytes.Equal(original, integrationRead(t, profile)) {
				t.Fatal("single provider failed exact restoration")
			}
			integrationMissing(t, link)
			integrationMissing(t, filepath.Join(f.b.root(id), "path.json"))
			mustRun(t, f, "integrate", "--provider", id, "--remove-path")
			mustRun(t, f, "integrate", "--provider", id)
			mustRun(t, f, "uninstall", "--provider", id)
			if !bytes.Equal(original, integrationRead(t, profile)) {
				t.Fatal("uninstall changed unrelated profile")
			}
			if _, _, err := f.b.inspect(context.Background(), id, true); err != nil {
				t.Fatal(err)
			}
			integrationMissing(t, filepath.Join(home, ".atm-ai-path.lock"))
		})
	}
}

func TestIntegrationProviderSeparation(t *testing.T) {
	f := integrationFixture(t, "picoclaw", "codex-cli", "hermes-desktop", "ollama")
	home := os.Getenv("HOME")
	writeFixture(t, filepath.Join(home, ".zshrc"), []byte("# zsh\n"), 0644)
	mustRun(t, f, "integrate")
	for _, name := range profileNames {
		data := integrationRead(t, filepath.Join(home, name))
		for _, id := range []string{"picoclaw", "codex-cli", "ollama"} {
			if strings.Count(string(data), f.b.pathBlock(id)) != 1 {
				t.Fatal("missing independent provider block", id, name)
			}
		}
	}
	integrationMissing(t, filepath.Join(f.b.root("hermes-desktop"), "bin"))
	mustRun(t, f, "integrate", "--provider", "ollama", "--remove-path")
	for _, id := range []string{"picoclaw", "codex-cli"} {
		if _, _, err := f.b.inspect(context.Background(), id, true); err != nil {
			t.Fatal("Ollama detach changed another provider", id, err)
		}
	}
	mustRun(t, f, "integrate", "--provider", "picoclaw", "--remove-path")
	if _, _, err := f.b.inspect(context.Background(), "codex-cli", true); err != nil {
		t.Fatal("removing first block corrupted second", err)
	}
	profile := string(integrationRead(t, filepath.Join(home, ".profile")))
	if !strings.HasPrefix(profile, "# original without newline\n# >>>") {
		t.Fatal("separator lost", profile)
	}
	mustRun(t, f, "integrate", "--provider", "codex-cli", "--remove-path")
	if got := string(integrationRead(t, filepath.Join(home, ".profile"))); got != "# original without newline\n" {
		t.Fatalf("unexpected retained separator: %q", got)
	}
}

func TestIntegrationShellLiteralAndEmptyPATH(t *testing.T) {
	f := integrationFixture(t, "picoclaw")
	for _, value := range []string{"", "/usr/bin:/bin", "'literal $x `no execution`"} {
		for _, unset := range []bool{false, true} {
			var env []string
			if !unset {
				env = append(env, "PATH="+value)
			}
			assignment := strings.TrimPrefix(strings.Split(f.b.pathBlock("picoclaw"), "\n")[1], "export ")
			node, err := syntax.NewParser().Parse(strings.NewReader(assignment), "fixture")
			if err != nil {
				t.Fatal(err)
			}
			word := node.Stmts[0].Cmd.(*syntax.CallExpr).Assigns[0].Value
			// The existing Go shell expander performs no execution; command
			// substitution is forbidden by its default nil handler.
			out, err := expand.Literal(&expand.Config{Env: expand.ListEnviron(env...)}, word)
			if err != nil {
				t.Fatal(err)
			}
			want := filepath.Join(f.b.root("picoclaw"), "bin")
			if !unset && value != "" {
				want += ":" + value
			}
			if out != want {
				t.Fatalf("literal PATH mismatch: got %q want %q", out, want)
			}
		}
	}
}

func TestIntegrationUnsafeProfilesPreflight(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink", "fifo", "oversize", "writable", "special-mode", "foreign", "duplicate", "partial", "unowned", "embedded", "edited"} {
		t.Run(kind, func(t *testing.T) {
			f := integrationFixture(t, "picoclaw")
			home := os.Getenv("HOME")
			profile := filepath.Join(home, ".profile")
			before := integrationRead(t, profile)
			bad := filepath.Join(home, ".zshrc")
			block := f.b.pathBlock("picoclaw")
			switch kind {
			case "symlink":
				if err := os.Symlink(profile, bad); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(profile, bad); err != nil {
					t.Fatal(err)
				}
			case "fifo":
				if err := unix.Mkfifo(bad, 0600); err != nil {
					t.Fatal(err)
				}
			case "oversize":
				writeFixture(t, bad, bytes.Repeat([]byte("x"), maxProfile+1), 0600)
			case "writable":
				writeFixture(t, bad, []byte("x"), 0664)
			case "special-mode":
				writeFixture(t, bad, []byte("x"), 0600|os.ModeSetuid)
			case "foreign":
				if os.Geteuid() != 0 {
					t.Skip("foreign owner fixture requires root; never uses sudo")
				}
				writeFixture(t, bad, []byte("x"), 0600)
				if err := os.Chown(bad, 1, 1); err != nil {
					t.Fatal(err)
				}
			case "duplicate":
				writeFixture(t, bad, []byte(block+block), 0600)
			case "partial":
				writeFixture(t, bad, []byte(strings.Split(block, "\n")[0]+"\n"), 0600)
			case "unowned":
				writeFixture(t, bad, []byte(block), 0600)
			case "embedded":
				writeFixture(t, bad, []byte("x"+block), 0600)
			case "edited":
				writeFixture(t, bad, []byte(strings.Replace(block, "export PATH=", "export OTHER=", 1)), 0600)
			}
			if err := f.run(context.Background(), "integrate", "--provider", "picoclaw"); err == nil {
				t.Fatal("unsafe profile accepted")
			}
			if !bytes.Equal(before, integrationRead(t, profile)) {
				t.Fatal("partial profile edit before preflight finished")
			}
			integrationMissing(t, filepath.Join(f.b.root("picoclaw"), "path.json"))
			integrationMissing(t, filepath.Join(home, ".atm-ai-path.lock"))
		})
	}
}

func TestIntegrationCorruptOwnershipRefusesUninstall(t *testing.T) {
	for _, kind := range []string{"receipt", "home", "profile-list", "missing-block", "edited-block", "missing-link", "foreign-link", "extra-bin", "receipt-hardlink", "binary-hardlink", "missing-receipt"} {
		t.Run(kind, func(t *testing.T) {
			f := integrationFixture(t, "picoclaw")
			mustRun(t, f, "integrate", "--provider", "picoclaw")
			root := f.b.root("picoclaw")
			path := filepath.Join(root, "path.json")
			switch kind {
			case "receipt":
				writeFixture(t, path, []byte("{}"), 0600)
			case "home", "profile-list":
				var rec pathReceipt
				if err := readJSON(path, &rec); err != nil {
					t.Fatal(err)
				}
				if kind == "home" {
					rec.Home = f.b.req.Home
				} else {
					rec.Profiles[0].Name = ".other"
				}
				data, _ := json.Marshal(rec)
				writeFixture(t, path, data, 0600)
			case "missing-block":
				writeFixture(t, filepath.Join(os.Getenv("HOME"), ".bashrc"), []byte("# retained"), 0600)
			case "edited-block":
				p := filepath.Join(os.Getenv("HOME"), ".bashrc")
				writeFixture(t, p, bytes.ReplaceAll(integrationRead(t, p), []byte("export PATH="), []byte("export WRONG=")), 0600)
			case "missing-link", "foreign-link":
				link := filepath.Join(root, "bin", "picoclaw")
				if err := os.Remove(link); err != nil {
					t.Fatal(err)
				}
				if kind == "foreign-link" {
					if err := os.Symlink("/usr/bin/false", link); err != nil {
						t.Fatal(err)
					}
				}
			case "extra-bin":
				writeFixture(t, filepath.Join(root, "bin", "unowned"), []byte("keep"), 0600)
			case "receipt-hardlink", "binary-hardlink":
				source := path
				if kind == "binary-hardlink" {
					source = filepath.Join(root, "versions", "1.2.3", "tool")
				}
				if err := os.Link(source, filepath.Join(os.Getenv("HOME"), "alias")); err != nil {
					t.Fatal(err)
				}
			case "missing-receipt":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			}
			for _, op := range []string{"integrate", "uninstall"} {
				if err := f.run(context.Background(), op, "--provider", "picoclaw"); err == nil {
					t.Fatal("corruption accepted", op)
				}
			}
			if v, err := f.b.current("picoclaw"); err != nil || v != "1.2.3" {
				t.Fatal("destructive uninstall before refusal", v, err)
			}
			if _, err := os.Stat(filepath.Join(root, "versions", "1.2.3", "tool")); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestIntegrationRechecksBeforePublication(t *testing.T) {
	for _, kind := range []string{"binary", "receipt", "selection", "profile", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			f := integrationFixture(t, "picoclaw")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			root := f.b.root("picoclaw")
			f.b.checkpoint = func(point string) error {
				if point != "path-before-publish" {
					return nil
				}
				switch kind {
				case "binary":
					writeFixture(t, filepath.Join(root, "versions", "1.2.3", "tool"), bytes.Repeat([]byte("x"), 256), 0755)
				case "receipt":
					p := filepath.Join(root, "versions", "1.2.3", "receipt.json")
					var r receipt
					if err := readJSON(p, &r); err != nil {
						t.Fatal(err)
					}
					r.Artifact.SHA256 = strings.Repeat("a", 64)
					data, _ := json.Marshal(r)
					writeFixture(t, p, data, 0600)
				case "selection":
					if err := os.Remove(filepath.Join(root, "current")); err != nil {
						t.Fatal(err)
					}
				case "profile":
					writeFixture(t, filepath.Join(os.Getenv("HOME"), ".bashrc"), []byte("intervening edit"), 0600)
				case "cancel":
					cancel()
				}
				return nil
			}
			if err := f.run(ctx, "integrate", "--provider", "picoclaw"); err == nil {
				t.Fatal("changed state published")
			}
			integrationMissing(t, filepath.Join(root, "path.json"))
			integrationMissing(t, filepath.Join(root, "bin", "picoclaw"))
			integrationMissing(t, filepath.Join(os.Getenv("HOME"), ".atm-ai-path.lock"))
		})
	}
}

func TestIntegrationRollbackAndRetainedRecovery(t *testing.T) {
	for _, kind := range []string{"error", "cancel", "edited-bytes", "edited-mode", "replaced-inode"} {
		t.Run(kind, func(t *testing.T) {
			f := integrationFixture(t, "picoclaw")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			home := os.Getenv("HOME")
			path := filepath.Join(home, ".profile")
			before := integrationRead(t, path)
			f.b.checkpoint = func(point string) error {
				if point != "path-published-.profile" {
					return nil
				}
				switch kind {
				case "cancel":
					cancel()
					return nil
				case "edited-bytes":
					writeFixture(t, path, []byte("intervening user edit"), 0640)
				case "edited-mode":
					if err := os.Chmod(path, 0600); err != nil {
						t.Fatal(err)
					}
				case "replaced-inode":
					data := integrationRead(t, path)
					tmp := filepath.Join(home, "replacement")
					writeFixture(t, tmp, data, 0640)
					if err := os.Rename(tmp, path); err != nil {
						t.Fatal(err)
					}
				}
				return errors.New("synthetic publication failure")
			}
			err := f.run(ctx, "integrate", "--provider", "picoclaw")
			if err == nil {
				t.Fatal("expected failure")
			}
			if kind == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			lock := filepath.Join(home, ".atm-ai-path.lock")
			if kind == "error" || kind == "cancel" {
				if !bytes.Equal(before, integrationRead(t, path)) {
					t.Fatal("profile not restored")
				}
				if i, e := os.Stat(path); e != nil || i.Mode().Perm() != 0640 {
					t.Fatal("mode not restored", e)
				}
				integrationMissing(t, lock)
			} else {
				if !strings.Contains(err.Error(), "restoration incomplete") {
					t.Fatal(err)
				}
				var journal struct {
					Schema  int
					Changes []pathChange
				}
				if e := json.Unmarshal(integrationRead(t, filepath.Join(lock, "journal.json")), &journal); e != nil {
					t.Fatal(e)
				}
				found := false
				for _, c := range journal.Changes {
					if c.Path == path && bytes.Equal(c.Before.Data, before) && c.Before.Mode == 0640 {
						found = true
					}
				}
				if !found {
					t.Fatal("missing recovery bytes/mode")
				}
				if kind == "edited-bytes" && string(integrationRead(t, path)) != "intervening user edit" {
					t.Fatal("intervening edit overwritten")
				}
			}
		})
	}
}

func TestIntegrationSharedHomeLockAndForeignRoot(t *testing.T) {
	f := integrationFixture(t, "picoclaw", "codex-cli")
	home := os.Getenv("HOME")
	other := *f.b
	other.req.AppsDir = filepath.Join(filepath.Dir(f.b.req.AppsDir), "another-apps")
	other.req.Operation = "install"
	if err := other.locked(context.Background(), "picoclaw", func() error {
		return other.install(context.Background(), options{provider: "picoclaw", version: "latest", trust: true})
	}); err != nil {
		t.Fatal(err)
	}
	other.req.Operation = "integrate"
	other.req.Arguments = []string{"--provider", "picoclaw"}
	f.b.checkpoint = func(point string) error {
		if point != "path-preflight" {
			return nil
		}
		if _, err := other.execute(context.Background()); err == nil || !strings.Contains(err.Error(), "profiles locked") {
			t.Fatalf("other Apps root escaped shared lock: %v", err)
		}
		if err := f.b.locked(context.Background(), "codex-cli", func() error { return f.b.changeIntegration(context.Background(), "codex-cli", false) }); err == nil || !strings.Contains(err.Error(), "profiles locked") {
			t.Fatalf("other provider escaped shared lock: %v", err)
		}
		return nil
	}
	mustRun(t, f, "integrate", "--provider", "picoclaw")
	f.b.checkpoint = nil
	before := integrationRead(t, filepath.Join(home, ".profile"))
	if _, err := other.execute(context.Background()); err == nil {
		t.Fatal("adopted another Apps root's markers")
	}
	if !bytes.Equal(before, integrationRead(t, filepath.Join(home, ".profile"))) {
		t.Fatal("foreign root changed profile")
	}
	if err := os.Mkdir(filepath.Join(home, ".atm-ai-path.lock"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := f.run(context.Background(), "uninstall", "--provider", "picoclaw"); err == nil {
		t.Fatal("uninstall bypassed HOME lock")
	}
	if current, _ := f.b.current("picoclaw"); current != "1.2.3" {
		t.Fatal("uninstall destroyed locked selection")
	}
}
