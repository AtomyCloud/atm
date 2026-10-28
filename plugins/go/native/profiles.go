package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const profileStart = "# >>> ATM native go PATH >>>"
const profileEnd = "# <<< ATM native go PATH <<<"

type profilePlan struct {
	path          string
	before, after []byte
	mode          os.FileMode
	exists        bool
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }

func profileContent(data []byte, block string) ([]byte, error) {
	text := string(data)
	if strings.Count(text, profileStart) > 1 || strings.Count(text, profileEnd) > 1 {
		return nil, fmt.Errorf("duplicate Go PATH blocks; preserve and repair manually")
	}
	start, end := strings.Index(text, profileStart), strings.Index(text, profileEnd)
	if (start < 0) != (end < 0) || end < start {
		return nil, fmt.Errorf("malformed Go PATH block")
	}
	if start >= 0 {
		end += len(profileEnd)
		if (start > 0 && text[start-1] != '\n') || (end < len(text) && text[end] != '\n') {
			return nil, fmt.Errorf("Go PATH marker is not a complete line")
		}
		if end < len(text) {
			end++
		}
		text = text[:start] + text[end:]
	}
	if block != "" {
		if len(text) > 0 && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		text += block
	}
	return []byte(text), nil
}
func readProfile(name string) ([]byte, os.FileMode, bool, error) {
	info, err := os.Lstat(name)
	if os.IsNotExist(err) {
		return nil, 0600, false, nil
	}
	if err != nil {
		return nil, 0, false, err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return nil, 0, false, fmt.Errorf("profile is a symlink, special file or exceeds 1 MiB: %s", name)
	}
	f, err := os.Open(name)
	if err != nil {
		return nil, 0, false, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if len(data) > 1<<20 {
		return nil, 0, false, fmt.Errorf("profile grew beyond limit")
	}
	return data, info.Mode().Perm(), true, err
}
func (b *backend) profilePlans(remove bool) ([]profilePlan, error) {
	if err := realDirs(b.home, false); err != nil {
		return nil, err
	}
	block := ""
	if !remove {
		block = profileStart + "\n" + b.shellEnvironment("\n") + "\n" + profileEnd + "\n"
	}
	plans := []profilePlan{}
	for _, name := range []string{".profile", ".bashrc", ".zshrc"} {
		file := filepath.Join(b.home, name)
		before, mode, exists, err := readProfile(file)
		if err != nil {
			return nil, err
		}
		if !exists && (name == ".zshrc" || remove) {
			continue
		}
		after, err := profileContent(before, block)
		if err != nil {
			return nil, err
		}
		if bytes.Equal(before, after) {
			continue
		}
		plans = append(plans, profilePlan{file, before, after, mode, exists})
	}
	return plans, nil
}

func (b *backend) shellEnvironment(separator string) string {
	return "export GOROOT=" + shellQuote(filepath.Join(b.root, "current")) + separator +
		"export GOPATH=" + shellQuote(filepath.Join(b.root, "workspace")) + separator +
		"export PATH=" + shellQuote(filepath.Join(b.root, "current/bin")) + ":" + shellQuote(filepath.Join(b.root, "workspace/bin")) + ":\"$PATH\""
}
func (b *backend) shellRefresh() string { return b.shellEnvironment("; ") + "; hash -r" }
func replaceProfile(p profilePlan, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(p.path), ".atm-go-profile-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Chmod(p.mode)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), p.path)
}
func applyProfiles(ctx context.Context, plans []profilePlan) (func() error, error) {
	applied := []profilePlan{}
	rollback := func() error {
		for i := len(applied) - 1; i >= 0; i-- {
			p := applied[i]
			now, _, exists, err := readProfile(p.path)
			if err != nil || !exists || !bytes.Equal(now, p.after) {
				return fmt.Errorf("profile changed during rollback: %s", p.path)
			}
			if p.exists {
				err = replaceProfile(p, p.before)
			} else {
				err = os.Remove(p.path)
			}
			if err != nil {
				return err
			}
		}
		return nil
	}
	fail := func(err error) (func() error, error) {
		if undo := rollback(); undo != nil {
			return nil, fmt.Errorf("%v; rollback: %w", err, undo)
		}
		return nil, err
	}
	for _, p := range plans {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		now, mode, exists, err := readProfile(p.path)
		if err != nil {
			return fail(err)
		}
		if exists != p.exists || mode != p.mode || !bytes.Equal(now, p.before) {
			return fail(fmt.Errorf("profile changed before write: %s", p.path))
		}
		if err = replaceProfile(p, p.after); err != nil {
			return fail(err)
		}
		applied = append(applied, p)
	}
	return rollback, nil
}
