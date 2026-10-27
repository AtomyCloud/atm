package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/AtomyCloud/atm/internal/catalog"
	"github.com/AtomyCloud/atm/internal/plugin"
)

func TestCatalogDryRunAndDemoNoRequests(t *testing.T) {
	for _, demo := range []bool{false, true} {
		m := model(t)
		defer m.Close()
		m.demo, m.c.DryRun = demo, !demo
		m.catalogController.search = func(context.Context, string) (catalog.Snapshot, error) {
			t.Error("network in dry-run/demo")
			return catalog.Snapshot{}, nil
		}
		updated, cmd := m.startCatalog()
		if cmd != nil || updated.(Model).screen != "message" || !strings.Contains(updated.(Model).message, catalog.Source) {
			t.Fatal("dry-run not isolated")
		}
	}
}

func TestCatalogResultFilterMouseAndDetails(t *testing.T) {
	m := model(t)
	defer m.Close()
	m.c.DryRun = false
	m.catalogController.search = func(context.Context, string) (catalog.Snapshot, error) {
		return catalog.Snapshot{Source: catalog.Source, Commit: strings.Repeat("a", 40), Entries: []catalog.Entry{
			{Manifest: plugin.Manifest{ID: "node", Name: "Node", Version: "0.0.1", Executor: "legacy-bash"}, Paths: []string{"plugins/node/special.txt"}},
			{Manifest: plugin.Manifest{ID: "other", Name: "Other", Version: "0.0.1"}},
		}}, nil
	}
	updated, cmd := m.startCatalog()
	m = updated.(Model)
	if m.screen != "catalog-loading" || cmd == nil {
		t.Fatal("no asynchronous search")
	}
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if m.screen != "catalog" || m.catalogActive != nil || !m.catalogSnapshot.Entries[0].Local {
		t.Fatal("result not shown/local not marked")
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = updated.(Model)
	if !m.editing {
		t.Fatal("catalog filter unavailable")
	}
	m.input.SetValue("special.txt")
	if len(m.rows()) != 1 || m.rows()[0].key != "node" {
		t.Fatal("path filter failed")
	}
	m.editing = false
	updated, _ = m.Update(tea.MouseClickMsg{X: 1, Y: 6, Button: tea.MouseLeft})
	m = updated.(Model)
	if m.screen != "catalog-details" || !strings.Contains(m.message, "Node") || !strings.Contains(m.message, "Commit:") {
		t.Fatal("details not shown")
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updated.(Model)
	if m.screen != "catalog" || m.input.Value() != "special.txt" {
		t.Fatal("details lost catalog filter")
	}
}

func TestCatalogCancellationAndStaleResult(t *testing.T) {
	m := model(t)
	defer m.Close()
	m.c.DryRun = false
	m.catalogController.search = func(ctx context.Context, _ string) (catalog.Snapshot, error) {
		<-ctx.Done()
		return catalog.Snapshot{}, ctx.Err()
	}
	updated, cmd := m.startCatalog()
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updated.(Model)
	if m.catalogActive != nil || m.screen != "plugins" || m.status != m.words.T("ATM_TUI_CANCELLED") {
		t.Fatal("escape did not leave search")
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		updated, _ = m.Update(msg)
		if updated.(Model).screen != "plugins" {
			t.Fatal("stale search result changed navigation")
		}
	case <-time.After(time.Second):
		t.Fatal("search did not cancel")
	}
}

func TestCatalogCloseFromOriginalModelAndFailure(t *testing.T) {
	original := model(t)
	original.c.DryRun = false
	original.catalogController.search = func(ctx context.Context, _ string) (catalog.Snapshot, error) {
		<-ctx.Done()
		return catalog.Snapshot{}, ctx.Err()
	}
	_, cmd := original.startCatalog()
	closed := make(chan struct{})
	go func() { original.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("close did not cancel model copy")
	}
	if cmd().(catalogMessage).err == nil {
		t.Fatal("missing cancellation error")
	}
	if _, err := original.catalogController.start(); err == nil {
		t.Fatal("start after close")
	}
	m := model(t)
	defer m.Close()
	m.c.DryRun = false
	m.catalogController.search = func(context.Context, string) (catalog.Snapshot, error) {
		return catalog.Snapshot{}, fmt.Errorf("rate limit")
	}
	updated, cmd := m.startCatalog()
	m = updated.(Model)
	updated, _ = m.Update(cmd())
	if updated.(Model).screen != "core" || !strings.Contains(updated.(Model).status, "rate limit") {
		t.Fatal("missing failure UI")
	}
}
