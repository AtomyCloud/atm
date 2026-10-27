package tui

import (
	"context"
	"fmt"
	"sync"

	tea "charm.land/bubbletea/v2"
	"github.com/AtomyCloud/atm/internal/config"
	"github.com/AtomyCloud/atm/internal/protocol"
	"github.com/AtomyCloud/atm/internal/runner"
)

type nativeSession struct {
	events chan protocol.Event
	done   chan runner.Outcome
	cancel context.CancelFunc
}
type nativeMessage struct {
	session *nativeSession
	event   protocol.Event
	outcome *runner.Outcome
}

// Shared by Bubble Tea's model copies; Close also covers terminal/program errors.
type nativeController struct {
	mu     sync.Mutex
	wg     sync.WaitGroup
	closed bool
	active *nativeSession
}

func (c *nativeController) start(cfg config.Config, plan runner.Plan) (*nativeSession, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.active != nil {
		return nil, fmt.Errorf("native executor is closed or busy")
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &nativeSession{events: make(chan protocol.Event, 64), done: make(chan runner.Outcome, 1), cancel: cancel}
	c.active = s
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		defer cancel()
		// UI backpressure never blocks process cleanup; terminal outcomes are separate.
		emit := func(e protocol.Event) error {
			select {
			case s.events <- e:
			default:
			}
			return nil
		}
		outcome := runner.RunNative(ctx, cfg, plan, runner.NativeOptions{Timeout: cfg.NativeTimeout, Event: emit, Diagnostic: func(text string) error { return emit(protocol.Event{Type: "log", Message: text}) }})
		c.mu.Lock()
		c.active = nil
		c.mu.Unlock()
		s.done <- outcome
	}()
	return s, nil
}

func (c *nativeController) close() {
	c.mu.Lock()
	c.closed = true
	if c.active != nil {
		c.active.cancel()
	}
	c.mu.Unlock()
	c.wg.Wait()
}

func (m Model) Close() {
	if m.catalogController != nil {
		m.catalogController.close()
	}
	if m.controller != nil {
		m.controller.close()
	}
}

func waitNative(s *nativeSession) tea.Cmd {
	return func() tea.Msg {
		select {
		case outcome := <-s.done:
			return nativeMessage{session: s, outcome: &outcome}
		default:
		}
		select {
		case outcome := <-s.done:
			return nativeMessage{session: s, outcome: &outcome}
		case event := <-s.events:
			return nativeMessage{session: s, event: event}
		}
	}
}

func (m Model) startNative() (tea.Model, tea.Cmd) {
	s, err := m.controller.start(m.c, m.plan)
	if err != nil {
		m.status = err.Error()
		return m, nil
	}
	m.active = s
	m.nativeLogs = nil
	m.nativeProgress = 0
	m.quitAfterNative = false
	m.navigate("native")
	m.status = m.words.T("ATM_TUI_RUNNING")
	return m, waitNative(s)
}

func (m Model) cancelNative(quit bool) (tea.Model, tea.Cmd) {
	m.quitAfterNative = m.quitAfterNative || quit
	m.active.cancel()
	m.status = m.words.T("ATM_TUI_CANCELLING")
	return m, nil
}

func (m *Model) appendNativeLog(s string) {
	if s == "" {
		return
	}
	m.nativeLogs = append(m.nativeLogs, protocol.SafeText(s))
	if len(m.nativeLogs) > 100 {
		m.nativeLogs = m.nativeLogs[len(m.nativeLogs)-100:]
	}
}

func (m Model) receiveNative(msg nativeMessage) (tea.Model, tea.Cmd) {
	if msg.session != m.active {
		return m, nil
	}
	if msg.outcome == nil {
		m.appendNativeLog(msg.event.Message)
		if msg.event.Progress != nil {
			m.nativeProgress = *msg.event.Progress
		}
		return m, waitNative(m.active)
	}
	// Drain retained events before showing the final, validated process outcome.
	for len(m.active.events) > 0 {
		event := <-m.active.events
		m.appendNativeLog(event.Message)
	}
	o := *msg.outcome
	m.active = nil
	m.status = m.words.T("ATM_TUI_DONE")
	if o.Status == "success" {
		m.nativeProgress = 1
	} else if o.Status == "cancelled" {
		m.status = m.words.T("ATM_TUI_CANCELLED")
	} else {
		m.status = m.words.T("ATM_TUI_FAILED")
	}
	m.appendNativeLog(o.Status + ": " + o.Message)
	m.results = append(m.results, m.planLabel()+": "+m.status)
	if len(m.results) > 100 {
		m.results = m.results[len(m.results)-100:]
	}
	if m.quitAfterNative {
		return m, tea.Quit
	}
	return m, nil
}
