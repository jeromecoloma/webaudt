// Package tui is the bubbletea-based interactive terminal UI for webaudt.
package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/wordwrap"
	"github.com/muesli/reflow/wrap"

	"github.com/jeromecoloma/webaudt/internal/audit"
	"github.com/jeromecoloma/webaudt/internal/cache"
	"github.com/jeromecoloma/webaudt/internal/config"
	"github.com/jeromecoloma/webaudt/internal/types"
	"github.com/jeromecoloma/webaudt/internal/ui"
)

const (
	paneSidebar = 0
	panePreview = 1

	sidebarWidth = 32 // content width inside borders+padding
)

func (m *model) chromeRows() int {
	// footer(1) + top border(1) + bottom border(1) + 1 row of bottom-clipping safety
	return 4
}

// Run launches the TUI.
func Run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	m := newModel(cfg)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

type model struct {
	cfg            *config.File
	sites          []siteRow
	cursor         int
	focus          int
	width, height  int
	previewScroll  int
	refreshing     map[string]bool
	statusMsg      string
	previewContent string // re-built whenever site/size changes
}

type siteRow struct {
	site  config.Site
	entry *cache.Entry
	worst string
}

func newModel(cfg *config.File) *model {
	m := &model{
		cfg:        cfg,
		refreshing: map[string]bool{},
		focus:      paneSidebar,
	}
	m.loadSites()
	return m
}

func (m *model) loadSites() {
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
		m.cursor = max(0, len(m.sites)-1)
	}
	m.rebuildPreview()
}

// ---- bubbletea Model ----

func (m *model) Init() tea.Cmd { return nil }

type refreshDoneMsg struct {
	name string
	err  error
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.rebuildPreview()
		return m, nil

	case refreshDoneMsg:
		delete(m.refreshing, msg.name)
		m.loadSites()
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
			m.statusMsg = "Keys: 1/2 pane · j/k or ↑↓ move · r refresh · R all · ? help · q quit"
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
					m.previewScroll = 0
					m.rebuildPreview()
				}
			case "k", "up":
				if m.cursor > 0 {
					m.cursor--
					m.previewScroll = 0
					m.rebuildPreview()
				}
			case "g", "home":
				m.cursor = 0
				m.previewScroll = 0
				m.rebuildPreview()
			case "G", "end":
				m.cursor = max(0, len(m.sites)-1)
				m.previewScroll = 0
				m.rebuildPreview()
			}
			return m, nil
		}
		// Preview pane focused: scroll content.
		switch msg.String() {
		case "j", "down":
			m.previewScroll++
		case "k", "up":
			if m.previewScroll > 0 {
				m.previewScroll--
			}
		case "ctrl+d", "pgdown":
			m.previewScroll += m.contentHeight() / 2
		case "ctrl+u", "pgup":
			m.previewScroll = max(0, m.previewScroll-m.contentHeight()/2)
		case "g", "home":
			m.previewScroll = 0
		case "G", "end":
			m.previewScroll = max(0, m.previewLineCount()-m.contentHeight())
		}
		return m, nil
	}
	return m, nil
}

func (m *model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	footer := m.renderFooter()

	sidebar := m.renderPane(paneSidebar, m.renderSidebarBody(), sidebarWidth)
	preview := m.renderPane(panePreview, m.renderPreviewBody(), m.previewWidth())

	body := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, preview)
	return lipgloss.JoinVertical(lipgloss.Left, body, footer)
}

// previewWidth = total width - sidebar's rendered width.
// Sidebar rendered width = sidebarWidth (content) + 2 (padding) + 2 (borders) = sidebarWidth + 4.
func (m *model) previewWidth() int {
	w := m.width - sidebarWidth - 4 - 4 // - sidebar chrome - own chrome
	if w < 20 {
		w = 20
	}
	return w
}

// contentHeight is the inner usable height of a pane (lines that fit between
// the top/bottom borders).
func (m *model) contentHeight() int {
	h := m.height - m.chromeRows()
	if h < 5 {
		h = 5
	}
	return h
}

// renderPane wraps a body string in a bordered box. No inline title — pane
// identifiers live in the footer to avoid getting cut off by terminal chrome.
func (m *model) renderPane(pane int, body string, contentWidth int) string {
	active := m.focus == pane
	borderColor := lipgloss.Color("244")
	if active {
		borderColor = lipgloss.Color("51")
	}

	wrapWidth := contentWidth - 2
	if wrapWidth < 8 {
		wrapWidth = contentWidth
	}
	var bodyLines []string
	for _, line := range strings.Split(body, "\n") {
		// wordwrap first (breaks on spaces), then wrap (hard-breaks long
		// unbreakable tokens like CVE version ranges so lipgloss.Width()
		// doesn't re-wrap them and overflow the pane's fixed Height).
		wrapped := wrap.String(wordwrap.String(line, wrapWidth), wrapWidth)
		parts := strings.Split(wrapped, "\n")
		bodyLines = append(bodyLines, parts[0])
		for _, cont := range parts[1:] {
			bodyLines = append(bodyLines, "  "+cont)
		}
	}
	maxLines := m.contentHeight()
	if len(bodyLines) > maxLines {
		bodyLines = bodyLines[:maxLines]
	}
	for len(bodyLines) < maxLines {
		bodyLines = append(bodyLines, " ")
	}
	body = strings.Join(bodyLines, "\n")

	// MaxHeight/MaxWidth clip the final rendered box (including borders) so a
	// stray long line or ambiguous-width unicode rune can't push the bottom
	// border off-screen. Height/Width set the minimum; MaxHeight/MaxWidth cap.
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(contentWidth).
		Height(maxLines).
		MaxWidth(contentWidth + 4).
		MaxHeight(maxLines + 2).
		AlignVertical(lipgloss.Top).
		Padding(0, 1).
		Render(body)
}

