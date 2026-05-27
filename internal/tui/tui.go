// Package tui is the bubbletea-based interactive terminal UI for webaudt.
package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
	"github.com/jeromecoloma/webaudt/internal/registry"
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
	cfg                 *config.File
	sites               []siteRow
	cursor              int
	focus               int
	width, height       int
	refreshing          map[string]bool
	statusMsg           string
	previewContent      string // raw, unwrapped — viewport handles the wrap+scroll
	preview             viewport.Model
	previewReady        bool
	previewWrappedTotal int // total lines after wrap, for scrollbar math
	sidebarOffset       int // top line of the sidebar viewport, for scroll

	// filter modal
	filterOpen    bool
	filterInput   textinput.Model
	filterMatches []int // indices into m.sites
	filterCursor  int   // index into filterMatches

	// add modal
	addOpen       bool
	addStage      int // 0 = path input, 1 = confirm resolved
	addInput      textinput.Model
	addError      string
	addResolution *registry.Resolution
	addCands      []string // tab-completion candidates (basenames)
	addCandDir    string   // dir those candidates live in

	// remove confirmation modal
	removeOpen bool

	// help modal
	helpOpen bool

	// error modal
	errorOpen  bool
	errorTitle string
	errorMsg   string
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

// moveSite shifts the site under the cursor by delta (-1 = up, +1 = down),
// persists the new order to config.toml, and keeps the cursor on that site.
func (m *model) moveSite(delta int) {
	if len(m.sites) < 2 {
		return
	}
	j := m.cursor + delta
	if j < 0 || j >= len(m.sites) {
		return
	}
	m.sites[m.cursor], m.sites[j] = m.sites[j], m.sites[m.cursor]
	m.cfg.Sites[m.cursor], m.cfg.Sites[j] = m.cfg.Sites[j], m.cfg.Sites[m.cursor]
	m.cursor = j
	if err := m.cfg.Save(); err != nil {
		m.statusMsg = "save config: " + err.Error()
		return
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
		if m.filterOpen || m.addOpen || m.removeOpen || m.helpOpen || m.errorOpen {
			return m, nil
		}
		var cmd tea.Cmd
		m.preview, cmd = m.preview.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		if m.filterOpen {
			return m.updateFilter(msg)
		}
		if m.addOpen {
			return m.updateAdd(msg)
		}
		if m.removeOpen {
			return m.updateRemove(msg)
		}
		if m.helpOpen {
			switch msg.String() {
			case "esc", "q", "?", "enter":
				m.helpOpen = false
			}
			return m, nil
		}
		if m.errorOpen {
			switch msg.String() {
			case "esc", "q", "enter":
				m.errorOpen = false
			}
			return m, nil
		}
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "q", "esc":
			return m, tea.Quit
		case "/":
			m.openFilter()
			return m, textinput.Blink
		case "a":
			m.openAdd()
			return m, textinput.Blink
		case "d", "delete":
			if len(m.sites) > 0 {
				m.removeOpen = true
				m.statusMsg = ""
			}
			return m, nil
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
			m.helpOpen = true
			m.statusMsg = ""
			return m, nil
		case "r":
			return m, m.refreshCurrent()
		case "R":
			return m, m.refreshAll()
		case "o":
			m.openInTmuxWindow()
			return m, nil
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
			case "K", "shift+up":
				m.moveSite(-1)
			case "J", "shift+down":
				m.moveSite(+1)
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

	sidebar := m.renderSidebarPane()
	preview := m.renderPreviewPane()

	body := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, preview)
	modalOpen := m.filterOpen || m.addOpen || m.removeOpen || m.helpOpen || m.errorOpen
	if modalOpen {
		body = dimBackground(body)
	}
	if m.filterOpen {
		body = overlayCenter(body, m.renderFilterModal())
	}
	if m.addOpen {
		body = overlayCenter(body, m.renderAddModal())
	}
	if m.removeOpen {
		body = overlayCenter(body, m.renderRemoveModal())
	}
	if m.helpOpen {
		body = overlayCenter(body, m.renderHelpModal())
	}
	if m.errorOpen {
		body = overlayCenter(body, m.renderErrorModal())
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

// openInTmuxWindow opens a new tmux window in the selected site's directory.
// If webaudt is not running inside a tmux session, surfaces an error modal.
func (m *model) openInTmuxWindow() {
	if len(m.sites) == 0 {
		return
	}
	if os.Getenv("TMUX") == "" {
		m.errorTitle = "Not running in tmux"
		m.errorMsg = "“Open in another window” requires webaudt to be running inside a tmux session.\n\nStart a tmux session (e.g. `tmux new -s work`) and re-run webaudt from inside it."
		m.errorOpen = true
		return
	}
	row := m.sites[m.cursor]
	cmd := exec.Command("tmux", "new-window", "-c", row.site.Path, "-n", row.site.Name)
	if out, err := cmd.CombinedOutput(); err != nil {
		m.errorTitle = "tmux new-window failed"
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		m.errorMsg = msg
		m.errorOpen = true
		return
	}
	m.statusMsg = "opened " + row.site.Name + " in new tmux window"
}

// renderErrorModal shows a generic error/info dialog dismissed with esc/q/enter.
func (m *model) renderErrorModal() string {
	width := 60
	if width > m.width-4 {
		width = m.width - 4
	}
	var b strings.Builder
	title := m.errorTitle
	if title == "" {
		title = "Error"
	}
	b.WriteString(ui.Failure(title))
	b.WriteString("\n\n")
	b.WriteString(m.errorMsg)
	b.WriteString("\n\n")
	b.WriteString(ui.Dim("enter/esc/q dismiss"))
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("196")).
		Padding(0, 1).
		Width(width).
		Render(b.String())
}

// renderHelpModal shows the full keybinding list, grouped — description on
// the left, key right-aligned. OpenCode-style. Dismiss with ?, esc, q, enter.
func (m *model) renderHelpModal() string {
	type row struct{ desc, key string }
	groups := []struct {
		title string
		rows  []row
	}{
		{"Navigation", []row{
			{"Focus sites pane", "1"},
			{"Focus details pane", "2"},
			{"Cycle panes", "tab"},
			{"Move down", "j"},
			{"Move up", "k"},
			{"Jump to first", "g"},
			{"Jump to last", "G"},
		}},
		{"Sites", []row{
			{"Reorder down", "J"},
			{"Reorder up", "K"},
			{"Filter", "/"},
			{"Add", "a"},
			{"Remove", "d"},
			{"Refresh selected", "r"},
			{"Refresh all", "R"},
			{"Open in another window (tmux)", "o"},
		}},
		{"App", []row{
			{"Toggle this help", "?"},
			{"Quit", "q"},
		}},
	}

	innerW := 44 // width of the content row (desc ... key)
	descW, keyW := 0, 0
	for _, g := range groups {
		for _, r := range g.rows {
			if len(r.desc) > descW {
				descW = len(r.desc)
			}
			if len(r.key) > keyW {
				keyW = len(r.key)
			}
		}
	}
	if descW+keyW+2 > innerW {
		innerW = descW + keyW + 4
	}

	titleStyle := lipgloss.NewStyle().Bold(true)
	headStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("141")).Bold(true)
	keyStyle := ui.Dim
	descStyle := lipgloss.NewStyle()

	rowLine := func(left, right string) string {
		pad := innerW - lipgloss.Width(left) - lipgloss.Width(right)
		if pad < 1 {
			pad = 1
		}
		return left + strings.Repeat(" ", pad) + right
	}

	var b strings.Builder
	b.WriteString(rowLine(titleStyle.Render("Keybindings"), keyStyle("esc")))
	b.WriteString("\n")
	for _, g := range groups {
		b.WriteString("\n" + headStyle.Render(g.title) + "\n")
		for _, r := range g.rows {
			b.WriteString(rowLine(descStyle.Render(r.desc), keyStyle(r.key)) + "\n")
		}
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("51")).
		Padding(1, 2).
		Render(strings.TrimRight(b.String(), "\n"))
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

// dimBackground strips ANSI styling from s and re-renders it in a dark gray,
// simulating the darker backdrop shown behind modal overlays.
func dimBackground(s string) string {
	stripped := stripANSI(s)
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("237"))
	lines := strings.Split(stripped, "\n")
	for i, l := range lines {
		lines[i] = style.Render(l)
	}
	return strings.Join(lines, "\n")
}

