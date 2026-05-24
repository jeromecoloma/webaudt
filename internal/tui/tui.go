// Package tui is the bubbletea-based interactive terminal UI for webaudt.
package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
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
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err = p.Run()
	return err
}

type model struct {
	cfg            *config.File
	sites          []siteRow
	cursor         int
	focus          int
	width, height  int
	refreshing     map[string]bool
	statusMsg      string
	previewContent      string // raw, unwrapped — viewport handles the wrap+scroll
	preview             viewport.Model
	previewReady        bool
	previewWrappedTotal int // total lines after wrap, for scrollbar math

	// filter modal
	filterOpen    bool
	filterInput   textinput.Model
	filterMatches []int // indices into m.sites
	filterCursor  int   // index into filterMatches
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
		m.resizePreview()
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

	case tea.MouseMsg:
		if m.filterOpen {
			return m, nil
		}
		var cmd tea.Cmd
		m.preview, cmd = m.preview.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		if m.filterOpen {
			return m.updateFilter(msg)
		}
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "q", "esc":
			return m, tea.Quit
		case "/":
			m.openFilter()
			return m, textinput.Blink
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
			m.statusMsg = "Keys: 1/2 pane · j/k or ↑↓ move · / filter · r refresh · R all · ? help · q quit"
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
					m.rebuildPreview()
				}
			case "k", "up":
				if m.cursor > 0 {
					m.cursor--
					m.rebuildPreview()
				}
			case "g", "home":
				m.cursor = 0
				m.rebuildPreview()
			case "G", "end":
				m.cursor = max(0, len(m.sites)-1)
				m.rebuildPreview()
			}
			return m, nil
		}
		// Preview pane focused: forward to viewport.
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
	footer := m.renderFooter()

	sidebar := m.renderPane(paneSidebar, m.renderSidebarBody(), sidebarWidth)
	preview := m.renderPreviewPane()

	body := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, preview)
	if m.filterOpen {
		body = overlayCenter(body, m.renderFilterModal())
	}
	return lipgloss.JoinVertical(lipgloss.Left, body, footer)
}

// ---- filter modal ----

func (m *model) openFilter() {
	ti := textinput.New()
	ti.Placeholder = "filter sites…"
	ti.Prompt = "› "
	ti.CharLimit = 64
	ti.Width = 30
	ti.Focus()
	m.filterInput = ti
	m.filterOpen = true
	m.filterCursor = 0
	m.recomputeFilterMatches()
}

func (m *model) closeFilter() {
	m.filterOpen = false
	m.filterInput.Blur()
	m.filterMatches = nil
}

func (m *model) recomputeFilterMatches() {
	q := strings.ToLower(strings.TrimSpace(m.filterInput.Value()))
	m.filterMatches = m.filterMatches[:0]
	for i, row := range m.sites {
		if q == "" || strings.Contains(strings.ToLower(row.site.Name), q) || strings.Contains(strings.ToLower(row.site.Path), q) {
			m.filterMatches = append(m.filterMatches, i)
		}
	}
	if m.filterCursor >= len(m.filterMatches) {
		m.filterCursor = max(0, len(m.filterMatches)-1)
	}
}

func (m *model) updateFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.closeFilter()
		return m, nil
	case "enter":
		if len(m.filterMatches) > 0 {
			m.cursor = m.filterMatches[m.filterCursor]
			m.focus = paneSidebar
			m.rebuildPreview()
		}
		m.closeFilter()
		return m, nil
	case "down", "ctrl+n", "tab":
		if m.filterCursor < len(m.filterMatches)-1 {
			m.filterCursor++
		}
		return m, nil
	case "up", "ctrl+p", "shift+tab":
		if m.filterCursor > 0 {
			m.filterCursor--
		}
		return m, nil
	}
	prev := m.filterInput.Value()
	var cmd tea.Cmd
	m.filterInput, cmd = m.filterInput.Update(msg)
	if m.filterInput.Value() != prev {
		m.filterCursor = 0
		m.recomputeFilterMatches()
	}
	return m, cmd
}

