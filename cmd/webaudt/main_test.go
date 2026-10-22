package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jeromecoloma/webaudt/internal/cache"
	"github.com/jeromecoloma/webaudt/internal/types"
)

// binPath is the webaudt binary built once for the whole package. The exit-code
// contract can only be tested for real by running the program: status/refresh
// call os.Exit directly.
var binPath string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "webaudt-cmdtest")
	if err != nil {
		fmt.Fprintln(os.Stderr, "mktemp:", err)
		os.Exit(1)
	}
	binPath = filepath.Join(dir, "webaudt")

	build := exec.Command("go", "build", "-o", binPath, ".")
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build webaudt: %v\n%s", err, out)
		os.Remove(dir)
		os.Exit(1)
	}

	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// env is an isolated XDG home for one test: its own config.toml and cache.
type env struct {
	home     string
	siteDir  string
	siteName string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	home := t.TempDir()
	e := &env{
		home:     home,
		siteDir:  t.TempDir(),
		siteName: "acme",
	}

	// Point *this process* at the same dirs, so we can seed the cache through
	// the cache package instead of hand-rolling its file naming.
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))

	cfgDir := filepath.Join(home, ".config", "webaudt")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf("[[sites]]\nname = %q\npath = %q\ntype = \"composer\"\n", e.siteName, e.siteDir)
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return e
}

// seed writes a cache entry for the site with the given composer ecosystem.
func (e *env) seed(t *testing.T, eco cache.Ecosystem) {
	t.Helper()
	entry := &cache.Entry{
		SchemaVersion: types.CacheSchemaVersion,
		Name:          e.siteName,
		Path:          e.siteDir,
		CheckedAt:     1752460000,
		DurationMS:    42,
		Composer:      eco,
		NPM:           cache.Ecosystem{Status: types.StatusNotApplicable},
	}
	if err := cache.Write(entry); err != nil {
		t.Fatal(err)
	}
}

// run executes the built binary in this env and returns stdout and the exit code.
func (e *env) run(t *testing.T, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(binPath, args...)
	cmd.Env = append(os.Environ(),
		"HOME="+e.home,
		"XDG_CONFIG_HOME="+filepath.Join(e.home, ".config"),
		"XDG_CACHE_HOME="+filepath.Join(e.home, ".cache"),
		"NO_COLOR=1",
	)
	out, err := cmd.Output()

	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return string(out), 0
	case errors.As(err, &exitErr):
		return string(out), exitErr.ExitCode()
	default:
		t.Fatalf("run %v: %v", args, err)
		return "", -1
	}
}

func okEco(c cache.Counts) cache.Ecosystem {
	return cache.Ecosystem{Status: types.StatusOK, AuditPath: "/site", Counts: c}
}

// The documented contract (README "Exit codes"):
//
//	0 clean | 1 moderate/low/info/unrated | 2 high | 3 critical | 10 audit failed
func TestStatus_ExitCodeContract(t *testing.T) {
	cases := []struct {
		name string
		eco  cache.Ecosystem
		want int
	}{
		{"clean", okEco(cache.Counts{}), 0},
		{"info", okEco(cache.Counts{Info: 1}), 1},
		{"low", okEco(cache.Counts{Low: 2}), 1},
		{"moderate", okEco(cache.Counts{Moderate: 1}), 1},
		{"unknown/unrated", okEco(cache.Counts{Unknown: 1}), 1},
		{"high", okEco(cache.Counts{High: 1}), 2},
		{"critical", okEco(cache.Counts{Critical: 1}), 3},
		{"audit failed", cache.Ecosystem{Status: types.StatusErrored, Error: "boom"}, 10},

		// Worst-severity wins when several buckets are populated.
		{"critical outranks high", okEco(cache.Counts{Critical: 1, High: 9, Low: 3}), 3},
		{"high outranks moderate", okEco(cache.Counts{High: 1, Moderate: 5}), 2},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			e.seed(t, c.eco)

			if _, code := e.run(t, "status"); code != c.want {
				t.Errorf("status exit = %d, want %d", code, c.want)
			}
			// --json must report the same code as the human output.
			if _, code := e.run(t, "status", "--json"); code != c.want {
				t.Errorf("status --json exit = %d, want %d", code, c.want)
			}
		})
	}
}

