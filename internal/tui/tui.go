// Package tui is the bubbletea-based interactive terminal UI for webaudt.
//
// Layout: two panes side-by-side.
//
//   ┌──────────────────────┬─────────────────────────────┐
//   │ [1] Sites            │ [2] Details                 │
//   │  ● vendors.docomo... │ vendors.docomopacific.com   │
//   │  ● pnccpalau.com     │ Path: ...                   │
//   │                      │ Last: 5m ago                │
//   │                      │ ...                         │
//   └──────────────────────┴─────────────────────────────┘
//     1/2 focus pane · j/k move · r refresh · R all · q quit
//
// Pane focus is jumpable with 1/2 like lazydocker; j/k (or ↑/↓) move within
// the focused pane.
package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/jeromecoloma/webaudt/internal/audit"
	"github.com/jeromecoloma/webaudt/internal/cache"
	"github.com/jeromecoloma/webaudt/internal/config"
	"github.com/jeromecoloma/webaudt/internal/types"
	"github.com/jeromecoloma/webaudt/internal/ui"
)

const (
	paneSidebar = 0
	panePreview = 1
)

// Run launches the TUI. Returns after the user quits.
func Run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	m := newModel(cfg)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err = p.Run()
	return err
}

type model struct {
	cfg          *config.File
	sites        []siteRow
	cursor       int
	focus        int // paneSidebar | panePreview
	preview      viewport.Model
	width        int
	height       int
	refreshing   map[string]bool
	statusMsg    string
}

type siteRow struct {
	site  config.Site
	entry *cache.Entry // nil if no cache
	worst string
}

func newModel(cfg *config.File) *model {
	m := &model{
		cfg:        cfg,
		refreshing: map[string]bool{},
		focus:      paneSidebar,
	}
	m.preview = viewport.New(0, 0)
	m.reloadSites()
	return m
}

// reloadSites re-reads cache for every registered site.
func (m *model) reloadSites() {
	m.sites = m.sites[:0]
	for _, s := range m.cfg.Sites {
		row := siteRow{site: s, worst: types.SevNever}
		if cache.Exists(s.Path) {
			if e, err := cache.Read(s.Path); err == nil {
				row.entry = e
				row.worst = e.Worst()
			}
		}
		m.sites = append(m.sites, row)
	}
	if m.cursor >= len(m.sites) {
		m.cursor = max0(len(m.sites) - 1)
	}
	m.setPreviewContent()
}

// ---- bubbletea Model interface ----

func (m *model) Init() tea.Cmd { return nil }

// Messages sent from background audit goroutines.
type refreshDoneMsg struct {
	name string
	err  error
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.layoutPanes()
		return m, nil

	case refreshDoneMsg:
		delete(m.refreshing, msg.name)
		m.reloadSites()
		if msg.err != nil {
			m.statusMsg = "refresh " + msg.name + ": " + msg.err.Error()
		} else {
			m.statusMsg = "refreshed " + msg.name
		}
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "1":
			m.focus = paneSidebar
			return m, nil
		case "2":
			m.focus = panePreview
			return m, nil
		case "tab":
			m.focus = (m.focus + 1) % 2
			return m, nil
		case "?":
			m.statusMsg = helpLine()
			return m, nil
		case "r":
			return m, m.refreshCurrent()
		case "R":
			return m, m.refreshAll()
		}

		if m.focus == paneSidebar {
			switch msg.String() {
			case "j", "down":
				if m.cursor < len(m.sites)-1 {
					m.cursor++
					m.setPreviewContent()
				}
			case "k", "up":
				if m.cursor > 0 {
					m.cursor--
					m.setPreviewContent()
				}
			case "g", "home":
				m.cursor = 0
				m.setPreviewContent()
			case "G", "end":
				m.cursor = max0(len(m.sites) - 1)
				m.setPreviewContent()
			}
			return m, nil
		}

		// Preview pane: hand off scroll keys to viewport.
		var cmd tea.Cmd
		m.preview, cmd = m.preview.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	sidebar := m.renderSidebar()
	preview := m.renderPreview()
	body := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, preview)

	header := ui.Heading("webaudt")
	footer := m.renderFooter()

	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

// layoutPanes sizes the panes based on the current terminal width/height.
func (m *model) layoutPanes() {
	sidebarWidth := 36
	if m.width > 0 && sidebarWidth > m.width/2 {
		sidebarWidth = m.width / 2
	}
	previewWidth := m.width - sidebarWidth - 4 // 2 borders × 2 panes ≈ 4 chars
	if previewWidth < 20 {
		previewWidth = 20
	}
	previewHeight := m.height - 4 // header + footer + borders
	if previewHeight < 5 {
		previewHeight = 5
	}
	m.preview.Width = previewWidth
	m.preview.Height = previewHeight
	m.setPreviewContent()
}

// ---- rendering helpers ----

func (m *model) renderSidebar() string {
	sidebarWidth := 36
	if m.width > 0 && sidebarWidth > m.width/2 {
		sidebarWidth = m.width / 2
	}
	height := m.height - 4
	if height < 5 {
		height = 5
	}

	var lines []string
	if len(m.sites) == 0 {
		lines = append(lines, ui.Dim("(no sites — webaudt add /path)"))
	}
	for i, row := range m.sites {
		icon := ui.StatusIcon(row.worst)
		if m.refreshing[row.site.Name] {
			icon = ui.StatusIcon(types.SevRunning)
		}
		name := row.site.Name
		if i == m.cursor {
			name = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("51")).Render("▸ " + name)
		} else {
			name = "  " + name
		}
		summary := compactSummary(row.entry)
		line := fmt.Sprintf("%s %s", icon, name)
		if summary != "" {
			line += "  " + ui.Dim(summary)
		}
		lines = append(lines, line)
	}

	body := strings.Join(lines, "\n")
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Width(sidebarWidth).
		Height(height).
		Padding(0, 1)
	if m.focus == paneSidebar {
		style = style.BorderForeground(lipgloss.Color("51"))
	} else {
		style = style.BorderForeground(lipgloss.Color("244"))
	}
	title := paneTitle("1 · sites", m.focus == paneSidebar)
	return style.Render(title + "\n" + body)
}

