// Package setup provides opt-in user-only setup without executing payloads or shells.
package setup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/AtomyCloud/atm/internal/config"
	"github.com/AtomyCloud/atm/internal/plugin"
)

var (
	ErrConsent     = errors.New("setup requires explicit consent")
	ErrExists      = errors.New("existing installation preserved; use the separate updater")
	ErrUnavailable = errors.New("integration unavailable")
)

type Options struct {
	UserHome string
	AppsDir  string
	Source   string // Extracted distribution, never a source checkout.
	// Pin must come from an independently verified distribution or an explicitly
	// approved local build. Hashing Source alone is not authentication. This
	// package does not define a production release trust root.
	TrustedManifestSHA256 string
	DryRun                bool
	Consent               bool
	Shell                 string             // bash, zsh, fish; empty selects bash on Linux, zsh on macOS.
	LinkCLI               bool               // Optional ~/.local/bin/atm link on Unix; never replaces a link.
	checkpoint            func(string) error // Failure injection, tests only.
}

func (o Options) boundary(name string) error {
	if o.checkpoint != nil {
		return o.checkpoint(name)
	}
	return nil
}

type Status struct {
	Destination string `json:"destination"`
	Executable  string `json:"executable"`
	Installed   bool   `json:"installed"`
	Verified    bool   `json:"verified"` // CORE integrity, not plugin integrity or release authenticity.
	Desktop     string `json:"desktop"`
}

type Result struct {
	Status
	DryRun  bool     `json:"dry_run"`
	Changed []string `json:"changed,omitempty"`
	Planned []string `json:"planned,omitempty"`
	Backups []string `json:"backups,omitempty"`
}

// DefaultOptions never uses c.Home as destination or treats AssumeYes as consent.
func DefaultOptions(c config.Config, userHome string) Options {
	apps := c.AppsDir
	if apps == "" {
		apps = filepath.Join(userHome, "Apps")
	}
	o := Options{UserHome: userHome, AppsDir: apps, Source: c.Home, DryRun: c.DryRun}
	if runtime.GOOS != "windows" {
		switch shell := filepath.Base(os.Getenv("SHELL")); shell {
		case "bash", "zsh", "fish", "sh":
			o.Shell = shell
		}
	}
	return o
}

func (o Options) paths() (string, string, error) {
	if !filepath.IsAbs(o.UserHome) || filepath.Clean(o.UserHome) == filepath.VolumeName(o.UserHome)+string(filepath.Separator) {
		return "", "", fmt.Errorf("an absolute user home is required")
	}
	apps := o.AppsDir
	if apps == "" {
		apps = filepath.Join(o.UserHome, "Apps")
	}
	dest := filepath.Join(apps, "atm")
	rel, err := filepath.Rel(o.UserHome, dest)
	if err != nil || !filepath.IsLocal(rel) || rel == "." || !filepath.IsAbs(apps) {
		return "", "", fmt.Errorf("setup destination must be inside the selected user home")
	}
	if strings.ContainsAny(dest, "\x00\r\n") || strings.ContainsRune(dest, os.PathListSeparator) {
		return "", "", fmt.Errorf("destination cannot be represented safely in PATH")
	}
	for _, r := range dest {
		if r < 32 || r == 127 {
			return "", "", fmt.Errorf("control character in destination")
		}
	}
	return dest, rel, nil
}

func executableName() string {
	if runtime.GOOS == "windows" {
		return "atm.exe"
	}
	return "atm"
}

func baseStatus(dest string) Status {
	return Status{Destination: dest, Executable: filepath.Join(dest, executableName()), Desktop: "unavailable: plugin contracts do not declare desktop integration"}
}

// DesktopSupport never infers integration from names or legacy functions.
func DesktopSupport(p plugin.Plugin) error {
	return fmt.Errorf("%w: plugin %s has no supported desktop contract", ErrUnavailable, p.ID)
}

func openHome(o Options) (*os.Root, error) {
	info, err := os.Lstat(o.UserHome)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("user home must be a real directory")
	}
	return os.OpenRoot(o.UserHome)
}

// Mutations also use os.Root to confine racing path resolution.
func checkPath(root *os.Root, name string) error {
	if !filepath.IsLocal(name) {
		return fmt.Errorf("unsafe relative path")
	}
	parts := strings.Split(filepath.Clean(name), string(filepath.Separator))
	for i := range parts {
		p := filepath.Join(parts[:i+1]...)
		info, err := root.Lstat(p)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || (i < len(parts)-1 && !info.IsDir()) {
			return fmt.Errorf("unsafe linked path: %s", p)
		}
	}
	return nil
}

func lock(root *os.Root) (func(), error) {
	const name = ".atm-setup-lock"
	if err := root.Mkdir(name, 0700); err != nil {
		return nil, fmt.Errorf("setup busy or interrupted; inspect %s before retrying: %w", name, err)
	}
	return func() { _ = root.Remove(name) }, nil
}

// Inspect is read-only. Verified covers the pinned manifest and non-plugin
// distribution files; plugin updates are independently owned and verified.
func Inspect(ctx context.Context, o Options) (Status, error) {
	dest, rel, err := o.paths()
	s := baseStatus(dest)
	if err != nil {
		return s, err
	}
	if err = ctx.Err(); err != nil {
		return s, err
	}
	home, err := openHome(o)
	if err != nil {
		return s, err
	}
	defer home.Close()
	if err = checkPath(home, rel); err != nil {
		return s, err
	}
	if _, err = home.Lstat(rel); os.IsNotExist(err) {
		return s, nil
	} else if err != nil {
		return s, err
	}
	s.Installed = true
	_, err = verifyInstalled(ctx, dest, o.TrustedManifestSHA256)
	s.Verified = err == nil
	return s, err
}
