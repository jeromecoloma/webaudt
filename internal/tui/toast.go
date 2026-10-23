package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/wordwrap"
)

type toastKind int

const (
	toastSuccess toastKind = iota
	toastInfo
	toastError
)

// toast is a short notice shown in the top-right corner, confirming an action
// or reporting that it failed. A newer toast replaces the current one.
type toast struct {
	kind  toastKind
	text  string
	until time.Time
}

const (
	toastTTL      = 4 * time.Second
	toastErrorTTL = 8 * time.Second // errors stay longer, they need reading
	toastMaxWidth = 48
)

func (m *model) notify(kind toastKind, text string) {
	ttl := toastTTL
	if kind == toastError {
		ttl = toastErrorTTL
	}
	m.toast = &toast{kind: kind, text: text, until: time.Now().Add(ttl)}
}

func (m *model) notifySuccess(text string) { m.notify(toastSuccess, text) }
func (m *model) notifyError(text string)   { m.notify(toastError, text) }

// expireToast clears the toast once its time is up.
func (m *model) expireToast(now time.Time) {
	if m.toast != nil && !now.Before(m.toast.until) {
		m.toast = nil
	}
}

func (t *toast) render() string {
	icon, color := "✓", lipgloss.Color("46")
	switch t.kind {
	case toastInfo:
		icon, color = "•", lipgloss.Color("51")
	case toastError:
		icon, color = "✗", lipgloss.Color("196")
	}
	text := wordwrap.String(t.text, toastMaxWidth-2)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(color).
		Padding(0, 1).
		Render(lipgloss.NewStyle().Foreground(color).Bold(true).Render(icon) + " " + text)
}

// overlayTopRight places over on bg, one row down and two cells in from the
// right edge, so it sits inside the panes rather than on their borders.
func overlayTopRight(bg, over string) string {
	bgLines := strings.Split(bg, "\n")
	overLines := strings.Split(over, "\n")
	bgW := 0
	for _, l := range bgLines {
		bgW = max(bgW, lipgloss.Width(l))
	}
	overW := 0
	for _, l := range overLines {
		overW = max(overW, lipgloss.Width(l))
	}
	left := max(0, bgW-overW-2)
	for i, ol := range overLines {
		row := 1 + i
		if row >= len(bgLines) {
			break
		}
		line := bgLines[row]
		if w := lipgloss.Width(line); w < bgW {
			line += strings.Repeat(" ", bgW-w)
		}
		bgLines[row] = truncateCells(line, left) + ol + dropCells(line, left+overW)
	}
	return strings.Join(bgLines, "\n")
}
