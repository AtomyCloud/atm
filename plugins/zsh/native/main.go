//go:build linux

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/AtomyCloud/atm/internal/config"
	"github.com/AtomyCloud/atm/internal/protocol"
)

type backend struct {
	req                                    protocol.Request
	home, dot, omz, custom, backups, state string
	log                                    func(string)
	account                                func() (account, error)
	removalSafe                            func() error
	shells                                 func() ([]string, error)
	resolve                                func(string) (string, error)
	run                                    func(context.Context, string, []string) (string, error)
	fetch                                  func(context.Context, string) ([]byte, error)
	discover                               func(context.Context) (string, error)
	rename                                 func(string, string) error
}

func envDefault(key, fallback string) string {
	if s := os.Getenv(key); s != "" {
		return s
	}
	return fallback
}
func newBackend(req protocol.Request, log func(string)) (*backend, error) {
	if req.PluginID != "zsh" || (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64") {
		return nil, fmt.Errorf("Linux amd64/arm64 required; other platforms Pending")
	}
	home, e := os.UserHomeDir()
	if e != nil {
		return nil, e
	}
	cfg, e := config.Load(req.Home, false)
	if e != nil {
		return nil, e
	}
	home = envDefault("ATM_ZSH_USER_HOME", home)
	b := &backend{req: req, home: home, log: log, account: currentAccount, removalSafe: localRemovalSafe, shells: registeredShells, resolve: systemCommand, run: runCommand, rename: renameAbsent}
	b.dot = envDefault("ATM_ZSH_DOT_DIR", envDefault("ZDOTDIR", home))
	b.omz = envDefault("ATM_ZSH_OMZ_DIR", envDefault("ZSH", filepath.Join(home, ".oh-my-zsh")))
	b.custom = envDefault("ATM_ZSH_CUSTOM_DIR", envDefault("ZSH_CUSTOM", filepath.Join(b.omz, "custom")))
	b.backups = envDefault("ATM_ZSH_BACKUP_DIR", filepath.Join(req.AppsDir, "Backups"))
	b.state = filepath.Join(cfg.StateDir, "zsh", "native-v1")
	if e = b.validatePaths(); e != nil {
		return nil, e
	}
	b.fetch = fetchOfficial
	b.discover = discoverOfficial
	return b, nil
}
func (b *backend) roots() map[string]string {
	m := map[string]string{"omz": b.omz, "p10k": filepath.Join(b.home, ".p10k.zsh"), "shell-pre": filepath.Join(b.home, ".shell.pre-oh-my-zsh")}
	for _, s := range []string{"zshrc", "zshenv", "zprofile", "zlogin", "zlogout", "zsh_history", "zshrc.pre-oh-my-zsh"} {
		m[s] = filepath.Join(b.dot, "."+s)
	}
	if b.custom != filepath.Join(b.omz, "custom") {
		m["custom"] = b.custom
	}
	return m
}
func (b *backend) validatePaths() error {
	for _, p := range []string{b.home, b.dot, b.omz, b.custom, b.backups, b.state, b.req.Home, b.req.AppsDir} {
		if e := absolutePath(p); e != nil {
			return e
		}
	}
	roots := b.roots()
	for k, p := range roots {
		if p == b.home || p == b.dot {
			return fmt.Errorf("data root cannot equal home/dot directory")
		}
		for _, protected := range []string{b.backups, b.state, b.req.Home, b.req.AppsDir} {
			if overlaps(p, protected) {
				return fmt.Errorf("data overlaps backup/state/runtime/apps root")
			}
		}
		for j, q := range roots {
			if k != j && overlaps(p, q) {
				return fmt.Errorf("overlapping data roots")
			}
		}
	}
	for _, p := range []string{b.home, b.dot} {
		if overlaps(b.backups, p) && !strings.HasPrefix(b.backups, p+"/") {
			return fmt.Errorf("backup cannot contain home")
		}
	}
	if overlaps(b.backups, b.state) || overlaps(b.backups, b.req.Home) || overlaps(b.state, b.req.Home) {
		return fmt.Errorf("overlapping state/runtime/backups")
	}
	return nil
}
func choice(id, label, op string, args ...string) protocol.Choice {
	return protocol.Choice{ID: id, Label: label, Operation: op, Arguments: args}
}
func (b *backend) status() error {
	b.log("System Zsh version unknown; no Zsh, Git, profiles or external tools executed")
	for _, p := range []string{b.omz, filepath.Join(b.dot, ".zshrc")} {
		if _, e := os.Lstat(p); e == nil {
			b.log("Existing path preserved: " + p)
		} else if !os.IsNotExist(e) {
			return e
		}
	}
	b.log("Backups: " + b.backups)
	if _, e := os.Lstat(filepath.Join(b.state, "operation")); e == nil {
		b.log("Unresolved operation: inspect private operation journal and recovery copies before retrying")
	}
	if _, e := os.Lstat(filepath.Join(b.state, "receipt.json")); e == nil {
		b.log("Native receipt present; ownership is revalidated before changes")
	}
	return nil
}
func (b *backend) execute(ctx context.Context) ([]protocol.Choice, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if os.Getenv("ATM_DRY_RUN") == "1" {
		b.log("Dry-run: no network, commands or writes")
		return nil, nil
	}
	a := b.req.Arguments
	switch b.req.Operation {
	case "status":
		if len(a) != 0 {
			return nil, fmt.Errorf("status takes no arguments")
		}
		return nil, b.status()
	case "install", "update", "remove", "uninstall":
		action := b.req.Operation
		if action == "uninstall" {
			action = "remove"
		}
		o, e := parseOptions(action, a)
		if e != nil {
			return nil, e
		}
		return nil, b.apply(ctx, action, o)
	case "use":
		if len(a) != 2 || a[1] != "--accept-shell-change" {
			return nil, fmt.Errorf("use requires SHELL --accept-shell-change")
		}
		return nil, b.changeShell(ctx, a[0])
	case "menu":
		if len(a) == 0 {
			if e := b.status(); e != nil {
				return nil, e
			}
			return []protocol.Choice{
				choice("install", label("install"), "menu", "install"), choice("update", label("update"), "menu", "update"), choice("remove", label("remove"), "menu", "remove"),
				choice("backup", label("backup"), "menu", "backup"), choice("backups", label("backups"), "menu", "backups"), choice("shells", label("shells"), "menu", "shells")}, nil
		}
		switch a[0] {
		case "install", "update":
			if len(a) == 1 {
				c := choice("commit", label("commit"), "menu", a[0])
				c.Prompt = label("commit")
				return []protocol.Choice{choice("latest", label("latest"), "menu", a[0], "latest"), c}, nil
			}
			if len(a) == 2 && a[1] == "latest" {
				commit, e := b.discover(ctx)
				if e != nil {
					return nil, e
				}
				a = []string{a[0], commit}
			}
			if len(a) != 2 || !validHex(a[1], 20) {
				return nil, fmt.Errorf("review requires full lowercase 40-hex official commit")
			}
			b.log("Trust official https://codeload.github.com/ohmyzsh/ohmyzsh/tar.gz/" + a[1] + " over HTTPS. No independent publisher signature; no downloaded shell execution. APT uses configured administrator repositories.")
			b.log("Close all Zsh sessions. Private unencrypted snapshot first. Existing .zshrc is preserved; only an absent .zshrc gets a native loader. Local edits block replacement. System changes cannot be rolled back.")
			return []protocol.Choice{choice("apply", label("apply"), a[0], "--commit", a[1], "--trust-official-omz", "--accept-system-packages", "--accept-snapshot")}, nil
		case "remove":
			if len(a) != 1 {
				return nil, fmt.Errorf("remove review takes no arguments")
			}
			b.log("Close all Zsh sessions. Snapshot first; remove only verified native framework and unchanged generated loader. Other configuration/backups retained. Choose a non-Zsh login shell first. APT removal may affect dependents; no autoremove/purge.")
			return []protocol.Choice{choice("apply", label("apply-remove"), "remove", "--accept-system-packages", "--accept-removal", "--accept-snapshot")}, nil
		case "backup":
			if len(a) == 1 {
				b.log("Close all Zsh sessions. Snapshot includes private history and configuration; not encrypted. Links and special files refused.")
				return []protocol.Choice{choice("snapshot", label("snapshot"), "menu", "backup", "--accept-snapshot")}, nil
			}
			if len(a) != 2 || a[1] != "--accept-snapshot" {
				return nil, fmt.Errorf("backup requires --accept-snapshot")
			}
			return nil, b.backup(ctx)
		case "backups":
			if len(a) != 1 {
				return nil, fmt.Errorf("backups takes no arguments")
			}
			return b.backupMenu(ctx)
		case "restore":
			if len(a) != 6 || a[2] != "--sha256" || !validHex(a[3], 32) || a[4] != "--accept-restore" || a[5] != "--accept-snapshot" {
				return nil, fmt.Errorf("restore requires ARCHIVE --sha256 HEX --accept-restore --accept-snapshot")
			}
			return nil, b.restore(ctx, a[1], a[3])
		case "shells":
			if len(a) != 1 {
				return nil, fmt.Errorf("shells takes no arguments")
			}
			return b.shellMenu()
		}
	}
	return nil, fmt.Errorf("unsupported Zsh operation or arguments")
}
func serve(ctx context.Context, in io.Reader, out io.Writer) error {
	req, e := protocol.ReadRequest(in)
	if e != nil {
		return e
	}
	enc := json.NewEncoder(out)
	emit := func(kind, msg, status string, cs []protocol.Choice) error {
		msg = protocol.SafeText(msg)
		if len(msg) > 7000 {
			msg = "Diagnostic exceeds display limit"
		}
		return enc.Encode(protocol.Event{Protocol: 1, RequestID: req.RequestID, Type: kind, Message: msg, Status: status, Choices: cs})
	}
	if e = emit("started", label("title"), "", nil); e != nil {
		return e
	}
	b, e := newBackend(req, func(s string) { _ = emit("log", s, "", nil) })
	var cs []protocol.Choice
	if e == nil {
		cs, e = b.execute(ctx)
	}
	if e == nil {
		e = protocol.ValidateChoices(cs)
	}
	status, msg := "success", label("complete")
	if e != nil {
		status, msg = "error", e.Error()
		cs = nil
		if ctx.Err() != nil {
			status = "cancelled"
		}
	}
	return emit("result", msg, status, cs)
}
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if e := serve(ctx, os.Stdin, os.Stdout); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}