func (m *model) renderFilterModal() string {
	width := 48
	if width > m.width-4 {
		width = m.width - 4
	}
	maxRows := 10
	if h := m.height - 8; h < maxRows {
		maxRows = h
	}
	if maxRows < 3 {
		maxRows = 3
	}

	var b strings.Builder
	b.WriteString(ui.Bold("Filter sites"))
	b.WriteString("\n")
	b.WriteString(m.filterInput.View())
	b.WriteString("\n")

	if len(m.filterMatches) == 0 {
		b.WriteString(ui.Dim("  (no matches)"))
	} else {
		start := 0
		if m.filterCursor >= maxRows {
			start = m.filterCursor - maxRows + 1
		}
		end := start + maxRows
		if end > len(m.filterMatches) {
			end = len(m.filterMatches)
		}
		for i := start; i < end; i++ {
			idx := m.filterMatches[i]
			row := m.sites[idx]
			icon := ui.StatusIcon(row.worst)
			name := truncate(row.site.Name, width-8)
			line := fmt.Sprintf("  %s %s", icon, name)
			if i == m.filterCursor {
				line = lipgloss.NewStyle().
					Foreground(lipgloss.Color("51")).Bold(true).
					Render(fmt.Sprintf("▸ %s %s", icon, name))
			}
			b.WriteString(line)
			if i < end-1 {
				b.WriteString("\n")
			}
		}
		if len(m.filterMatches) > maxRows {
			b.WriteString("\n" + ui.Dim(fmt.Sprintf("  %d/%d matches", m.filterCursor+1, len(m.filterMatches))))
		}
	}
	b.WriteString("\n" + ui.Dim("↑/↓ move · enter select · esc cancel"))

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("51")).
		Padding(0, 1).
		Width(width).
		Render(b.String())
}

// overlayCenter places `over` centered on top of `bg`, replacing the lines
// behind it. Both inputs are pre-rendered lipgloss strings.
func overlayCenter(bg, over string) string {
	bgLines := strings.Split(bg, "\n")
	overLines := strings.Split(over, "\n")
	bgH := len(bgLines)
	overH := len(overLines)
	if overH > bgH {
		return over
	}
	overW := 0
	for _, l := range overLines {
		if w := lipgloss.Width(l); w > overW {
			overW = w
		}
	}
	bgW := 0
	for _, l := range bgLines {
		if w := lipgloss.Width(l); w > bgW {
			bgW = w
		}
	}
	top := (bgH - overH) / 2
	left := (bgW - overW) / 2
	if left < 0 {
		left = 0
	}
	out := make([]string, bgH)
	copy(out, bgLines)
	for i, ol := range overLines {
		row := top + i
		if row < 0 || row >= bgH {
			continue
		}
		bgLine := bgLines[row]
		bgLineW := lipgloss.Width(bgLine)
		// Right pad bg line to bgW.
		padded := bgLine
		if bgLineW < bgW {
			padded += strings.Repeat(" ", bgW-bgLineW)
		}
		// Slice cells: prefix + over + suffix. Use simple byte slicing as a
		// best-effort — overlay sits in the middle where ASCII is the norm in
		// this app's rendered chrome.
		prefix := truncateCells(padded, left)
		suffix := dropCells(padded, left+overW)
		out[row] = prefix + ol + suffix
	}
	return strings.Join(out, "\n")
}

