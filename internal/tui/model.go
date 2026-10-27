package tui

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"runtime"
	"strings"
	"time"

	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/AtomyCloud/atm/internal/catalog"
	"github.com/AtomyCloud/atm/internal/config"
	"github.com/AtomyCloud/atm/internal/core"
	"github.com/AtomyCloud/atm/internal/i18n"
	"github.com/AtomyCloud/atm/internal/plugin"
	"github.com/AtomyCloud/atm/internal/protocol"
	"github.com/AtomyCloud/atm/internal/runner"
	"github.com/AtomyCloud/atm/internal/version"
)

type tickMsg time.Time
type finishedMsg struct{ err error }
type row struct{ key, label, detail string }
type hit struct {
	x, y, width, index int
	nav                string
}

type Model struct {
	c                 config.Config
	words             i18n.Catalog
	plugins           []plugin.Plugin
	locales           []string
	screen            string
	selected          string
	cursor, offset    int
	width, height     int
	input             textinput.Model
	editing           bool
	pendingAction     string
	plan              runner.Plan
	message           string
	status            string
	results           []string
	started, now      time.Time
	demo              bool
	demoStart         time.Time
	bar               progress.Model
	color             bool
	controller        *nativeController
	active            *nativeSession
	nativeLogs        []string
	nativeProgress    float64
	quitAfterNative   bool
	catalogController *catalogController
	catalogActive     *catalogSession
	catalogSnapshot   catalog.Snapshot
	catalogID         string
	downloadPlan      *catalog.DownloadPlan
	quitAfterCatalog  bool
	pendingPlugins    map[string]bool
	corePlan          core.Plan
	corePrepared      *core.Execution
	updateStates      map[string]string
}

func New(c config.Config, plugins []plugin.Plugin, words i18n.Catalog, demo bool) Model {
	input := textinput.New()
	input.Prompt = ""
	input.CharLimit = 200
	input.SetWidth(40)
	locales, _ := i18n.Locales(c.Home)
	now := time.Now()
	return Model{c: c, plugins: plugins, words: words, locales: locales, screen: "plugins", input: input,
		width: 80, height: 24, started: now, now: now, demo: demo,
		controller:        &nativeController{},
		catalogController: newCatalogController(),
		pendingPlugins:    map[string]bool{},
		updateStates:      map[string]string{},
		bar:               progress.New(progress.WithDefaultBlend()), color: os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb",
		status: words.T("ATM_TUI_READY")}
}

