package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jeromecoloma/webaudt/internal/config"
	"github.com/jeromecoloma/webaudt/internal/types"
)

// firstMsg runs cmd, following batches, and returns the first message of
// type T it produces.
func firstMsg[T tea.Msg](t *testing.T, cmd tea.Cmd) T {
	t.Helper()
	if m, ok := findMsg[T](cmd); ok {
		return m
	}
	var zero T
	t.Fatalf("no %T from command", zero)
	return zero
}

func findMsg[T tea.Msg](cmd tea.Cmd) (T, bool) {
	var zero T
	if cmd == nil {
		return zero, false
	}
	switch msg := cmd().(type) {
	case T:
		return msg, true
	case tea.BatchMsg:
		for _, c := range msg {
			if m, ok := findMsg[T](c); ok {
				return m, true
			}
		}
	}
	return zero, false
}

func testConfig(t *testing.T, names ...string) *config.File {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	cfg := &config.File{}
	for _, n := range names {
		cfg.Sites = append(cfg.Sites, config.Site{Name: n, Path: t.TempDir(), Type: types.TypeComposer})
	}
	return cfg
}

func TestStartup_ShowsLoaderUntilSitesLoad(t *testing.T) {
	cfg := testConfig(t, "a", "b", "c")
	m := newModel(cfg)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	if !m.loading || len(m.sites) != 0 {
		t.Fatalf("loading = %v, sites = %d; want loading with no sites yet", m.loading, len(m.sites))
	}
	if v := m.View(); !strings.Contains(v, "Loading 3 sites") || strings.Contains(v, "no sites") {
		t.Errorf("view should show the loader and not claim there are no sites:\n%s", v)
	}
	// Keys other than quit do nothing while loading.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if m.addOpen {
		t.Error("add opened while loading")
	}

	m.Update(firstMsg[sitesLoadedMsg](t, m.Init()))
	if m.loading || len(m.sites) != 3 || m.sites[1].site.Name != "b" {
		t.Fatalf("after load: loading = %v, sites = %+v", m.loading, m.sites)
	}
	if v := m.View(); strings.Contains(v, "Loading") {
		t.Errorf("loader still shown after load:\n%s", v)
	}
}

func TestRemove_ShowsProgressThenDropsOnlyThatRow(t *testing.T) {
	cfg := testConfig(t, "a", "b", "c")
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	m := newModel(cfg)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.Update(sitesLoadedMsg{rows: loadRows(cfg.Sites)})
	m.cursor = 1

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.removing || !strings.Contains(m.View(), "Removing b") {
		t.Fatalf("removing = %v; view should show progress:\n%s", m.removing, m.View())
	}
	// Keys are ignored until the removal finishes.
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !m.removeOpen {
		t.Error("esc closed the dialog mid-removal")
	}

	m.Update(firstMsg[removeDoneMsg](t, cmd))
	if m.removing || m.removeOpen {
		t.Errorf("removing = %v, removeOpen = %v after done", m.removing, m.removeOpen)
	}
	var names []string
	for _, r := range m.sites {
		names = append(names, r.site.Name)
	}
	if strings.Join(names, ",") != "a,c" || m.cursor != 1 {
		t.Errorf("sites = %v cursor = %d, want a,c and cursor on c", names, m.cursor)
	}
	saved, err := os.ReadFile(filepath.Join(config.ConfigDir(), "config.toml"))
	if err != nil || strings.Contains(string(saved), `"b"`) {
		t.Errorf("config.toml still has b (err %v):\n%s", err, saved)
	}
}
