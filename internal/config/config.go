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

// Settings are the [settings] block in config.toml. Per-site overrides
// for ComposerBin/NPMBin live on Site.
type Settings struct {
	CacheTTL       int    `toml:"cache_ttl"`
	ParallelAudits int    `toml:"parallel_audits"`
	ComposerBin    string `toml:"composer_bin"`
	NPMBin         string `toml:"npm_bin"`
	Color          string `toml:"color"`
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
func Defaults() Settings {
	return Settings{
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
	body := `# webaudt configuration. See README + ` + "`webaudt --help`" + ` for the full schema.

[settings]
cache_ttl = 3600
parallel_audits = 4
composer_bin = "composer"
npm_bin = "npm"
color = "auto"

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

	var f File
	f.Settings = Defaults()
	if _, err := toml.DecodeFile(path, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	// Expand `~` and `$VAR` / `${VAR}` in path-bearing fields so users can
	// hand-write `composer_bin = "~/bin/composer74"` in config.toml.
	f.Settings.ComposerBin = expandPath(f.Settings.ComposerBin)
	f.Settings.NPMBin = expandPath(f.Settings.NPMBin)
	for i := range f.Sites {
		f.Sites[i].Path = expandPath(f.Sites[i].Path)
		f.Sites[i].ComposerPath = expandPath(f.Sites[i].ComposerPath)
		f.Sites[i].NPMPath = expandPath(f.Sites[i].NPMPath)
		f.Sites[i].ComposerBin = expandPath(f.Sites[i].ComposerBin)
		f.Sites[i].NPMBin = expandPath(f.Sites[i].NPMBin)
	}

	// Re-apply defaults for any fields the user omitted.
	if f.Settings.CacheTTL == 0 && !zeroIsExplicit(path, "cache_ttl") {
		f.Settings.CacheTTL = 3600
	}
	if f.Settings.ParallelAudits == 0 {
		f.Settings.ParallelAudits = 4
	}
	if f.Settings.ComposerBin == "" {
		f.Settings.ComposerBin = "composer"
	}
	if f.Settings.NPMBin == "" {
		f.Settings.NPMBin = "npm"
	}
	if f.Settings.Color == "" {
		f.Settings.Color = "auto"
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

// zeroIsExplicit checks whether the raw TOML actually set the field to 0
// (vs being omitted). Cheap text check — good enough for one numeric field.
func zeroIsExplicit(path, key string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(b), "\n") {
		l := strings.TrimSpace(line)
		if strings.HasPrefix(l, key+" ") || strings.HasPrefix(l, key+"=") {
			return strings.Contains(l, "0")
		}
	}
	return false
}

// Validate enforces the rules in PRD §5.1.
func (f *File) Validate() error {
	s := f.Settings
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
