package setup

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const blockBegin = "# >>> ATM native command >>>"
const blockEnd = "# <<< ATM native command <<<"

func (o Options) shell() string {
	if o.Shell != "" {
		return o.Shell
	}
	if runtime.GOOS == "darwin" {
		return "zsh"
	}
	if runtime.GOOS == "windows" {
		return "windows"
	}
	return "bash"
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func fishQuote(s string) string {
	return "'" + strings.NewReplacer("\\", "\\\\", "'", "\\'").Replace(s) + "'"
}

// PathBlock renders ATM's compiled command directory only. It does not claim
// plugin PATH/CLI/desktop integration, and never reads or writes user files.
// On Windows it returns the entry for the per-user registry PATH, not a script.
func PathBlock(o Options) (string, error) {
	dest, _, err := o.paths()
	if err != nil {
		return "", err
	}
	var body string
	switch o.shell() {
	case "bash", "zsh", "sh":
		q := shellQuote(dest)
		body = "case \":${PATH-}:\" in\n  *:" + q + ":*) ;;\n  *) export PATH=" + q + "${PATH:+\":$PATH\"} ;;\nesac\n"
	case "fish":
		q := fishQuote(dest)
		body = "if not contains -- " + q + " $PATH\n    set -gx PATH " + q + " $PATH\nend\n"
	case "windows":
		return dest, nil
	default:
		return "", fmt.Errorf("%w: shell %s", ErrUnavailable, o.shell())
	}
	return blockBegin + "\n" + body + blockEnd + "\n", nil
}

type profileEdit struct {
	name          string
	before, after []byte
	mode          fs.FileMode
	existed       bool
}

func profileNames(home *os.Root, shell string) ([]string, error) {
	switch shell {
	case "bash":
		login := ".profile"
		for _, name := range []string{".bash_profile", ".bash_login"} {
			if _, err := home.Lstat(name); err == nil {
				login = name
				break
			} else if !os.IsNotExist(err) {
				return nil, err
			}
		}
		return []string{login, ".bashrc"}, nil
	case "sh":
		return []string{".profile"}, nil
	case "zsh":
		return []string{".zprofile", ".zshrc"}, nil
	case "fish":
		return []string{filepath.Join(".config", "fish", "conf.d", "atm.fish")}, nil
	default:
		return nil, fmt.Errorf("%w: shell %s", ErrUnavailable, shell)
	}
}

func prepareProfile(ctx context.Context, home *os.Root, name, block string) (profileEdit, error) {
	e := profileEdit{name: name, mode: 0600}
	if err := checkPath(home, name); err != nil {
		return e, err
	}
	info, err := home.Lstat(name)
	if err != nil && !os.IsNotExist(err) {
		return e, err
	}
	if err == nil {
		e.existed, e.mode = true, info.Mode().Perm()
		if !info.Mode().IsRegular() || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
			return e, fmt.Errorf("unsafe profile: %s", name)
		}
		e.before, err = readRegular(ctx, home, name, 1<<20)
		if err != nil {
			return e, err
		}
	}
	content := string(e.before)
	newline := "\n"
	if strings.Contains(content, "\r\n") {
		newline = "\r\n"
		block = strings.ReplaceAll(block, "\n", newline)
	}
	// Never replace unknown or locally edited blocks, including legacy blocks.
	if strings.Contains(content, "# >>> ATM") || strings.Contains(content, "# <<< ATM") {
		if strings.Count(content, blockBegin) == 1 && strings.Count(content, blockEnd) == 1 && strings.Count(content, "# >>> ATM") == 1 && strings.Count(content, "# <<< ATM") == 1 && strings.Contains(content, block) {
			e.after = e.before
			return e, nil
		}
		return e, fmt.Errorf("existing or edited ATM block preserved in %s; review it before integration", name)
	}
	if len(content) > 0 && !strings.HasSuffix(content, "\n") {
		content += newline
	}
	e.after = []byte(content + block)
	return e, nil
}