func (m *model) renderPreview() string {
	width := m.preview.Width
	height := m.preview.Height
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Width(width).
		Height(height).
		Padding(0, 1)
	if m.focus == panePreview {
		style = style.BorderForeground(lipgloss.Color("51"))
	} else {
		style = style.BorderForeground(lipgloss.Color("244"))
	}
	title := paneTitle("2 · details", m.focus == panePreview)
	return style.Render(title + "\n" + m.preview.View())
}

func (m *model) renderFooter() string {
	hints := "1/2 focus pane · j/k move · r refresh · R refresh all · ? help · q quit"
	if m.statusMsg != "" {
		hints = m.statusMsg
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Render("  " + hints)
}

func paneTitle(text string, active bool) string {
	if active {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("51")).Bold(true).Render(text)
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Render(text)
}

// setPreviewContent regenerates the right-pane content for the current site.
func (m *model) setPreviewContent() {
	if len(m.sites) == 0 {
		m.preview.SetContent(ui.Dim("no sites registered yet."))
		return
	}
	row := m.sites[m.cursor]
	var b strings.Builder
	b.WriteString(ui.Name(row.site.Name))
	b.WriteString("\n")
	b.WriteString(ui.Dim("path:  ") + row.site.Path + "\n")
	b.WriteString(ui.Dim("type:  ") + string(row.site.Type) + "\n")

	if row.entry == nil {
		b.WriteString("\n" + ui.Dim("(never checked — press r to refresh)") + "\n")
		m.preview.SetContent(b.String())
		return
	}

	b.WriteString(ui.Dim("checked: ") + ui.AbsTime(row.entry.CheckedAt) + " " + ui.Dim("("+ui.RelativeTime(row.entry.CheckedAt)+")") + "\n")

	for _, p := range []struct {
		label string
		eco   cache.Ecosystem
	}{
		{"composer", row.entry.Composer},
		{"npm", row.entry.NPM},
	} {
		if p.eco.Status == types.StatusNotApplicable {
			continue
		}
		b.WriteString("\n" + ui.Bold(p.label) + "\n")
		if p.eco.AuditPath != "" && p.eco.AuditPath != row.site.Path {
			b.WriteString("  " + ui.Dim("auditing: ") + p.eco.AuditPath + "\n")
		}
		if p.eco.Status == types.StatusErrored {
			b.WriteString("  " + ui.Failure("ERROR: ") + p.eco.Error + "\n")
			continue
		}
		b.WriteString("  " + ui.CountsSummaryLong(p.eco.Counts) + "\n")
		n := len(p.eco.Advisories)
		if n > 10 {
			n = 10
		}
		for i := 0; i < n; i++ {
			a := p.eco.Advisories[i]
			b.WriteString(fmt.Sprintf("   • %s (%s)\n     %s  %s\n",
				a.ID, ui.SeverityBadge(a.Severity), a.Package, a.Affected))
			if a.Title != "" {
				b.WriteString("     " + ui.Dim(a.Title) + "\n")
			}
		}
		if len(p.eco.Advisories) > 10 {
			b.WriteString(fmt.Sprintf("   … and %d more\n", len(p.eco.Advisories)-10))
		}
	}
	m.preview.SetContent(b.String())
	m.preview.GotoTop()
}

// compactSummary renders the sidebar's right-side summary, e.g. "11U" / "1C 2H".
func compactSummary(entry *cache.Entry) string {
	if entry == nil {
		return ""
	}
	merged := mergeCounts(entry.Composer.Counts, entry.NPM.Counts)
	return ui.CountsSummaryShort(merged)
}

func mergeCounts(a, b cache.Counts) cache.Counts {
	return cache.Counts{
		Critical: a.Critical + b.Critical,
		High:     a.High + b.High,
		Unknown:  a.Unknown + b.Unknown,
		Moderate: a.Moderate + b.Moderate,
		Low:      a.Low + b.Low,
		Info:     a.Info + b.Info,
	}
}

// ---- async refresh ----

func (m *model) refreshCurrent() tea.Cmd {
	if len(m.sites) == 0 {
		return nil
	}
	site := m.sites[m.cursor].site
	return m.startRefresh(site)
}

func (m *model) refreshAll() tea.Cmd {
	if len(m.sites) == 0 {
		return nil
	}
	cmds := make([]tea.Cmd, 0, len(m.sites))
	for _, row := range m.sites {
		cmds = append(cmds, m.startRefresh(row.site))
	}
	return tea.Batch(cmds...)
}

func (m *model) startRefresh(site config.Site) tea.Cmd {
	m.refreshing[site.Name] = true
	m.statusMsg = "refreshing " + site.Name
	cfg := m.cfg
	return func() tea.Msg {
		err := audit.RunSite(context.Background(), cfg.Settings, site)
		return refreshDoneMsg{name: site.Name, err: err}
	}
}

func helpLine() string {
	return "Keys: 1/2 focus pane, j/k or ↑/↓ move, r refresh site, R refresh all, ? help, q quit"
}

func max0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}
