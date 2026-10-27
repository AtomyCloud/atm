package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/AtomyCloud/atm/internal/catalog"
	"github.com/AtomyCloud/atm/internal/config"
	"github.com/AtomyCloud/atm/internal/core"
	"github.com/AtomyCloud/atm/internal/coreupdate"
	"github.com/AtomyCloud/atm/internal/runner"
)

func TestCoreTaskDeadlineSelection(t *testing.T) {
	for _, tc := range []struct {
		name             string
		configured, want time.Duration
	}{
		{"configured", 2 * time.Second, 2 * time.Second},
		{"longer-than-catalog", 20 * time.Minute, 20 * time.Minute},
		{"default", 0, runner.DefaultTimeout},
		{"negative-default", -time.Second, runner.DefaultTimeout},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newCatalogController()
			defer c.close()
			started := time.Now()
			s, err := c.taskWithTimeout(true, coreTimeout(config.Config{NativeTimeout: tc.configured}), func(ctx context.Context) catalogMessage {
				deadline, ok := ctx.Deadline()
				if !ok || deadline.Before(started.Add(tc.want)) || deadline.After(time.Now().Add(tc.want)) {
					t.Errorf("incorrect CORE deadline: %v, expected duration %v", deadline, tc.want)
				}
				return catalogMessage{}
			})
			if err != nil {
				t.Fatal(err)
			}
			msg := <-s.done
			if msg.session != s || !s.publishing {
				t.Fatal("CORE session lost its lifetime/navigation guard")
			}
		})
	}
}

func TestCatalogTaskDeadlinesUnchanged(t *testing.T) {
	for _, publishing := range []bool{false, true} {
		c := newCatalogController()
		want := catalog.Timeout
		if publishing {
			want = catalog.DownloadTimeout
		}
		started := time.Now()
		s, err := c.task(publishing, func(ctx context.Context) catalogMessage {
			deadline, ok := ctx.Deadline()
			if !ok || deadline.Before(started.Add(want)) || deadline.After(time.Now().Add(want)) {
				t.Errorf("catalog deadline changed: %v, expected duration %v", deadline, want)
			}
			return catalogMessage{}
		})
		if err != nil {
			c.close()
			t.Fatal(err)
		}
		<-s.done
		c.close()
	}
}

func TestCoreTaskDeadlineStopsWholeBatch(t *testing.T) {
	c := newCatalogController()
	defer c.close()
	p := core.Plan{Plugins: []runner.Plan{{PluginID: "first"}, {PluginID: "later"}}}
	s, err := c.taskWithTimeout(true, 20*time.Millisecond, func(ctx context.Context) catalogMessage {
		r, err := core.RunBatch(ctx, p, func(ctx context.Context, _ runner.Plan) error {
			<-ctx.Done()
			return ctx.Err()
		})
		return catalogMessage{core: true, coreResult: &core.Result{Value: r}, err: err}
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case msg := <-s.done:
		if !errors.Is(msg.err, context.DeadlineExceeded) {
			t.Fatal("missing overall deadline", msg.err)
		}
		r := msg.coreResult.Value.(core.BatchResult)
		if len(r.Items) != 2 || r.Items[1].Status != "not_run" {
			t.Fatal("batch continued after deadline", r)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("CORE task ignored deadline")
	}
}

func TestCoreTaskDeadlineRetainsCloseCancellation(t *testing.T) {
	c := newCatalogController()
	s, err := c.taskWithTimeout(true, time.Hour, func(ctx context.Context) catalogMessage {
		<-ctx.Done()
		return catalogMessage{err: ctx.Err()}
	})
	if err != nil {
		t.Fatal(err)
	}
	c.close()
	if msg := <-s.done; !errors.Is(msg.err, context.Canceled) {
		t.Fatal("close did not cancel CORE task", msg.err)
	}
	if _, err := c.taskWithTimeout(true, time.Hour, nil); err == nil {
		t.Fatal("CORE task started after close")
	}
}

// Consume the batch deadline before the runner starts its per-plugin timeout.
type deadlineWriter struct{ delay time.Duration }

func (w deadlineWriter) Write(b []byte) (int, error) {
	time.Sleep(w.delay)
	return len(b), nil
}

func TestTerminalBatchUsesOverallCoreDeadline(t *testing.T) {
	t.Setenv("PATH", "")
	var out bytes.Buffer
	b := terminalBatch{
		c: config.Config{Home: t.TempDir(), PluginsDir: t.TempDir(), NativeTimeout: 20 * time.Millisecond},
		plan: core.Plan{Arguments: []string{"install"}, Plugins: []runner.Plan{
			{PluginID: "first", Executor: "native"}, {PluginID: "later", Executor: "native"},
		}},
		in: strings.NewReader("\n"), out: &out, stderr: deadlineWriter{delay: 60 * time.Millisecond},
	}
	if err := b.Run(); runner.ExitCode(err) != 124 {
		t.Fatal("batch did not expire before runner validation", err)
	}
	var result core.BatchResult
	if err := json.NewDecoder(&out).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 2 || result.Items[1].Status != "not_run" {
		t.Fatal("terminal batch continued after deadline", result)
	}
}

func TestCoreRecoveryRequiredLocksSessionWithoutChangedFlag(t *testing.T) {
	m := model(t)
	r := core.Result{Operation: "updates core", Value: coreupdate.BundleResult{State: "recovery-required", Changed: false}}
	updated, _ := m.receiveCore(catalogMessage{core: true, coreResult: &r, err: io.ErrUnexpectedEOF})
	m = updated.(Model)
	if !m.restartRequired || !strings.Contains(m.message, m.words.T("ATM_TUI_CORE_RESTART")) {
		t.Fatal("partial staging failure did not lock session")
	}
	for _, key := range []rune{'p', 'u', 'l'} {
		updated, cmd := m.Update(tea.KeyPressMsg{Code: key})
		if cmd != nil || updated.(Model).screen != "message" {
			t.Fatal("navigation allowed while recovery is required")
		}
	}
	if _, cmd := m.prepareCore([]string{"install"}); cmd != nil {
		t.Fatal("CORE execution allowed while recovery is required")
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q'})
	if cmd == nil {
		t.Fatal("locked session cannot exit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("expected quit")
	}
}
