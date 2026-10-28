//go:build linux

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AtomyCloud/atm/internal/protocol"
)

type account struct {
	Name  string
	UID   int
	Home  string
	Shell string
}

func parseAccount(v []byte, uid int) (account, error) {
	var found []account
	for _, line := range strings.Split(string(v), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, ":")
		if len(parts) != 7 {
			return account{}, fmt.Errorf("malformed local account database")
		}
		n, e := strconv.Atoi(parts[2])
		if e != nil {
			return account{}, fmt.Errorf("malformed account uid")
		}
		if n != uid {
			continue
		}
		name := parts[0]
		if name == "" || strings.HasPrefix(name, "-") {
			return account{}, fmt.Errorf("unsafe account name")
		}
		for _, r := range name {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.') {
				return account{}, fmt.Errorf("unsafe account name")
			}
		}
		if absolutePath(parts[5]) != nil || absolutePath(parts[6]) != nil {
			return account{}, fmt.Errorf("unsafe account home/shell")
		}
		found = append(found, account{name, n, parts[5], parts[6]})
	}
	if len(found) != 1 {
		return account{}, fmt.Errorf("unique local account required; remote/NSS accounts Pending")
	}
	return found[0], nil
}
func currentAccount() (account, error) {
	if os.Getuid() == 0 || os.Getuid() != os.Geteuid() {
		return account{}, fmt.Errorf("run as a normal, non-setuid account")
	}
	if e := trustedFile("/etc/passwd", false); e != nil {
		return account{}, e
	}
	v, e := readFile("/etc/passwd", 1<<20, false)
	if e != nil {
		return account{}, e
	}
	return parseAccount(v, os.Getuid())
}
func registeredShells() ([]string, error) {
	if e := trustedFile("/etc/shells", false); e != nil {
		return nil, e
	}
	v, e := readFile("/etc/shells", 64<<10, false)
	if e != nil {
		return nil, e
	}
	out := []string{}
	seen := map[string]bool{}
	for _, line := range strings.Split(string(v), "\n") {
		s := strings.TrimSpace(line)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		if absolutePath(s) != nil || seen[s] {
			continue
		}
		p, e := filepath.EvalSymlinks(s)
		if e != nil {
			continue
		}
		if e = trustedFile(p, true); e != nil {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	if len(out) > 60 {
		return nil, fmt.Errorf("too many registered shells")
	}
	sort.Strings(out)
	return out, nil
}
func (b *backend) verifiedAccount() (account, error) {
	a, e := b.account()
	if e != nil {
		return a, e
	}
	if a.Home != b.home || a.UID != os.Getuid() {
		return a, fmt.Errorf("configured home differs from verified local account home")
	}
	return a, nil
}

func localRemovalSafe() error {
	if e := trustedFile("/etc/passwd", false); e != nil {
		return e
	}
	v, e := readFile("/etc/passwd", 1<<20, false)
	if e != nil {
		return e
	}
	known := []os.FileInfo{}
	for _, p := range []string{"/usr/bin/zsh", "/bin/zsh"} {
		i, e := os.Stat(p)
		if e == nil {
			known = append(known, i)
		} else if !os.IsNotExist(e) {
			return e
		}
	}
	resolve := func(p string) (string, error) {
		r, e := filepath.EvalSymlinks(p)
		if e != nil {
			return "", e
		}
		if e = trustedFile(r, true); e != nil {
			return "", e
		}
		return r, nil
	}
	return verifyLoginShells(v, resolve, func(p string) (bool, error) {
		i, e := os.Stat(p)
		if e != nil {
			return false, e
		}
		for _, k := range known {
			if os.SameFile(i, k) {
				return true, nil
			}
		}
		return false, nil
	})
}
func verifyLoginShells(v []byte, resolve func(string) (string, error), sameZsh func(string) (bool, error)) error {
	if e := noLocalZsh(v); e != nil {
		return e
	}
	for _, line := range strings.Split(string(v), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, ":")
		p := parts[6]
		if absolutePath(p) != nil {
			return fmt.Errorf("unresolved local account shell; system removal refused")
		}
		resolved, e := resolve(p)
		if e != nil {
			return fmt.Errorf("unresolved/untrusted local account shell; system removal refused: %w", e)
		}
		if strings.HasPrefix(filepath.Base(resolved), "zsh") {
			return fmt.Errorf("local account shell alias resolves to Zsh; removal refused")
		}
		same, e := sameZsh(resolved)
		if e != nil {
			return e
		}
		if same {
			return fmt.Errorf("local account shell is a Zsh hardlink; removal refused")
		}
	}
	return nil
}
func noLocalZsh(v []byte) error {
	for _, line := range strings.Split(string(v), "\n") {
		if line == "" {
			continue
		}
		p := strings.Split(line, ":")
		if len(p) != 7 {
			return fmt.Errorf("malformed account database")
		}
		if strings.HasPrefix(filepath.Base(p[6]), "zsh") {
			return fmt.Errorf("a local account still uses Zsh; system removal refused")
		}
	}
	return nil
}
func (b *backend) shellMenu() ([]protocol.Choice, error) {
	a, e := b.verifiedAccount()
	if e != nil {
		return nil, e
	}
	shells, e := b.shells()
	if e != nil {
		return nil, e
	}
	b.log("Verified local account: " + a.Name + "; current login shell: " + a.Shell)
	cs := []protocol.Choice{}
	for i, s := range shells {
		label := s
		if s == a.Shell {
			label += " (current)"
		}
		cs = append(cs, choice(fmt.Sprintf("shell-%d", i), label, "use", s, "--accept-shell-change"))
	}
	return cs, nil
}
func (b *backend) changeShell(ctx context.Context, target string) error {
	if absolutePath(target) != nil {
		return fmt.Errorf("absolute registered shell required")
	}
	before, e := b.verifiedAccount()
	if e != nil {
		return e
	}
	shells, e := b.shells()
	if e != nil {
		return e
	}
	found := false
	for _, s := range shells {
		if s == target {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("shell is not registered and trusted")
	}
	if before.Shell == target {
		b.log("Default shell unchanged")
		return nil
	}
	chsh, e := b.resolve("chsh")
	if e != nil {
		return e
	}
	sudo, e := b.resolve("sudo")
	if e != nil {
		return e
	}
	o, e := b.begin("shell-change")
	if e != nil {
		return e
	}
	now, e := b.verifiedAccount()
	if e != nil {
		return e
	}
	if now != before {
		return fmt.Errorf("account changed before shell change")
	}
	shells, e = b.shells()
	if e != nil {
		return e
	}
	found = false
	for _, s := range shells {
		if s == target {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("registered shell changed")
	}
	if e = o.phase("chsh-running"); e != nil {
		return e
	}
	_, e = b.run(ctx, sudo, []string{"-n", "--", chsh, "-s", target, before.Name})
	if e != nil {
		return fmt.Errorf("chsh failed; inspect account and retained operation journal: %w", e)
	}
	after, e := b.verifiedAccount()
	if e != nil {
		return e
	}
	expected := before
	expected.Shell = target
	if after != expected {
		return fmt.Errorf("account shell verification failed; inspect retained journal")
	}
	if e = o.finish(); e != nil {
		return e
	}
	b.log("You must log out and log back in for the new default shell to take effect.")
	return nil
}

type options struct{ commit string }

func parseOptions(action string, args []string) (options, error) {
	o := options{}
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		s := args[i]
		if seen[s] {
			return o, fmt.Errorf("duplicate argument")
		}
		seen[s] = true
		switch s {
		case "--commit":
			if action == "remove" || i+1 == len(args) {
				return o, fmt.Errorf("invalid commit argument")
			}
			i++
			o.commit = args[i]
			if !validHex(o.commit, 20) {
				return o, fmt.Errorf("full lowercase 40-hex commit required")
			}
		case "--trust-official-omz":
			if action == "remove" {
				return o, fmt.Errorf("unexpected archive trust")
			}
		case "--accept-system-packages", "--accept-snapshot":
		case "--accept-removal":
			if action != "remove" {
				return o, fmt.Errorf("unexpected removal consent")
			}
		default:
			return o, fmt.Errorf("unknown argument: %s", s)
		}
	}
	if !seen["--accept-system-packages"] || !seen["--accept-snapshot"] {
		return o, fmt.Errorf("explicit --accept-system-packages and --accept-snapshot required")
	}
	if action == "remove" {
		if !seen["--accept-removal"] {
			return o, fmt.Errorf("--accept-removal required")
		}
	} else if o.commit == "" || !seen["--trust-official-omz"] {
		return o, fmt.Errorf("--commit and --trust-official-omz required")
	}
	return o, nil
}
func aptArguments(action string) [][]string {
	policy := []string{"-o", "APT::Get::AllowUnauthenticated=false", "-o", "Acquire::AllowInsecureRepositories=false", "-o", "Acquire::AllowDowngradeToInsecureRepositories=false", "-o", "APT::Get::AutomaticRemove=false", "-o", "APT::Get::Purge=false"}
	command := func(args ...string) []string { return append(append([]string(nil), policy...), args...) }
	switch action {
	case "install":
		return [][]string{command("-o", "APT::Update::Error-Mode=any", "update"), command("install", "-y", "--no-remove", "--no-install-recommends", "zsh", "git")}
	case "update":
		return [][]string{command("-o", "APT::Update::Error-Mode=any", "update"), command("install", "-y", "--no-remove", "--only-upgrade", "zsh")}
	case "remove":
		return [][]string{command("remove", "-y", "zsh")}
	}
	return nil
}

type boundedOutput struct {
	mu   sync.Mutex
	data []byte
}

func (w *boundedOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	if len(w.data) < 32<<10 {
		remain := (32 << 10) - len(w.data)
		if len(p) > remain {
			p = p[:remain]
		}
		w.data = append(w.data, p...)
	}
	return n, nil
}
func runCommand(ctx context.Context, path string, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = "/"
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "DEBIAN_FRONTEND=noninteractive", "TERM=dumb"}
	cmd.Stdin = nil
	cmd.WaitDelay = time.Second
	out := &boundedOutput{}
	cmd.Stdout = out
	cmd.Stderr = out
	e := cmd.Run()
	if ctx.Err() != nil {
		e = ctx.Err()
	}
	return string(out.data), e
}
