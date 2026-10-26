package tui

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/AtomyCloud/atm/internal/config"
	"github.com/AtomyCloud/atm/internal/i18n"
	"github.com/AtomyCloud/atm/internal/plugin"
	"github.com/charmbracelet/x/ansi"
)

func model(t *testing.T) Model {
	t.Helper()
	home := filepath.Join("..", "..")
	words, err := i18n.Load(home, "en-us")
	if err != nil {
		t.Fatal(err)
	}
	c := config.Config{Home: home, PluginsDir: filepath.Join(home, "plugins"), DryRun: true, Language: "en-us", Sort: "default", ConfigDir: t.TempDir(), Preferences: config.Preferences{Schema: 1, Language: "en-us", Sort: "default"}}
	plugins, err := plugin.Discover(c, "linux")
	if err != nil {
		t.Fatal(err)
	}
	return New(c, plugins, words, false)
}

func TestLayoutsFitAndHitRegions(t *testing.T) {
	for _, size := range [][2]int{{20, 8}, {32, 12}, {60, 18}, {80, 24}, {140, 40}} {
		for _, screen := range []string{"plugins", "actions", "core", "languages", "sort", "confirm", "logs", "message", "version", "download", "demo"} {
			m := model(t)
			m.width, m.height = size[0], size[1]
			m.screen = screen
			m.selected = "node"
			m.input.SetWidth(max(1, m.width-16))
			text, hits := m.render()
			lines := strings.Split(text, "\n")
			if len(lines) > m.height {
				t.Fatalf("%s %v: %d rows", screen, size, len(lines))
			}
			for _, line := range lines {
				if ansi.StringWidth(line) > m.width {
					t.Fatalf("overflow %s %v: %q", screen, size, line)
				}
			}
			for _, h := range hits {
				if h.x < 0 || h.x+h.width > m.width || h.y < 0 || h.y >= m.height {
					t.Fatal("invalid hit", h)
				}
			}
		}
	}
}

func TestMouseSearchAndScrollAlignment(t *testing.T) {
	m := model(t)
	m.height = 12
	m.cursor = 10
	m.clamp()
	_, hits := m.render()
	var target hit
	for _, h := range hits {
		if h.nav == "" {
			target = h
			break
		}
	}
	want := m.rows()[target.index].key
	updated, _ := m.Update(tea.MouseClickMsg{X: 1, Y: target.y, Button: tea.MouseLeft})
	got := updated.(Model)
	if got.selected != want || got.screen != "actions" {
		t.Fatalf("selected=%s want=%s screen=%s", got.selected, want, got.screen)
	}
	m.navigate("plugins")
	m.input.SetValue("node")
	if len(m.rows()) != 1 || m.rows()[0].key != "node" {
		t.Fatal("search failed")
	}
	updated, _ = m.Update(tea.MouseClickMsg{X: 1, Y: 6, Button: tea.MouseLeft})
	if updated.(Model).selected != "node" {
		t.Fatal("filtered click misaligned")
	}
}

func TestDryRunConfirmationCannotStartProcess(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux plans")
	}
	m := model(t)
	m.selected = "node"
	updated, _ := m.preparePlugin("install", nil)
	m = updated.(Model)
	if m.screen != "confirm" || m.cursor != 0 {
		t.Fatal("default must be Cancel")
	}
	m.cursor = 1
	updated, cmd := m.activate()
	m = updated.(Model)
	if cmd != nil || m.screen != "message" || !strings.Contains(m.message, "\"dry_run\": true") {
		t.Fatal("dry-run executed", m.screen)
	}
}

func TestDemoAndLogoAnimation(t *testing.T) {
	m := model(t)
	m.color = true
	before := strings.Join(m.logo(), "\n")
	m.now = m.started.Add(time.Second)
	if before == strings.Join(m.logo(), "\n") {
		t.Fatal("logo did not animate")
	}
	m.c.Preferences.DisableAnimation = true
	before = strings.Join(m.logo(), "\n")
	m.now = m.now.Add(time.Second)
	if before != strings.Join(m.logo(), "\n") {
		t.Fatal("disabled animation changed")
	}
	m.screen = "demo"
	m.demoStart = time.Now().Add(-3 * time.Second)
	updated, _ := m.Update(tickMsg(time.Now()))
	if updated.(Model).screen != "plugins" {
		t.Fatal("demo did not finish")
	}
}

func TestSmallTerminalAndDisabledMouse(t *testing.T) {
	m := model(t)
	m.width = 10
	m.height = 5
	_, hits := m.render()
	if len(hits) != 0 {
		t.Fatal("small-screen active hit regions")
	}
	m.width = 80
	m.height = 24
	m.c.Preferences.DisableMouse = true
	updated, _ := m.Update(tea.MouseClickMsg{X: 1, Y: 6, Button: tea.MouseLeft})
	if updated.(Model).screen != "plugins" {
		t.Fatal("disabled mouse acted")
	}
}