// No sites registered at all is clean, not an error.
func TestStatus_NoSitesIsClean(t *testing.T) {
	home := t.TempDir()
	e := &env{home: home}
	cfgDir := filepath.Join(home, ".config", "webaudt")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte("[settings]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, code := e.run(t, "status")
	if code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	if !strings.Contains(out, "no sites registered") {
		t.Errorf("output = %q, want it to mention no sites", out)
	}
}

// A never-audited site has no cache entry: clean exit, and it says so.
func TestStatus_NeverChecked(t *testing.T) {
	e := newEnv(t) // no seed

	out, code := e.run(t, "status")
	if code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	if !strings.Contains(out, "never checked") {
		t.Errorf("output = %q, want it to say never checked", out)
	}
}

func TestStatus_UnknownSiteFails(t *testing.T) {
	e := newEnv(t)
	e.seed(t, okEco(cache.Counts{}))

	if _, code := e.run(t, "status", "no-such-site"); code == 0 {
		t.Error("status of an unregistered site should not exit 0")
	}
}

// The --json payload is consumed by scripts; pin its shape.
func TestStatus_JSONShape(t *testing.T) {
	e := newEnv(t)
	e.seed(t, cache.Ecosystem{
		Status:    types.StatusOK,
		AuditPath: e.siteDir,
		Counts:    cache.Counts{High: 1, Low: 2},
		Advisories: []cache.Advisory{{
			ID:       "CVE-2024-1234",
			Package:  "vendor/pkg",
			Severity: types.SevHigh,
			Title:    "Something bad",
			Affected: "<1.2.3",
		}},
	})

	out, code := e.run(t, "status", "--json")
	if code != 2 { // high
		t.Fatalf("exit = %d, want 2", code)
	}

	var entries []struct {
		SchemaVersion int    `json:"schema_version"`
		Name          string `json:"name"`
		Path          string `json:"path"`
		CheckedAt     int64  `json:"checked_at"`
		DurationMS    int64  `json:"duration_ms"`
		Composer      struct {
			Status     string       `json:"status"`
			AuditPath  string       `json:"audit_path"`
			Counts     cache.Counts `json:"counts"`
			Advisories []struct {
				ID       string `json:"id"`
				Package  string `json:"package"`
				Severity string `json:"severity"`
				Title    string `json:"title"`
				Affected string `json:"affected"`
			} `json:"advisories"`
		} `json:"composer"`
		NPM struct {
			Status string `json:"status"`
		} `json:"npm"`
	}
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("status --json emitted invalid JSON: %v\n%s", err, out)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}

	got := entries[0]
	if got.SchemaVersion != types.CacheSchemaVersion {
		t.Errorf("schema_version = %d, want %d", got.SchemaVersion, types.CacheSchemaVersion)
	}
	if got.Name != e.siteName || got.Path != e.siteDir {
		t.Errorf("name/path = %q/%q, want %q/%q", got.Name, got.Path, e.siteName, e.siteDir)
	}
	if got.CheckedAt == 0 {
		t.Error("checked_at missing")
	}
	if got.Composer.Status != string(types.StatusOK) {
		t.Errorf("composer.status = %q, want ok", got.Composer.Status)
	}
	if got.Composer.Counts.High != 1 || got.Composer.Counts.Low != 2 {
		t.Errorf("composer.counts = %+v", got.Composer.Counts)
	}
	if len(got.Composer.Advisories) != 1 {
		t.Fatalf("advisories = %d, want 1", len(got.Composer.Advisories))
	}
	adv := got.Composer.Advisories[0]
	if adv.ID != "CVE-2024-1234" || adv.Package != "vendor/pkg" || adv.Severity != types.SevHigh {
		t.Errorf("advisory = %+v", adv)
	}
	if got.NPM.Status != string(types.StatusNotApplicable) {
		t.Errorf("npm.status = %q, want n/a", got.NPM.Status)
	}
}

// --json carries the versions found in the site's files, each with its source,
// and null for anything not found.
func TestStatus_JSONVersions(t *testing.T) {
	e := newEnv(t)
	writeSiteFile(t, e.siteDir, "composer.json", `{"require":{"php":"^8.2","laravel/framework":"^12.0"}}`)
	writeSiteFile(t, e.siteDir, "composer.lock", `{"packages":[{"name":"laravel/framework","version":"v12.33.0"}]}`)
	e.seed(t, okEco(cache.Counts{}))

	out, code := e.run(t, "status", "--json")
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &entries); err != nil || len(entries) != 1 {
		t.Fatalf("want one entry, got err=%v\n%s", err, out)
	}
	if _, ok := entries[0]["schema_version"]; !ok {
		t.Error("cache entry fields must stay at the top level")
	}
	var v struct {
		Laravel *struct{ Version, Source string } `json:"laravel"`
		PHP     *struct{ Version, Source string } `json:"php"`
		Node    *struct{ Version, Source string } `json:"node"`
		Branch  *string                           `json:"branch"`
	}
	raw, ok := entries[0]["versions"]
	if !ok {
		t.Fatalf("versions missing:\n%s", out)
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	if v.Laravel == nil || v.Laravel.Version != "12.33.0" || v.Laravel.Source != "composer.lock" {
		t.Errorf("laravel = %+v", v.Laravel)
	}
	if v.PHP == nil || v.PHP.Version != "^8.2" || v.PHP.Source != "composer.json require" {
		t.Errorf("php = %+v", v.PHP)
	}
	if v.Node != nil {
		t.Errorf("node = %+v, want null for a composer-only site", v.Node)
	}
	if v.Branch != nil {
		t.Errorf("branch = %q, want null outside a git repo", *v.Branch)
	}
	for _, key := range []string{`"node": null`, `"branch": null`} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("versions should spell out %s, got %s", key, raw)
		}
	}
}

func writeSiteFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A site that was never audited contributes no entry to the JSON array — the
// array is present and empty rather than null-ish garbage.
func TestStatus_JSONNeverChecked(t *testing.T) {
	e := newEnv(t)

	out, code := e.run(t, "status", "--json")
	if code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	var entries []cache.Entry
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if len(entries) != 0 {
		t.Errorf("got %d entries, want 0", len(entries))
	}
}

func TestVersion_IsDevSentinelWhenUnstamped(t *testing.T) {
	e := newEnv(t)

	out, code := e.run(t, "--version")
	if code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	// TestMain builds without ldflags, so it must report the sentinel.
	if !strings.Contains(out, "dev") {
		t.Errorf("--version = %q, want it to report the dev sentinel", out)
	}
}

func TestDoctor_Runs(t *testing.T) {
	e := newEnv(t)

	out, _ := e.run(t, "doctor")
	if !strings.Contains(out, "dependency check") {
		t.Errorf("doctor output = %q, want a dependency check report", out)
	}
}
