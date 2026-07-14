package tui

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

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
	m.loadSites()
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
