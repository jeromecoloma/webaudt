// Package config loads and validates ~/.config/webaudt/config.toml.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/jeromecoloma/webaudt/internal/types"
)

// Settings is the [settings] block in config.toml, exactly as written by the
// user. Every field is a pointer so we can tell "omitted" from "explicitly set
// to the zero value" — `cache_ttl = 0` (never cache) must survive a Save, and a
// field the user never wrote must not be stamped into their file.
//
// Do not read these fields directly; call Resolve and read the Resolved value,
// which has defaults applied. Per-site overrides for ComposerBin/NPMBin live on
// Site.
type Settings struct {
	CacheTTL       *int    `toml:"cache_ttl,omitempty"`
	ParallelAudits *int    `toml:"parallel_audits,omitempty"`
	ComposerBin    *string `toml:"composer_bin,omitempty"`
	NPMBin         *string `toml:"npm_bin,omitempty"`
	Color          *string `toml:"color,omitempty"`
	NerdFonts      *bool   `toml:"nerd_fonts,omitempty"`
}

// Resolved is Settings with defaults applied — the form the rest of the program
// consumes. Every field is a plain value, so there is no way to read an unset
// setting by accident.
type Resolved struct {
	CacheTTL       int
	ParallelAudits int
	ComposerBin    string
	NPMBin         string
	Color          string
	NerdFonts      bool
}

// Ptr returns a pointer to v. Handy for building Settings literals in tests and
// in code that sets a setting explicitly.
func Ptr[T any](v T) *T { return &v }

// Resolve applies the built-in defaults to every omitted field.
//
// An explicit zero survives where it is meaningful: cache_ttl = 0 resolves to 0
// (caching disabled), not to the 3600 default. For the string fields an explicit
// "" is treated as omitted, matching the previous behaviour — an empty binary
// name or colour mode is never something a user means.
func (s Settings) Resolve() Resolved {
	d := Defaults()
	r := Resolved{
		CacheTTL:       d.CacheTTL,
		ParallelAudits: d.ParallelAudits,
		ComposerBin:    d.ComposerBin,
		NPMBin:         d.NPMBin,
		Color:          d.Color,
	}
	if s.CacheTTL != nil {
		r.CacheTTL = *s.CacheTTL
	}
	if s.ParallelAudits != nil {
		r.ParallelAudits = *s.ParallelAudits
	}
	if s.ComposerBin != nil && *s.ComposerBin != "" {
		r.ComposerBin = *s.ComposerBin
	}
	if s.NPMBin != nil && *s.NPMBin != "" {
		r.NPMBin = *s.NPMBin
	}
	if s.Color != nil && *s.Color != "" {
		r.Color = *s.Color
	}
	if s.NerdFonts != nil {
		r.NerdFonts = *s.NerdFonts
	}
	// Expand `~` and `$VAR` here rather than in Load: mutating the Settings
	// struct at load time would write the expanded absolute path back to the
	// user's config.toml on the next Save.
	r.ComposerBin = expandPath(r.ComposerBin)
	r.NPMBin = expandPath(r.NPMBin)
	return r
}

// Site is one [[sites]] entry.
type Site struct {
	Name         string         `toml:"name"`
	Path         string         `toml:"path"`
	Type         types.SiteType `toml:"type"`
	Enabled      *bool          `toml:"enabled,omitempty"`
	ComposerPath string         `toml:"composer_path,omitempty"`
	NPMPath      string         `toml:"npm_path,omitempty"`
	ComposerBin  string         `toml:"composer_bin,omitempty"`
	NPMBin       string         `toml:"npm_bin,omitempty"`
}

// IsEnabled returns true unless the user explicitly set enabled=false.
func (s Site) IsEnabled() bool { return s.Enabled == nil || *s.Enabled }

// ResolvedComposerPath returns ComposerPath or Path if unset.
func (s Site) ResolvedComposerPath() string {
	if s.ComposerPath != "" {
		return s.ComposerPath
	}
	return s.Path
}

// ResolvedNPMPath returns NPMPath or Path if unset.
func (s Site) ResolvedNPMPath() string {
	if s.NPMPath != "" {
		return s.NPMPath
	}
	return s.Path
}

// File is the full config.toml structure.
type File struct {
	Settings Settings `toml:"settings"`
	Sites    []Site   `toml:"sites,omitempty"`
}

// Defaults returns the built-in defaults applied when a field is omitted.
func Defaults() Resolved {
	return Resolved{
		CacheTTL:       3600,
		ParallelAudits: 4,
		ComposerBin:    "composer",
		NPMBin:         "npm",
		Color:          "auto",
	}
}

// ConfigDir returns the XDG config dir for webaudt.
func ConfigDir() string {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, "webaudt")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "webaudt")
}

// ConfigFile returns the path to config.toml.
func ConfigFile() string { return filepath.Join(ConfigDir(), "config.toml") }

// CacheDir returns the XDG cache dir for webaudt.
func CacheDir() string {
	if v := os.Getenv("XDG_CACHE_HOME"); v != "" {
		return filepath.Join(v, "webaudt")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "webaudt")
}

