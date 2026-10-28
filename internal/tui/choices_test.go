package tui

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/AtomyCloud/atm/internal/plugin"
	"github.com/AtomyCloud/atm/internal/protocol"
	"github.com/AtomyCloud/atm/internal/runner"
)

func choiceModel(t *testing.T) Model {
	t.Helper()
	m := model(t)
	dir := filepath.Join(t.TempDir(), "choice_fixture")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	name := "backend"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte("not executed"), 0700); err != nil {
		t.Fatal(err)
	}
	m.plugins = []plugin.Plugin{{Manifest: plugin.Manifest{Schema: 2, MinAPI: 2, ID: "choice_fixture", Name: "Choice fixture", Version: "0.0.1", Platforms: []string{runtime.GOOS}, Executor: "native", Protocol: "jsonl-v1", Operations: []string{"menu", "install"}, Executables: map[string]string{runtime.GOOS + "/" + runtime.GOARCH: name}}, Directory: dir, Available: true, Enabled: true}}
	m.selected = "choice_fixture"
	m.plan = runner.Plan{Executor: "native", PluginID: m.selected, Operation: "menu"}
	s := &nativeSession{events: make(chan protocol.Event, 64)}
	m.active = s
	o := runner.Outcome{Status: "success", Message: "Version selection", Choices: []protocol.Choice{{ID: "custom", Label: "Custom version", Operation: "install", Arguments: []string{"--version"}, Prompt: "Version"}, {ID: "major", Label: "Major", Operation: "menu", Arguments: []string{"--major", "24"}}}}
	updated, cmd := m.receiveNative(nativeMessage{session: s, outcome: &o})
	if cmd != nil {
		t.Fatal("terminal result started another process")
	}
	return updated.(Model)
}

func TestNativeChoicePromptAndDryRunConfirmation(t *testing.T) {
	m := choiceModel(t)
	if m.screen != "native-menu" || len(m.rows()) != 3 {
		t.Fatal("missing choices")
	}
	u, _ := m.activate()
	m = u.(Model)
	if m.screen != "native-input" || !m.editing {
		t.Fatal("missing input")
	}
	u, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = u.(Model)
	if m.screen != "native-input" || !m.editing {
		t.Fatal("empty input was submitted")
	}
	m.input.SetValue("26.2.0")
	u, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = u.(Model)
	if cmd != nil || m.screen != "confirm" || m.cursor != 0 || len(m.plan.Arguments) != 2 || m.plan.Arguments[1] != "26.2.0" {
		t.Fatalf("unsafe plan: %+v", m.plan)
	}
	m.cursor = 1
	u, cmd = m.activate()
	m = u.(Model)
	if cmd != nil || m.active != nil || m.screen != "message" {
		t.Fatal("dry-run executed choices")
	}
}

func TestNativeChoiceMenuArgumentsAndCancel(t *testing.T) {
	m := choiceModel(t)
	m.cursor = 1
	u, cmd := m.activate()
	m = u.(Model)
	if cmd != nil || m.plan.Operation != "menu" || len(m.plan.Arguments) != 2 || m.screen != "confirm" {
		t.Fatal("submenu planning failed")
	}
	u, cmd = m.activate()
	if cmd != nil || u.(Model).active != nil {
		t.Fatal("default cancel started plugin")
	}
	m = choiceModel(t)
	u, _ = m.activate()
	m = u.(Model)
	u, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if u.(Model).screen != "native-menu" || u.(Model).editing {
		t.Fatal("input cancellation lost choices")
	}
}

func TestNativeChoicesNeverDispatchUndeclaredOperation(t *testing.T) {
	m := choiceModel(t)
	m.nativeChoices = []protocol.Choice{{ID: "bad", Label: "Bad", Operation: "uninstall"}}
	u, cmd := m.activate()
	if cmd != nil || u.(Model).screen == "confirm" || u.(Model).active != nil {
		t.Fatal("undeclared operation dispatched")
	}
}
