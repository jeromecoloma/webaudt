package cache

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/jeromecoloma/webaudt/internal/types"
)

func tmpHome(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, ".cache"))
}

func TestWriteReadRoundtrip(t *testing.T) {
	tmpHome(t)
	e := &Entry{
		SchemaVersion: types.CacheSchemaVersion,
		Name:          "acme",
		Path:          "/var/www/acme",
		CheckedAt:     time.Now().Unix(),
		Composer: Ecosystem{
			Status:    types.StatusOK,
			AuditPath: "/var/www/acme",
			Counts:    Counts{High: 2, Moderate: 1},
		},
		NPM: Ecosystem{Status: types.StatusNotApplicable},
	}
	if err := Write(e); err != nil {
		t.Fatal(err)
	}
	out, err := Read("/var/www/acme")
	if err != nil {
		t.Fatal(err)
	}
	if out.Composer.Counts.High != 2 {
		t.Errorf("counts.high lost: %+v", out.Composer.Counts)
	}
	if out.NPM.Status != types.StatusNotApplicable {
		t.Errorf("npm status lost: %q", out.NPM.Status)
	}
}

func TestCountsWorst(t *testing.T) {
	cases := []struct {
		c    Counts
		want string
	}{
		{Counts{}, types.SevClean},
		{Counts{Info: 1}, types.SevInfo},
		{Counts{Low: 1, Info: 2}, types.SevLow},
		{Counts{Moderate: 1, Low: 1}, types.SevModerate},
		{Counts{Unknown: 3, Moderate: 1}, types.SevUnknown},
		{Counts{High: 1, Unknown: 5}, types.SevHigh},
		{Counts{Critical: 1, High: 100}, types.SevCritical},
	}
	for _, tc := range cases {
		if got := tc.c.Worst(); got != tc.want {
			t.Errorf("Worst(%+v) = %q, want %q", tc.c, got, tc.want)
		}
	}
}

func TestEntryWorstAcrossEcosystems(t *testing.T) {
	e := Entry{
		Composer: Ecosystem{Status: types.StatusOK, Counts: Counts{Moderate: 1}},
		NPM:      Ecosystem{Status: types.StatusOK, Counts: Counts{High: 1}},
	}
	if got := e.Worst(); got != types.SevHigh {
		t.Errorf("entry worst: got %q want high", got)
	}
}

func TestEntryWorstErrorSticky(t *testing.T) {
	e := Entry{
		Composer: Ecosystem{Status: types.StatusErrored, Error: "boom"},
		NPM:      Ecosystem{Status: types.StatusNotApplicable},
	}
	if got := e.Worst(); got != types.SevError {
		t.Errorf("error sticky: got %q want error", got)
	}
}

func TestIsFreshAndDelete(t *testing.T) {
	tmpHome(t)
	now := time.Now().Unix()
	e := &Entry{Path: "/p", CheckedAt: now}
	if err := Write(e); err != nil {
		t.Fatal(err)
	}
	if !IsFresh("/p", 60) {
		t.Errorf("expected fresh within 60s of write")
	}
	stale := &Entry{Path: "/old", CheckedAt: now - 300}
	if err := Write(stale); err != nil {
		t.Fatal(err)
	}
	if IsFresh("/old", 60) {
		t.Errorf("expected /old to be stale (300s ago, ttl 60s)")
	}
	if err := Delete("/p"); err != nil {
		t.Fatal(err)
	}
	if Exists("/p") {
		t.Errorf("delete didn't remove cache")
	}
	if err := Delete("/nonexistent"); err != nil {
		t.Errorf("delete of missing should be no-op, got %v", err)
	}
}

func TestLocks(t *testing.T) {
	tmpHome(t)
	if !AcquireLock("/p") {
		t.Fatal("first acquire should succeed")
	}
	if AcquireLock("/p") {
		t.Errorf("second acquire should fail")
	}
	if !IsLocked("/p") {
		t.Errorf("IsLocked should be true while held")
	}
	ReleaseLock("/p")
	if IsLocked("/p") {
		t.Errorf("IsLocked should be false after release")
	}
	if !AcquireLock("/p") {
		t.Errorf("acquire after release should succeed")
	}
}
