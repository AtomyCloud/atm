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
	"unicode"
	"unicode/utf8"
)

const (
	discoveryMaxPath       = 2000
	discoveryMaxPathBytes  = 32 << 10
	discoveryMaxPathDirs   = 64
	discoveryMaxSystemDirs = 8
	discoveryMaxCandidates = 448
	discoveryMaxLinks      = 40
	discoveryMaxSteps      = 512
)

// This metadata-only configuration is injectable in tests, never through ATM_*.
// Request.Home is the ATM runtime root, not the user's home directory.
type discoveryConfig struct {
	home, path, cwd string
	systemDirs      []string
}

func defaultDiscoveryConfig() *discoveryConfig {
	cwd, _ := os.Getwd()
	return &discoveryConfig{home: os.Getenv("HOME"), path: os.Getenv("PATH"), cwd: cwd,
		systemDirs: []string{"/usr/local/bin", "/usr/bin", "/bin"}}
}

type discoveryEvidence struct {
	Provider, Path, Source, State string
	resolved                      string
	info                          os.FileInfo
}

type discoveryReport struct {
	Evidence []discoveryEvidence
	Skipped  int
	Limited  bool
}

var discoveryCommands = []struct{ provider, command string }{
	{"hermes-agent", "hermes-agent"}, {"hermes-agent", "hermes"},
	{"ollama", "ollama"}, {"picoclaw", "picoclaw"},
	{"hermes-desktop", "hermes-desktop"}, {"codex-cli", "codex"},
}

var discoveryOrder = []string{"picoclaw", "hermes-desktop", "codex-cli", "hermes-agent", "ollama"}

