// Package registry adds, removes, and lists registered sites. Includes
// auto-detection of project-local composer/npm binaries and ecosystem paths.
package registry

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jeromecoloma/webaudt/internal/cache"
	"github.com/jeromecoloma/webaudt/internal/config"
	"github.com/jeromecoloma/webaudt/internal/types"
)

// AddOptions captures all flags the `webaudt add` command accepts.
type AddOptions struct {
	Path         string // user-supplied path; will be resolved to absolute
	Name         string
	Type         string // composer | npm | both, or empty for auto
	ComposerPath string
	NPMPath      string
	ComposerBin  string
	NPMBin       string
	AssumeYes    bool // skip confirmation prompt
}

// Resolution is the resolved-but-not-yet-persisted view of the site, used by
// the CLI to render the confirmation block before saving.
type Resolution struct {
	Name             string
	AbsPath          string
	Type             types.SiteType
	ComposerPath     string
	NPMPath          string
	ComposerBin      string
	NPMBin           string
	NVMRC            string // contents of .nvmrc if present
	DefaultComposer  string
	DefaultNPM       string
}

// Resolve runs the detection logic for AddOptions and returns a fully-populated
// Resolution. Does not write anything to disk.
func Resolve(cfg *config.File, opts AddOptions) (*Resolution, error) {
	if opts.Path == "" {
		return nil, errors.New("path is required")
	}
	abs, err := filepath.Abs(expandHome(opts.Path))
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}
	if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("path does not exist or is not a directory: %s", abs)
	}

	// Refuse duplicate path registration.
	if existing, ok := cfg.SiteByPath(abs); ok {
		return nil, &DuplicatePathError{ExistingName: existing.Name, Path: abs}
	}

	composerPath := opts.ComposerPath
	if composerPath == "" {
		composerPath = detectEcosystemPath(abs, "composer.json")
	} else {
		composerPath, _ = filepath.Abs(expandHome(composerPath))
	}
	npmPath := opts.NPMPath
	if npmPath == "" {
		npmPath = detectEcosystemPath(abs, "package.json")
	} else {
		npmPath, _ = filepath.Abs(expandHome(npmPath))
	}

	siteType := types.SiteType(opts.Type)
	if siteType == "" {
		switch {
		case composerPath != "" && npmPath != "":
			siteType = types.TypeBoth
		case composerPath != "":
			siteType = types.TypeComposer
		case npmPath != "":
			siteType = types.TypeNPM
		default:
			return nil, fmt.Errorf("no composer.json or package.json found in %s or %s/www; pass --type plus --composer-path / --npm-path to register anyway", abs, abs)
		}
	} else {
		switch siteType {
		case types.TypeComposer:
			if composerPath == "" {
				return nil, errors.New("--type composer but no composer.json detected; pass --composer-path")
			}
		case types.TypeNPM:
			if npmPath == "" {
				return nil, errors.New("--type npm but no package.json detected; pass --npm-path")
			}
		case types.TypeBoth:
			if composerPath == "" || npmPath == "" {
				return nil, errors.New("--type both requires both manifests; pass --composer-path and --npm-path")
			}
		default:
			return nil, fmt.Errorf("--type must be composer|npm|both, got %q", opts.Type)
		}
	}

	name := opts.Name
	if name == "" {
		name = filepath.Base(abs)
	}
	name = cfg.UniqueName(name)

	composerBin := opts.ComposerBin
	npmBin := opts.NPMBin
	if (siteType == types.TypeComposer || siteType == types.TypeBoth) && composerBin == "" {
		composerBin = detectComposerBin(composerPath)
		if composerBin == "" {
			composerBin = cfg.Settings.ComposerBin
		}
	}
	if (siteType == types.TypeNPM || siteType == types.TypeBoth) && npmBin == "" {
		npmBin = detectNpmBin(npmPath)
		if npmBin == "" {
			npmBin = cfg.Settings.NPMBin
		}
	}

	nvmrc := ""
	if (siteType == types.TypeNPM || siteType == types.TypeBoth) && npmPath != "" {
		nvmrc = readNVMRC(npmPath)
	}

	return &Resolution{
		Name:            name,
		AbsPath:         abs,
		Type:            siteType,
		ComposerPath:    composerPath,
		NPMPath:         npmPath,
		ComposerBin:     composerBin,
		NPMBin:          npmBin,
		NVMRC:           nvmrc,
		DefaultComposer: cfg.Settings.ComposerBin,
		DefaultNPM:      cfg.Settings.NPMBin,
	}, nil
}

