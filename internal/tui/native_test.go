package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/AtomyCloud/atm/internal/protocol"
	"github.com/AtomyCloud/atm/internal/runner"
)

func TestNativeEventsCancelAndQuit(t *testing.T) {
	m := model(t)
	defer m.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &nativeSession{cancel: cancel, events: make(chan protocol.Event, 64), done: make(chan runner.Outcome, 1)}
	m.active = s
	m.navigate("native")
	progress := 0.5
	u, cmd := m.Update(nativeMessage{session: s, event: protocol.Event{Type: "progress", Progress: &progress, Message: "Halfway\x1b[2J"}})
	m = u.(Model)
	if cmd == nil || m.nativeProgress != 0.5 || strings.Contains(m.nativeLogs[0], "\x1b") {
		t.Fatal("event not applied safely")
	}
	u, cmd = m.Update(tea.KeyPressMsg{Code: 'q'})
	m = u.(Model)
	if ctx.Err() == nil || !m.quitAfterNative || cmd != nil {
		t.Fatal("quit must wait for cleanup")
	}
	o := runner.Outcome{Status: "cancelled", ExitCode: 130}
	u, cmd = m.Update(nativeMessage{session: s, outcome: &o})
	m = u.(Model)
	if m.active != nil || cmd == nil {
		t.Fatal("cleanup did not release quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("not quit")
	}
}

func TestNativeLogsBoundedAndControllerClosed(t *testing.T) {
	m := model(t)
	for i := 0; i < 1000; i++ {
		m.appendNativeLog("line")
	}
	if len(m.nativeLogs) != 100 {
		t.Fatal("unbounded log history")
	}
	m.Close()
	if _, err := m.controller.start(m.c, runner.Plan{}); err == nil {
		t.Fatal("started after Close")
	}
}

func TestNativeControllerShutdown(t *testing.T) {
	m := model(t)
	s, err := m.controller.start(m.c, runner.Plan{})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { m.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Close blocked")
	}
	select {
	case <-s.done:
	default:
		t.Fatal("Close returned before worker outcome")
	}
}

func TestNativeDryRunNeverStartsWorker(t *testing.T) {
	m := model(t)
	defer m.Close()
	m.plan = runner.Plan{Schema: 2, Executor: "native", Protocol: "jsonl-v1", PluginID: "fixture", Operation: "install", DryRun: true}
	m.navigate("confirm")
	m.cursor = 1
	updated, cmd := m.activate()
	m = updated.(Model)
	if cmd != nil || m.active != nil || m.controller.active != nil || m.screen != "message" {
		t.Fatal("native dry-run started worker")
	}
}
