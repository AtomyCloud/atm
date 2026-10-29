//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func discoveryLink(t *testing.T, target, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}

func discoveryFind(t *testing.T, r discoveryReport, path, state string) discoveryEvidence {
	t.Helper()
	for _, e := range r.Evidence {
		if e.Path == path {
			if e.State != state {
				t.Fatalf("%q: state=%s, want %s", path, e.State, state)
			}
			return e
		}
	}
	t.Fatalf("no discovery evidence for %q", path)
	return discoveryEvidence{}
}

func discoveryRun(t *testing.T, b *backend) discoveryReport {
	t.Helper()
	r, err := b.discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestDiscoveryLegacyAndUserPathsWithoutPATH(t *testing.T) {
	f := newFixture(t, "amd64")
	paths := map[string]string{
		"picoclaw":       filepath.Join(f.b.req.AppsDir, "ai/picoclaw/picoclaw"),
		"hermes-desktop": filepath.Join(f.b.req.AppsDir, "hermes/hermes-desktop/current.AppImage"),
		"codex-cli":      filepath.Join(f.b.req.AppsDir, "ai/codex/cli/bin/codex"),
		"hermes-agent":   filepath.Join(f.b.discovery.home, ".local/bin/hermes"),
		"ollama":         filepath.Join(f.b.discovery.home, ".local/bin/ollama"),
	}
	for _, path := range paths {
		writeFixture(t, path, []byte("not read or executed"), 0711)
	}
	// Request.Home and obsolete environment overrides must not select user paths.
	writeFixture(t, filepath.Join(f.b.req.Home, ".local/bin/picoclaw"), []byte("wrong home"), 0755)
	t.Setenv("ATM_AI_PICOCLAW_INSTALL_DIR", filepath.Join(f.b.req.Home, "override"))
	r := discoveryRun(t, f.b)
	for id, path := range paths {
		e := discoveryFind(t, r, path, "candidate")
		if e.Provider != id {
			t.Fatalf("provider %s, want %s", e.Provider, id)
		}
	}
	for _, e := range r.Evidence {
		if strings.HasPrefix(e.Path, f.b.req.Home+"/") {
			t.Fatal("used ATM runtime as user home or legacy override")
		}
	}
	if f.calls != 0 {
		t.Fatal("discovery accessed network")
	}
}

func TestDiscoveryAllCandidatesAndSystemInjection(t *testing.T) {
	f := newFixture(t, "amd64")
	root := filepath.Dir(f.b.req.AppsDir)
	dirs := []string{filepath.Join(root, "manual one"), filepath.Join(root, "manual-two"), filepath.Join(root, "system")}
	for _, dir := range dirs {
		writeFixture(t, filepath.Join(dir, "codex"), []byte("arbitrary script metadata"), 0755)
	}
	f.b.discovery.path = strings.Join(dirs[:2], ":")
	f.b.discovery.systemDirs = dirs[2:]
	r := discoveryRun(t, f.b)
	for i, dir := range dirs {
		e := discoveryFind(t, r, filepath.Join(dir, "codex"), "candidate")
		want := "PATH"
		if i == 2 {
			want = "system-path"
		}
		if e.Source != want {
			t.Fatalf("source %s, want %s", e.Source, want)
		}
	}
	// A directory whose name resembles a product is not executable evidence.
	if err := os.MkdirAll(filepath.Join(dirs[0], "hermes-desktop"), 0700); err != nil {
		t.Fatal(err)
	}
	discoveryFind(t, discoveryRun(t, f.b), filepath.Join(dirs[0], "hermes-desktop"), "not-regular")
}

func TestDiscoveryRejectsSpecialFilesBrokenLinksAndCycles(t *testing.T) {
	f := newFixture(t, "amd64")
	dir := filepath.Join(f.b.discovery.home, ".local/bin")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(dir, "ollama"), 0700); err != nil {
		t.Fatal(err)
	}
	discoveryLink(t, "missing", filepath.Join(dir, "codex"))
	discoveryLink(t, "picoclaw", filepath.Join(dir, "picoclaw"))
	discoveryLink(t, "ollama", filepath.Join(dir, "hermes"))
	writeFixture(t, filepath.Join(dir, "hermes-agent"), []byte("not executable"), 0600)
	r := discoveryRun(t, f.b)
	for name, state := range map[string]string{"ollama": "not-regular", "codex": "broken-link", "picoclaw": "symlink-cycle", "hermes": "not-regular", "hermes-agent": "not-executable", "hermes-desktop": "missing"} {
		discoveryFind(t, r, filepath.Join(dir, name), state)
	}
	// Broken parent links are distinct from absent children under valid links.
	parent := filepath.Join(f.b.discovery.home, "broken-parent")
	discoveryLink(t, "absent", parent)
	_, _, state, err := discoveryStat(context.Background(), filepath.Join(parent, "codex"))
	if err != nil || state != "broken-link" {
		t.Fatalf("broken parent: %s, %v", state, err)
	}
	valid := filepath.Join(f.b.discovery.home, "valid-parent")
	discoveryLink(t, dir, valid)
	_, _, state, err = discoveryStat(context.Background(), filepath.Join(valid, "absent"))
	if err != nil || state != "missing" {
		t.Fatalf("missing under valid parent: %s, %v", state, err)
	}
}