// EnsureDirs creates the config + cache dirs if missing.
func EnsureDirs() error {
	for _, dir := range []string{
		ConfigDir(),
		filepath.Join(CacheDir(), "sites"),
		filepath.Join(CacheDir(), "lock"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}
	return nil
}

// writeDefault creates a starter config file with defaults and an empty sites list.
func writeDefault(path string) error {
	// The defaults are shown commented out rather than written as live values:
	// an omitted setting resolves to its default at read time, so a user who
	// never touches these keeps a config.toml that tracks future default changes
	// instead of freezing today's values into their file.
	body := `# webaudt configuration. See README + ` + "`webaudt --help`" + ` for the full schema.

[settings]
# Defaults are shown below. Uncomment a line to override it.
# cache_ttl = 3600        # seconds; 0 disables caching
# parallel_audits = 4     # 1-16
# composer_bin = "composer"
# npm_bin = "npm"
# color = "auto"          # auto|always|never
# nerd_fonts = false

# Register sites with: webaudt add /path/to/site
`
	return os.WriteFile(path, []byte(body), 0o644)
}

// Load reads + validates config.toml. Creates a default file if missing.
func Load() (*File, error) {
	if err := EnsureDirs(); err != nil {
		return nil, err
	}
	path := ConfigFile()
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := writeDefault(path); err != nil {
			return nil, fmt.Errorf("write default config: %w", err)
		}
	}

	// Settings are left exactly as the user wrote them — defaults are applied at
	// read time by Settings.Resolve, never stamped into the struct. Anything we
	// write into f.Settings here would be echoed back into config.toml by Save.
	var f File
	if _, err := toml.DecodeFile(path, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	// Expand `~` and `$VAR` / `${VAR}` in the site paths. (The settings-level
	// binaries are expanded in Resolve, for the reason above.)
	for i := range f.Sites {
		f.Sites[i].Path = expandPath(f.Sites[i].Path)
		f.Sites[i].ComposerPath = expandPath(f.Sites[i].ComposerPath)
		f.Sites[i].NPMPath = expandPath(f.Sites[i].NPMPath)
		f.Sites[i].ComposerBin = expandPath(f.Sites[i].ComposerBin)
		f.Sites[i].NPMBin = expandPath(f.Sites[i].NPMBin)
	}

	if err := f.Validate(); err != nil {
		return nil, err
	}
	return &f, nil
}

// expandPath resolves a leading `~` to the user's home dir and expands
// `$VAR` / `${VAR}` references. Bare names (e.g. "composer") pass through
// unchanged so PATH lookups still work.
func expandPath(p string) string {
	if p == "" {
		return p
	}
	if strings.HasPrefix(p, "~/") || p == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			if p == "~" {
				p = home
			} else {
				p = filepath.Join(home, p[2:])
			}
		}
	}
	return os.ExpandEnv(p)
}

// Validate enforces the rules in PRD §5.1.
func (f *File) Validate() error {
	s := f.Settings.Resolve()
	if s.CacheTTL < 0 {
		return fmt.Errorf("settings.cache_ttl must be a non-negative integer, got %d", s.CacheTTL)
	}
	if s.ParallelAudits < 1 || s.ParallelAudits > 16 {
		return fmt.Errorf("settings.parallel_audits must be in [1,16], got %d", s.ParallelAudits)
	}
	switch s.Color {
	case "auto", "always", "never":
	default:
		return fmt.Errorf("settings.color must be auto|always|never, got %q", s.Color)
	}

	names := make(map[string]bool, len(f.Sites))
	for i, site := range f.Sites {
		if site.Name == "" {
			return fmt.Errorf("sites[%d]: name must be non-empty", i)
		}
		if names[site.Name] {
			return fmt.Errorf("duplicate site name: %s", site.Name)
		}
		names[site.Name] = true

		if !filepath.IsAbs(site.Path) {
			return fmt.Errorf("sites[%s].path must be an absolute path, got %q", site.Name, site.Path)
		}
		if site.ComposerPath != "" && !filepath.IsAbs(site.ComposerPath) {
			return fmt.Errorf("sites[%s].composer_path must be absolute, got %q", site.Name, site.ComposerPath)
		}
		if site.NPMPath != "" && !filepath.IsAbs(site.NPMPath) {
			return fmt.Errorf("sites[%s].npm_path must be absolute, got %q", site.Name, site.NPMPath)
		}
		switch site.Type {
		case types.TypeComposer, types.TypeNPM, types.TypeBoth:
		default:
			return fmt.Errorf("sites[%s].type must be composer|npm|both, got %q", site.Name, site.Type)
		}
	}
	return nil
}

// Save writes the config back to disk. Caller is responsible for any
// concurrent-access concerns (webaudt is single-user, single-process here).
func (f *File) Save() error {
	if err := EnsureDirs(); err != nil {
		return err
	}
	path := ConfigFile()
	tmp := path + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	enc := toml.NewEncoder(out)
	if err := enc.Encode(f); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("encode toml: %w", err)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// SiteByName returns the matching site (and true), or zero value and false.
func (f *File) SiteByName(name string) (Site, bool) {
	for _, s := range f.Sites {
		if s.Name == name {
			return s, true
		}
	}
	return Site{}, false
}

// SiteByPath returns the matching site (and true), or zero value and false.
func (f *File) SiteByPath(path string) (Site, bool) {
	for _, s := range f.Sites {
		if s.Path == path {
			return s, true
		}
	}
	return Site{}, false
}

// UniqueName returns base, or base-2, base-3, ... if base is already taken.
func (f *File) UniqueName(base string) string {
	if _, ok := f.SiteByName(base); !ok {
		return base
	}
	for n := 2; ; n++ {
		c := fmt.Sprintf("%s-%d", base, n)
		if _, ok := f.SiteByName(c); !ok {
			return c
		}
	}
}