// stripANSI removes CSI escape sequences (\x1b[...m and friends) from s.
func stripANSI(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inEsc := false
	for _, r := range s {
		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		if r == 0x1b {
			inEsc = true
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
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
		MaxWidth(contentWidth+4).
		MaxHeight(maxLines+2).
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

// sidebarLines builds the sidebar rows and returns the first line index of the
// row currently under the cursor (for scroll math).
func (m *model) sidebarLines() ([]string, int) {
	if len(m.sites) == 0 {
		return []string{ui.Dim("(no sites — run `webaudt add /path`)")}, 0
	}
	var lines []string
	cursorLine := 0
	for i, row := range m.sites {
		icon := ui.StatusIcon(row.worst)
		if m.refreshing[row.site.Name] {
			icon = ui.StatusIcon(types.SevRunning)
		}
		// listWidth in renderSidebarPane is sidebarWidth-4; the row prefix
		// ("  ○ " or "▸ ○ ") consumes 4 cells before the name, so cap at -8.
		name := truncate(row.site.Name, sidebarWidth-8)
		prefix := "  "
		nameStyled := name
		if i == m.cursor {
			cursorLine = len(lines)
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
	return lines, cursorLine
}

// renderSidebarPane renders the Sites list inside a bordered box, slicing the
// visible window around the cursor and drawing a scrollbar column on the right
// when content overflows.
func (m *model) renderSidebarPane() string {
	active := m.focus == paneSidebar
	borderColor := lipgloss.Color("244")
	if active {
		borderColor = lipgloss.Color("51")
	}

	contentWidth := sidebarWidth
	visible := m.contentHeight()
	// .Width(contentWidth).Padding(0,1) leaves contentWidth-2 inner cells;
	// reserve another 2 for the scrollbar column (" │") on the right.
	listWidth := contentWidth - 4
	if listWidth < 8 {
		listWidth = 8
	}

	all, cursorLine := m.sidebarLines()

	// Wrap each line to listWidth so the scrollbar column doesn't get pushed.
	var wrapped []string
	// Track where each source line lands after wrapping so we can map cursorLine.
	wrappedCursor := 0
	for i, line := range all {
		w := wrap.String(wordwrap.String(line, listWidth), listWidth)
		parts := strings.Split(w, "\n")
		if i == cursorLine {
			wrappedCursor = len(wrapped)
		}
		wrapped = append(wrapped, parts[0])
		for _, cont := range parts[1:] {
			wrapped = append(wrapped, "  "+cont)
		}
	}

	total := len(wrapped)
	// Clamp offset so cursor stays visible.
	if wrappedCursor < m.sidebarOffset {
		m.sidebarOffset = wrappedCursor
	}
	if wrappedCursor >= m.sidebarOffset+visible {
		m.sidebarOffset = wrappedCursor - visible + 1
	}
	if m.sidebarOffset > total-visible {
		m.sidebarOffset = total - visible
	}
	if m.sidebarOffset < 0 {
		m.sidebarOffset = 0
	}

	end := m.sidebarOffset + visible
	if end > total {
		end = total
	}
	view := wrapped[m.sidebarOffset:end]
	for len(view) < visible {
		view = append(view, "")
	}

	var bar []string
	if total > visible {
		bar = scrollbarColumn(m.sidebarOffset, total, visible, active)
	}
	for i := range view {
		w := lipgloss.Width(view[i])
		if w < listWidth {
			view[i] += strings.Repeat(" ", listWidth-w)
		}
		if i < len(bar) {
			view[i] += bar[i]
		}
	}
	body := strings.Join(view, "\n")

	rendered := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(contentWidth).
		Height(visible).
		MaxWidth(contentWidth+4).
		MaxHeight(visible+2).
		AlignVertical(lipgloss.Top).
		Padding(0, 1).
		Render(body)

	return injectFieldsetTitle(rendered, m.fieldsetTitle(paneSidebar), borderColor)
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
	defer func() {
		if m.previewReady {
			m.preview.GotoTop()
			m.setPreviewContent()
		}
	}()
	if len(m.sites) == 0 {
		m.previewContent = ui.Dim("no sites registered yet.")
		return
	}
	row := m.sites[m.cursor]
	var b strings.Builder
	b.WriteString(ui.Legend() + "\n")
	sepW := m.preview.Width
	if sepW <= 0 {
		sepW = 40
	}
	b.WriteString(ui.Dim(strings.Repeat("─", sepW)) + "\n")
	b.WriteString(ui.Name(row.site.Name))
	b.WriteString("\n")
	b.WriteString(ui.Dim("path:  ") + row.site.Path + "\n")
	typeLabel := string(row.site.Type)
	if row.site.Type == types.TypeBoth {
		typeLabel = "composer+npm"
	}
	b.WriteString(ui.Dim("type:  ") + typeLabel + "\n")

	if row.entry == nil {
		b.WriteString("\n" + ui.Warn("NOT YET AUDITED") + "\n")
		b.WriteString("\n" + ui.Dim("This site has not been audited yet.") + "\n")
		b.WriteString(ui.Dim("Press ") + ui.Bold("r") + ui.Dim(" to run an audit for this site,") + "\n")
		b.WriteString(ui.Dim("or ") + ui.Bold("R") + ui.Dim(" to audit all sites.") + "\n")
		m.previewContent = b.String()
		return
	}

	b.WriteString(ui.Dim("checked: ") + ui.AbsTime(row.entry.CheckedAt) + " " + ui.Dim("("+ui.RelativeTime(row.entry.CheckedAt)+")") + "\n")

	// Site-wide counts header.
	total := mergeCounts(row.entry.Composer.Counts, row.entry.NPM.Counts)
	header := ui.CountsBadges(total)
	if total.Total() == 0 && (row.entry.Composer.Status == types.StatusErrored || row.entry.NPM.Status == types.StatusErrored) {
		header = ui.SeverityBadge(types.SevError)
	}
	b.WriteString("\n" + header + "\n")

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
		summary := ui.CountsSummaryLong(p.eco.Counts)
		if p.eco.Status == types.StatusErrored {
			summary = ui.SeverityBadge(types.SevError)
		}
		b.WriteString("\n" + ui.Bold(p.label) + "  " + summary + "\n")
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
			b.WriteString("\n " + ui.Bold(g.pkg) + "\n")
			b.WriteString(m.renderPackageTable(g, perPkgLimit) + "\n")
			if len(g.items) > perPkgLimit {
				b.WriteString(ui.Dim(fmt.Sprintf("  … and %d more in this package", len(g.items)-perPkgLimit)) + "\n")
			}
		}
	}
	m.previewContent = b.String()
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
				Render("   ·  a add · ? help · q quit")
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

// ---- add modal ----

func (m *model) openAdd() {
	ti := textinput.New()
	ti.Placeholder = "/path/to/site"
	ti.Prompt = "› "
	ti.CharLimit = 512
	ti.Width = 44
	ti.Focus()
	m.addInput = ti
	m.addOpen = true
	m.addStage = 0
	m.addError = ""
	m.addResolution = nil
	m.addCands = nil
	m.addCandDir = ""
}

func (m *model) closeAdd() {
	m.addOpen = false
	m.addInput.Blur()
	m.addError = ""
	m.addResolution = nil
	m.addStage = 0
	m.addCands = nil
	m.addCandDir = ""
}

func (m *model) updateAdd(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.closeAdd()
		return m, nil
	}
	// "q" closes the confirm stage too; in the input stage it must remain a
	// typeable character.
	if m.addStage == 1 && msg.String() == "q" {
		m.closeAdd()
		return m, nil
	}

	if m.addStage == 0 {
		if msg.String() == "tab" {
			m.completeAddPath()
			return m, nil
		}
		if msg.String() == "enter" {
			path := strings.TrimSpace(m.addInput.Value())
			if path == "" {
				m.addError = "path is required"
				return m, nil
			}
			res, err := registry.Resolve(m.cfg, registry.AddOptions{Path: path})
			if err != nil {
				m.addError = err.Error()
				return m, nil
			}
			m.addResolution = res
			m.addError = ""
			m.addStage = 1
			return m, nil
		}
		prev := m.addInput.Value()
		var cmd tea.Cmd
		m.addInput, cmd = m.addInput.Update(msg)
		if m.addInput.Value() != prev {
			m.addCands = nil
			m.addCandDir = ""
		}
		return m, cmd
	}

	// stage 1: confirm
	switch msg.String() {
	case "enter", "y", "Y":
		if err := registry.Apply(m.cfg, m.addResolution); err != nil {
			m.addError = err.Error()
			m.addStage = 0
			return m, nil
		}
		name := m.addResolution.Name
		m.closeAdd()
		m.loadSites()
		// Move cursor to the newly added site.
		for i, row := range m.sites {
			if row.site.Name == name {
				m.cursor = i
				break
			}
		}
		m.rebuildPreview()
		m.statusMsg = "added " + name
		return m, nil
	case "n", "N", "backspace":
		m.addStage = 0
		m.addError = ""
		m.addInput.Focus()
		return m, nil
	}
	return m, nil
}

func (m *model) renderAddModal() string {
	width := 60
	if width > m.width-4 {
		width = m.width - 4
	}
	var b strings.Builder
	if m.addStage == 0 {
		b.WriteString(ui.Bold("Add site"))
		b.WriteString("\n")
		b.WriteString(ui.Dim("Enter the path to a site directory."))
		b.WriteString("\n\n")
		b.WriteString(m.addInput.View())
		if len(m.addCands) > 0 {
			b.WriteString("\n\n" + ui.Dim("candidates:") + "\n")
			b.WriteString(formatCandidates(m.addCands, width-4))
		}
		if m.addError != "" {
			b.WriteString("\n\n" + ui.Failure("error: ") + m.addError)
		}
		b.WriteString("\n\n" + ui.Dim("tab complete · enter resolve · esc cancel"))
	} else {
		// Wider modal for the confirm stage so long paths don't wrap.
		width = 84
		if width > m.width-4 {
			width = m.width - 4
		}
		r := m.addResolution
		labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
		valStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
		nameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("51")).Bold(true)
		typeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)

		field := func(label, val string, style lipgloss.Style) string {
			return labelStyle.Render(fmt.Sprintf("%-13s", label)) + style.Render(val)
		}

		b.Reset()
		b.WriteString(ui.Bold("Confirm add"))
		b.WriteString("\n")
		b.WriteString(ui.Dim(strings.Repeat("─", width-4)))
		b.WriteString("\n\n")
		typeLabel := string(r.Type)
		if r.Type == types.TypeBoth {
			typeLabel = "composer+npm"
		}
		b.WriteString(field("name", r.Name, nameStyle) + "\n")
		b.WriteString(field("type", typeLabel, typeStyle) + "\n")
		b.WriteString(field("path", r.AbsPath, valStyle) + "\n")
		if r.ComposerPath != "" && r.ComposerPath != r.AbsPath {
			b.WriteString(field("composer", r.ComposerPath, valStyle) + "\n")
		}
		if r.NPMPath != "" && r.NPMPath != r.AbsPath {
			b.WriteString(field("npm", r.NPMPath, valStyle) + "\n")
		}
		if r.ComposerBin != "" && r.ComposerBin != r.DefaultComposer {
			b.WriteString(field("composer-bin", r.ComposerBin, valStyle) + "\n")
		}
		if r.NPMBin != "" && r.NPMBin != r.DefaultNPM {
			b.WriteString(field("npm-bin", r.NPMBin, valStyle) + "\n")
		}
		if r.NVMRC != "" {
			b.WriteString(field(".nvmrc", r.NVMRC, valStyle) + "\n")
		}
		b.WriteString("\n" + ui.Dim(strings.Repeat("─", width-4)) + "\n")
		b.WriteString(ui.Dim("enter/y confirm · n back · esc/q cancel"))
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("51")).
		Padding(0, 1).
		Width(width).
		Render(b.String())
}

// completeAddPath performs shell-like tab completion on the add-modal path
// input. Expands ~, completes the trailing path segment against directory
// entries, fills the longest common prefix on first Tab, and stores the
// candidate list for display.
func expandHomePath(p string) string {
	if strings.HasPrefix(p, "~/") || p == "~" {
		home, _ := os.UserHomeDir()
		if p == "~" {
			return home
		}
		return filepath.Join(home, p[2:])
	}
	return p
}

func (m *model) completeAddPath() {
	raw := m.addInput.Value()
	if raw == "" {
		raw = "./"
	}
	// Treat a bare "~" as "~/" so the user can list home with one Tab.
	if raw == "~" {
		raw = "~/"
		m.addInput.SetValue(raw)
		m.addInput.SetCursor(len(raw))
	}

	sep := string(filepath.Separator)
	var rawDir, prefix string
	if i := strings.LastIndex(raw, sep); i >= 0 {
		rawDir = raw[:i+1] // keep trailing separator
		prefix = raw[i+1:]
	} else {
		rawDir = ""
		prefix = raw
	}

	dir := expandHomePath(rawDir)
	if dir == "" {
		dir = "."
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		m.addCands = nil
		m.addCandDir = ""
		return
	}
	var matches []string
	for _, e := range entries {
		name := e.Name()
		if prefix != "" && !strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix)) {
			continue
		}
		if prefix == "" && strings.HasPrefix(name, ".") {
			continue // hide dotfiles unless explicitly requested
		}
		if !isDirEntry(e, filepath.Join(dir, name)) {
			continue
		}
		matches = append(matches, name)
	}
	sort.Strings(matches)

	if len(matches) == 0 {
		m.addCands = nil
		m.addCandDir = dir
		return
	}

	// Determine completion: single match → full + "/", multiple → LCP.
	var completed string
	if len(matches) == 1 {
		completed = matches[0] + string(filepath.Separator)
	} else {
		completed = longestCommonPrefix(matches)
		if completed == prefix {
			// no further extension possible; just show candidates
			m.addCands = matches
			m.addCandDir = dir
			return
		}
	}

	// Rebuild input string, preserving the user's original dir style (~ etc).
	newVal := rawDir + completed
	m.addInput.SetValue(newVal)
	m.addInput.SetCursor(len(newVal))

	if len(matches) > 1 {
		m.addCands = matches
		m.addCandDir = dir
	} else {
		m.addCands = nil
		m.addCandDir = ""
	}
}

