package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestNativeOutputPreservesWrappedTextAndReturnsToChoices(t *testing.T) {
	for _, width := range []int{32, 40, 80, 120} {
		m := choiceModel(t)
		m.width, m.height = width, 14
		text := "FIRST " + strings.Repeat("long terms text with spaces ", 40) + strings.Repeat("\u754c e\u0301 ", 20) + "LAST"
		m.nativeLogs = []string{text, "second entry"}
		m.cursor = len(m.nativeChoices)
		u, cmd := m.activate()
		m = u.(Model)
		if cmd != nil || m.screen != "native-output" || m.active != nil {
			t.Fatal("output view started an operation")
		}
		rows := m.rows()
		var reconstructed strings.Builder
		for _, r := range rows[1:] {
			if ansi.StringWidth(r.label) > width-2 {
				t.Fatalf("wide row: %q", r.label)
			}
			reconstructed.WriteString(r.label)
		}
		if reconstructed.String() != text+"second entry" {
			t.Fatal("full output lost or reordered text")
		}
		m.cursor = len(rows) - 1
		m.clamp()
		rendered, _ := m.render()
		if !strings.Contains(rendered, "second entry") {
			t.Fatal("cannot scroll to end")
		}
		u, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		m = u.(Model)
		if cmd != nil || m.screen != "native-menu" || len(m.nativeChoices) != 2 {
			t.Fatal("lost menu after output")
		}
		u, _ = m.activate()
		if u.(Model).screen != "native-input" {
			t.Fatal("choice changed after viewing output")
		}
	}
}

func TestNativeOutputRowDoesNotHijackPluginChoiceID(t *testing.T) {
	m := choiceModel(t)
	m.nativeChoices[0].ID = "native-output"
	u, _ := m.activate()
	if u.(Model).screen != "native-input" {
		t.Fatal("plugin choice ID hijacked")
	}
	m.cursor = len(m.nativeChoices)
	u, cmd := m.activate()
	if cmd != nil || u.(Model).screen != "native-output" {
		t.Fatal("output row not independent")
	}
}

func TestNativeOutputWithoutChoicesAndControlSanitization(t *testing.T) {
	m := model(t)
	m.width, m.height = 40, 14
	m.screen = "native"
	m.appendNativeLog("safe\x1b[2J\ntext\tend")
	m.cursor = 1
	u, cmd := m.activate()
	m = u.(Model)
	if cmd != nil || m.screen != "native-output" {
		t.Fatal("completed output cannot be opened")
	}
	for _, r := range m.rows() {
		if strings.ContainsAny(r.label, "\x1b\n\t") {
			t.Fatal("unsafe controls in output")
		}
	}
	u, cmd = m.activate()
	if cmd != nil || u.(Model).screen != "native" {
		t.Fatal("back lost completed result")
	}
}
