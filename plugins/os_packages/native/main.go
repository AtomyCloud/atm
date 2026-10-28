package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"github.com/AtomyCloud/atm/internal/config"
	"github.com/AtomyCloud/atm/internal/data"
	"github.com/AtomyCloud/atm/internal/protocol"
)

type backend struct {
	req                protocol.Request
	packageFile, state string
	system             func() (systemInfo, error)
	resolve            func(string) (string, error)
	uid                func() int
	run                func(context.Context, string, []string) (string, error)
	log                func(string)
	arch               string
}
type options struct {
	action, view, plan      string
	accept, removal, ubuntu bool
}
type preparation struct {
	System                                           systemInfo
	Packages                                         []string
	Source, Action, PackageFile, PackageDigest, Arch string
}

func newBackend(req protocol.Request, log func(string)) (*backend, error) {
	if req.PluginID != "os_packages" || runtime.GOOS != "linux" || (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64") {
		return nil, fmt.Errorf("OS Packages requires Linux amd64/arm64")
	}
	cfg, err := config.Load(req.Home, false)
	if err != nil {
		return nil, err
	}
	file := os.Getenv("ATM_OS_PACKAGES_PACKAGE_FILE")
	if file == "" {
		file = filepath.Join(cfg.PluginsDir, "os_packages/packages.txt")
	}
	b := &backend{req: req, packageFile: file, state: filepath.Join(cfg.StateDir, "os_packages/native-operation.json"), system: hostSystem, resolve: systemCommand, uid: os.Geteuid, run: runCommand, log: log, arch: runtime.GOARCH}
	for _, p := range []string{b.packageFile, b.state} {
		if err = absolutePath(p); err != nil {
			return nil, err
		}
	}
	return b, nil
}
func hostSystem() (systemInfo, error) {
	bytes, err := data.Read("/etc/os-release")
	if os.IsNotExist(err) {
		bytes, err = data.Read("/usr/lib/os-release")
	}
	if err != nil {
		return systemInfo{}, err
	}
	i, e := os.Stat("/etc/arch-release")
	return detectSystem(bytes, e == nil && i.Mode().IsRegular())
}
func parseOptions(op string, args []string) (options, error) {
	o := options{action: op}
	if op == "uninstall" {
		o.action = "remove"
	}
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if op == "menu" && i == 0 && !strings.HasPrefix(a, "--") {
			o.view = a
			continue
		}
		if seen[a] {
			return o, fmt.Errorf("duplicate option")
		}
		seen[a] = true
		switch a {
		case "--accept-system-packages":
			o.accept = true
		case "--accept-removal":
			o.removal = true
		case "--ubuntu-sources":
			o.ubuntu = true
		case "--plan":
			i++
			if i == len(args) {
				return o, fmt.Errorf("missing plan fingerprint")
			}
			o.plan = args[i]
			v, e := hex.DecodeString(o.plan)
			if e != nil || len(v) != 32 {
				return o, fmt.Errorf("invalid plan fingerprint")
			}
		default:
			return o, fmt.Errorf("unknown argument: %s", a)
		}
	}
	switch o.action {
	case "status":
		if len(args) > 0 {
			return o, fmt.Errorf("status takes no arguments")
		}
	case "menu":
		if o.accept || o.removal || o.plan != "" {
			return o, fmt.Errorf("review menus do not accept mutation flags")
		}
		if o.view != "" && o.view != "install" && o.view != "remove" && o.view != "commands" {
			return o, fmt.Errorf("unknown submenu")
		}
		if o.ubuntu && o.view != "install" && o.view != "commands" {
			return o, fmt.Errorf("Ubuntu sources only apply to installation review")
		}
	case "install":
		if o.removal {
			return o, fmt.Errorf("removal acknowledgement is not applicable")
		}
	case "remove":
		if o.ubuntu {
			return o, fmt.Errorf("removal never changes sources or refreshes indexes")
		}
	default:
		return o, fmt.Errorf("unsupported operation")
	}
	return o, nil
}
func (b *backend) prepare(o options) (preparation, error) {
	p := preparation{Action: o.action, PackageFile: b.packageFile, Arch: b.arch}
	var err error
	p.System, err = b.system()
	if err != nil {
		return p, err
	}
	data, err := readRegular(b.packageFile, maxPackageFileBytes)
	if err != nil {
		return p, err
	}
	digest := sha256.Sum256(data)
	p.PackageDigest = hex.EncodeToString(digest[:])
	p.Packages, err = parsePackages(data, p.System.Manager)
	if err != nil {
		return p, err
	}
	if o.ubuntu {
		p.Source, err = ubuntuSources(p.System, b.arch)
	}
	return p, err
}
func fingerprint(p preparation) string {
	v, _ := json.Marshal(p)
	h := sha256.Sum256(v)
	return hex.EncodeToString(h[:])
}
func (b *backend) describe(p preparation) error {
	return b.describePlan(p, true)
}
func (b *backend) describePlan(p preparation, includePackages bool) error {
	b.log("System: " + p.System.Name + "; manager: " + p.System.Manager + "; package file: " + p.PackageFile)
	for i := 0; includePackages && i < len(p.Packages); i += 12 {
		end := i + 12
		if end > len(p.Packages) {
			end = len(p.Packages)
		}
		b.logReview("Packages: ", strings.Join(p.Packages[i:end], ", "))
	}
	if p.Source != "" {
		b.log("Temporary official Ubuntu sources only; existing /etc/apt files stay unchanged.")
		b.log(p.Source)
	}
	source := ""
	if p.Source != "" {
		source = "/temporary/atm-ubuntu.list"
	}
	commands, err := planCommands(p.System, p.Packages, p.Action, source)
	if err != nil {
		return err
	}
	for _, cmd := range commands {
		data, _ := json.Marshal(cmd.Args)
		b.logReview("Command argv: ", string(data))
	}
	b.log("System packages remain system-owned. Installation may upgrade selected packages/dependencies; removal may affect existing tools and dependency graphs. No rollback or automatic system recovery is provided.")
	if p.System.Manager == "pacman" {
		b.log("Pacman uses the current repository database. Perform a normal full system upgrade separately when needed; ATM never runs -Sy or -Syu.")
	}
	return nil
}
func (b *backend) logReview(prefix, text string) {
	// Leave room for labels below safeMessage's limit; never truncate consent data.
	runes := []rune(protocol.SafeText(text))
	const chunkSize = 1600
	for len(runes) > chunkSize {
		b.log(prefix + string(runes[:chunkSize]))
		runes = runes[chunkSize:]
		if !strings.HasSuffix(prefix, "(continued) ") {
			prefix += "(continued) "
		}
	}
	b.log(prefix + string(runes))
}
func (b *backend) status() error {
	s, err := b.system()
	if err != nil {
		return err
	}
	b.log("Detected: " + s.Name + " / " + s.Manager + ". This is not an installed-package inventory.")
	r, _, err := readRecord(b.state)
	if err != nil {
		return err
	}
	if r != nil {
		b.log("Last ATM request: " + r.Action + " / " + r.Status + " / " + r.Phase + " at " + r.Updated + ". A recorded request does not prove current package state.")
	}
	return nil
}
func (b *backend) menu(o options) ([]protocol.Choice, error) {
	if o.view == "" {
		if err := b.status(); err != nil {
			return nil, err
		}
		choices := []protocol.Choice{{ID: "install", Label: label("install"), Operation: "menu", Arguments: []string{"install"}}, {ID: "remove", Label: label("remove"), Operation: "menu", Arguments: []string{"remove"}}, {ID: "commands", Label: label("commands"), Operation: "menu", Arguments: []string{"commands"}}}
		s, e := b.system()
		if e == nil && s.Ubuntu {
			choices = append(choices, protocol.Choice{ID: "ubuntu", Label: label("ubuntu"), Operation: "menu", Arguments: []string{"install", "--ubuntu-sources"}})
		}
		return choices, nil
	}
	action := o.view
	if action == "commands" {
		action = "install"
	}
	o.action = action
	p, err := b.prepare(o)
	if err != nil {
		return nil, err
	}
	if o.view == "commands" {
		// Argv already contains every package; avoid duplicate lists exceeding TUI's queue.
		if err = b.describePlan(p, false); err != nil {
			return nil, err
		}
		p.Action = "remove"
		p.Source = ""
		return nil, b.describePlan(p, false)
	}
	if err = b.describe(p); err != nil {
		return nil, err
	}
	args := []string{"--accept-system-packages", "--plan", fingerprint(p)}
	if o.ubuntu {
		args = append(args, "--ubuntu-sources")
	}
	if action == "remove" {
		args = append(args, "--accept-removal")
	}
	b.log("Review the complete package list/commands. Commands use sudo -n with cached authorization or command-specific NOPASSWD permission. Authenticate separately with sudo -v in a terminal if needed; ATM never reads a password.")
	return []protocol.Choice{{ID: "apply", Label: label("apply_" + action), Operation: action, Arguments: args}}, nil
}
func (b *backend) execute(ctx context.Context) ([]protocol.Choice, error) {
	o, err := parseOptions(b.req.Operation, b.req.Arguments)
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if o.action == "status" {
		return nil, b.status()
	}
	if o.action == "menu" {
		return b.menu(o)
	}
	if !o.accept {
		return nil, fmt.Errorf("system changes require CORE confirmation and --accept-system-packages; review menu install/remove first")
	}
	if o.action == "remove" && !o.removal {
		return nil, fmt.Errorf("removal of existing system packages additionally requires --accept-removal")
	}
	p, err := b.prepare(o)
	if err != nil {
		return nil, err
	}
	if o.plan != "" && o.plan != fingerprint(p) {
		return nil, fmt.Errorf("system/package/source plan changed; review again")
	}
	return nil, b.apply(ctx, p)
}
func (b *backend) apply(ctx context.Context, p preparation) error {
	if err := b.describe(p); err != nil {
		return err
	}
	manager, err := b.resolve(p.System.Manager)
	if err != nil {
		return err
	}
	sudo := ""
	if b.uid() != 0 {
		sudo, err = b.resolve("sudo")
		if err != nil {
			return err
		}
	}
	journal, release, err := openJournal(b.state)
	if err != nil {
		return err
	}
	launched := false
	defer func() {
		// Cancellation cannot establish that privileged descendants have stopped.
		if !launched || ctx.Err() == nil {
			release()
		}
	}()
	fresh, err := b.prepare(options{action: p.Action, ubuntu: p.Source != ""})
	if err != nil {
		return err
	}
	if fingerprint(fresh) != fingerprint(p) {
		return fmt.Errorf("review inputs changed before lock acquisition; review again")
	}
	if p.System.Manager == "apt-get" && p.Action == "install" {
		output, e := b.run(ctx, manager, []string{"--version"})
		if e != nil {
			return e
		}
		if !modernAPT(output) {
			return fmt.Errorf("APT 2.2 or newer is required for strict update failure handling")
		}
	}
	source := ""
	if p.Source != "" {
		dir, e := os.MkdirTemp("", "atm-os-packages-")
		if e != nil {
			return e
		}
		defer func() {
			if !launched || ctx.Err() == nil {
				_ = os.RemoveAll(dir)
			}
		}()
		source = filepath.Join(dir, "ubuntu.list")
		if err = os.WriteFile(source, []byte(p.Source), 0600); err != nil {
			return err
		}
	}
	commands, err := planCommands(p.System, p.Packages, p.Action, source)
	if err != nil {
		return err
	}
	record := operationRecord{Schema: 1, Plugin: "os_packages", Action: p.Action, System: p.System.ID, Manager: p.System.Manager, Packages: p.Packages, Plan: fingerprint(p), Status: "running", Phase: "prepared"}
	if err = journal.save(record); err != nil {
		return err
	}
	for _, cmd := range commands {
		if err = ctx.Err(); err != nil {
			break
		}
		record.Phase = cmd.Label
		if err = journal.save(record); err != nil {
			return err
		}
		b.log("Running: " + cmd.Label)
		if len(cmd.Args) < 2 || cmd.Args[0] != p.System.Manager {
			return fmt.Errorf("invalid package-manager command plan")
		}
		path, args := manager, cmd.Args[1:]
		if sudo != "" {
			path = sudo
			args = append([]string{"-n", "--preserve-env=DEBIAN_FRONTEND", "--", manager}, args...)
		}
		var output string
		if err = ctx.Err(); err != nil {
			break
		}
		launched = true
		output, err = b.run(ctx, path, args)
		if output != "" {
			lines := strings.Split(output, "\n")
			if len(lines) > 30 {
				lines = lines[len(lines)-30:]
				b.log("Earlier package-manager output omitted.")
			}
			for _, line := range lines {
				if line != "" {
					b.log(line)
				}
			}
		}
		if err != nil {
			break
		}
	}
	if err == nil {
		err = ctx.Err()
	}
	if launched && ctx.Err() != nil {
		return fmt.Errorf("package operation interrupted: %w; privileged processes and partial system changes may remain; running journal and lock retained; inspect active processes and package state before manual recovery, do not retry automatically", ctx.Err())
	}
	record.Status = "completed"
	if err != nil {
		record.Status = "failed"
	}
	saveErr := journal.save(record)
	if err != nil {
		if sudo != "" {
			return fmt.Errorf("package operation via sudo -n stopped: %w; sudo policy may deny this command; authenticate separately with sudo -v in a terminal if policy permits; ATM never reads a password; partial system changes may remain; inspect the package manager before retrying (journal error: %v)", err, saveErr)
		}
		return fmt.Errorf("package operation stopped: %w; partial system changes may remain; inspect the package manager before retrying (journal error: %v)", err, saveErr)
	}
	if saveErr != nil {
		return fmt.Errorf("commands completed but journal could not be finalized: %w", saveErr)
	}
	b.log("Requested package-manager commands completed successfully. No ownership of system packages is claimed.")
	return nil
}
func safeMessage(s string) string {
	r := []rune(protocol.SafeText(s))
	if len(r) > 1800 {
		return string(r[:1800]) + " [truncated]"
	}
	return string(r)
}
func modernAPT(output string) bool {
	fields := strings.Fields(output)
	if len(fields) < 2 || fields[0] != "apt" {
		return false
	}
	parts := strings.Split(fields[1], ".")
	if len(parts) < 2 {
		return false
	}
	major, e := strconv.Atoi(parts[0])
	minor, f := strconv.Atoi(parts[1])
	return e == nil && f == nil && (major > 2 || major == 2 && minor >= 2)
}
func serve(ctx context.Context, in io.Reader, out io.Writer) error {
	return serveWith(ctx, in, out, newBackend)
}
func serveWith(ctx context.Context, in io.Reader, out io.Writer, factory func(protocol.Request, func(string)) (*backend, error)) error {
	req, err := protocol.ReadRequest(in)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(out)
	emit := func(kind, message, status string, choices []protocol.Choice) error {
		return enc.Encode(protocol.Event{Protocol: 1, RequestID: req.RequestID, Type: kind, Message: safeMessage(message), Status: status, Choices: choices})
	}
	if err = emit("started", label("title"), "", nil); err != nil {
		return err
	}
	if os.Getenv("ATM_DRY_RUN") == "1" {
		return emit("result", "Dry-run: no system commands or writes", "success", nil)
	}
	b, err := factory(req, func(s string) { _ = emit("log", s, "", nil) })
	var choices []protocol.Choice
	if err == nil {
		choices, err = b.execute(ctx)
	}
	status, message := "success", label("complete")
	if err != nil {
		status = "error"
		message = err.Error()
		choices = nil
		if ctx.Err() != nil {
			status = "cancelled"
		}
	}
	return emit("result", message, status, choices)
}
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := serve(ctx, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
