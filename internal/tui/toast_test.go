package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/jeromecoloma/webaudt/internal/cache"
	"github.com/jeromecoloma/webaudt/internal/types"
)

func loadedModel(t *testing.T, names ...string) *model {
	t.Helper()
	cfg := testConfig(t, names...)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	m := newModel(cfg)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.Update(sitesLoadedMsg{rows: loadRows(cfg.Sites)})
	return m
}

func TestToast_RemoveConfirmsAndExpires(t *testing.T) {
	m := loadedModel(t, "a", "b")
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.Update(firstMsg[removeDoneMsg](t, cmd))

	if v := m.View(); !strings.Contains(v, "✓ Removed a") {
		t.Fatalf("no removal toast:\n%s", v)
	}
	m.expireToast(time.Now().Add(toastTTL))
	if strings.Contains(m.View(), "Removed a") {
		t.Error("toast still shown after it expired")
	}
}

func TestToast_RemoveFailure(t *testing.T) {
	m := loadedModel(t, "a")
	m.Update(removeDoneMsg{name: "a", err: errors.New("disk full")})
	if v := m.View(); !strings.Contains(v, "✗ Could not remove a: disk full") {
		t.Fatalf("no failure toast:\n%s", v)
	}
	if len(m.sites) != 1 {
		t.Error("a failed removal dropped the row")
	}
}

// Refresh-all reports once, when the last site is done, and counts failures.
func TestToast_RefreshAllSummary(t *testing.T) {
	m := loadedModel(t, "a", "b", "c")
	m.refreshAll()
	if !strings.Contains(m.View(), "Refreshing 3 sites") {
		t.Fatalf("no start toast:\n%s", m.View())
	}

	failed := &cache.Entry{Name: "b", Path: m.sites[1].site.Path, Composer: cache.Ecosystem{Status: types.StatusErrored}}
	if err := cache.Write(failed); err != nil {
		t.Fatal(err)
	}
	m.Update(refreshDoneMsg{name: "a"})
	m.Update(refreshDoneMsg{name: "b"})
	if !strings.Contains(m.View(), "Refreshing 3 sites") {
		t.Error("summary shown before every site finished")
	}
	m.Update(refreshDoneMsg{name: "c"})
	if v := m.View(); !strings.Contains(v, "✗ Refreshed 3 sites · 1 failed") {
		t.Errorf("summary toast missing:\n%s", v)
	}
}

func TestToast_SingleRefreshSaysWhatItFound(t *testing.T) {
	m := loadedModel(t, "a")
	entry := &cache.Entry{Name: "a", Path: m.sites[0].site.Path,
		Composer: cache.Ecosystem{Status: types.StatusOK, Counts: cache.Counts{High: 2}}}
	if err := cache.Write(entry); err != nil {
		t.Fatal(err)
	}
	m.refreshing["a"] = true
	m.Update(refreshDoneMsg{name: "a"})
	if v := m.View(); !strings.Contains(v, "✓ Refreshed a · 2 high") {
		t.Errorf("refresh toast missing:\n%s", v)
	}
}

func TestTabKeys(t *testing.T) {
	m := loadedModel(t, "a")
	m.bottomOpen = false
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("5")})
	if !m.bottomOpen || m.bottomTab != bottomTabRecent || m.focus != paneBottom {
		t.Errorf("5: open %v tab %d focus %d", m.bottomOpen, m.bottomTab, m.focus)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("4")})
	if m.bottomTab != bottomTabLeaderboard {
		t.Errorf("4: tab %d", m.bottomTab)
	}
	if v := m.View(); !strings.Contains(v, "[4] Most vulnerable") || !strings.Contains(v, "[5] Recent scans") {
		t.Errorf("tab labels do not match their keys:\n%s", v)
	}
}

func TestHelp_FitsSmallScreens(t *testing.T) {
	for _, size := range [][2]int{{140, 50}, {140, 30}, {80, 30}, {140, 18}, {80, 14}} {
		m := loadedModel(t, "a")
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
		v := m.View()
		if h := lipgloss.Height(v); h > size[1] {
			t.Errorf("%dx%d: view is %d rows tall", size[0], size[1], h)
		}
		if lines := strings.Split(v, "\n"); !strings.HasPrefix(lines[0], "╭") || strings.Contains(lines[0], "Keybindings") || strings.Contains(lines[1], "Keybindings") {
			t.Errorf("%dx%d: the help pushed the layout off screen:\n%s", size[0], size[1], v)
		}
		if m.helpMaxScroll() > 0 {
			before := m.renderHelpModal()
			m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
			if m.helpScroll != 1 || m.renderHelpModal() == before {
				t.Errorf("%dx%d: j did not scroll the help", size[0], size[1])
			}
		}
	}
}
