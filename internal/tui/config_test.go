package tui

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/jeromecoloma/webaudt/internal/config"
)

func TestReloadConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	site := t.TempDir()
	write := func(body string) {
		t.Helper()
		path, err := config.EnsureFile()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("[[sites]]\nname = \"one\"\npath = \"" + site + "\"\ntype = \"composer\"\n")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	m := newModel(cfg)
	m.setRows(loadRows(cfg.Sites))

	// A valid edit is picked up.
	write("[settings]\nparallel_audits = 3\n[[sites]]\nname = \"renamed\"\npath = \"" + site + "\"\ntype = \"composer\"\n")
	_, cmd := m.Update(configEditedMsg{})
	if !m.loading || cmd == nil {
		t.Fatal("a valid edit should start reloading the site list")
	}
	m.Update(sitesLoadedMsg{rows: loadRows(m.cfg.Sites)})
	if m.errorOpen {
		t.Fatalf("unexpected error: %s", m.errorMsg)
	}
	if m.sites[0].site.Name != "renamed" || cap(m.refreshSem) != 3 {
		t.Errorf("site = %q, sem = %d; want renamed, 3", m.sites[0].site.Name, cap(m.refreshSem))
	}

	// A broken edit keeps the old config and says why.
	write("this is = = not toml")
	if _, cmd := m.Update(configEditedMsg{}); cmd != nil || m.loading {
		t.Error("a broken edit should not reload the site list")
	}
	if !m.errorOpen || !strings.Contains(m.errorMsg, "previous config is still in use") {
		t.Errorf("errorOpen = %v, msg = %q", m.errorOpen, m.errorMsg)
	}
	if m.sites[0].site.Name != "renamed" {
		t.Errorf("site = %q, want the previous config kept", m.sites[0].site.Name)
	}

	// The editor failing to run is reported, not treated as an edit.
	m.errorOpen = false
	m.Update(configEditedMsg{err: errors.New("exit status 1")})
	if !m.errorOpen || m.errorTitle != "Editor failed" {
		t.Errorf("errorOpen = %v, title = %q", m.errorOpen, m.errorTitle)
	}
}