func tick() tea.Cmd {
	return tea.Tick(50*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}
func (m Model) Init() tea.Cmd {
	if !m.animationDisabled() {
		return tick()
	}
	return nil
}

func (m Model) rows() []row {
	rows := []row{}
	switch m.screen {
	case "catalog":
		for _, e := range m.catalogSnapshot.Filter(m.input.Value(), m.c.Sort) {
			state := m.words.T("ATM_TUI_CATALOG_REMOTE")
			if e.Local {
				state = m.words.T("ATM_TUI_CATALOG_LOCAL")
			}
			rows = append(rows, row{e.ID, e.Name, e.Version + " | " + e.Executor + " | " + state})
		}
	case "catalog-loading", "catalog-downloading", "core-running":
		rows = append(rows, row{"cancel-catalog", m.words.T("ATM_TUI_NO"), ""})
	case "plugins":
		query := strings.ToLower(m.input.Value())
		for _, p := range m.plugins {
			if !strings.Contains(strings.ToLower(p.Name+" "+p.ID+" "+p.Description), query) {
				continue
			}
			state := m.words.T("ATM_TUI_LEGACY")
			if p.Executor == "native" {
				state = m.words.T("ATM_TUI_NATIVE")
			}
			if !p.Available {
				state = p.Reason
			}
			rows = append(rows, row{p.ID, p.Name, p.Version + " | " + state})
		}
	case "actions":
		p, err := plugin.Find(m.plugins, m.selected)
		if err != nil {
			return rows
		}
		for _, action := range p.Operations {
			rows = append(rows, row{action, m.words.T("ATM_TUI_" + strings.ToUpper(action)), ""})
		}
		rows = append(rows, row{"update-plugin", m.words.T("ATM_TUI_UPDATE_PLUGIN"), ""})
	case "core":
		rows = []row{{"doctor", m.words.T("ATM_MENU_DOCTOR"), ""},
			{"check", m.words.T("ATM_TUI_UPDATE_CHECK"), ""}, {"update-all", m.words.T("ATM_TUI_UPDATE_ALL"), ""},
			{"catalog", m.words.T("ATM_TUI_CATALOG"), ""}, {"download", m.words.T("ATM_PLUGINS_AVAILABLE"), ""},
			{"full", m.words.T("ATM_MENU_FULL_SETUP"), ""}, {"path", m.words.T("ATM_TUI_CORE_PATH"), ""},
			{"update-one", m.words.T("ATM_TUI_UPDATE_PLUGIN"), ""},
			{"changes", m.words.T("ATM_TUI_CORE_CHANGES"), ""}, {"backups", m.words.T("ATM_TUI_CORE_BACKUPS"), ""},
			{"restore", m.words.T("ATM_TUI_CORE_RESTORE"), ""}, {"recover", m.words.T("ATM_TUI_CORE_RECOVER"), ""},
			{"setup", m.words.T("ATM_TUI_CORE_SETUP"), ""}, {"core-update", m.words.T("ATM_TUI_CORE_UPDATE"), ""},
			{"trust", m.words.T("ATM_TUI_CORE_SIGNING_KEY"), ""}}
	case "languages":
		for _, locale := range m.locales {
			rows = append(rows, row{locale, locale, ""})
		}
	case "sort":
		rows = []row{{"az", "A-Z", ""}, {"za", "Z-A", ""}, {"default", m.words.T("ATM_PLUGINS_SORT_DEFAULT"), ""}}
	case "confirm", "catalog-confirm":
		rows = []row{{"no", m.words.T("ATM_TUI_NO"), ""}, {"yes", m.words.T("ATM_TUI_YES"), ""}}
		if m.screen == "confirm" && m.plan.Executor == "native-core" {
			var value any = m.corePlan
			if m.corePrepared != nil {
				value = m.corePrepared.Preview
			}
			b, _ := json.MarshalIndent(value, "", "  ")
			for _, line := range strings.Split(string(b), "\n") {
				rows = append(rows, row{"result", line, ""})
			}
		}
		if m.screen == "catalog-confirm" && m.downloadPlan != nil {
			i := m.downloadPlan.Info()
			for _, line := range []string{i.ID, i.Source, "Commit: " + i.Commit, fmt.Sprintf("%d files / %d bytes", i.Files, i.Bytes), m.c.PluginsDir} {
				rows = append(rows, row{"result", line, ""})
			}
		}
	case "logs":
		for i := len(m.results) - 1; i >= 0; i-- {
			rows = append(rows, row{"result", m.results[i], ""})
		}
	case "native":
		label := m.words.T("ATM_TUI_CLOSE")
		if m.active != nil {
			label = m.words.T("ATM_TUI_NO")
		}
		rows = append(rows, row{"native-close", label, ""})
		for i := len(m.nativeLogs) - 1; i >= 0; i-- {
			rows = append(rows, row{"result", m.nativeLogs[i], ""})
		}
	case "message", "catalog-details":
		if m.screen == "catalog-details" {
			for _, e := range m.catalogSnapshot.Entries {
				if e.ID == m.catalogID && !e.Local {
					rows = append(rows, row{"catalog-download", m.words.T("ATM_TUI_CATALOG_DOWNLOAD"), ""})
				}
			}
		}
		for _, line := range strings.Split(m.message, "\n") {
			rows = append(rows, row{"result", line, ""})
		}
	}
	return rows
}

func (m *Model) navigate(screen string) {
	if m.catalogActive != nil {
		m.catalogActive.cancel()
		m.catalogActive = nil
		m.status = m.words.T("ATM_TUI_CANCELLED")
	}
	m.screen, m.cursor, m.offset, m.editing = screen, 0, 0, false
	m.input.Blur()
	if screen == "plugins" || screen == "catalog" {
		m.input.SetValue("")
	}
}

func (m *Model) clamp() {
	n := len(m.rows())
	m.cursor = max(0, min(m.cursor, n-1))
	page := max(1, m.height-9)
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+page {
		m.offset = m.cursor - page + 1
	}
	m.offset = max(0, min(m.offset, max(0, n-page)))
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case catalogMessage:
		return m.receiveCatalog(msg)
	case nativeMessage:
		return m.receiveNative(msg)
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.SetWidth(max(1, m.width-16))
		m.bar.SetWidth(max(1, m.width-4))
		m.clamp()
	case tickMsg:
		m.now = time.Time(msg)
		if m.screen == "demo" && m.now.Sub(m.demoStart) >= 2*time.Second {
			m.navigate("plugins")
			m.status = m.words.T("ATM_TUI_DEMO")
			m.results = append(m.results, m.status)
		}
		if !m.animationDisabled() || m.screen == "demo" {
			return m, tick()
		}
	case finishedMsg:
		m.navigate("plugins")
		m.status = m.words.T("ATM_TUI_DONE")
		if msg.err != nil {
			m.status = m.words.T("ATM_TUI_FAILED") + ": " + msg.err.Error()
		}
		m.results = append(m.results, time.Now().Format("15:04:05")+" "+m.planLabel()+": "+m.status)
		if len(m.results) > 100 {
			m.results = m.results[len(m.results)-100:]
		}
		if refreshed, err := plugin.Discover(m.c, runtime.GOOS); err == nil {
			m.plugins = nil
			for _, p := range refreshed {
				if !m.pendingPlugins[p.ID] {
					m.plugins = append(m.plugins, p)
				}
			}
		} else {
			m.status = err.Error()
		}
	case tea.KeyPressMsg:
		key := msg.String()
		if m.catalogActive != nil && m.catalogActive.publishing {
			switch key {
			case "q", "ctrl+c":
				return m.cancelDownload(true)
			case "esc", "enter":
				return m.cancelDownload(false)
			}
			return m, nil
		}
		if m.active != nil {
			switch key {
			case "q", "ctrl+c":
				return m.cancelNative(true)
			case "esc":
				return m.cancelNative(false)
			case "enter":
				if m.cursor == 0 {
					return m.cancelNative(false)
				}
			case "up", "k":
				m.cursor--
				m.clamp()
			case "down", "j":
				m.cursor++
				m.clamp()
			}
			return m, nil
		}
		if key == "ctrl+c" {
			return m, tea.Quit
		}
		if m.editing {
			if key == "esc" {
				m.editing = false
				m.input.Blur()
				if m.screen == "version" {
					m.navigate("actions")
				}
				if m.screen == "download" {
					m.navigate("core")
				}
				if m.screen == "core-input" {
					m.navigate("core")
				}
				return m, nil
			}
			if key == "enter" {
				m.editing = false
				m.input.Blur()
				if m.screen == "version" {
					return m.preparePlugin(m.pendingAction, []string{m.input.Value()})
				}
				if m.screen == "download" {
					return m.prepareDownload(m.input.Value(), nil)
				}
				if m.screen == "core-input" {
					return m.submitCoreInput()
				}
				return m, nil
			}
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			m.cursor, m.offset = 0, 0
			return m, cmd
		}
		if m.screen == "demo" {
			if key == "esc" {
				m.navigate("plugins")
			}
			return m, nil
		}
		switch key {
		case "q":
			return m, tea.Quit
		case "esc", "backspace":
			if m.screen == "catalog-details" {
				query := m.input.Value()
				m.navigate("catalog")
				m.input.SetValue(query)
			} else {
				m.navigate("plugins")
			}
		case "up", "k":
			m.cursor--
			m.clamp()
		case "down", "j":
			m.cursor++
			m.clamp()
		case "pgup":
			m.cursor -= max(1, m.height-9)
			m.clamp()
		case "pgdown":
			m.cursor += max(1, m.height-9)
			m.clamp()
		case "home":
			m.cursor = 0
			m.clamp()
		case "end":
			m.cursor = len(m.rows()) - 1
			m.clamp()
		case "enter":
			return m.activate()
		case "/":
			if m.screen == "plugins" || m.screen == "catalog" {
				m.editing = true
				return m, m.input.Focus()
			}
		case "p":
			m.navigate("plugins")
		case "u":
			m.navigate("core")
		case "l":
			m.navigate("languages")
		case "s":
			m.navigate("sort")
		case "h":
			m.navigate("logs")
		}
	case tea.MouseWheelMsg:
		if m.mouseDisabled() || m.editing || m.screen == "demo" {
			return m, nil
		}
		if msg.Button == tea.MouseWheelUp {
			m.cursor--
		}
		if msg.Button == tea.MouseWheelDown {
			m.cursor++
		}
		m.clamp()
	case tea.MouseClickMsg:
		if m.mouseDisabled() || m.screen == "demo" || msg.Button != tea.MouseLeft {
			return m, nil
		}
		_, hits := m.render()
		for _, h := range hits {
			if msg.Y == h.y && msg.X >= h.x && msg.X < h.x+h.width {
				if m.catalogActive != nil && m.catalogActive.publishing {
					if h.nav == "quit" {
						return m.cancelDownload(true)
					}
					if h.nav == "cancel-catalog" || (h.nav == "" && h.index == 0) {
						return m.cancelDownload(false)
					}
					return m, nil
				}
				if m.active != nil {
					if h.nav == "quit" {
						return m.cancelNative(true)
					}
					if h.nav == "cancel-native" || (h.nav == "" && h.index == 0) {
						return m.cancelNative(false)
					}
					return m, nil
				}
				if h.nav == "search" {
					m.editing = true
					return m, m.input.Focus()
				}
				if h.nav == "quit" {
					return m, tea.Quit
				}
				if h.nav != "" {
					m.navigate(h.nav)
					return m, nil
				}
				if m.editing {
					return m, nil
				}
				m.cursor = h.index
				return m.activate()
			}
		}
	}
	return m, nil
}