func TestDiscoveryInaccessibleIsNotMissing(t *testing.T) {
	f := newFixture(t, "amd64")
	dir := filepath.Join(f.b.discovery.home, "inaccessible")
	path := filepath.Join(dir, "codex")
	writeFixture(t, path, []byte("metadata only"), 0755)
	if err := os.Chmod(dir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })
	f.b.discovery.path = dir
	if os.Geteuid() != 0 {
		discoveryFind(t, discoveryRun(t, f.b), path, "inaccessible")
	}
	if discoveryError(syscall.EACCES, false) != "inaccessible" || discoveryError(syscall.EPERM, true) != "inaccessible" {
		t.Fatal("permission errors conflated with absence")
	}
}

func TestDiscoveryLinkTargetsRequireDirectorySemantics(t *testing.T) {
	f := newFixture(t, "amd64")
	dir := filepath.Join(f.b.discovery.home, "targets")
	executable := filepath.Join(dir, "executable")
	writeFixture(t, executable, []byte("metadata only"), 0755)
	for i, target := range []string{"executable/", "executable/.", "executable//", "executable/./..", executable + "/", executable + "/."} {
		path := filepath.Join(dir, fmt.Sprintf("link-%d", i))
		discoveryLink(t, target, path)
		_, _, state, err := discoveryStat(context.Background(), path)
		if err != nil || state != "not-regular" {
			t.Fatalf("link target %q: state=%s, err=%v", target, state, err)
		}
	}
	valid := filepath.Join(dir, "valid")
	discoveryLink(t, "./executable", valid)
	_, info, state, err := discoveryStat(context.Background(), valid)
	if err != nil || state != "" || !info.Mode().IsRegular() {
		t.Fatalf("valid dot prefix: %s, %v", state, err)
	}
}

func TestDiscoveryIgnoresUnsafeRelativeAndCurrentDirectoryPaths(t *testing.T) {
	f := newFixture(t, "amd64")
	root := filepath.Dir(f.b.req.AppsDir)
	cwd := filepath.Join(root, "cwd")
	writeFixture(t, filepath.Join(cwd, "codex"), []byte("cwd executable"), 0755)
	alias := filepath.Join(root, "cwd-alias")
	discoveryLink(t, cwd, alias)
	unsafe := filepath.Join(root, "unsafe\npath")
	writeFixture(t, filepath.Join(unsafe, "codex"), []byte("unsafe executable"), 0755)
	local := filepath.Join(f.b.discovery.home, ".local/bin")
	discoveryLink(t, filepath.Join(unsafe, "codex"), filepath.Join(local, "codex"))
	discoveryLink(t, filepath.Join(cwd, "codex"), filepath.Join(local, "picoclaw"))
	f.b.discovery.path = ":.:relative:" + cwd + ":" + alias + ":" + unsafe
	r := discoveryRun(t, f.b)
	if r.Skipped < 6 {
		t.Fatalf("unsafe entries not skipped: %+v", r)
	}
	for _, e := range r.Evidence {
		if e.Source == "PATH" {
			t.Fatalf("searched unsafe PATH entry %+v", e)
		}
	}
	discoveryFind(t, r, filepath.Join(local, "codex"), "unsafe-path")
	discoveryFind(t, r, filepath.Join(local, "picoclaw"), "excluded-cwd")
	for _, path := range []string{"", "relative", "/tmp/a\x1b", "/tmp/a\u0085", "/tmp/a\u202e", "/tmp/a\x00", "/tmp/\xff", "/tmp/../other"} {
		if discoverySafePath(path) {
			t.Fatalf("accepted unsafe path %q", path)
		}
	}
}

