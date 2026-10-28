package tui

import (
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/AtomyCloud/atm/internal/core"
	"github.com/AtomyCloud/atm/internal/coreupdate"
	"github.com/AtomyCloud/atm/internal/updates"
)

type coreTestTransport func(*http.Request) (*http.Response, error)

func (f coreTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func selectCoreRow(t *testing.T, m Model, key string) (Model, tea.Cmd) {
	t.Helper()
	m.navigate("core")
	for i, r := range m.rows() {
		if r.key == key {
			m.cursor = i
			updated, cmd := m.activate()
			return updated.(Model), cmd
		}
	}
	t.Fatalf("missing CORE menu row %q", key)
	return m, nil
}

func TestCoreMenuDryRunDispatchParity(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	var requests atomic.Int32
	http.DefaultTransport = coreTestTransport(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return nil, fmt.Errorf("unexpected dry-run network request")
	})
	for _, tc := range []struct {
		key, input string
		args       []string
		mutating   bool
	}{
		{"check", "", []string{"updates", "check"}, false},
		{"update-all", "", []string{"updates", "all"}, true},
		{"full", "", []string{"install"}, true},
		{"path", "", []string{"path", "apply"}, true},
		{"integration", "", []string{"path", "apply", "--plugins"}, true},
		{"update-one", "node", []string{"updates", "plugin", "node"}, true},
		{"changes", "", []string{"updates", "changes"}, false},
		{"backups", "", []string{"updates", "backups"}, false},
		{"restore", "backup-123", []string{"updates", "restore", "backup-123"}, true},
		{"recover", "", []string{"updates", "recover", "plugins"}, true},
		{"setup", strings.Repeat("a", 64), []string{"setup", "portable", "--manifest-sha256", strings.Repeat("a", 64)}, true},
		{"core-update", "", []string{"updates", "core"}, true},
		{"trust", "", []string{"updates", "trust"}, false},
	} {
		t.Run(tc.key, func(t *testing.T) {
			m, cmd := selectCoreRow(t, model(t), tc.key)
			if tc.input != "" {
				if m.screen != "core-input" || !m.editing {
					t.Fatal("missing CORE input", m.screen)
				}
				m.input.SetValue(tc.input)
				updated, next := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
				m, cmd = updated.(Model), next
			}
			if cmd != nil || m.catalogActive != nil || m.screen != "confirm" || m.cursor != 0 {
				t.Fatal("dry-run resolved or skipped default-Cancel confirmation", m.screen)
			}
			if m.plan.Executor != "native-core" || !m.corePlan.DryRun || m.corePlan.Mutating != tc.mutating || !reflect.DeepEqual(m.corePlan.Arguments, tc.args) {
				t.Fatalf("incorrect CORE dispatch: %+v", m.corePlan)
			}
			cancelled, cmd := m.activate()
			if cmd != nil || cancelled.(Model).screen != "plugins" || cancelled.(Model).status != m.words.T("ATM_TUI_CANCELLED") {
				t.Fatal("default confirmation executed an operation")
			}
			m.cursor = 1
			confirmed, cmd := m.activate()
			if cmd != nil || confirmed.(Model).screen != "message" || !strings.Contains(confirmed.(Model).message, `"dry_run": true`) {
				t.Fatal("confirmed dry-run did not remain a plan")
			}
		})
	}
	if requests.Load() != 0 {
		t.Fatal("dry-run contacted network")
	}
}

func TestCorePreparedConfirmationIsNotReportedRunning(t *testing.T) {
	m := model(t)
	m.status = m.words.T("ATM_TUI_RUNNING")
	prepared := &core.Execution{}
	updated, cmd := m.receiveCore(catalogMessage{core: true, corePrepared: prepared})
	m = updated.(Model)
	if cmd != nil || m.screen != "confirm" || m.corePrepared != prepared || m.status != m.words.T("ATM_TUI_CORE_TRUST") {
		t.Fatal("prepared operation is still reported running", m.screen, m.status)
	}
}

func TestCoreInputValidationKeepsFocusForCorrection(t *testing.T) {
	for _, tc := range []struct{ key, valid string }{
		{"update-one", "node"}, {"restore", "backup-123"}, {"setup", strings.Repeat("a", 64)},
	} {
		t.Run(tc.key, func(t *testing.T) {
			m, _ := selectCoreRow(t, model(t), tc.key)
			m.input.SetValue("/")
			updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			m = updated.(Model)
			if m.screen != "core-input" || !m.editing || !m.input.Focused() || !strings.Contains(m.status, "unsupported native CORE operation") {
				t.Fatal("invalid CORE input cannot be corrected", m.screen, m.status)
			}
			m.input.SetValue("")
			for _, r := range tc.valid {
				updated, _ = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
				m = updated.(Model)
			}
			updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			m = updated.(Model)
			if cmd != nil || m.screen != "confirm" || m.cursor != 0 || m.editing {
				t.Fatal("corrected CORE input did not reach confirmation", m.screen)
			}
		})
	}
}

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
	m.selected = "go"
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
