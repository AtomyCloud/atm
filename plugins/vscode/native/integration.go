package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const integrationReceipt = ".atm-integration.json"

type integrationState struct {
	Schema  int               `json:"schema"`
	Path    bool              `json:"path"`
	CLI     string            `json:"cli,omitempty"`
	Desktop map[string]string `json:"desktop,omitempty"`
}

func desktopScalar(s string) string {
	return strings.NewReplacer("\\", "\\\\", "\n", "\\n", "\r", "\\r", "\t", "\\t").Replace(s)
}
func desktopExec(s string) (string, error) {
	if strings.ContainsAny(s, "\x00\n\r=") {
		return "", fmt.Errorf("desktop executable path contains unsupported characters")
	}
	quoted := strings.NewReplacer("%", "%%", "\\", "\\\\", "\"", "\\\"", "`", "\\`", "$", "\\$").Replace(s)
	return "\"" + desktopScalar(quoted) + "\"", nil
}
func (b *backend) desktopContent(name string) ([]byte, error) {
	command, err := desktopExec(filepath.Join(b.root, "current", "code"))
	if err != nil {
		return nil, err
	}
	suffix := " %F"
	mime := ""
	display := "Visual Studio Code"
	if name == "code-url-handler.desktop" {
		suffix = " --open-url %U"
		mime = "MimeType=x-scheme-handler/vscode;\nNoDisplay=true\n"
		display += " - URL Handler"
	}
	text := "[Desktop Entry]\nType=Application\nName=" + display + "\nComment=Code Editing. Redefined.\nExec=" + command + suffix + "\nIcon=" + desktopScalar(filepath.Join(b.root, "current", "resources/app/resources/linux/code.png")) + "\nTerminal=false\nCategories=Development;IDE;\nStartupWMClass=Code\n" + mime
	return []byte(text), nil
}
func (b *backend) readIntegration() (integrationState, profilePlan, error) {
	p := profilePlan{path: filepath.Join(b.root, integrationReceipt)}
	var err error
	p.before, p.mode, p.exists, err = readProfile(p.path)
	if err != nil {
		return integrationState{}, p, err
	}
	s := integrationState{Schema: 1, Desktop: map[string]string{}}
	if !p.exists {
		return s, p, nil
	}
	dec := json.NewDecoder(bytes.NewReader(p.before))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&s); err != nil {
		return s, p, err
	}
	if dec.Decode(new(any)) != io.EOF || s.Schema != 1 || (s.CLI != "" && s.CLI != filepath.Join(b.root, "current/bin/code")) {
		return s, p, fmt.Errorf("invalid integration receipt")
	}
	for name, hash := range s.Desktop {
		if (name != "code.desktop" && name != "code-url-handler.desktop") || !hashRE.MatchString(hash) {
			return s, p, fmt.Errorf("invalid desktop receipt")
		}
	}
	if s.Desktop == nil {
		s.Desktop = map[string]string{}
	}
	return s, p, nil
}
func (b *backend) cliPath() string { return filepath.Join(b.home, ".local/bin/code") }
func readLink(name string) (string, bool, error) {
	i, err := os.Lstat(name)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if i.Mode()&os.ModeSymlink == 0 {
		return "", true, fmt.Errorf("CLI path is not a symlink; preserving %s", name)
	}
	target, err := os.Readlink(name)
	return target, true, err
}
func (b *backend) validateIntegration(s integrationState, repair bool) error {
	if s.CLI != "" {
		if err := realDirs(filepath.Dir(b.cliPath()), false); err != nil && !(repair && os.IsNotExist(err)) {
			return err
		}
		target, exists, err := readLink(b.cliPath())
		if err != nil {
			return err
		}
		if (exists && target != s.CLI) || (!exists && !repair) {
			return fmt.Errorf("owned CLI link changed/missing; preserving it")
		}
	}
	for name, hash := range s.Desktop {
		file := filepath.Join(b.home, ".local/share/applications", name)
		if err := realDirs(filepath.Dir(file), false); err != nil && !(repair && os.IsNotExist(err)) {
			return err
		}
		data, mode, exists, err := readProfile(file)
		if err != nil {
			return err
		}
		h := sha256.Sum256(data)
		if (exists && (mode != 0644 || hex.EncodeToString(h[:]) != hash)) || (!exists && !repair) {
			return fmt.Errorf("owned desktop changed/missing; preserving %s", file)
		}
	}
	return nil
}
func (b *backend) selectionIntegrationPlans() ([]profilePlan, error) {
	s, _, err := b.readIntegration()
	if err != nil {
		return nil, err
	}
	return nil, b.validateIntegration(s, false)
}
func (b *backend) integrate(ctx context.Context, args []string) error {
	current, err := b.current()
	if err != nil {
		return err
	}
	if current == "" {
		return fmt.Errorf("no VS Code selected")
	}
	if err = b.validInstalled(current); err != nil {
		return err
	}
	if len(args) == 1 && args[0] == "extensions" {
		return b.installExtensions(ctx, current)
	}
	kind := "all"
	if len(args) > 0 {
		kind = args[0]
	}
	_, err = b.applyIntegration(ctx, false, kind)
	return err
}