func (m Model) activate() (tea.Model, tea.Cmd) {
	rows := m.rows()
	if len(rows) == 0 || m.cursor >= len(rows) {
		return m, nil
	}
	key := rows[m.cursor].key
	switch m.screen {
	case "catalog-confirm":
		if key == "yes" {
			return m.startDownload()
		}
		if key == "no" {
			m.downloadPlan = nil
			m.navigate("core")
		}
	case "catalog-details":
		if key == "catalog-download" {
			return m.prepareDownload(m.catalogID, &m.catalogSnapshot)
		}
	case "catalog-loading":
		m.navigate("core")
		m.status = m.words.T("ATM_TUI_CANCELLED")
	case "catalog":
		for _, e := range m.catalogSnapshot.Entries {
			if e.ID == key {
				m.catalogID = e.ID
				m.message = fmt.Sprintf("%s (%s)\n%s\n%s\n%s\nPlatforms: %s\nCommit: %s\n%s", e.Name, e.ID,
					e.Version, e.Description, e.Executor, strings.Join(e.Platforms, ", "), m.catalogSnapshot.Commit, catalog.Source)
				m.navigate("catalog-details")
				break
			}
		}
	case "native":
		if key == "native-close" {
			m.navigate("plugins")
		}
	case "plugins":
		m.selected = key
		p, _ := plugin.Find(m.plugins, key)
		if !p.Available {
			m.status = p.Reason
			return m, nil
		}
		m.navigate("actions")
	case "actions":
		if key == "update-plugin" {
			return m.prepareCore([]string{"updates", "plugin", m.selected})
		}
		if key == "remove" || key == "use" {
			m.pendingAction = key
			m.navigate("version")
			m.editing = true
			m.input.SetValue("")
			return m, m.input.Focus()
		}
		return m.preparePlugin(key, nil)
	case "core":
		switch key {
		case "doctor":
			m.message = fmt.Sprintf("ATM %s (%s)\nPlatform: %s\nHome: %s\nApps: %s\nPlugins: %d\nNative catalog: available\nNative plugin code updates/recovery: available\nNative setup/ATM PATH/Full Setup: available\nApplication integration/backends: pending\nCompiled CORE updater: full-package replacement pending; public key approved", version.Version, version.Commit, m.c.Platform(), m.c.Home, m.c.AppsDir, len(m.plugins))
			m.navigate("message")
		case "download":
			m.navigate("download")
			m.editing = true
			m.input.SetValue("")
			return m, m.input.Focus()
		case "check":
			return m.prepareCore([]string{"updates", "check"})
		case "update-all":
			return m.prepareCore([]string{"updates", "all"})
		case "catalog":
			return m.startCatalog()
		case "full":
			return m.prepareCore([]string{"install"})
		case "path":
			return m.prepareCore([]string{"path", "apply"})
		case "changes", "backups", "trust":
			return m.prepareCore([]string{"updates", key})
		case "recover":
			return m.prepareCore([]string{"updates", "recover", "plugins"})
		case "core-update":
			return m.prepareCore([]string{"updates", "core"})
		case "restore", "update-one", "setup":
			m.pendingAction = key
			m.navigate("core-input")
			m.input.SetValue("")
			m.editing = true
			return m, m.input.Focus()
		}
	case "languages":
		words, err := i18n.Load(m.c.Home, key)
		if err != nil {
			m.status = err.Error()
			return m, nil
		}
		p := m.c.Preferences
		p.Language = key
		if err := m.c.Save(p); err != nil {
			m.status = err.Error()
			return m, nil
		}
		m.c.Preferences, m.c.Language, m.words = p, key, words
		m.status = words.T("ATM_TUI_SAVED")
		if m.c.DryRun {
			m.status = words.T("ATM_TUI_DRY_RUN")
		}
		m.navigate("plugins")
	case "sort":
		p := m.c.Preferences
		p.Sort = key
		if err := m.c.Save(p); err != nil {
			m.status = err.Error()
			return m, nil
		}
		m.c.Preferences, m.c.Sort = p, key
		plugin.Sort(m.plugins, key)
		m.navigate("plugins")
	case "confirm":
		if key != "yes" && key != "no" {
			return m, nil
		}
		if key == "no" {
			m.navigate("plugins")
			return m, nil
		}
		if m.demo {
			m.navigate("demo")
			m.demoStart = time.Now()
			if m.animationDisabled() {
				return m, tick()
			}
			return m, nil
		}
		if m.c.DryRun {
			var value any = m.plan
			if m.plan.Executor == "native-core" {
				value = m.corePlan
			}
			b, _ := json.MarshalIndent(value, "", "  ")
			m.message = string(b)
			m.navigate("message")
			m.status = m.words.T("ATM_TUI_DRY_RUN")
			return m, nil
		}
		if m.plan.Executor == "native" {
			return m.startNative()
		}
		if m.plan.Executor == "native-core" {
			return m.startCoreExecution()
		}
		cmd, err := runner.Command(m.c, m.plan)
		if err != nil {
			m.status = err.Error()
			m.navigate("plugins")
			return m, nil
		}
		return m, tea.Exec(&terminalCommand{cmd: cmd, notice: m.words.T("ATM_TUI_CONTINUE")}, func(err error) tea.Msg { return finishedMsg{err} })
	}
	return m, nil
}

