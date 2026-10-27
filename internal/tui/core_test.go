package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/AtomyCloud/atm/internal/core"
	"github.com/AtomyCloud/atm/internal/coreupdate"
	"github.com/AtomyCloud/atm/internal/updates"
)

func TestCoreDryRunConfirmationIsNative(t *testing.T) {
	m := model(t)
	updated, cmd := m.prepareCore([]string{"updates", "all"})
	m = updated.(Model)
	if cmd != nil || m.screen != "confirm" || m.cursor != 0 || m.plan.Executor != "native-core" {
		t.Fatal(m.screen, m.plan)
	}
	m.cursor = 2
	updated, cmd = m.activate()
	if cmd != nil || updated.(Model).screen != "confirm" {
		t.Fatal("preview row acted as confirmation")
	}
	m.cursor = 1
	updated, cmd = m.activate()
	m = updated.(Model)
	if cmd != nil || m.screen != "message" || !strings.Contains(m.message, "native-core") {
		t.Fatal(m.screen, m.message)
	}
}

func TestCoreTrustMenuIsReadOnly(t *testing.T) {
	m := model(t)
	m.c.DryRun = false
	m.navigate("core")
	index := -1
	for i, item := range m.rows() {
		if item.key == "trust" {
			index = i
		}
	}
	if index < 0 {
		t.Fatal("missing trust menu item")
	}
	m.cursor = index
	updated, cmd := m.activate()
	m = updated.(Model)
	if cmd == nil || m.screen != "core-running" || m.corePlan.Mutating {
		t.Fatal(m.screen, m.corePlan)
	}
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if m.screen != "message" || !strings.Contains(m.message, coreupdate.OfficialKeyFingerprint) {
		t.Fatal(m.screen, m.message)
	}
	m.Close()
}

func TestCoreReplacementLocksSessionUntilExit(t *testing.T) {
	m := model(t)
	r := core.Result{RestartRequired: true, Operation: "updates core"}
	updated, _ := m.receiveCore(catalogMessage{core: true, coreResult: &r})
	m = updated.(Model)
	if !m.restartRequired || m.screen != "message" {
		t.Fatal(m.screen)
	}
	for _, key := range []rune{'p', 'u', 'l'} {
		updated, cmd := m.Update(tea.KeyPressMsg{Code: key})
		if cmd != nil || updated.(Model).screen != "message" {
			t.Fatal("session continued after CORE replacement")
		}
	}
	if _, cmd := m.prepareCore([]string{"install"}); cmd != nil {
		t.Fatal("CORE execution after replacement")
	}
	if _, cmd := m.preparePlugin("install", nil); cmd != nil {
		t.Fatal("plugin execution after replacement")
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q'})
	if cmd == nil {
		t.Fatal("quit unavailable")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("expected quit")
	}
}

func TestCoreUpdateResultsRequireRestart(t *testing.T) {
	m := model(t)
	s := &catalogSession{}
	m.catalogActive = s
	r := core.Result{Value: updates.Report{Rows: []updates.Row{{Component: "plugin:node", Status: "available"}}}}
	updated, _ := m.receiveCatalog(catalogMessage{session: s, core: true, coreResult: &r})
	m = updated.(Model)
	if m.updateStates["node"] != "available" {
		t.Fatal(m.updateStates)
	}
	m.catalogActive = s
	r = core.Result{ChangedPlugins: []string{"node"}}
	updated, _ = m.receiveCatalog(catalogMessage{session: s, core: true, coreResult: &r})
	m = updated.(Model)
	if !m.pendingPlugins["node"] {
		t.Fatal("missing restart guard")
	}
	for _, p := range m.plugins {
		if p.ID == "node" && p.Available {
			t.Fatal("updated plugin immediately executable")
		}
	}
}

func TestCorePendingBlocksNavigationAndCancels(t *testing.T) {
	m := model(t)
	cancelled := false
	m.catalogActive = &catalogSession{publishing: true, cancel: func() { cancelled = true }}
	m.screen = "core-running"
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'p'})
	m = updated.(Model)
	if m.screen != "core-running" {
		t.Fatal("navigated during mutation")
	}
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'q'})
	m = updated.(Model)
	if !cancelled || !m.quitAfterCatalog || cmd != nil {
		t.Fatal("quit did not await cleanup")
	}
}

func TestChangedCodeBlocksAllLegacyEntryPointsUntilRestart(t *testing.T) {
	m := model(t)
	m.pendingPlugins["brave"] = true
	m.selected = "node"
	m.screen = "actions"
	updated, cmd := m.preparePlugin("install", nil)
	if cmd != nil || updated.(Model).screen == "confirm" {
		t.Fatal("unchanged legacy plugin could load changed code")
	}
	updated, cmd = m.prepareCore([]string{"install"})
	if cmd != nil || updated.(Model).screen == "confirm" {
		t.Fatal("Full Setup bypassed restart guard")
	}
}
