package i18n

import (
	"path/filepath"
	"testing"
)

func TestEveryRepositoryLocale(t *testing.T) {
	home := filepath.Join("..", "..")
	locales, err := Locales(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(locales) < 10 {
		t.Fatal("missing locales")
	}
	for _, locale := range locales {
		words, err := Load(home, locale)
		if err != nil {
			t.Errorf("%s: %v", locale, err)
			continue
		}
		if words.T("ATM_TUI_CONFIRM") == "ATM_TUI_CONFIRM" {
			t.Fatal("missing English fallback")
		}
		if words.T("ATM_MENU_APP_TITLE") == "ATM_MENU_APP_TITLE" {
			t.Fatal("missing legacy translations")
		}
	}
	if _, err := Load(home, "../invalid"); err == nil {
		t.Fatal("accepted traversal")
	}
}