func discoverySafeText(s string) bool {
	if !utf8.ValidString(s) || len(s) > discoveryMaxPath {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}

func discoverySafePath(p string) bool {
	return discoverySafeText(p) && filepath.IsAbs(p) && p != "/" && filepath.Clean(p) == p
}

func discoveryError(err error, linkTarget bool) string {
	switch {
	case errors.Is(err, syscall.ELOOP):
		return "symlink-cycle"
	case errors.Is(err, os.ErrNotExist):
		if linkTarget {
			return "broken-link"
		}
		return "missing"
	case errors.Is(err, syscall.ENOTDIR):
		return "not-regular"
	default:
		return "inaccessible"
	}
}

// Resolve one literal path with metadata only. Each component and symlink is
// bounded and cancellation-checked; no directory enumeration or file open occurs.
func discoveryStat(ctx context.Context, path string) (string, os.FileInfo, string, error) {
	if err := ctx.Err(); err != nil {
		return "", nil, "", err
	}
	if !discoverySafePath(path) {
		return "", nil, "unsafe-path", nil
	}
	type part struct {
		name string
		link bool
	}
	parts := func(s string, link bool) []part {
		var out []part
		for _, name := range strings.Split(s, "/") {
			if name != "" {
				out = append(out, part{name, link})
			}
		}
		// A trailing slash requires the previous component to be a directory.
		if strings.HasSuffix(s, "/") {
			out = append(out, part{".", link})
		}
		return out
	}
	pending := parts(path, false)
	resolved := "/"
	links := 0
	var info os.FileInfo
	for step := 0; len(pending) > 0; step++ {
		if err := ctx.Err(); err != nil {
			return "", nil, "", err
		}
		if step >= discoveryMaxSteps {
			return "", nil, "resolution-limit", nil
		}
		p := pending[0]
		pending = pending[1:]
		if p.name == "." {
			continue
		}
		if p.name == ".." {
			resolved = filepath.Dir(resolved)
			info = nil
			continue
		}
		next := filepath.Join(resolved, p.name)
		if !discoverySafePath(next) {
			return "", nil, "unsafe-path", nil
		}
		i, err := os.Lstat(next)
		if err != nil {
			return "", nil, discoveryError(err, p.link), nil
		}
		if i.Mode()&os.ModeSymlink != 0 {
			links++
			if links > discoveryMaxLinks {
				return "", nil, "symlink-cycle", nil
			}
			target, err := os.Readlink(next)
			if err != nil {
				return "", nil, discoveryError(err, true), nil
			}
			if target == "" || !discoverySafeText(target) {
				return "", nil, "unsafe-path", nil
			}
			if filepath.IsAbs(target) {
				resolved = "/"
			}
			pending = append(parts(target, true), pending...)
			if len(pending) > discoveryMaxSteps {
				return "", nil, "resolution-limit", nil
			}
			info = nil
			continue
		}
		if len(pending) != 0 && !i.IsDir() {
			return "", nil, "not-regular", nil
		}
		resolved, info = next, i
	}
	if err := ctx.Err(); err != nil {
		return "", nil, "", err
	}
	if info == nil { // A link ending in '/', '.' or '..' can resolve to a directory.
		var err error
		info, err = os.Lstat(resolved)
		if err != nil {
			return "", nil, discoveryError(err, links > 0), nil
		}
	}
	return resolved, info, "", nil
}

func discoveryWithin(path, root string) bool {
	return root != "" && (path == root || strings.HasPrefix(path, root+string(filepath.Separator)))
}

func (b *backend) discover(ctx context.Context) (discoveryReport, error) {
	r := discoveryReport{}
	if err := ctx.Err(); err != nil {
		return r, err
	}
	c := b.discovery
	if c == nil {
		c = defaultDiscoveryConfig()
	}
	seen := map[string]bool{}
	add := func(id, path, source string) {
		if !discoverySafePath(path) {
			r.Skipped++
			return
		}
		if seen[path] {
			return
		}
		if len(r.Evidence) >= discoveryMaxCandidates {
			r.Limited = true
			return
		}
		seen[path] = true
		r.Evidence = append(r.Evidence, discoveryEvidence{Provider: id, Path: path, Source: source})
	}
	var nativeRoots []string
	for _, id := range providerOrder {
		root := b.root(id)
		nativeRoots = append(nativeRoots, root)
		resolved, _, _, err := discoveryStat(ctx, root)
		if err != nil {
			return r, err
		}
		if resolved != "" {
			nativeRoots = append(nativeRoots, resolved)
		}
		add(id, filepath.Join(root, "current"), "managed-preview")
	}
	add("picoclaw", filepath.Join(b.req.AppsDir, "ai/picoclaw/picoclaw"), "legacy-default")
	add("hermes-desktop", filepath.Join(b.req.AppsDir, "hermes/hermes-desktop/current.AppImage"), "legacy-default")
	add("codex-cli", filepath.Join(b.req.AppsDir, "ai/codex/cli/bin/codex"), "legacy-default")
	cwd, _, _, err := discoveryStat(ctx, c.cwd)
	if err != nil {
		return r, err
	}
	addDir := func(dir, source string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !discoverySafePath(dir) || dir == c.cwd {
			r.Skipped++
			return nil
		}
		resolved, _, _, err := discoveryStat(ctx, dir)
		if err != nil {
			return err
		}
		if cwd != "" && resolved == cwd {
			r.Skipped++
			return nil
		}
		for _, cmd := range discoveryCommands {
			add(cmd.provider, filepath.Join(dir, cmd.command), source)
		}
		return nil
	}
	if discoverySafePath(c.home) {
		if err := addDir(filepath.Join(c.home, ".local/bin"), "user-local"); err != nil {
			return r, err
		}
	} else {
		r.Skipped++
	}
	for i, dir := range c.systemDirs {
		if i >= discoveryMaxSystemDirs {
			r.Limited = true
			break
		}
		if err := addDir(dir, "system-path"); err != nil {
			return r, err
		}
	}
	// Never use a truncated PATH component as a different search directory.
	path := c.path
	if len(path) > discoveryMaxPathBytes {
		r.Limited = true
		path = path[:discoveryMaxPathBytes]
		if end := strings.LastIndexByte(path, ':'); end >= 0 {
			path = path[:end]
		} else {
			path = ""
		}
	}
	for i, dir := range strings.Split(path, ":") {
		if i >= discoveryMaxPathDirs {
			r.Limited = true
			break
		}
		if err := addDir(dir, "PATH"); err != nil {
			return r, err
		}
	}
	var nativeFiles []os.FileInfo
	var externalFiles []os.FileInfo
	for i := range r.Evidence {
		e := &r.Evidence[i]
		resolved, info, state, err := discoveryStat(ctx, e.Path)
		if err != nil {
			return r, err
		}
		e.resolved, e.info, e.State = resolved, info, state
		if state != "" {
			continue
		}
		switch {
		case !info.Mode().IsRegular():
			e.State = "not-regular"
		case info.Mode().Perm()&0111 == 0:
			e.State = "not-executable"
		case e.Source == "managed-preview":
			e.State = "preview-candidate"
			nativeFiles = append(nativeFiles, info)
		default:
			e.State = "candidate"
			if cwd != "" && filepath.Dir(resolved) == cwd {
				e.State = "excluded-cwd"
			}
			for _, root := range nativeRoots {
				if discoveryWithin(e.Path, root) || discoveryWithin(resolved, root) {
					e.State = "native-preview-alias"
				}
			}
			for _, info := range nativeFiles {
				if os.SameFile(e.info, info) {
					e.State = "native-preview-alias"
				}
			}
			if stat, ok := e.info.Sys().(*syscall.Stat_t); e.State == "candidate" && ok && stat.Nlink > 1 {
				// An unscanned inactive preview version may share this inode.
				e.State = "ambiguous-hardlink"
			}
			if e.State == "candidate" {
				for _, info := range externalFiles {
					if os.SameFile(e.info, info) {
						e.State = "duplicate"
					}
				}
				externalFiles = append(externalFiles, e.info)
			}
		}
	}
	return r, ctx.Err()
}

func (b *backend) logDiscovery(ctx context.Context, provider string) error {
	r, err := b.discover(ctx)
	if err != nil {
		return err
	}
	if provider == hermesSystem {
		provider = "hermes-desktop"
	}
	b.log(label("discovery_scope"))
	for _, id := range discoveryOrder {
		if provider != "" && id != provider {
			continue
		}
		checked, missing := 0, 0
		for _, e := range r.Evidence {
			if e.Provider != id {
				continue
			}
			checked++
			if e.State == "missing" {
				missing++
				if e.Source != "legacy-default" && e.Source != "managed-preview" {
					continue
				}
			}
			b.log(fmt.Sprintf(label("discovery_evidence"), id, e.Source, e.Path, label("discovery_"+e.State)))
		}
		b.log(fmt.Sprintf(label("discovery_summary"), id, checked, missing))
	}
	if r.Limited || r.Skipped > 0 {
		b.log(fmt.Sprintf(label("discovery_limits"), r.Limited, r.Skipped))
	}
	if provider == "" {
		b.log(label("discovery_codex_desktop"))
	}
	return ctx.Err()
}
