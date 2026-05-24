// Package ui provides lipgloss-based styling primitives shared by both the
// CLI subcommands and the bubbletea TUI. Honors NO_COLOR + WEBAUDT_COLOR env vars.
package ui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/jeromecoloma/webaudt/internal/cache"
	"github.com/jeromecoloma/webaudt/internal/types"
)

// Severity color palette (lipgloss colors).
var (
	colorCritical = lipgloss.Color("196") // red
	colorHigh     = lipgloss.Color("208") // orange
	colorUnknown  = lipgloss.Color("213") // pink/magenta
	colorModerate = lipgloss.Color("226") // yellow
	colorLow      = lipgloss.Color("39")  // blue
	colorInfo     = lipgloss.Color("39")
	colorClean    = lipgloss.Color("46")  // green
	colorNever    = lipgloss.Color("244") // dim
	colorRunning  = lipgloss.Color("51")  // cyan
	colorError    = lipgloss.Color("208")
	colorDimmed   = lipgloss.Color("244")
	colorAccent   = lipgloss.Color("51")
	colorBorder   = lipgloss.Color("39")
	colorName     = lipgloss.Color("51")
)

// SeverityColor returns the lipgloss color for a severity bucket.
func SeverityColor(sev string) lipgloss.Color {
	switch sev {
	case types.SevCritical:
		return colorCritical
	case types.SevHigh:
		return colorHigh
	case types.SevUnknown:
		return colorUnknown
	case types.SevModerate:
		return colorModerate
	case types.SevLow:
		return colorLow
	case types.SevInfo:
		return colorInfo
	case types.SevClean:
		return colorClean
	default:
		return colorDimmed
	}
}

// StatusIcon returns the small colored glyph for a status bucket. Honors
// AUDT_NO_EMOJI for pure-ASCII output and WEBAUDT_EMOJI_ICONS for the
// chunky-emoji variant.
func StatusIcon(status string) string {
	if os.Getenv("AUDT_NO_EMOJI") != "" || os.Getenv("WEBAUDT_NO_EMOJI") != "" {
		switch status {
		case types.SevCritical:
			return "!"
		case types.SevHigh:
			return "H"
		case types.SevUnknown:
			return "U"
		case types.SevModerate:
			return "M"
		case types.SevLow, types.SevInfo:
			return "L"
		case types.SevClean:
			return "."
		case types.SevNever:
			return "?"
		case types.SevRunning:
			return "~"
		case types.SevError:
			return "x"
		default:
			return "·"
		}
	}
	var glyph string
	var color lipgloss.Color
	switch status {
	case types.SevCritical:
		glyph, color = "●", colorCritical
	case types.SevHigh:
		glyph, color = "●", colorHigh
	case types.SevUnknown:
		glyph, color = "◆", colorUnknown
	case types.SevModerate:
		glyph, color = "●", colorModerate
	case types.SevLow, types.SevInfo:
		glyph, color = "●", colorLow
	case types.SevClean:
		glyph, color = "●", colorClean
	case types.SevNever:
		glyph, color = "○", colorNever
	case types.SevRunning:
		glyph, color = "◐", colorRunning
	case types.SevError:
		glyph, color = "▲", colorError
	default:
		glyph, color = "·", colorDimmed
	}
	return lipgloss.NewStyle().Foreground(color).Render(glyph)
}

// SeverityBadge renders a severity bucket name in its bucket color.
func SeverityBadge(sev string) string {
	return lipgloss.NewStyle().Foreground(SeverityColor(sev)).Render(sev)
}

// Name renders a site/identifier name (cyan + bold).
func Name(s string) string {
	return lipgloss.NewStyle().Foreground(colorName).Bold(true).Render(s)
}

// Dim renders text in the dim/muted color.
func Dim(s string) string {
	return lipgloss.NewStyle().Foreground(colorDimmed).Render(s)
}

// Bold renders text in bright white bold.
func Bold(s string) string {
	return lipgloss.NewStyle().Bold(true).Render(s)
}

// Accent renders text in the accent color (cyan).
func Accent(s string) string {
	return lipgloss.NewStyle().Foreground(colorAccent).Render(s)
}

// Success renders a green ✓ check + text.
func Success(s string) string {
	check := lipgloss.NewStyle().Foreground(colorClean).Render("✓")
	return check + " " + s
}

// Failure renders a red ✗ + text.
func Failure(s string) string {
	x := lipgloss.NewStyle().Foreground(colorCritical).Render("✗")
	return x + " " + s
}

// Warn renders an orange ▲ + text.
func Warn(s string) string {
	w := lipgloss.NewStyle().Foreground(colorHigh).Render("▲")
	return w + " " + s
}

// Heading prints a "▸ title" line in accent color.
func Heading(title string) string {
	return lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render("▸ " + title)
}

// Banner returns the bordered ASCII banner block. Renders plain text when stdout isn't a TTY.
func Banner(version string) string {
	art := `░█░█░█▀▀░█▀▄░█▀█░█░█░█▀▄░▀█▀
░█▄█░█▀▀░█▀▄░█▀█░█░█░█░█░░█░
░▀░▀░▀▀▀░▀▀░░▀░▀░▀▀▀░▀▀░░░▀░`
	tagline := fmt.Sprintf("composer + npm audit monitor   v%s", version)

	if !ColorEnabled() {
		return "\n" + art + "\n\n  " + tagline + "\n"
	}

	body := lipgloss.JoinVertical(lipgloss.Center, art, "", tagline)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorBorder).
		Foreground(colorAccent).
		Padding(1, 3).
		Render(body)
}

// CountsSummaryShort renders compact counts like "1C 2H 3M" (TUI sidebar).
func CountsSummaryShort(c cache.Counts) string {
	if c.Total() == 0 {
		return "clean"
	}
	parts := []string{}
	add := func(n int, code, sev string) {
		if n == 0 {
			return
		}
		parts = append(parts, lipgloss.NewStyle().Foreground(SeverityColor(sev)).Render(fmt.Sprintf("%d%s", n, code)))
	}
	add(c.Critical, "C", types.SevCritical)
	add(c.High, "H", types.SevHigh)
	add(c.Unknown, "U", types.SevUnknown)
	add(c.Moderate, "M", types.SevModerate)
	add(c.Low, "L", types.SevLow)
	add(c.Info, "I", types.SevInfo)
	return strings.Join(parts, " ")
}

// CountsSummaryLong renders spelled-out counts like "1 critical · 2 high · 11 unrated".
func CountsSummaryLong(c cache.Counts) string {
	if c.Total() == 0 {
		return lipgloss.NewStyle().Foreground(colorClean).Render("clean")
	}
	parts := []string{}
	add := func(n int, label, sev string) {
		if n == 0 {
			return
		}
		txt := fmt.Sprintf("%d %s", n, label)
		parts = append(parts, lipgloss.NewStyle().Foreground(SeverityColor(sev)).Render(txt))
	}
	add(c.Critical, "critical", types.SevCritical)
	add(c.High, "high", types.SevHigh)
	add(c.Unknown, "unrated", types.SevUnknown)
	add(c.Moderate, "moderate", types.SevModerate)
	add(c.Low, "low", types.SevLow)
	add(c.Info, "info", types.SevInfo)
	return strings.Join(parts, " · ")
}

// ColorEnabled reports whether ANSI colors should be emitted, based on
// NO_COLOR / WEBAUDT_COLOR / stdout-is-tty.
func ColorEnabled() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	switch os.Getenv("WEBAUDT_COLOR") {
	case "never":
		return false
	case "always":
		return true
	}
	return isStdoutTTY()
}

func isStdoutTTY() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
