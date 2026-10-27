package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"time"

	"github.com/AtomyCloud/atm/internal/data"
)

var LocalePattern = regexp.MustCompile("^[a-z]{2}(-[a-z]{2})?$")

type Preferences struct {
	Schema           int    `json:"schema"`
	Language         string `json:"language"`
	Sort             string `json:"sort"`
	AppsDir          string `json:"apps_dir,omitempty"`
	DisableAnimation bool   `json:"disable_animation,omitempty"`
	DisableMouse     bool   `json:"disable_mouse,omitempty"`
}

type Config struct {
	Home          string
	PluginsDir    string
	ConfigDir     string
	AppsDir       string
	Language      string
	Sort          string
	DryRun        bool
	NoMouse       bool
	NoAnimation   bool
	NativeTimeout time.Duration
	AssumeYes     bool
	Preferences   Preferences
	Disabled      map[string]bool
}

func ResolveHome(explicit string) (string, error) {
	root := explicit
	if root == "" {
		root = os.Getenv("ATM_HOME")
	}
	if root != "" {
		return validateHome(root)
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	for _, candidate := range []string{filepath.Dir(exe), filepath.Dir(filepath.Dir(exe))} {
		if root, err := validateHome(candidate); err == nil {
			return root, nil
		}
	}
	return "", fmt.Errorf("ATM home not found beside the executable; pass --home <ATM directory>")
}

func validateHome(root string) (string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(filepath.Join(root, "core-api.json")); err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("not an ATM directory: %s (missing core-api.json)", root)
	}
	return root, nil
}

func Load(home string, dry bool) (Config, error) {
	userHome, err := os.UserHomeDir()
	if err != nil {
		return Config{}, err
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return Config{}, err
	}
	configDir = env("ATM_CONFIG_DIR", filepath.Join(configDir, "atm"))
	c := Config{Home: home, PluginsDir: env("ATM_PLUGIN_DIR", filepath.Join(home, "plugins")), ConfigDir: configDir,
		DryRun: dry || os.Getenv("ATM_DRY_RUN") == "1", Disabled: map[string]bool{},
		Preferences: Preferences{Schema: 1, Language: "en-us", Sort: "default"}}
	// Import data only. Native preferences never rewrite legacy configuration.
	legacy, err := data.Assignments(env("ATM_CONFIG_FILE", filepath.Join(configDir, "atm.conf")))
	if err != nil {
		return c, err
	}
	for k, v := range legacy {
		switch k {
		case "ATM_LANG":
			if v != "" {
				c.Preferences.Language = v
			}
		case "ATM_PLUGIN_SORT_ORDER":
			if v != "" {
				c.Preferences.Sort = v
			}
		case "ATM_APPS_DIR":
			c.Preferences.AppsDir = v
		}
	}
	if err := data.JSON(c.PreferenceFile(), &c.Preferences); err != nil && !os.IsNotExist(err) {
		return c, err
	}
	if err := c.Preferences.Validate(); err != nil {
		return c, err
	}
	plugins, err := data.Assignments(env("ATM_PLUGINS_CONFIG_FILE", filepath.Join(configDir, "plugins.conf")))
	if err != nil {
		return c, err
	}
	for k, v := range plugins {
		c.Disabled[k] = v == "0"
	}
	c.AppsDir = env("ATM_APPS_DIR", c.Preferences.AppsDir)
	if c.AppsDir == "" {
		c.AppsDir = filepath.Join(userHome, "Apps")
	}
	if !filepath.IsAbs(c.AppsDir) {
		return c, fmt.Errorf("ATM apps directory must be absolute")
	}
	c.Language = env("ATM_LANG", c.Preferences.Language)
	c.Sort = env("ATM_PLUGIN_SORT_ORDER", c.Preferences.Sort)
	check := c.Preferences
	check.Language, check.Sort = c.Language, c.Sort
	return c, check.Validate()
}

func (p Preferences) Validate() error {
	if p.Schema != 1 {
		return fmt.Errorf("unsupported preferences schema: %d", p.Schema)
	}
	if !LocalePattern.MatchString(p.Language) {
		return fmt.Errorf("invalid locale: %s", p.Language)
	}
	if p.Sort != "default" && p.Sort != "az" && p.Sort != "za" {
		return fmt.Errorf("invalid sort order: %s", p.Sort)
	}
	return nil
}

func (c Config) PreferenceFile() string { return filepath.Join(c.ConfigDir, "atm-go.json") }

func (c Config) Save(p Preferences) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if c.DryRun {
		return nil
	}
	return data.WriteJSON(c.PreferenceFile(), p)
}

func (c Config) Platform() string { return runtime.GOOS + "/" + runtime.GOARCH }

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
