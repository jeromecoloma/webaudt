package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jeromecoloma/webaudt/internal/types"
)

// tmpHome points HOME at a temp dir so Load() creates files in isolation.
func tmpHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, ".cache"))
	return dir
}

func TestLoad_BootstrapsDefaults(t *testing.T) {
	tmpHome(t)
	f, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if f.Settings.CacheTTL != 3600 {
		t.Errorf("cache_ttl: got %d", f.Settings.CacheTTL)
	}
	if f.Settings.ParallelAudits != 4 {
		t.Errorf("parallel_audits: got %d", f.Settings.ParallelAudits)
	}
	if _, err := os.Stat(ConfigFile()); err != nil {
		t.Errorf("default config not created: %v", err)
	}
}

func TestValidate_DuplicateName(t *testing.T) {
	f := &File{
		Settings: Defaults(),
		Sites: []Site{
			{Name: "a", Path: "/x", Type: types.TypeBoth},
			{Name: "a", Path: "/y", Type: types.TypeNPM},
		},
	}
	err := f.Validate()
	if err == nil || !strings.Contains(err.Error(), "duplicate site name") {
		t.Errorf("expected duplicate-name error, got %v", err)
	}
}

func TestValidate_NonAbsolutePath(t *testing.T) {
	f := &File{
		Settings: Defaults(),
		Sites:    []Site{{Name: "a", Path: "relative", Type: types.TypeBoth}},
	}
	err := f.Validate()
	if err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Errorf("expected absolute-path error, got %v", err)
	}
}

func TestValidate_BadType(t *testing.T) {
	f := &File{
		Settings: Defaults(),
		Sites:    []Site{{Name: "a", Path: "/x", Type: "yarn"}},
	}
	err := f.Validate()
	if err == nil || !strings.Contains(err.Error(), "composer|npm|both") {
		t.Errorf("expected type error, got %v", err)
	}
}

func TestValidate_ParallelOutOfRange(t *testing.T) {
	f := &File{Settings: Settings{CacheTTL: 1, ParallelAudits: 99, Color: "auto"}}
	if err := f.Validate(); err == nil || !strings.Contains(err.Error(), "parallel_audits") {
		t.Errorf("expected parallel range error, got %v", err)
	}
}

func TestValidate_BadColor(t *testing.T) {
	f := &File{Settings: Settings{CacheTTL: 1, ParallelAudits: 1, Color: "rainbow"}}
	if err := f.Validate(); err == nil || !strings.Contains(err.Error(), "color") {
		t.Errorf("expected color error, got %v", err)
	}
}

func TestValidate_Empty(t *testing.T) {
	f := &File{Settings: Defaults()}
	if err := f.Validate(); err != nil {
		t.Errorf("empty sites list should be valid, got %v", err)
	}
}

func TestUniqueName(t *testing.T) {
	f := &File{
		Settings: Defaults(),
		Sites: []Site{
			{Name: "site", Path: "/a", Type: types.TypeBoth},
			{Name: "site-2", Path: "/b", Type: types.TypeBoth},
		},
	}
	if got := f.UniqueName("fresh"); got != "fresh" {
		t.Errorf("UniqueName('fresh') = %q", got)
	}
	if got := f.UniqueName("site"); got != "site-3" {
		t.Errorf("UniqueName('site') = %q, want site-3", got)
	}
}

func TestSiteByPath(t *testing.T) {
	f := &File{
		Sites: []Site{
			{Name: "a", Path: "/x", Type: types.TypeBoth},
			{Name: "b", Path: "/y", Type: types.TypeNPM},
		},
	}
	if s, ok := f.SiteByPath("/x"); !ok || s.Name != "a" {
		t.Errorf("by /x: got %+v ok=%v", s, ok)
	}
	if _, ok := f.SiteByPath("/missing"); ok {
		t.Errorf("missing path should return ok=false")
	}
}

func TestSaveAndReload(t *testing.T) {
	tmpHome(t)
	if err := EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	enabled := false
	f := &File{
		Settings: Defaults(),
		Sites: []Site{
			{Name: "acme", Path: "/var/www/acme", Type: types.TypeBoth, NPMPath: "/var/www/acme/www"},
			{Name: "blog", Path: "/srv/blog", Type: types.TypeComposer, Enabled: &enabled},
		},
	}
	if err := f.Save(); err != nil {
		t.Fatal(err)
	}
	f2, err := Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(f2.Sites) != 2 {
		t.Fatalf("expected 2 sites, got %d", len(f2.Sites))
	}
	if f2.Sites[1].IsEnabled() {
		t.Errorf("blog should be disabled after roundtrip")
	}
	if f2.Sites[0].NPMPath != "/var/www/acme/www" {
		t.Errorf("npm_path lost after roundtrip: %q", f2.Sites[0].NPMPath)
	}
}
