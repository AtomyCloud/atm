package tui

import (
	"context"
	"fmt"
	"os"
	"sync"

	tea "charm.land/bubbletea/v2"
	"github.com/AtomyCloud/atm/internal/catalog"
)

type catalogSession struct {
	cancel context.CancelFunc
	done   chan catalogMessage
}

type catalogMessage struct {
	session  *catalogSession
	snapshot catalog.Snapshot
	err      error
}

// A shared lifetime makes Close cancel searches started by any model copy.
type catalogController struct {
	mu     sync.Mutex
	wg     sync.WaitGroup
	closed bool
	ctx    context.Context
	cancel context.CancelFunc
	search func(context.Context, string) (catalog.Snapshot, error)
}

func newCatalogController() *catalogController {
	ctx, cancel := context.WithCancel(context.Background())
	return &catalogController{ctx: ctx, cancel: cancel, search: catalog.New(os.Getenv("GITHUB_TOKEN")).Search}
}

func (c *catalogController) start() (*catalogSession, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, fmt.Errorf("catalog is closed")
	}
	ctx, cancel := context.WithTimeout(c.ctx, catalog.Timeout)
	s := &catalogSession{cancel: cancel, done: make(chan catalogMessage, 1)}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		defer cancel()
		snapshot, err := c.search(ctx, "")
		s.done <- catalogMessage{session: s, snapshot: snapshot, err: err}
	}()
	return s, nil
}

func (c *catalogController) close() {
	c.mu.Lock()
	c.closed = true
	c.cancel()
	c.mu.Unlock()
	c.wg.Wait()
}

func (m Model) startCatalog() (tea.Model, tea.Cmd) {
	if m.demo || m.c.DryRun {
		m.message = "DRY-RUN: plugins search\n" + catalog.Source
		m.navigate("message")
		m.status = m.words.T("ATM_TUI_DRY_RUN")
		return m, nil
	}
	s, err := m.catalogController.start()
	if err != nil {
		m.status = err.Error()
		return m, nil
	}
	m.navigate("catalog-loading")
	m.catalogActive = s
	m.status = m.words.T("ATM_TUI_CATALOG_LOADING")
	return m, func() tea.Msg { return <-s.done }
}

func (m Model) receiveCatalog(msg catalogMessage) (tea.Model, tea.Cmd) {
	if msg.session != m.catalogActive || m.catalogActive == nil {
		return m, nil
	}
	m.catalogActive = nil
	if msg.err != nil {
		m.navigate("core")
		m.status = m.words.T("ATM_TUI_FAILED") + ": " + msg.err.Error()
		return m, nil
	}
	msg.snapshot.MarkLocal(m.plugins)
	m.catalogSnapshot = msg.snapshot
	m.navigate("catalog")
	m.status = "Commit: " + msg.snapshot.Commit
	return m, nil
}