// truncateCells keeps the first n display cells of s (ANSI-aware via lipgloss
// width estimates on byte windows).
func truncateCells(s string, n int) string {
	if n <= 0 {
		return ""
	}
	// Walk runes accumulating until we hit n cells. ANSI escapes pass through.
	var b strings.Builder
	var cells int
	inEsc := false
	for _, r := range s {
		if inEsc {
			b.WriteRune(r)
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		if r == 0x1b {
			inEsc = true
			b.WriteRune(r)
			continue
		}
		w := lipgloss.Width(string(r))
		if cells+w > n {
			break
		}
		b.WriteRune(r)
		cells += w
	}
	for cells < n {
		b.WriteByte(' ')
		cells++
	}
	return b.String()
}

// dropCells drops the first n display cells of s and returns the rest.
func dropCells(s string, n int) string {
	var b strings.Builder
	var cells int
	inEsc := false
	for _, r := range s {
		if inEsc {
			if cells >= n {
				b.WriteRune(r)
			}
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		if r == 0x1b {
			inEsc = true
			if cells >= n {
				b.WriteRune(r)
			}
			continue
		}
		w := lipgloss.Width(string(r))
		if cells >= n {
			b.WriteRune(r)
			cells += w
			continue
		}
		cells += w
	}
	return b.String()
}

// previewWidth = total width - sidebar's rendered width - own border chrome.
// lipgloss .Width(W) already accounts for padding; only the 2 border cols are
// added on top of W, so each pane's visual width is W + 2.
func (m *model) previewWidth() int {
	w := m.width - sidebarWidth - 2 - 2 // - sidebar chrome (border) - own border
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

// injectFieldsetTitle overwrites the top border of a rendered lipgloss box with
// a right-aligned fieldset-style inline label: "╭──...── label ──╮".
func injectFieldsetTitle(rendered, label string, borderColor lipgloss.Color) string {
	if label == "" {
		return rendered
	}
	lines := strings.Split(rendered, "\n")
	if len(lines) == 0 {
		return rendered
	}
	top := lines[0]
	totalW := lipgloss.Width(top)
	if totalW < 12 {
		return rendered
	}

	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	dashStyle := lipgloss.NewStyle().Foreground(borderColor)

	inline := " " + labelStyle.Render(label) + " "
	inlineW := lipgloss.Width(inline)

	// caps (2) + leading dashes (>=2) + inline + trailing dashes (2)
	leading := totalW - 2 - 2 - inlineW
	if leading < 2 {
		return rendered
	}
	newTop := dashStyle.Render("╭"+strings.Repeat("─", leading)) + inline + dashStyle.Render("──╮")
	lines[0] = newTop
	return strings.Join(lines, "\n")
}

// renderPane wraps a body string in a bordered box with a fieldset-style title
// overlaid on the top border (label + optional badge).
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

	rendered := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(contentWidth).
		Height(maxLines).
		MaxWidth(contentWidth + 4).
		MaxHeight(maxLines + 2).
		AlignVertical(lipgloss.Top).
		Padding(0, 1).
		Render(body)

	return injectFieldsetTitle(rendered, m.fieldsetTitle(pane), borderColor)
}

// fieldsetTitle returns the label shown on a pane's top border.
func (m *model) fieldsetTitle(pane int) string {
	switch pane {
	case paneSidebar:
		return "Sites"
	case panePreview:
		return "Details"
	}
	return ""
}

// renderPreviewPane renders the preview viewport inside a bordered box,
// alongside a scrollbar column on the right when content overflows.
func (m *model) renderPreviewPane() string {
	active := m.focus == panePreview
	borderColor := lipgloss.Color("244")
	if active {
		borderColor = lipgloss.Color("51")
	}

	contentWidth := m.previewWidth()
	maxLines := m.contentHeight()

	lines := strings.Split(m.preview.View(), "\n")
	var bar []string
	if m.previewWrappedTotal > m.preview.Height {
		bar = scrollbarColumn(m.preview.YOffset, m.previewWrappedTotal, m.preview.Height, active)
	}
	for i := range lines {
		w := lipgloss.Width(lines[i])
		if w < m.preview.Width {
			lines[i] += strings.Repeat(" ", m.preview.Width-w)
		}
		if i < len(bar) {
			lines[i] += bar[i]
		}
	}
	body := strings.Join(lines, "\n")

	rendered := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(contentWidth).
		Height(maxLines).
		MaxWidth(contentWidth + 4).
		MaxHeight(maxLines + 2).
		AlignVertical(lipgloss.Top).
		Padding(0, 1).
		Render(body)

	return injectFieldsetTitle(rendered, m.fieldsetTitle(panePreview), borderColor)
}

// scrollbarColumn returns one " │"/" ┃" string per visible row. Thumb size
// and position track the viewport's window over the full wrapped content.
func scrollbarColumn(offset, total, visible int, active bool) []string {
	if visible <= 0 || total <= visible {
		return nil
	}
	thumbSize := visible * visible / total
	if thumbSize < 1 {
		thumbSize = 1
	}
	denom := total - visible
	if denom < 1 {
		denom = 1
	}
	thumbStart := offset * (visible - thumbSize) / denom
	thumbEnd := thumbStart + thumbSize

	thumbColor := lipgloss.Color("244")
	if active {
		thumbColor = lipgloss.Color("51")
	}
	thumbStyle := lipgloss.NewStyle().Foreground(thumbColor)
	trackStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("237"))

	out := make([]string, visible)
	for i := 0; i < visible; i++ {
		if i >= thumbStart && i < thumbEnd {
			out[i] = " " + thumbStyle.Render("┃")
		} else {
			out[i] = " " + trackStyle.Render("│")
		}
	}
	return out
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

// resizePreview (re)creates or resizes the viewport to fit the current pane
// dimensions, then re-applies the wrapped content. The pane's content area is
// previewWidth-2 (lipgloss Padding(0,1) eats 2 cols); subtract 2 more for the
// scrollbar column.
func (m *model) resizePreview() {
	w := m.previewWidth() - 4
	if w < 8 {
		w = 8
	}
	h := m.contentHeight()
	if !m.previewReady {
		m.preview = viewport.New(w, h)
		m.previewReady = true
	} else {
		m.preview.Width = w
		m.preview.Height = h
	}
	// Rebuild so package tables size to the new viewport width.
	m.rebuildPreview()
}

// setPreviewContent wraps m.previewContent for the current viewport width,
// updates the viewport, and records the wrapped line count for the scrollbar.
func (m *model) setPreviewContent() {
	if !m.previewReady {
		return
	}
	wrapWidth := m.preview.Width
	if wrapWidth < 8 {
		wrapWidth = 8
	}
	// Wrap prose lines, but pass table-border lines through untouched so the
	// rounded borders survive.
	var out []string
	for _, line := range strings.Split(m.previewContent, "\n") {
		if isTableLine(line) {
			out = append(out, line)
			continue
		}
		w := wrap.String(wordwrap.String(line, wrapWidth), wrapWidth)
		out = append(out, w)
	}
	wrapped := strings.TrimRight(strings.Join(out, "\n"), "\n")
	m.preview.SetContent(wrapped)
	if wrapped == "" {
		m.previewWrappedTotal = 0
	} else {
		m.previewWrappedTotal = strings.Count(wrapped, "\n") + 1
	}
}

// rebuildPreview regenerates the right-pane body for the currently-selected site.
func (m *model) rebuildPreview() {
	if len(m.sites) == 0 {
		m.previewContent = ui.Dim("no sites registered yet.")
		return
	}
	row := m.sites[m.cursor]
	var b strings.Builder
	b.WriteString(ui.Legend() + "\n")
	b.WriteString(ui.Dim(strings.Repeat("─", 40)) + "\n")
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

	// Site-wide counts header.
	total := mergeCounts(row.entry.Composer.Counts, row.entry.NPM.Counts)
	b.WriteString("\n" + ui.CountsBadges(total) + "\n")

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
		b.WriteString("\n" + ui.Bold(p.label) + "  " + ui.CountsSummaryLong(p.eco.Counts) + "\n")
		if p.eco.AuditPath != "" && p.eco.AuditPath != row.site.Path {
			b.WriteString(ui.Dim("auditing: ") + p.eco.AuditPath + "\n")
		}
		if p.eco.Status == types.StatusErrored {
			b.WriteString("  " + ui.Failure("ERROR: ") + p.eco.Error + "\n")
			continue
		}
		if len(p.eco.Advisories) == 0 {
			continue
		}
		// Group by package, preserving first-seen order.
		groups := groupAdvisoriesByPackage(p.eco.Advisories)
		const perPkgLimit = 6
		for _, g := range groups {
			b.WriteString("\n" + ui.Bold(g.pkg) + "\n")
			b.WriteString(m.renderPackageTable(g, perPkgLimit) + "\n")
			if len(g.items) > perPkgLimit {
				b.WriteString(ui.Dim(fmt.Sprintf("  … and %d more in this package", len(g.items)-perPkgLimit)) + "\n")
			}
		}
	}
	m.previewContent = b.String()
	if m.previewReady {
		m.preview.GotoTop()
		m.setPreviewContent()
	}
}

type advGroup struct {
	pkg   string
	items []cache.Advisory
}

// isTableLine returns true if the rendered line is part of a lipgloss table
// (contains box-drawing chars). Such lines must not be re-wrapped.
func isTableLine(s string) bool {
	for _, r := range s {
		switch r {
		case '─', '│', '╭', '╮', '╰', '╯', '├', '┤', '┬', '┴', '┼':
			return true
		}
	}
	return false
}

// renderPackageTable renders one bordered table containing up to `limit`
// advisories for a package. Each row has two stacked lines: "CVE [severity]"
// on top, title underneath.
func (m *model) renderPackageTable(g advGroup, limit int) string {
	n := len(g.items)
	if n > limit {
		n = limit
	}
	width := m.preview.Width
	if width <= 0 {
		width = m.previewWidth() - 4
	}
	if width < 24 {
		width = 24
	}
	rows := make([][]string, 0, n)
	for i := 0; i < n; i++ {
		a := g.items[i]
		id := a.ID
		if id == "" {
			id = "(no id)"
		}
		header := id + "  [" + ui.SeverityBadge(a.Severity) + "]"
		title := stripCVEPrefix(a.Title, a.ID)
		if title == "" {
			title = ui.Dim("(no title)")
		} else {
			title = ui.Dim(title)
		}
		affects := ""
		if a.Affected != "" {
			affects = ui.Dim("affects: " + compactVersionRange(a.Affected))
		}
		cell := header + "\n" + title
		if affects != "" {
			cell += "\n" + affects
		}
		rows = append(rows, []string{cell})
	}
	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("244"))).
		BorderRow(true).
		BorderColumn(true).
		Width(width).
		Wrap(true).
		Rows(rows...).
		StyleFunc(func(_, _ int) lipgloss.Style {
			return lipgloss.NewStyle().Padding(0, 1)
		})
	return t.Render()
}

