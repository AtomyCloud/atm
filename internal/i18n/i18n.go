package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/AtomyCloud/atm/internal/config"
	"github.com/AtomyCloud/atm/internal/data"
)

//go:embed en-us.json
var resources embed.FS

type Catalog map[string]string

func Load(home, locale string) (Catalog, error) {
	if !config.LocalePattern.MatchString(locale) {
		return nil, fmt.Errorf("invalid locale")
	}
	base, _ := resources.ReadFile("en-us.json")
	c := Catalog{}
	if err := json.Unmarshal(base, &c); err != nil {
		return nil, err
	}
	for _, name := range []string{"en-us", locale} {
		path := filepath.Join(home, "lang", name+".lang")
		if _, err := os.Stat(path); err != nil {
			return nil, err
		}
		values, err := data.Assignments(path)
		if err != nil {
			return nil, err
		}
		for k, v := range values {
			c[k] = v
		}
	}
	return c, nil
}

func (c Catalog) T(key string) string {
	if value, ok := c[key]; ok {
		return value
	}
	return key
}

func Locales(home string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(home, "lang"))
	if err != nil {
		return nil, err
	}
	var locales []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".lang") {
			name := strings.TrimSuffix(e.Name(), ".lang")
			if config.LocalePattern.MatchString(name) {
				locales = append(locales, name)
			}
		}
	}
	sort.Strings(locales)
	return locales, nil
}
