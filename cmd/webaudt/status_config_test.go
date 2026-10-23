package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jeromecoloma/webaudt/internal/cache"
	"github.com/jeromecoloma/webaudt/internal/types"
)

// A failed audit makes the result incomplete, so exit 10 wins even when the
// other ecosystem found something.
func TestStatus_FailedAuditWinsOverFindings(t *testing.T) {
	e := newEnv(t)
	entry := &cache.Entry{
		SchemaVersion: types.CacheSchemaVersion,
		Name:          e.siteName,
		Path:          e.siteDir,
		CheckedAt:     1752460000,
		Composer:      okEco(cache.Counts{Critical: 1}),
		NPM:           cache.Ecosystem{Status: types.StatusErrored, Error: "ENOLOCK"},
	}
	if err := cache.Write(entry); err != nil {
		t.Fatal(err)
	}

	out, code := e.run(t, "status")
	if code != 10 {
		t.Errorf("status exit = %d, want 10", code)
	}
	if !strings.Contains(out, "overall: critical · error") {
		t.Errorf("overall line should show the failure too:\n%s", out)
	}
	if _, code := e.run(t, "status", "--json"); code != 10 {
		t.Errorf("status --json exit = %d, want 10", code)
	}
}

func TestStatus_JSONUnknownSiteFails(t *testing.T) {
	e := newEnv(t)
	e.seed(t, okEco(cache.Counts{}))

	out, code := e.run(t, "status", "no-such-site", "--json")
	if code == 0 {
		t.Errorf("exit = 0, want non-zero; stdout %q", out)
	}
}

func TestStatus_ShowsChanges(t *testing.T) {
	e := newEnv(t)
	eco := okEco(cache.Counts{High: 1})
	eco.New = []string{"vendor/pkg CVE-1"}
	eco.Resolved = []string{"vendor/pkg CVE-0", "vendor/other CVE-9"}
	eco.Abandoned = []cache.Abandoned{{Package: "old/pkg"}}
	eco.Ignored = 3
	e.seed(t, eco)

	out, _ := e.run(t, "status")
	if want := "1 new · 2 fixed since last scan · 1 abandoned · 3 ignored"; !strings.Contains(out, want) {
		t.Errorf("status output missing %q:\n%s", want, out)
	}
}

// writeEditor installs a fake editor script that runs body with the file as $1.
func writeEditor(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "fake-editor")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestConfigEdit(t *testing.T) {
	e := newEnv(t)

	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", writeEditor(t, `echo "# touched" >> "$1"`))
	out, code := e.run(t, "config", "edit")
	if code != 0 {
		t.Fatalf("valid edit: exit = %d\n%s", code, out)
	}
	b, _ := os.ReadFile(filepath.Join(e.home, ".config", "webaudt", "config.toml"))
	if !strings.Contains(string(b), "# touched") {
		t.Error("editor did not receive the config path")
	}

	t.Setenv("EDITOR", writeEditor(t, `echo "this is = = not toml" >> "$1"`))
	if _, code := e.run(t, "config", "edit"); code == 0 {
		t.Error("an edit that breaks config.toml should exit non-zero")
	}
}