func (m Model) preparePlugin(action string, args []string) (tea.Model, tea.Cmd) {
	p, err := plugin.Find(m.plugins, m.selected)
	if err == nil && p.Executor == "legacy-bash" && len(m.pendingPlugins) > 0 {
		m.status = m.words.T("ATM_TUI_CATALOG_RESTART")
		return m, nil
	}
	if err == nil {
		m.plan, err = runner.Plugin(m.c, p, action, args)
	}
	if err != nil {
		m.status = err.Error()
		m.navigate("actions")
		return m, nil
	}
	m.navigate("confirm")
	return m, nil
}

func (m Model) prepareCore(args []string) (tea.Model, tea.Cmd) {
	if len(args) == 1 && args[0] == "install" && len(m.pendingPlugins) > 0 {
		for _, p := range m.plugins {
			if p.Enabled && p.FullSetup && p.Executor == "legacy-bash" {
				m.status = m.words.T("ATM_TUI_CATALOG_RESTART")
				return m, nil
			}
		}
	}
	var err error
	m.corePlan, err = core.Prepare(m.c, args, m.plugins)
	if err != nil {
		m.status = err.Error()
		return m, nil
	}
	m.plan = runner.Plan{Executor: "native-core", Arguments: m.corePlan.Arguments, DryRun: m.c.DryRun}
	m.corePrepared = nil
	if m.c.DryRun || m.demo {
		m.navigate("confirm")
		return m, nil
	}
	return m.resolveCore()
}

