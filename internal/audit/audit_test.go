package audit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/jeromecoloma/webaudt/internal/cache"
	"github.com/jeromecoloma/webaudt/internal/config"
	"github.com/jeromecoloma/webaudt/internal/types"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// internal/audit/ -> repo root -> tests/fixtures/
	root := filepath.Join(wd, "..", "..")
	b, err := os.ReadFile(filepath.Join(root, "tests", "fixtures", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

func TestParseComposer_Clean(t *testing.T) {
	eco, err := ParseComposer(readFixture(t, "composer-audit-clean.json"), "/site", nil)
	if err != nil {
		t.Fatal(err)
	}
	if eco.Status != types.StatusOK {
		t.Errorf("status: got %q want ok", eco.Status)
	}
	if eco.Counts.Total() != 0 {
		t.Errorf("total: got %d want 0", eco.Counts.Total())
	}
	if eco.AuditPath != "/site" {
		t.Errorf("audit_path: got %q", eco.AuditPath)
	}
}

func TestParseComposer_Vulns(t *testing.T) {
	eco, err := ParseComposer(readFixture(t, "composer-audit-vulns.json"), "/site", nil)
	if err != nil {
		t.Fatal(err)
	}
	if eco.Counts.Critical != 1 || eco.Counts.High != 1 || eco.Counts.Moderate != 1 {
		t.Errorf("counts: %+v", eco.Counts)
	}
	for _, a := range eco.Advisories {
		if a.Severity == "medium" {
			t.Errorf("medium leaked into normalized output: %+v", a)
		}
	}
}

func TestParseComposer_NullSeverity(t *testing.T) {
	raw := []byte(`{"advisories":{"v/x":[{"advisoryId":"A","cve":"CVE-1","title":"t","affectedVersions":"*","severity":null}]}}`)
	eco, err := ParseComposer(raw, "/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	if eco.Counts.Unknown != 1 {
		t.Errorf("unknown count: got %d want 1 (%+v)", eco.Counts.Unknown, eco.Counts)
	}
}

func TestParseComposer_AdvisoryCap(t *testing.T) {
	// Synthesize a fixture with 75 advisories.
	advs := make([]string, 0, 75)
	for i := 0; i < 75; i++ {
		advs = append(advs, `{"advisoryId":"A","cve":"CVE","title":"t","affectedVersions":"*","severity":"low"}`)
	}
	raw := []byte(`{"advisories":{"v/x":[`)
	for i, a := range advs {
		if i > 0 {
			raw = append(raw, ',')
		}
		raw = append(raw, []byte(a)...)
	}
	raw = append(raw, []byte(`]}}`)...)
	eco, err := ParseComposer(raw, "/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(eco.Advisories) != AdvisoryCap {
		t.Errorf("advisories cap: got %d want %d", len(eco.Advisories), AdvisoryCap)
	}
}

func TestParseNpm_Clean(t *testing.T) {
	eco, err := ParseNpm(readFixture(t, "npm-audit-clean.json"), "/site/www", nil)
	if err != nil {
		t.Fatal(err)
	}
	if eco.Counts.Total() != 0 {
		t.Errorf("total: %d", eco.Counts.Total())
	}
	if eco.AuditPath != "/site/www" {
		t.Errorf("audit_path: %q", eco.AuditPath)
	}
}

func TestParseNpm_Vulns(t *testing.T) {
	eco, err := ParseNpm(readFixture(t, "npm-audit-vulns.json"), "/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	if eco.Counts.High != 1 || eco.Counts.Moderate != 1 {
		t.Errorf("counts: %+v", eco.Counts)
	}
	if len(eco.Advisories) != 2 {
		t.Errorf("advisory count: %d", len(eco.Advisories))
	}
	foundProto := false
	for _, a := range eco.Advisories {
		if strings.Contains(a.Title, "Prototype pollution") {
			foundProto = true
		}
	}
	if !foundProto {
		t.Errorf("expected to find Prototype pollution title; got %+v", eco.Advisories)
	}
}

func TestNormSeverity(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{nil, types.SevUnknown},
		{"", types.SevUnknown},
		{"CRITICAL", types.SevCritical},
		{"Medium", types.SevModerate},
		{"info", types.SevInfo},
		{"weird", types.SevUnknown},
	}
	for _, c := range cases {
		if got := normSeverity(c.in); got != c.want {
			t.Errorf("normSeverity(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// fakeBin writes an executable shell script named `name` into a fresh temp dir,
// prepends that dir to PATH, and returns the script's path.
func fakeBin(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return p
}

// shrinkTimeout swaps auditTimeout (and its companion WaitDelay) for the
// duration of one test, so a timeout case costs milliseconds instead of 120s.
func shrinkTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	prevTimeout, prevDelay := auditTimeout, auditWaitDelay
	auditTimeout, auditWaitDelay = d, d
	t.Cleanup(func() { auditTimeout, auditWaitDelay = prevTimeout, prevDelay })
}

func TestRunComposer_Timeout(t *testing.T) {
	fakeBin(t, "composer", "sleep 30")
	shrinkTimeout(t, 150*time.Millisecond)

	start := time.Now()
	eco := RunComposer(context.Background(), t.TempDir(), "composer", nil)
	elapsed := time.Since(start)

	if eco.Status != types.StatusErrored {
		t.Fatalf("status = %q, want %q", eco.Status, types.StatusErrored)
	}
	if !strings.Contains(eco.Error, "timed out") {
		t.Errorf("error = %q, want it to mention the timeout", eco.Error)
	}
	if elapsed > 5*time.Second {
		t.Errorf("took %s — the subprocess was not killed at the deadline", elapsed)
	}
}

func TestRunNpm_Timeout(t *testing.T) {
	fakeBin(t, "npm", "sleep 30")
	shrinkTimeout(t, 150*time.Millisecond)

	start := time.Now()
	eco := RunNpm(context.Background(), t.TempDir(), "npm", nil)
	elapsed := time.Since(start)

	if eco.Status != types.StatusErrored {
		t.Fatalf("status = %q, want %q", eco.Status, types.StatusErrored)
	}
	if !strings.Contains(eco.Error, "timed out") {
		t.Errorf("error = %q, want it to mention the timeout", eco.Error)
	}
	// Regression: a grandchild holding the stdout pipe open used to block Run
	// for the full `sleep`, long past the deadline. WaitDelay bounds that.
	if elapsed > 5*time.Second {
		t.Errorf("took %s — Run blocked past the deadline draining pipes", elapsed)
	}
}

// A timed-out audit must still release the per-site lock, or the site is
// permanently unrefreshable until the lock file is removed by hand.
func TestRunSite_TimeoutReleasesLock(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	fakeBin(t, "composer", "sleep 30")
	shrinkTimeout(t, 150*time.Millisecond)

	siteDir := t.TempDir()
	site := config.Site{Name: "slow", Path: siteDir, Type: types.TypeComposer}

	if err := RunSite(context.Background(), config.Resolved{ComposerBin: "composer"}, site); err != nil {
		t.Fatalf("RunSite: %v", err)
	}

	if !cache.AcquireLock(siteDir) {
		t.Fatal("lock still held after a timed-out audit")
	}
	cache.ReleaseLock(siteDir)

	entry, err := cache.Read(siteDir)
	if err != nil {
		t.Fatalf("cache.Read: %v", err)
	}
	if entry.Composer.Status != types.StatusErrored {
		t.Errorf("cached composer status = %q, want %q", entry.Composer.Status, types.StatusErrored)
	}
	if !strings.Contains(entry.Composer.Error, "timed out") {
		t.Errorf("cached error = %q, want it to mention the timeout", entry.Composer.Error)
	}
}

func TestTruncate_RuneSafe(t *testing.T) {
	cases := []struct {
		name string
		in   string
		n    int
		want string
	}{
		{"under the cap", "hello", 10, "hello"},
		{"exactly the cap", "hello", 5, "hello"},
		{"ascii cut", "hello world", 5, "hello…"},
		{"zero", "hello", 0, ""},
		{"negative", "hello", -1, ""},
		// The bug: byte-slicing here split the 2-byte "é" / 3-byte CJK runes.
		{"multibyte cut mid-rune", "café café", 4, "café…"},
		{"cjk", "東京都渋谷区", 3, "東京都…"},
		{"emoji", "🔥🔥🔥🔥", 2, "🔥🔥…"},
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
		})
	}
}

// The cap exists to bound subprocess stderr; make sure it counts runes, so a
// 1000-rune non-ASCII message is not silently cut to garbage.
func TestTruncate_CountsRunesNotBytes(t *testing.T) {
	in := strings.Repeat("東", 20)
	got := truncate(in, 20)
	if got != in {
		t.Errorf("20 CJK runes capped at 20 should pass through unchanged, got %q", got)
	}
}