// formatCandidates lays directory names out in columns to fit `width` cells.
// Trailing slash is appended to each so it's obvious these are directories.
// At most 24 entries are shown; the rest are summarized.
func formatCandidates(names []string, width int) string {
	const maxShown = 24
	truncated := false
	if len(names) > maxShown {
		names = names[:maxShown]
		truncated = true
	}
	maxLen := 0
	for _, n := range names {
		if l := len(n) + 1; l > maxLen { // +1 for trailing /
			maxLen = l
		}
	}
	colW := maxLen + 2
	if colW < 8 {
		colW = 8
	}
	cols := width / colW
	if cols < 1 {
		cols = 1
	}
	var b strings.Builder
	for i, n := range names {
		entry := n + "/"
		if pad := colW - len(entry); pad > 0 {
			entry += strings.Repeat(" ", pad)
		}
		b.WriteString(ui.Dim(entry))
		if (i+1)%cols == 0 && i != len(names)-1 {
			b.WriteString("\n")
		}
	}
	if truncated {
		b.WriteString("\n" + ui.Dim("…more (refine to narrow)"))
	}
	return b.String()
}

// isDirEntry reports whether e (at full path full) is a directory, following
// symlinks so that symlinked project dirs show up in tab-completion.
func isDirEntry(e os.DirEntry, full string) bool {
	if e.IsDir() {
		return true
	}
	if e.Type()&os.ModeSymlink == 0 {
		return false
	}
	fi, err := os.Stat(full)
	if err != nil {
		return false
	}
	return fi.IsDir()
}