func clean(s string) string {
	return protocol.SafeText(s)
}

func (m Model) paint(s, color string, bold bool) string {
	if !m.color {
		return s
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Bold(bold).Render(s)
}

func (m Model) logo() []string {
	lines := []string{"  /\\  _____ __  __", " /__\\   |   | \\/ |", "/    \\  |   |    |"}
	phase := 0.0
	if !m.animationDisabled() {
		phase = m.now.Sub(m.started).Seconds() * 0.7
	}
	for y, line := range lines {
		var out strings.Builder
		for x, r := range line {
			wave := phase + float64(x)*0.12 + float64(y)*0.2
			red := int(140 + 90*math.Sin(wave))
			green := int(155 + 75*math.Sin(wave+2))
			blue := int(145 + 85*math.Sin(wave+4))
			out.WriteString(m.paint(string(r), fmt.Sprintf("#%02x%02x%02x", red, green, blue), true))
		}
		lines[y] = out.String()
	}
	return lines
}

// Rendering and hit testing share the same cell coordinates, including scroll offset.
func (m Model) render() (string, []hit) {
	if m.width < 32 || m.height < 12 {
		return ansi.Truncate(m.words.T("ATM_TUI_RESIZE"), max(1, m.width), ""), nil
	}
	lines := m.logo()
	lines[0] += "  ATM " + version.Version
	lines[1] += "  " + m.c.Platform()
	lines[2] += "  " + m.c.Language
	lines = append(lines, strings.Repeat("-", m.width))
	title := m.words.T("ATM_TUI_" + strings.ToUpper(m.screen))
	switch m.screen {
	case "catalog":
		title = m.words.T("ATM_TUI_FILTER") + ": " + m.input.View()
	case "catalog-loading":
		title = m.words.T("ATM_TUI_CATALOG_LOADING")
	case "catalog-downloading":
		title = m.words.T("ATM_TUI_CATALOG_DOWNLOADING")
	case "catalog-confirm":
		title = m.words.T("ATM_TUI_CATALOG_DOWNLOAD")
	case "plugins":
		title = m.words.T("ATM_TUI_FILTER") + ": " + m.input.View()
	case "actions":
		title = m.selected + " / " + m.words.T("ATM_TUI_ACTIONS")
	case "languages":
		title = m.words.T("ATM_TUI_LANGUAGE")
	case "confirm":
		title = m.words.T("ATM_TUI_CONFIRM") + " " + clean(m.planLabel())
	case "version":
		title = m.words.T("ATM_TUI_VERSION_PROMPT") + ": " + m.input.View()
	case "download":
		title = "Plugin ID: " + m.input.View()
	case "core-input":
		title = m.words.T("ATM_TUI_CORE_INPUT") + ": " + m.input.View()
	case "core-running":
		title = m.words.T("ATM_TUI_RUNNING")
	case "demo":
		title = m.bar.ViewAs(min(1, m.now.Sub(m.demoStart).Seconds()/2))
	case "native":
		title = m.bar.ViewAs(m.nativeProgress)
	case "catalog-details":
		title = m.words.T("ATM_TUI_CATALOG")
	case "message":
		title = m.words.T("ATM_TUI_LOGS")
	}
	lines = append(lines, title)
	if m.screen == "confirm" {
		key := "ATM_TUI_TRUST"
		if m.plan.Executor == "native" {
			key = "ATM_TUI_NATIVE_TRUST"
		}
		if m.plan.Executor == "native-core" {
			key = "ATM_TUI_CORE_TRUST"
		}
		lines = append(lines, m.words.T(key))
	} else if m.screen == "catalog-confirm" {
		lines = append(lines, m.words.T("ATM_TUI_CATALOG_TRUST"))
	} else if m.screen == "catalog" {
		lines = append(lines, m.words.T("ATM_TUI_CATALOG"))
	} else {
		lines = append(lines, "")
	}
	hits := []hit{}
	if m.screen == "plugins" || m.screen == "catalog" {
		hits = append(hits, hit{0, 4, m.width, 0, "search"})
	}
	rows := m.rows()
	page := m.height - 9
	for i := 0; i < page; i++ {
		index := m.offset + i
		text := ""
		if index < len(rows) {
			r := rows[index]
			prefix := "  "
			if index == m.cursor {
				prefix = "> "
			}
			text = prefix + clean(r.label)
			if r.detail != "" {
				text += "  |  " + clean(r.detail)
			}
			if m.screen == "plugins" && m.updateStates[r.key] == "available" {
				text += "  " + m.paint("Update", "#55aaff", true)
			}
			text = ansi.Truncate(text, m.width, "...")
			if index == m.cursor {
				text = m.paint(text, "#5ed6b4", true)
			}
			hits = append(hits, hit{0, 6 + i, m.width, index, ""})
		} else if i == 0 {
			if m.screen == "plugins" || m.screen == "catalog" {
				text = m.words.T("ATM_TUI_EMPTY")
			}
			if m.screen == "logs" {
				text = m.words.T("ATM_TUI_RESULTS_EMPTY")
			}
		}
		lines = append(lines, text)
	}
	footer := ""
	nav := []row{{"plugins", m.words.T("ATM_TUI_PLUGINS"), ""}, {"core", "ATM", ""}, {"languages", m.words.T("ATM_TUI_LANGUAGE"), ""}, {"sort", m.words.T("ATM_TUI_SORT"), ""}, {"logs", m.words.T("ATM_TUI_LOGS"), ""}, {"quit", m.words.T("ATM_MENU_EXIT"), ""}}
	if m.active != nil {
		nav = []row{{"cancel-native", m.words.T("ATM_TUI_NO"), ""}, {"quit", m.words.T("ATM_MENU_EXIT"), ""}}
	}
	if m.catalogActive != nil && m.catalogActive.publishing {
		nav = []row{{"cancel-catalog", m.words.T("ATM_TUI_NO"), ""}, {"quit", m.words.T("ATM_MENU_EXIT"), ""}}
	}
	for _, n := range nav {
		label := "[" + n.label + "] "
		x, width := ansi.StringWidth(footer), ansi.StringWidth(label)
		if x+width > m.width {
			break
		}
		hits = append(hits, hit{x, m.height - 3, width, 0, n.key})
		footer += label
	}
	lines = append(lines, footer, m.paint(clean(m.status), "#e8c86a", false), "")
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], m.width, "")
	}
	return strings.Join(lines, "\n"), hits
}

func (m Model) View() tea.View {
	content, _ := m.render()
	v := tea.NewView(content)
	v.AltScreen = true
	if !m.mouseDisabled() {
		v.MouseMode = tea.MouseModeCellMotion
	}
	return v
}

func (m Model) planLabel() string {
	if m.plan.Executor == "native-core" {
		return strings.Join(m.plan.Arguments, " ")
	}
	if m.plan.Executor == "native" {
		return strings.Join(append([]string{m.plan.PluginID, m.plan.Operation}, m.plan.Arguments...), " ")
	}
	return strings.Join(m.plan.Command[min(2, len(m.plan.Command)):], " ")
}

func (m Model) mouseDisabled() bool     { return m.c.NoMouse || m.c.Preferences.DisableMouse }
func (m Model) animationDisabled() bool { return m.c.NoAnimation || m.c.Preferences.DisableAnimation }
