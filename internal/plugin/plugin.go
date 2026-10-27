package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/AtomyCloud/atm/internal/config"
	"github.com/AtomyCloud/atm/internal/data"
)

// Native API; the legacy Bash core-api.json remains at API 1.
const API = 2

var IDPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
var Actions = []string{"menu", "status", "install", "remove", "uninstall", "use"}

type Manifest struct {
	Schema      int               `json:"schema"`
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	Description string            `json:"description"`
	Order       int               `json:"order"`
	MinAPI      int               `json:"min_core_api"`
	Platforms   []string          `json:"platforms"`
	Executor    string            `json:"executor"`
	Protocol    string            `json:"protocol"`
	Operations  []string          `json:"operations"`
	Executables map[string]string `json:"executables,omitempty"`
}

type Plugin struct {
	Manifest
	Directory      string `json:"directory"`
	LegacyMetadata bool   `json:"legacy_metadata"`
	Enabled        bool   `json:"enabled"`
	Available      bool   `json:"available"`
	Reason         string `json:"reason,omitempty"`
}

func (m Manifest) Validate(directory string) error {
	if (m.Schema != 1 && m.Schema != 2) || m.MinAPI != m.Schema {
		return fmt.Errorf("unsupported schema or CORE API for %s", m.ID)
	}
	if !IDPattern.MatchString(m.ID) || m.ID != directory {
		return fmt.Errorf("plugin ID must match its directory: %s", directory)
	}
	if strings.TrimSpace(m.Name) == "" || len(m.Name) > 120 || m.Version == "" {
		return fmt.Errorf("invalid name/version: %s", m.ID)
	}
	for _, value := range []string{m.Name, m.Description, m.Version} {
		for _, r := range value {
			if r < 32 || r == 127 {
				return fmt.Errorf("control character in metadata: %s", m.ID)
			}
		}
	}
	if m.Schema == 2 {
		return m.validateNative()
	}
	if m.Executor != "legacy-bash" || m.Protocol != "tty-v1" || len(m.Executables) != 0 {
		return fmt.Errorf("unsupported executor/protocol: %s", m.ID)
	}
	if len(m.Platforms) != 1 || m.Platforms[0] != "linux" {
		return fmt.Errorf("legacy plugins are Linux-only: %s", m.ID)
	}
	seen := map[string]bool{}
	for _, op := range m.Operations {
		if !slices.Contains(Actions, op) || seen[op] {
			return fmt.Errorf("invalid/duplicate operation %q: %s", op, m.ID)
		}
		seen[op] = true
	}
	return nil
}

func Discover(c config.Config, platform string) ([]Plugin, error) {
	entries, err := os.ReadDir(c.PluginsDir)
	if os.IsNotExist(err) {
		return []Plugin{}, nil
	}
	if err != nil {
		return nil, err
	}
	plugins := []Plugin{}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("symlink plugin directory refused: %s", entry.Name())
		}
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		p := Plugin{Directory: filepath.Join(c.PluginsDir, entry.Name()), Enabled: true}
		path := filepath.Join(p.Directory, "plugin.json")
		info, statErr := os.Lstat(path)
		if statErr == nil {
			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("plugin.json must be a regular file: %s", path)
			}
			if err := data.JSON(path, &p.Manifest); err != nil {
				return nil, err
			}
		} else if os.IsNotExist(statErr) {
			if _, err := os.Lstat(filepath.Join(p.Directory, "plugin.metadata")); os.IsNotExist(err) {
				continue
			}
			p.Manifest, err = readLegacy(p.Directory)
			if err != nil {
				return nil, err
			}
			p.LegacyMetadata = true
		} else {
			return nil, statErr
		}
		if err := p.Manifest.Validate(entry.Name()); err != nil {
			return nil, err
		}
		key := "ATM_PLUGIN_" + strings.ToUpper(p.ID) + "_ENABLED"
		p.Enabled = !c.Disabled[key]
		if value := os.Getenv(key); value != "" {
			p.Enabled = value != "0"
		}
		p.Available = p.Enabled && slices.Contains(p.Platforms, platform)
		switch {
		case !p.Enabled:
			p.Reason = "Plugin disabled"
		case !p.Available:
			p.Reason = "Linux Bash plugin; native implementation pending"
		}
		if p.Executor == "native" && p.Enabled {
			_, err := p.NativeExecutable(platform + "/" + runtime.GOARCH)
			p.Available = err == nil
			if err != nil {
				p.Reason = err.Error()
			} else {
				p.Reason = ""
			}
		}
		plugins = append(plugins, p)
	}
	Sort(plugins, c.Sort)
	return plugins, nil
}

func readLegacy(dir string) (Manifest, error) {
	values, err := data.Assignments(filepath.Join(dir, "plugin.metadata"))
	if err != nil {
		return Manifest{}, err
	}
	order, _ := strconv.Atoi(values["ATM_PLUGIN_ORDER_VALUE"])
	api := 1
	if value := values["ATM_PLUGIN_MIN_CORE_API_VALUE"]; value != "" {
		api, err = strconv.Atoi(value)
		if err != nil {
			return Manifest{}, err
		}
	}
	m := Manifest{Schema: 1, ID: values["ATM_PLUGIN_ID"], Name: values["ATM_PLUGIN_NAME_VALUE"],
		Version: values["ATM_PLUGIN_VERSION_VALUE"], Description: values["ATM_PLUGIN_DESCRIPTION_VALUE"],
		Order: order, MinAPI: api, Platforms: []string{"linux"}, Executor: "legacy-bash", Protocol: "tty-v1"}
	for _, action := range Actions {
		if values["ATM_PLUGIN_"+strings.ToUpper(action)+"_FUNC_VALUE"] != "" {
			m.Operations = append(m.Operations, action)
		}
	}
	return m, nil
}

func Sort(plugins []Plugin, mode string) {
	sort.SliceStable(plugins, func(i, j int) bool {
		a, b := plugins[i], plugins[j]
		if mode == "default" && a.Order != b.Order {
			return a.Order < b.Order
		}
		if mode == "az" || mode == "za" {
			x, y := strings.ToLower(a.Name), strings.ToLower(b.Name)
			if x != y {
				if mode == "za" {
					return x > y
				}
				return x < y
			}
		}
		return a.ID < b.ID
	})
}

func Find(plugins []Plugin, id string) (Plugin, error) {
	for _, p := range plugins {
		if p.ID == id {
			return p, nil
		}
	}
	return Plugin{}, fmt.Errorf("plugin not found: %s", id)
}