// Apply persists the Resolution as a new Site in cfg and writes the config file.
func Apply(cfg *config.File, r *Resolution) error {
	site := config.Site{
		Name: r.Name,
		Path: r.AbsPath,
		Type: r.Type,
	}
	if r.ComposerPath != "" && r.ComposerPath != r.AbsPath {
		site.ComposerPath = r.ComposerPath
	}
	if r.NPMPath != "" && r.NPMPath != r.AbsPath {
		site.NPMPath = r.NPMPath
	}
	if r.ComposerBin != "" && r.ComposerBin != r.DefaultComposer {
		site.ComposerBin = r.ComposerBin
	}
	if r.NPMBin != "" && r.NPMBin != r.DefaultNPM {
		site.NPMBin = r.NPMBin
	}
	cfg.Sites = append(cfg.Sites, site)
	return cfg.Save()
}

// Remove deletes the site with the given name (and its cached audit).
func Remove(cfg *config.File, name string) error {
	site, ok := cfg.SiteByName(name)
	if !ok {
		return fmt.Errorf("no such site: %s", name)
	}
	out := make([]config.Site, 0, len(cfg.Sites)-1)
	for _, s := range cfg.Sites {
		if s.Name == site.Name {
			continue
		}
		out = append(out, s)
	}
	cfg.Sites = out
	if err := cfg.Save(); err != nil {
		return err
	}
	return cache.Delete(site.Path)
}

// DuplicatePathError is returned by Resolve when the given path is already
// registered under another name.
type DuplicatePathError struct {
	ExistingName string
	Path         string
}

func (e *DuplicatePathError) Error() string {
	return fmt.Sprintf("path already registered as %q: %s", e.ExistingName, e.Path)
}

// ---- detection helpers ----

func detectEcosystemPath(base, manifest string) string {
	if _, err := os.Stat(filepath.Join(base, manifest)); err == nil {
		return base
	}
	if _, err := os.Stat(filepath.Join(base, "www", manifest)); err == nil {
		return filepath.Join(base, "www")
	}
	return ""
}

// detectComposerBin returns the first matching project-local composer binary,
// falling back to whatever's on PATH.
func detectComposerBin(path string) string {
	for _, cand := range []string{"composer.phar", "bin/composer", "vendor/bin/composer", "composer"} {
		full := filepath.Join(path, cand)
		if isExec(full) {
			return full
		}
	}
	if p, err := exec.LookPath("composer"); err == nil {
		return p
	}
	return ""
}

func detectNpmBin(path string) string {
	full := filepath.Join(path, "node_modules", ".bin", "npm")
	if isExec(full) {
		return full
	}
	if p, err := exec.LookPath("npm"); err == nil {
		return p
	}
	return ""
}

func isExec(p string) bool {
	fi, err := os.Stat(p)
	if err != nil || fi.IsDir() {
		return false
	}
	return fi.Mode().Perm()&0o111 != 0
}

func readNVMRC(path string) string {
	f, err := os.Open(filepath.Join(path, ".nvmrc"))
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	if sc.Scan() {
		return strings.TrimSpace(sc.Text())
	}
	return ""
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") || p == "~" {
		home, _ := os.UserHomeDir()
		if p == "~" {
			return home
		}
		return filepath.Join(home, p[2:])
	}
	return p
}
