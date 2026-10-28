package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	req                           protocol.Request
	profile, backups, state, arch string
	log                           func(string)
	closed                        func(context.Context, string) error
	manager                       func() (string, error)
	resolve                       func(string) (string, error)
	run                           func(context.Context, string, []string) (string, error)
	client                        *http.Client
	uid                           func() int
	sources                       func() ([]sourceFile, error)
	preferences                   func() ([]sourceFile, error)
	fetch                         func(context.Context, systemPlan) ([]byte, error)
	stageCheck                    func(string) error
	inputCheck                    func(string, []byte) error
	stageValid                    func(string) bool
}

func newBackend(req protocol.Request, log func(string)) (*backend, error) {
	if req.PluginID != "brave" || runtime.GOOS != "linux" || (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64") {
		return nil, fmt.Errorf("Brave requires Linux amd64/arm64")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(req.Home, false)
	if err != nil {
		return nil, err
	}
	b := &backend{req: req, profile: filepath.Join(home, ".config/BraveSoftware/Brave-Browser"), backups: filepath.Join(req.AppsDir, "backups/brave"), state: filepath.Join(cfg.StateDir, "brave/native-operation.json"), arch: runtime.GOARCH, log: log, closed: browserClosed, manager: detectManager, resolve: systemCommand, run: runCommand, uid: os.Geteuid}
	if p := os.Getenv("ATM_BRAVE_PROFILE_DIR"); p != "" {
		b.profile = p
	}
	if p := os.Getenv("ATM_BRAVE_BACKUP_DIR"); p != "" {
		b.backups = p
	}
	for _, p := range []string{home, req.Home, req.AppsDir, b.profile, b.backups, b.state} {
		if err = absolutePath(p); err != nil {
			return nil, err
		}
	}
	// These roots must never be backed up, replaced or moved inside each other.
	for _, p := range []string{b.backups, filepath.Dir(b.state), req.Home, req.AppsDir} {
		if overlaps(b.profile, p) {
			return nil, fmt.Errorf("profile overlaps backup/state/runtime/apps root")
		}
	}
	b.client = &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("signing key redirects refused") }}
	b.sources = func() ([]sourceFile, error) { return aptSources("/etc/apt") }
	b.preferences = func() ([]sourceFile, error) { return aptPreferences("/etc/apt") }
	b.fetch = b.fetchKey
	b.stageCheck = checkNewStage
	b.inputCheck = checkSystemInput
	b.stageValid = validStage
	return b, nil
}

func digest(v []byte) string { h := sha256.Sum256(v); return hex.EncodeToString(h[:]) }
func validDigest(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && s == strings.ToLower(s)
}
func (b *backend) execute(ctx context.Context) ([]protocol.Choice, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if os.Getenv("ATM_DRY_RUN") == "1" {
		b.log("Dry-run: no network, execution or writes")
		return nil, nil
	}
	a := b.req.Arguments
	switch b.req.Operation {
	case "status":
		if len(a) != 0 {
			return nil, fmt.Errorf("status takes no arguments")
		}
		return nil, b.status()
	case "use":
		return nil, fmt.Errorf("use is unsupported: Brave is a system-managed stable-channel package")
	case "install", "remove", "uninstall":
		action := b.req.Operation
		if action == "uninstall" {
			action = "remove"
		}
		o, err := systemOptions(action, a)
		if err != nil {
			return nil, err
		}
		return nil, b.applySystem(ctx, o)
	case "menu":
		if len(a) == 0 {
			if err := b.status(); err != nil {
				return nil, err
			}
			return []protocol.Choice{
				menuChoice("install", "install"), menuChoice("remove", "remove"), menuChoice("commands", "commands"), menuChoice("backups", "backups"), menuChoice("backup-bookmarks", "backup-bookmarks")}, nil
		}
		switch a[0] {
		case "install", "remove", "commands":
			if len(a) != 1 {
				return nil, fmt.Errorf("review takes no flags")
			}
			return b.systemMenu(a[0])
		case "backups":
			if len(a) != 1 {
				return nil, fmt.Errorf("backups takes no arguments")
			}
			return b.backupMenu(ctx)
		case "backup-profile", "backup-bookmarks":
			if len(a) != 1 {
				return nil, fmt.Errorf("backup takes no arguments")
			}
			return nil, b.backup(ctx, a[0] == "backup-bookmarks")
		case "restore":
			if len(a) != 5 || a[2] != "--sha256" || !validDigest(a[3]) || a[4] != "--accept-restore" {
				return nil, fmt.Errorf("restore requires ARCHIVE --sha256 HEX --accept-restore; review backups first")
			}
			return nil, b.restore(ctx, a[1], a[3])
		}
	}
	return nil, fmt.Errorf("unsupported Brave operation or arguments")
}
func menuChoice(id, view string) protocol.Choice {
	return protocol.Choice{ID: id, Label: label(id), Operation: "menu", Arguments: []string{view}}
}
func (b *backend) status() error {
	found := false
	for _, p := range []string{"/usr/bin/brave-browser", "/usr/bin/brave-browser-stable", "/opt/brave.com/brave/brave"} {
		if i, e := os.Lstat(p); e == nil && (i.Mode().IsRegular() || i.Mode()&os.ModeSymlink != 0) {
			b.log("External Brave path detected: " + p + "; version unknown (never executed)")
			found = true
		}
	}
	if !found {
		b.log("No Brave binary detected at standard system paths; other installations unknown")
	}
	b.log("Profile: " + b.profile)
	b.log("Backups: " + b.backups)
	r, _, e := readRecord(b.state)
	if e != nil {
		return e
	}
	if r != nil {
		b.log("Last request: " + r.Action + " / " + r.Status + " / " + r.Phase + ". This is not ownership or installed-state proof.")
	}
	return nil
}
func serve(ctx context.Context, in io.Reader, out io.Writer) error {
	req, err := protocol.ReadRequest(in)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(out)
	emit := func(kind, msg, status string, choices []protocol.Choice) error {
		msg = protocol.SafeText(msg)
		if len(msg) > 4000 {
			msg = string([]rune(msg)[:1000]) + " [truncated]"
		}
		return enc.Encode(protocol.Event{Protocol: 1, RequestID: req.RequestID, Type: kind, Message: msg, Status: status, Choices: choices})
	}
	if err = emit("started", label("title"), "", nil); err != nil {
		return err
	}
	b, err := newBackend(req, func(s string) { _ = emit("log", s, "", nil) })
	var choices []protocol.Choice
	if err == nil {
		choices, err = b.execute(ctx)
	}
	if err == nil {
		err = protocol.ValidateChoices(choices)
	}
	status, msg := "success", label("complete")
	if err != nil {
		status, msg = "error", err.Error()
		choices = nil
		if ctx.Err() != nil {
			status = "cancelled"
		}
	}
	return emit("result", msg, status, choices)
}
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := serve(ctx, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