func longestCommonPrefix(ss []string) string {
	if len(ss) == 0 {
		return ""
	}
	p := ss[0]
	for _, s := range ss[1:] {
		n := 0
		for n < len(p) && n < len(s) && p[n] == s[n] {
			n++
		}
		p = p[:n]
		if p == "" {
			break
		}
	}
	return p
}

// ---- remove modal ----

func (m *model) updateRemove(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c", "q", "n", "N":
		m.removeOpen = false
		return m, nil
	case "enter", "y", "Y":
		if len(m.sites) == 0 {
			m.removeOpen = false
			return m, nil
		}
		name := m.sites[m.cursor].site.Name
		if err := registry.Remove(m.cfg, name); err != nil {
			m.statusMsg = "remove " + name + ": " + err.Error()
			m.removeOpen = false
			return m, nil
		}
		m.removeOpen = false
		m.loadSites()
		m.statusMsg = "removed " + name
		return m, nil
	}
	return m, nil
}

func (m *model) renderRemoveModal() string {
	width := 56
	if width > m.width-4 {
		width = m.width - 4
	}
	row := m.sites[m.cursor]
	var b strings.Builder
	b.WriteString(ui.Bold("Remove site"))
	b.WriteString("\n\n")
	b.WriteString(ui.Dim("name: ") + row.site.Name + "\n")
	b.WriteString(ui.Dim("path: ") + row.site.Path + "\n")
	b.WriteString("\n")
	b.WriteString(ui.Warn("This removes the config entry and its cached audit."))
	b.WriteString("\n\n")
	b.WriteString(ui.Dim("y/enter confirm · n/esc/q cancel"))

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("196")).
		Padding(0, 1).
		Width(width).
		Render(b.String())
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
