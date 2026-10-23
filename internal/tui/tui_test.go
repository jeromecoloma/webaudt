package tui

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jeromecoloma/webaudt/internal/config"
	"github.com/jeromecoloma/webaudt/internal/types"
)

// TestRefreshAll_RespectsParallelAudits pins the concurrency cap on "refresh
// all". The TUI fans out its own Cmds rather than going through audit.RunMany,
// so it has to enforce settings.parallel_audits itself — without the semaphore
// this spawned one audit per site regardless of the setting.
func TestRefreshAll_RespectsParallelAudits(t *testing.T) {
	const (
		nSites    = 6
		parallel  = 2
		auditTime = 200 * time.Millisecond
	)

	// Fake composer: every audit sleeps, so wall-clock reveals the batching.
	dir := t.TempDir()
	script := filepath.Join(dir, "composer")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 0.2\necho '{}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	cfg := &config.File{
		Settings: config.Settings{
			ParallelAudits: config.Ptr(parallel),
			ComposerBin:    config.Ptr("composer"),
		},
	}
	for i := 0; i < nSites; i++ {
		cfg.Sites = append(cfg.Sites, config.Site{
			Name: string(rune('a' + i)),
			Path: t.TempDir(), // distinct paths => distinct per-site locks
			Type: types.TypeComposer,
		})
	}

	m := newModel(cfg)
	m.setRows(loadRows(cfg.Sites))
	if len(m.sites) != nSites {
		t.Fatalf("loaded %d sites, want %d", len(m.sites), nSites)
	}

	// refreshAll returns a tea.Batch; run each site's Cmd the way bubbletea
	// would — all at once — and let the semaphore do the throttling.
	cmds := make([]tea.Cmd, 0, nSites)
	for _, row := range m.sites {
		cmds = append(cmds, m.startRefresh(row.site))
	}

	start := time.Now()
	var wg sync.WaitGroup
	for _, cmd := range cmds {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd()
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)

	// ceil(6/2) = 3 sequential waves of ~200ms each.
	minWaves := (nSites + parallel - 1) / parallel
	floor := time.Duration(minWaves-1) * auditTime // one wave of slack for scheduling
	if elapsed < floor {
		t.Errorf("refresh-all finished in %s; with parallel_audits=%d and %d sites it "+
			"cannot beat ~%s — the cap is not being enforced",
			elapsed, parallel, nSites, time.Duration(minWaves)*auditTime)
	}
}

func TestNewModel_SemaphoreFloor(t *testing.T) {
	// An unset/zero parallel_audits must not produce a zero-capacity channel:
	// that would deadlock every refresh forever.
	m := newModel(&config.File{Settings: config.Settings{ParallelAudits: config.Ptr(0)}})
	if cap(m.refreshSem) < 1 {
		t.Fatalf("refreshSem cap = %d, want >= 1", cap(m.refreshSem))
	}
}

// tui.truncate fits a name into a column of n cells, so the ellipsis *replaces*
// the last rune rather than being appended.
func TestTruncate_RuneSafe(t *testing.T) {
	cases := []struct {
		name string
		in   string
		n    int
		want string
	}{
		{"under the cap", "blog", 10, "blog"},
		{"exactly the cap", "blog", 4, "blog"},
		{"ascii cut", "my-long-site", 6, "my-lo…"},
		{"n<=0 passes through", "blog", 0, "blog"},
		{"tiny n", "blog", 2, "bl"},
		// The bug: byte-slicing split these multibyte runes, printing U+FFFD and
		// throwing off the column width the truncation was meant to enforce.
		{"multibyte cut", "café-site", 6, "café-…"},
		{"cjk", "東京都渋谷区", 4, "東京都…"},
		{"multibyte under cap", "café", 10, "café"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := truncate(c.in, c.n)
			if got != c.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("truncate(%q, %d) produced invalid UTF-8: %q", c.in, c.n, got)
			}
			if n := utf8.RuneCountInString(got); c.n > 0 && n > c.n {
				t.Errorf("truncate(%q, %d) returned %d runes, over the cap", c.in, c.n, n)
			}
		})
	}
}