// File/profile replacements and the external symlink share a rollback sequence.
// Existing unowned or edited integrations are never adopted or overwritten.
func (b *backend) applyIntegration(ctx context.Context, remove bool, kind string) (func() error, error) {
	s, state, err := b.readIntegration()
	if err != nil {
		return nil, err
	}
	if err = b.validateIntegration(s, true); err != nil {
		return nil, err
	}
	plans := []profilePlan{}
	if (remove && s.Path) || (!remove && (kind == "all" || kind == "path")) {
		if !s.Path {
			for _, name := range []string{".profile", ".bashrc", ".zshrc"} {
				data, _, _, err := readProfile(filepath.Join(b.home, name))
				if err != nil {
					return nil, err
				}
				if bytes.Contains(data, []byte(profileStart)) {
					return nil, fmt.Errorf("unowned native PATH block; preserving profile")
				}
			}
		}
		p, err := b.profilePlans(remove)
		if err != nil {
			return nil, err
		}
		plans = append(plans, p...)
		s.Path = !remove
	}
	if remove || kind == "all" || kind == "desktop" {
		directory := filepath.Join(b.home, ".local/share/applications")
		if err = realDirs(directory, !remove); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		for _, name := range []string{"code.desktop", "code-url-handler.desktop"} {
			owned := s.Desktop[name] != ""
			if remove && !owned {
				continue
			}
			file := filepath.Join(directory, name)
			before, mode, exists, err := readProfile(file)
			if err != nil {
				return nil, err
			}
			if exists && !owned {
				return nil, fmt.Errorf("unowned desktop entry; preserving %s", file)
			}
			var after []byte
			if !remove {
				after, err = b.desktopContent(name)
				if err != nil {
					return nil, err
				}
				h := sha256.Sum256(after)
				s.Desktop[name] = hex.EncodeToString(h[:])
				mode = 0644
			} else {
				delete(s.Desktop, name)
			}
			if exists || !remove {
				plans = append(plans, profilePlan{file, before, after, mode, exists})
			}
		}
	}
	changeCLI := (remove && s.CLI != "") || (!remove && (kind == "all" || kind == "cli"))
	var beforeCLI, afterCLI string
	var existed bool
	if changeCLI {
		if err = realDirs(filepath.Dir(b.cliPath()), !remove); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		beforeCLI, existed, err = readLink(b.cliPath())
		if err != nil {
			return nil, err
		}
		if existed && s.CLI == "" {
			return nil, fmt.Errorf("unowned CLI link; preserving it")
		}
		if !remove {
			afterCLI = filepath.Join(b.root, "current/bin/code")
		}
		s.CLI = afterCLI
	}
	if remove {
		state.after = nil
		if state.exists {
			plans = append(plans, state)
		}
	} else {
		state.after, err = json.Marshal(s)
		if err != nil {
			return nil, err
		}
		plans = append(plans, state)
	}
	rollbackFiles, err := applyProfiles(ctx, plans)
	if err != nil {
		return nil, err
	}
	rollback := func() error { return rollbackFiles() }
	if changeCLI {
		if err = ctx.Err(); err == nil {
			err = replaceLink(b.cliPath(), beforeCLI, existed, afterCLI)
		}
		if err != nil {
			if undo := rollbackFiles(); undo != nil {
				return nil, fmt.Errorf("%v; rollback: %w", err, undo)
			}
			return nil, err
		}
		rollback = func() error {
			if err := replaceLink(b.cliPath(), afterCLI, afterCLI != "", beforeCLI); err != nil {
				return err
			}
			return rollbackFiles()
		}
	}
	if !remove {
		b.log("User integration configured. Refresh this shell: " + b.shellRefresh())
	}
	return rollback, nil
}
func replaceLink(file, before string, exists bool, after string) error {
	now, actual, err := readLink(file)
	if err != nil {
		return err
	}
	if now != before || actual != exists {
		return fmt.Errorf("CLI link changed during integration")
	}
	if after == "" {
		if !exists {
			return nil
		}
		return os.Remove(file)
	}
	stage, err := os.MkdirTemp(filepath.Dir(file), ".atm-vscode-link-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	link := filepath.Join(stage, "code")
	if err = os.Symlink(after, link); err != nil {
		return err
	}
	return os.Rename(link, file)
}
