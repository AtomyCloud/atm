package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/AtomyCloud/atm/internal/catalog"
	"github.com/AtomyCloud/atm/internal/core"
)

type catalogSession struct {
	cancel     context.CancelFunc
	done       chan catalogMessage
	publishing bool
}

type catalogMessage struct {
	session      *catalogSession
	snapshot     catalog.Snapshot
	err          error
	plan         *catalog.DownloadPlan
	result       *catalog.DownloadResult
	core         bool
	corePrepared *core.Execution
	coreResult   *core.Result
}

// A shared lifetime makes Close cancel searches started by any model copy.
type catalogController struct {
	mu       sync.Mutex
	wg       sync.WaitGroup
	closed   bool
	ctx      context.Context
	cancel   context.CancelFunc
	search   func(context.Context, string) (catalog.Snapshot, error)
	prepare  func(context.Context, string, *catalog.Snapshot) (*catalog.DownloadPlan, error)
	download func(context.Context, *catalog.DownloadPlan, string) (catalog.DownloadResult, error)
}

func newCatalogController() *catalogController {
	ctx, cancel := context.WithCancel(context.Background())
	client := catalog.New(os.Getenv("GITHUB_TOKEN"))
	return &catalogController{ctx: ctx, cancel: cancel, search: client.Search, prepare: client.Prepare, download: client.Download}
}

func (c *catalogController) start() (*catalogSession, error) {
	return c.task(false, func(ctx context.Context) catalogMessage {
		snapshot, err := c.search(ctx, "")
		return catalogMessage{snapshot: snapshot, err: err}
	})
}

func (c *catalogController) task(publishing bool, run func(context.Context) catalogMessage) (*catalogSession, error) {
	timeout := catalog.Timeout
	if publishing {
		timeout = catalog.DownloadTimeout
	}
	return c.taskWithTimeout(publishing, timeout, run)
}

func (c *catalogController) taskWithTimeout(publishing bool, timeout time.Duration, run func(context.Context) catalogMessage) (*catalogSession, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, fmt.Errorf("catalog is closed")
	}
	ctx, cancel := context.WithTimeout(c.ctx, timeout)
	s := &catalogSession{cancel: cancel, done: make(chan catalogMessage, 1), publishing: publishing}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		defer cancel()
		msg := run(ctx)
		msg.session = s
		s.done <- msg
	}()
	return s, nil
}

func (m Model) prepareDownload(id string, pinned *catalog.Snapshot) (tea.Model, tea.Cmd) {
	if err := catalog.CheckDestination(m.c.PluginsDir, id); err != nil {
		m.status = err.Error()
		if m.screen == "download" {
			m.editing = true
			return m, m.input.Focus()
		}
		return m, nil
	}
	if m.c.DryRun || m.demo {
		m.message = fmt.Sprintf("DRY-RUN: plugins download %s\n%s\n%s", id, catalog.Source, m.c.PluginsDir)
		m.navigate("message")
		m.status = m.words.T("ATM_TUI_DRY_RUN")
		return m, nil
	}
	s, err := m.catalogController.task(false, func(ctx context.Context) catalogMessage {
		plan, err := m.catalogController.prepare(ctx, id, pinned)
		return catalogMessage{plan: plan, err: err}
	})
	if err != nil {
		m.status = err.Error()
		return m, nil
	}
	m.navigate("catalog-loading")
	m.catalogActive = s
	m.status = m.words.T("ATM_TUI_CATALOG_PREPARING")
	return m, func() tea.Msg { return <-s.done }
}

func (m Model) startDownload() (tea.Model, tea.Cmd) {
	if m.downloadPlan == nil || m.c.DryRun || m.demo {
		return m, nil
	}
	s, err := m.catalogController.task(true, func(ctx context.Context) catalogMessage {
		result, err := m.catalogController.download(ctx, m.downloadPlan, m.c.PluginsDir)
		return catalogMessage{result: &result, err: err}
	})
	if err != nil {
		m.status = err.Error()
		return m, nil
	}
	m.navigate("catalog-downloading")
	m.catalogActive = s
	m.quitAfterCatalog = false
	m.status = m.words.T("ATM_TUI_CATALOG_DOWNLOADING")
	return m, func() tea.Msg { return <-s.done }
}

func (m Model) cancelDownload(quit bool) (tea.Model, tea.Cmd) {
	m.quitAfterCatalog = m.quitAfterCatalog || quit
	m.catalogActive.cancel()
	m.status = m.words.T("ATM_TUI_CATALOG_CANCELLING")
	return m, nil
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
	if msg.core {
		return m.receiveCore(msg)
	}
	m.catalogActive = nil
	if msg.err != nil {
		m.navigate("core")
		m.status = m.words.T("ATM_TUI_FAILED") + ": " + msg.err.Error()
		if errors.Is(msg.err, context.Canceled) {
			m.status = m.words.T("ATM_TUI_CANCELLED")
		}
		if m.quitAfterCatalog {
			return m, tea.Quit
		}
		return m, nil
	}
	if msg.plan != nil {
		m.downloadPlan = msg.plan
		m.navigate("catalog-confirm")
		m.status = m.words.T("ATM_TUI_CATALOG_TRUST")
		return m, nil
	}
	if msg.result != nil {
		result := msg.result
		m.pendingPlugins[result.ID] = true
		m.message = fmt.Sprintf("%s\n%s\nCommit: %s\n%s", result.ID, result.Directory, result.Commit, m.words.T("ATM_TUI_CATALOG_RESTART"))
		for _, warning := range result.Warnings {
			m.message += "\n" + warning
		}
		m.downloadPlan = nil
		m.navigate("message")
		m.status = m.words.T("ATM_TUI_DONE")
		m.results = append(m.results, "plugins download "+result.ID+": "+m.status)
		if len(m.results) > 100 {
			m.results = m.results[len(m.results)-100:]
		}
		for i := range m.catalogSnapshot.Entries {
			if m.catalogSnapshot.Entries[i].ID == result.ID {
				m.catalogSnapshot.Entries[i].Local = true
			}
		}
		// Do not rediscover or load the newly published plugin in this session.
		if m.quitAfterCatalog {
			return m, tea.Quit
		}
		return m, nil
	}
	msg.snapshot.MarkLocal(m.plugins)
	for i := range msg.snapshot.Entries {
		if m.pendingPlugins[msg.snapshot.Entries[i].ID] {
			msg.snapshot.Entries[i].Local = true
		}
	}
	m.catalogSnapshot = msg.snapshot
	m.navigate("catalog")
	m.status = "Commit: " + msg.snapshot.Commit
	return m, nil
}