// stripCVEPrefix removes a leading "CVE-...: " prefix from a title if it
// just repeats the advisory ID we already printed.
func stripCVEPrefix(title, id string) string {
	if id == "" {
		return title
	}
	prefix := id + ": "
	if strings.HasPrefix(title, prefix) {
		return strings.TrimPrefix(title, prefix)
	}
	return title
}

func groupAdvisoriesByPackage(advs []cache.Advisory) []advGroup {
	idx := map[string]int{}
	var out []advGroup
	for _, a := range advs {
		if i, ok := idx[a.Package]; ok {
			out[i].items = append(out[i].items, a)
			continue
		}
		idx[a.Package] = len(out)
		out = append(out, advGroup{pkg: a.Package, items: []cache.Advisory{a}})
	}
	return out
}

// compactVersionRange collapses composer/npm-style affected-version blobs
// (e.g. ">=6.1.0,<6.2.0|>=6.2.0,<6.3.0|...") into a short summary like
// "6.1.0 → 8.0.12 (8 ranges)". Falls back to the raw string if no version
// tokens are detected.
func compactVersionRange(s string) string {
	versions := versionTokens(s)
	if len(versions) == 0 {
		return s
	}
	first := versions[0]
	last := versions[len(versions)-1]
	// count comma- or pipe-separated ranges as a proxy
	ranges := strings.Count(s, ",") + strings.Count(s, "|") + 1
	if first == last {
		return first
	}
	return fmt.Sprintf("%s → %s (%d range%s)", first, last, ranges, plural(ranges))
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// versionTokens extracts dotted-number tokens (e.g. 6.1.0, 8.0.12) in order.
func versionTokens(s string) []string {
	var out []string
	var cur strings.Builder
	flush := func() {
		v := cur.String()
		cur.Reset()
		if strings.Count(v, ".") >= 1 {
			out = append(out, v)
		}
	}
	for _, r := range s {
		if (r >= '0' && r <= '9') || r == '.' {
			cur.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return out
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
				Render("   ·  j/k move · / filter · r refresh · R all · ? help · q quit")
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