// ---- pane bodies ----

func (m *model) renderSidebarBody() string {
	if len(m.sites) == 0 {
		return ui.Dim("(no sites — run `webaudt add /path`)")
	}
	var lines []string
	for i, row := range m.sites {
		icon := ui.StatusIcon(row.worst)
		if m.refreshing[row.site.Name] {
			icon = ui.StatusIcon(types.SevRunning)
		}
		name := truncate(row.site.Name, sidebarWidth-6)
		prefix := "  "
		nameStyled := name
		if i == m.cursor {
			prefix = lipgloss.NewStyle().Foreground(lipgloss.Color("51")).Bold(true).Render("▸ ")
			nameStyled = lipgloss.NewStyle().Foreground(lipgloss.Color("51")).Bold(true).Render(name)
		}
		first := fmt.Sprintf("%s%s %s", prefix, icon, nameStyled)
		lines = append(lines, first)
		summary := compactSummary(row.entry)
		if summary != "" {
			lines = append(lines, "    "+ui.Dim(summary))
		}
	}
	return strings.Join(lines, "\n")
}

func (m *model) renderPreviewBody() string {
	if m.previewContent == "" {
		m.rebuildPreview()
	}
	// Apply scroll offset.
	lines := strings.Split(m.previewContent, "\n")
	if m.previewScroll >= len(lines) {
		m.previewScroll = max(0, len(lines)-1)
	}
	if m.previewScroll > 0 {
		lines = lines[m.previewScroll:]
	}
	return strings.Join(lines, "\n")
}

func (m *model) previewLineCount() int {
	return len(strings.Split(m.previewContent, "\n"))
}

// rebuildPreview regenerates the right-pane body for the currently-selected site.
func (m *model) rebuildPreview() {
	if len(m.sites) == 0 {
		m.previewContent = ui.Dim("no sites registered yet.")
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
		m.previewContent = b.String()
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
			b.WriteString(fmt.Sprintf("   • %s (%s)\n     %s\n", a.ID, ui.SeverityBadge(a.Severity), a.Package))
			if a.Affected != "" {
				b.WriteString("     " + ui.Dim(truncate(a.Affected, m.previewWidth()-8)) + "\n")
			}
			if a.Title != "" {
				b.WriteString("     " + ui.Dim(a.Title) + "\n")
			}
		}
		if len(p.eco.Advisories) > 10 {
			b.WriteString(fmt.Sprintf("   … and %d more\n", len(p.eco.Advisories)-10))
		}
	}
	m.previewContent = b.String()
}

func (m *model) renderFooter() string {
	hints := m.statusMsg
	if hints == "" {
		paneTag := func(n int, label string) string {
			marker := fmt.Sprintf("[%d]", n)
			if m.focus == n-1 {
				marker = lipgloss.NewStyle().Foreground(lipgloss.Color("51")).Bold(true).Render(marker)
				label = lipgloss.NewStyle().Foreground(lipgloss.Color("51")).Render(label)
			} else {
				marker = lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Render(marker)
				label = lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Render(label)
			}
			return marker + " " + label
		}
		hints = paneTag(1, "sites") + "  " + paneTag(2, "details") +
			lipgloss.NewStyle().Foreground(lipgloss.Color("244")).
				Render("   ·  j/k move · r refresh · R refresh all · ? help · q quit")
	}
	return "  " + hints
}

// compactSummary renders e.g. "11U" or "1C 2H".
func compactSummary(entry *cache.Entry) string {
	if entry == nil {
		return ""
	}
	merged := mergeCounts(entry.Composer.Counts, entry.NPM.Counts)
	s := ui.CountsSummaryShort(merged)
	if s == "clean" {
		return ""
	}
	return s
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
	return m.startRefresh(m.sites[m.cursor].site)
}

func (m *model) refreshAll() tea.Cmd {
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

func truncate(s string, n int) string {
	if n <= 0 {
		return s
	}
	if len(s) <= n {
		return s
	}
	if n < 3 {
		return s[:n]
	}
	return s[:n-1] + "…"
}