func writeNew(root *os.Root, name string, b []byte, mode fs.FileMode) error {
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if err == nil {
		err = f.Chmod(mode)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func profileUnchanged(ctx context.Context, home *os.Root, e profileEdit) error {
	if err := checkPath(home, e.name); err != nil {
		return err
	}
	info, err := home.Lstat(e.name)
	if !e.existed && os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !e.existed || !info.Mode().IsRegular() || info.Mode().Perm() != e.mode {
		return fmt.Errorf("profile changed during integration: %s", e.name)
	}
	b, err := readRegular(ctx, home, e.name, 1<<20)
	if err != nil {
		return err
	}
	if !bytes.Equal(b, e.before) {
		return fmt.Errorf("profile changed during integration: %s", e.name)
	}
	return nil
}

// Integrate validates the installed CORE, excluding independently mutable plugin
// contents, before changing user PATH. It does not validate or integrate plugins.
// Result retains completed changes/backups on error; edits are atomic per file,
// not an all-profile transaction. Interrupted locks require explicit inspection.
func Integrate(ctx context.Context, o Options) (Result, error) {
	dest, rel, err := o.paths()
	result := Result{Status: baseStatus(dest), DryRun: o.DryRun}
	if err != nil {
		return result, err
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if !o.DryRun && !o.Consent {
		return result, ErrConsent
	}
	home, err := openHome(o)
	if err != nil {
		return result, err
	}
	defer home.Close()
	if err = checkPath(home, rel); err != nil {
		return result, err
	}
	if _, err = verifyInstalled(ctx, dest, o.TrustedManifestSHA256); err != nil {
		return result, err
	}
	result.Installed, result.Verified = true, true
	if runtime.GOOS == "windows" {
		return integrateWindows(ctx, home, o, result)
	}
	block, err := PathBlock(o)
	if err != nil {
		return result, err
	}
	names, err := profileNames(home, o.shell())
	if err != nil {
		return result, err
	}
	var edits []profileEdit
	for _, name := range names {
		e, err := prepareProfile(ctx, home, name, block)
		if err != nil {
			return result, err
		}
		if !bytes.Equal(e.before, e.after) {
			edits = append(edits, e)
			result.Planned = append(result.Planned, filepath.Join(o.UserHome, name))
		}
	}
	const linkName = ".local/bin/atm"
	linkNeeded := false
	if o.LinkCLI {
		if err = checkPath(home, filepath.Dir(linkName)); err != nil {
			return result, err
		}
		if _, err = home.Lstat(linkName); os.IsNotExist(err) {
			linkNeeded = true
			result.Planned = append(result.Planned, filepath.Join(o.UserHome, linkName))
		} else if err != nil {
			return result, err
		} else {
			target, linkErr := home.Readlink(linkName)
			if linkErr != nil || target != result.Executable {
				return result, fmt.Errorf("unmanaged CLI link or file preserved: %s", linkName)
			}
		}
	}
	if o.DryRun || len(result.Planned) == 0 {
		return result, nil
	}
	unlock, err := lock(home)
	if err != nil {
		return result, err
	}
	defer unlock()
	// Preflight every profile before writing any of them.
	for _, e := range edits {
		if err = profileUnchanged(ctx, home, e); err != nil {
			return result, err
		}
	}
	backupDir := ""
	for _, e := range edits {
		if err = ctx.Err(); err != nil {
			return result, err
		}
		if e.existed {
			if backupDir == "" {
				backupDir, err = privateDir(home, ".atm-setup-backup-")
				if err != nil {
					return result, err
				}
			}
			backup := filepath.Join(backupDir, filepath.Base(e.name))
			if err = writeNew(home, backup, e.before, 0600); err != nil {
				return result, err
			}
			result.Backups = append(result.Backups, filepath.Join(o.UserHome, backup))
		}
		if err = home.MkdirAll(filepath.Dir(e.name), 0700); err != nil {
			return result, err
		}
		temp := filepath.Join(filepath.Dir(e.name), ".atm-profile-"+fmt.Sprintf("%x", randomBytes()))
		if err = writeNew(home, temp, e.after, e.mode); err != nil {
			_ = home.Remove(temp)
			return result, err
		}
		if err = o.boundary("before-profile-write"); err != nil {
			_ = home.Remove(temp)
			return result, err
		}
		if err = profileUnchanged(ctx, home, e); err == nil {
			err = ctx.Err()
		}
		if err == nil {
			if e.existed {
				err = home.Rename(temp, e.name)
			} else {
				err = home.Link(temp, e.name)
			}
		}
		_ = home.Remove(temp)
		if err != nil {
			return result, err
		}
		result.Changed = append(result.Changed, filepath.Join(o.UserHome, e.name))
	}
	if linkNeeded {
		if err = ctx.Err(); err != nil {
			return result, err
		}
		if err = checkPath(home, filepath.Dir(linkName)); err != nil {
			return result, err
		}
		if err = home.MkdirAll(filepath.Dir(linkName), 0700); err != nil {
			return result, err
		}
		if err = home.Symlink(result.Executable, linkName); err != nil {
			return result, err
		}
		result.Changed = append(result.Changed, filepath.Join(o.UserHome, linkName))
	}
	return result, nil
}