func fetchOfficial(ctx context.Context, commit string) ([]byte, error) {
	if !validHex(commit, 20) {
		return nil, fmt.Errorf("invalid commit")
	}
	client := &http.Client{Timeout: 90 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("archive redirects refused") }}
	req, e := http.NewRequestWithContext(ctx, "GET", "https://codeload.github.com/ohmyzsh/ohmyzsh/tar.gz/"+commit, nil)
	if e != nil {
		return nil, e
	}
	req.Header.Set("Accept-Encoding", "identity")
	res, e := client.Do(req)
	if e != nil {
		return nil, e
	}
	defer res.Body.Close()
	if res.StatusCode != 200 || res.Header.Get("Content-Encoding") != "" || res.ContentLength > 32<<20 {
		return nil, fmt.Errorf("official archive response refused")
	}
	v, e := io.ReadAll(io.LimitReader(res.Body, (32<<20)+1))
	if len(v) > 32<<20 {
		return nil, fmt.Errorf("archive exceeds download limit")
	}
	return v, e
}

func discoverOfficial(ctx context.Context) (string, error) {
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("discovery redirects refused") }}
	req, e := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/repos/ohmyzsh/ohmyzsh/commits/master", nil)
	if e != nil {
		return "", e
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Accept-Encoding", "identity")
	res, e := client.Do(req)
	if e != nil {
		return "", e
	}
	defer res.Body.Close()
	if res.StatusCode != 200 || res.Header.Get("Content-Encoding") != "" {
		return "", fmt.Errorf("official commit lookup unavailable; use manual commit")
	}
	v, e := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	if e != nil {
		return "", e
	}
	if len(v) > 1<<20 {
		return "", fmt.Errorf("commit metadata exceeds bound")
	}
	return parseCommit(v)
}
func parseCommit(v []byte) (string, error) {
	if e := uniqueJSON(v); e != nil {
		return "", e
	}
	var data struct {
		SHA string `json:"sha"`
	}
	if e := json.Unmarshal(v, &data); e != nil {
		return "", e
	}
	if !validHex(data.SHA, 20) {
		return "", fmt.Errorf("official commit metadata lacks full SHA")
	}
	return data.SHA, nil
}