func TestDiscoveryBounds(t *testing.T) {
	f := newFixture(t, "amd64")
	root := filepath.Dir(f.b.req.AppsDir)
	var dirs []string
	for i := 0; i < discoveryMaxPathDirs+5; i++ {
		dirs = append(dirs, filepath.Join(root, fmt.Sprintf("path-%d", i)))
	}
	f.b.discovery.path = strings.Join(dirs, ":")
	r := discoveryRun(t, f.b)
	if !r.Limited || len(r.Evidence) > discoveryMaxCandidates {
		t.Fatal("PATH entry or candidate bound not applied")
	}
	for _, e := range r.Evidence {
		if strings.HasPrefix(e.Path, dirs[discoveryMaxPathDirs]+"/") {
			t.Fatal("scanned beyond PATH entry limit")
		}
	}
	f.b.discovery.path = strings.Repeat("x", discoveryMaxPathBytes+1) + ":" + dirs[0]
	r = discoveryRun(t, f.b)
	if !r.Limited {
		t.Fatal("PATH byte limit not applied")
	}
	for _, e := range r.Evidence {
		if e.Source == "PATH" {
			t.Fatal("scanned a truncated component or beyond PATH byte limit")
		}
	}
	f.b.discovery.path = ""
	f.b.discovery.systemDirs = dirs[:discoveryMaxSystemDirs+1]
	if !discoveryRun(t, f.b).Limited {
		t.Fatal("system directory bound not applied")
	}
	_, _, state, err := discoveryStat(context.Background(), root+"/"+strings.Repeat("x", discoveryMaxPath))
	if err != nil || state != "unsafe-path" {
		t.Fatalf("path bound: %s, %v", state, err)
	}
}

type discoveryCancelContext struct {
	context.Context
	remaining int
}

func (c *discoveryCancelContext) Err() error {
	c.remaining--
	if c.remaining <= 0 {
		return context.Canceled
	}
	return nil
}

func TestDiscoveryCancellation(t *testing.T) {
	f := newFixture(t, "amd64")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.b.discover(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-cancel: %v", err)
	}
	path := filepath.Join(f.b.discovery.home, ".local/bin/codex")
	writeFixture(t, path, []byte("metadata only"), 0755)
	during := &discoveryCancelContext{Context: context.Background(), remaining: 4}
	if _, _, _, err := discoveryStat(during, path); !errors.Is(err, context.Canceled) {
		t.Fatalf("component cancellation: %v", err)
	}
}

func TestDiscoveryPreviewAndExternalAliases(t *testing.T) {
	f := newFixture(t, "amd64")
	f.release(t, "picoclaw", "1.2.3", nil)
	mustRun(t, f, "install", "--provider", "picoclaw", "--trust-download")
	active := filepath.Join(f.b.root("picoclaw"), "current")
	local := filepath.Join(f.b.discovery.home, ".local/bin/picoclaw")
	discoveryLink(t, active, local)
	legacy := filepath.Join(f.b.req.AppsDir, "ai/picoclaw/picoclaw")
	writeFixture(t, legacy, []byte("separate legacy binary"), 0755)
	dir := filepath.Join(f.b.discovery.home, "manual")
	manual := filepath.Join(dir, "picoclaw")
	writeFixture(t, manual, []byte("separate manual binary"), 0755)
	f.b.discovery.path = dir
	r := discoveryRun(t, f.b)
	discoveryFind(t, r, active, "preview-candidate")
	discoveryFind(t, r, local, "native-preview-alias")
	discoveryFind(t, r, legacy, "candidate")
	discoveryFind(t, r, manual, "candidate")
	if err := os.Remove(local); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(f.b.root("picoclaw"), "versions/1.2.3/tool"), local); err != nil {
		t.Fatal(err)
	}
	discoveryFind(t, discoveryRun(t, f.b), local, "native-preview-alias")
	aliasDir := filepath.Join(f.b.discovery.home, "alias")
	discoveryLink(t, manual, filepath.Join(aliasDir, "picoclaw"))
	f.b.discovery.path += ":" + aliasDir + ":" + dir
	r = discoveryRun(t, f.b)
	discoveryFind(t, r, filepath.Join(aliasDir, "picoclaw"), "duplicate")
	count := 0
	for _, e := range r.Evidence {
		if e.Path == manual {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("duplicate literal path emitted %d times", count)
	}
}

