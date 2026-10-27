package tui

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	tea "charm.land/bubbletea/v2"
	"github.com/AtomyCloud/atm/internal/config"
	"github.com/AtomyCloud/atm/internal/core"
	"github.com/AtomyCloud/atm/internal/updates"
)

func (m Model) submitCoreInput() (tea.Model, tea.Cmd) {
	v := strings.TrimSpace(m.input.Value())
	switch m.pendingAction {
	case "restore":
		return m.prepareCore([]string{"updates", "restore", v})
	case "update-one":
		return m.prepareCore([]string{"updates", "plugin", v})
	case "setup":
		return m.prepareCore([]string{"setup", "portable", "--manifest-sha256", v})
	}
	return m, nil
}

func (m Model) resolveCore() (tea.Model, tea.Cmd) {
	s, err := m.catalogController.task(true, func(ctx context.Context) catalogMessage {
		e, err := core.Resolve(ctx, m.c, m.corePlan)
		if err != nil {
			return catalogMessage{core: true, err: err}
		}
		if !m.corePlan.Mutating {
			r, err := core.Execute(ctx, m.c, e, false)
			return catalogMessage{core: true, coreResult: &r, err: err}
		}
		return catalogMessage{core: true, corePrepared: e}
	})
	if err != nil {
		m.status = err.Error()
		return m, nil
	}
	m.navigate("core-running")
	m.catalogActive = s
	m.quitAfterCatalog = false
	m.status = m.words.T("ATM_TUI_RUNNING")
	return m, func() tea.Msg { return <-s.done }
}

func (m Model) startCoreExecution() (tea.Model, tea.Cmd) {
	if m.corePrepared == nil {
		m.status = "Missing prepared CORE operation"
		return m, nil
	}
	if m.corePlan.Arguments[0] == "install" {
		for _, p := range m.corePlan.Plugins {
			if p.Executor == "legacy-bash" {
				command := &terminalBatch{c: m.c, plan: m.corePlan, notice: m.words.T("ATM_TUI_CONTINUE")}
				return m, tea.Exec(command, func(err error) tea.Msg { return finishedMsg{err: err} })
			}
		}
	}
	s, err := m.catalogController.task(true, func(ctx context.Context) catalogMessage {
		var r core.Result
		var err error
		if m.corePlan.Arguments[0] == "install" {
			r.Operation = "install"
			r.Value, err = core.FullSetup(ctx, m.c, m.corePlan, nil, io.Discard, io.Discard)
		} else {
			r, err = core.Execute(ctx, m.c, m.corePrepared, true)
		}
		return catalogMessage{core: true, coreResult: &r, err: err}
	})
	if err != nil {
		m.status = err.Error()
		return m, nil
	}
	m.navigate("core-running")
	m.catalogActive = s
	m.quitAfterCatalog = false
	m.status = m.words.T("ATM_TUI_RUNNING")
	return m, func() tea.Msg { return <-s.done }
}

func (m Model) receiveCore(msg catalogMessage) (tea.Model, tea.Cmd) {
	m.catalogActive = nil
	if msg.coreResult != nil {
		if report, ok := msg.coreResult.Value.(updates.Report); ok {
			for _, row := range report.Rows {
				m.updateStates[strings.TrimPrefix(row.Component, "plugin:")] = row.Status
			}
		}
		for _, id := range msg.coreResult.ChangedPlugins {
			m.pendingPlugins[id] = true
			delete(m.updateStates, id)
			for i := range m.plugins {
				if m.plugins[i].ID == id {
					m.plugins[i].Available = false
					m.plugins[i].Reason = m.words.T("ATM_TUI_CATALOG_RESTART")
				}
			}
		}
		b, _ := json.MarshalIndent(msg.coreResult, "", "  ")
		m.message = string(b)
		m.navigate("message")
		m.status = m.words.T("ATM_TUI_DONE")
		if msg.err != nil {
			m.status = m.words.T("ATM_TUI_FAILED") + ": " + msg.err.Error()
		}
		m.results = append(m.results, m.planLabel()+": "+m.status)
		if len(m.results) > 100 {
			m.results = m.results[len(m.results)-100:]
		}
	} else if msg.corePrepared != nil && msg.err == nil {
		m.corePrepared = msg.corePrepared
		m.navigate("confirm")
	} else {
		m.navigate("core")
		if msg.err != nil {
			m.status = m.words.T("ATM_TUI_FAILED") + ": " + msg.err.Error()
		}
	}
	if errors.Is(msg.err, context.Canceled) {
		m.status = m.words.T("ATM_TUI_CANCELLED")
	}
	if m.quitAfterCatalog {
		return m, tea.Quit
	}
	return m, nil
}

// Legacy app prompts retain the real terminal; the loop itself is native Go.
type terminalBatch struct {
	c           config.Config
	plan        core.Plan
	notice      string
	in          io.Reader
	out, stderr io.Writer
}

func (b *terminalBatch) SetStdin(r io.Reader)  { b.in = r }
func (b *terminalBatch) SetStdout(w io.Writer) { b.out = w }
func (b *terminalBatch) SetStderr(w io.Writer) { b.stderr = w }
func (b *terminalBatch) Run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	r, err := core.FullSetup(ctx, b.c, b.plan, b.in, b.out, b.stderr)
	_ = json.NewEncoder(b.out).Encode(r)
	if err != nil {
		fmt.Fprintln(b.stderr, err)
	}
	fmt.Fprintln(b.out, "\n"+b.notice)
	_, _ = bufio.NewReader(b.in).ReadString('\n')
	return err
}
