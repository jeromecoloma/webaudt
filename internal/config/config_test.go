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
	// A freshly bootstrapped file leaves Settings sparse; defaults come from Resolve.
	r := f.Settings.Resolve()
	if r.CacheTTL != 3600 {
		t.Errorf("cache_ttl: got %d", r.CacheTTL)
	}
	if r.ParallelAudits != 4 {
		t.Errorf("parallel_audits: got %d", r.ParallelAudits)
	}
	if _, err := os.Stat(ConfigFile()); err != nil {
		t.Errorf("default config not created: %v", err)
	}
}

func TestValidate_DuplicateName(t *testing.T) {
	f := &File{
		Settings: Settings{},
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
		Settings: Settings{},
		Sites:    []Site{{Name: "a", Path: "relative", Type: types.TypeBoth}},
	}
	err := f.Validate()
	if err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Errorf("expected absolute-path error, got %v", err)
	}
}

func TestValidate_BadType(t *testing.T) {
	f := &File{
		Settings: Settings{},
		Sites:    []Site{{Name: "a", Path: "/x", Type: "yarn"}},
	}
	err := f.Validate()
	if err == nil || !strings.Contains(err.Error(), "composer|npm|both") {
		t.Errorf("expected type error, got %v", err)
	}
}

func TestValidate_ParallelOutOfRange(t *testing.T) {
	f := &File{Settings: Settings{ParallelAudits: Ptr(99)}}
	if err := f.Validate(); err == nil || !strings.Contains(err.Error(), "parallel_audits") {
		t.Errorf("expected parallel range error, got %v", err)
	}
}

func TestValidate_BadColor(t *testing.T) {
	f := &File{Settings: Settings{Color: Ptr("rainbow")}}
	if err := f.Validate(); err == nil || !strings.Contains(err.Error(), "color") {
		t.Errorf("expected color error, got %v", err)
	}
}

func TestValidate_Empty(t *testing.T) {
	f := &File{Settings: Settings{}}
	if err := f.Validate(); err != nil {
		t.Errorf("empty sites list should be valid, got %v", err)
	}
}

func TestUniqueName(t *testing.T) {
	f := &File{
		Settings: Settings{},
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
		Settings: Settings{},
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

// Save must not stamp defaults the user never wrote. Previously Load() filled
// every omitted setting into the struct, so the first `webaudt add` rewrote a
// user's three-line config.toml as a full settings block.
func TestSave_DoesNotStampDefaults(t *testing.T) {
	tmpHome(t)
	if err := EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	// A user config that sets exactly one thing.
	body := "[settings]\nnpm_bin = \"pnpm\"\n"
	if err := os.WriteFile(ConfigFile(), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	f, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	f.Sites = append(f.Sites, Site{Name: "a", Path: "/srv/a", Type: types.TypeNPM})
	if err := f.Save(); err != nil {
		t.Fatal(err)
	}

	out, err := os.ReadFile(ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	for _, unwanted := range []string{"cache_ttl", "parallel_audits", "composer_bin", "color", "nerd_fonts"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("Save stamped an unset default %q into config.toml:\n%s", unwanted, got)
		}
	}
	if !strings.Contains(got, "pnpm") {
		t.Errorf("Save dropped the user's explicit npm_bin:\n%s", got)
	}
}

// cache_ttl = 0 means "never cache". It must survive load and save — this is
// the case the old zeroIsExplicit text-scan hack existed to paper over.
func TestCacheTTL_ExplicitZeroSurvives(t *testing.T) {
	tmpHome(t)
	if err := EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ConfigFile(), []byte("[settings]\ncache_ttl = 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	f, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Settings.Resolve().CacheTTL; got != 0 {
		t.Fatalf("explicit cache_ttl = 0 resolved to %d, want 0 (caching disabled)", got)
	}

	if err := f.Save(); err != nil {
		t.Fatal(err)
	}
	f2, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := f2.Settings.Resolve().CacheTTL; got != 0 {
		t.Errorf("cache_ttl = 0 became %d after a save/load round-trip", got)
	}
}

func TestResolve_OmittedFieldsGetDefaults(t *testing.T) {
	r := Settings{}.Resolve()
	if r != Defaults() {
		t.Errorf("empty Settings resolved to %+v, want %+v", r, Defaults())
	}
}

func TestResolve_ExplicitValuesWin(t *testing.T) {
	r := Settings{
		CacheTTL:       Ptr(60),
		ParallelAudits: Ptr(2),
		ComposerBin:    Ptr("/usr/bin/composer2"),
		NerdFonts:      Ptr(true),
	}.Resolve()

	if r.CacheTTL != 60 || r.ParallelAudits != 2 {
		t.Errorf("numeric overrides lost: %+v", r)
	}
	if r.ComposerBin != "/usr/bin/composer2" {
		t.Errorf("composer_bin = %q", r.ComposerBin)
	}
	if !r.NerdFonts {
		t.Error("nerd_fonts override lost")
	}
	// Untouched fields still fall back.
	if r.NPMBin != "npm" || r.Color != "auto" {
		t.Errorf("defaults not applied to omitted fields: %+v", r)
	}
}