func TestDiscoveryStatusMenusAndNoLifecycleAuthorization(t *testing.T) {
	f := newFixture(t, "amd64")
	path := filepath.Join(f.b.discovery.home, ".local/bin/codex")
	writeFixture(t, path, []byte("unverified executable"), 0755)
	ollama := filepath.Join(f.b.discovery.home, ".local/bin/ollama")
	writeFixture(t, ollama, []byte("unverified Ollama executable"), 0755)
	var logs []string
	f.b.log = func(s string) { logs = append(logs, s) }
	mustRun(t, f, "status")
	text := strings.Join(logs, "\n")
	for _, want := range []string{fmt.Sprintf("path=%q", path), "unverified external executable candidate", "version=unknown", "runtime=unknown", "hermes-agent:", "ollama:", "codex-desktop-system", "managed-preview path=", "legacy-default path=", "missing"} {
		if !strings.Contains(text, want) {
			t.Fatalf("status missing %q: %s", want, text)
		}
	}
	logs = nil
	mustRun(t, f, "menu", "codex-cli")
	text = strings.Join(logs, "\n")
	if !strings.Contains(text, fmt.Sprintf("path=%q", path)) || strings.Contains(text, "ollama:") {
		t.Fatalf("submenu did not filter discovery: %s", text)
	}
	for _, id := range []string{"codex-desktop"} {
		if _, err := parseOptions("menu", []string{id}); err == nil {
			t.Fatalf("detection enabled menu route %s", id)
		}
	}
	mustRun(t, f, "menu", "ollama")
	for _, id := range []string{"codex-cli", "ollama"} {
		for _, op := range []string{"update", "use", "remove", "uninstall"} {
			args := []string{"--provider", id}
			if op == "update" {
				args = append(args, "--trust-download")
			} else if op != "uninstall" {
				args = append(args, "--version", "1.2.3")
			}
			if err := f.run(context.Background(), op, args...); err == nil {
				t.Fatalf("external candidate authorized %s %s", id, op)
			}
		}
	}
	if data, err := os.ReadFile(ollama); err != nil || string(data) != "unverified Ollama executable" {
		t.Fatalf("external Ollama changed: %q %v", data, err)
	}
	if f.calls != 0 {
		t.Fatal("discovery/lifecycle refusal accessed network")
	}
	if _, err := os.Lstat(f.b.req.AppsDir); !os.IsNotExist(err) {
		t.Fatal("discovery/lifecycle refusal wrote application state")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "unverified executable" {
		t.Fatal("external fixture modified")
	}
}

func TestDiscoveryInactivePreviewHardlinkIsAmbiguous(t *testing.T) {
	f := newFixture(t, "amd64")
	f.release(t, "picoclaw", "1.2.3", nil)
	mustRun(t, f, "install", "--provider", "picoclaw", "--trust-download")
	f.release(t, "picoclaw", "1.2.4", nil)
	mustRun(t, f, "update", "--provider", "picoclaw", "--trust-download")
	local := filepath.Join(f.b.discovery.home, ".local/bin/picoclaw")
	if err := os.MkdirAll(filepath.Dir(local), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(f.b.root("picoclaw"), "versions/1.2.3/tool"), local); err != nil {
		t.Fatal(err)
	}
	r := discoveryRun(t, f.b)
	discoveryFind(t, r, local, "ambiguous-hardlink")
	discoveryFind(t, r, filepath.Join(f.b.root("picoclaw"), "current"), "preview-candidate")
}

func TestDiscoveryEnvironmentHomeAndQuotedPaths(t *testing.T) {
	f := newFixture(t, "amd64")
	home := filepath.Join(filepath.Dir(f.b.req.AppsDir), "user \"quoted\" \\home")
	path := filepath.Join(home, ".local/bin/hermes-agent")
	writeFixture(t, path, []byte("metadata"), 0755)
	t.Setenv("HOME", home)
	t.Setenv("PATH", "")
	// Exercise environment selection without using host system roots as fixtures.
	c := defaultDiscoveryConfig()
	c.systemDirs = nil
	c.cwd = f.b.discovery.cwd
	f.b.discovery = c
	var logs []string
	f.b.log = func(s string) { logs = append(logs, s) }
	if err := f.b.logDiscovery(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(logs, "\n"), fmt.Sprintf("path=%q", path)) {
		t.Fatal("path identity not preserved with quoting")
	}
}
