// Package registry adds, removes, and lists registered sites. Includes
// auto-detection of project-local composer/npm binaries and ecosystem paths.
package registry

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
	Name            string
	AbsPath         string
	Type            types.SiteType
	ComposerPath    string
	NPMPath         string
	ComposerBin     string
	NPMBin          string
	NVMRC           string // contents of .nvmrc if present
	DefaultComposer string
	DefaultNPM      string
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

	defaults := cfg.Settings.Resolve()
	composerBin := opts.ComposerBin
	npmBin := opts.NPMBin
	if (siteType == types.TypeComposer || siteType == types.TypeBoth) && composerBin == "" {
		composerBin = detectComposerBin(composerPath)
		if composerBin == "" {
			composerBin = defaults.ComposerBin
		}
	}
	if (siteType == types.TypeNPM || siteType == types.TypeBoth) && npmBin == "" {
		npmBin = detectNpmBin(npmPath)
		if npmBin == "" {
			npmBin = defaults.NPMBin
		}
	}

	nvmrc := ""
	if (siteType == types.TypeNPM || siteType == types.TypeBoth) && npmPath != "" {
		nvmrc = readFirstLine(filepath.Join(npmPath, ".nvmrc"))
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
		DefaultComposer: defaults.ComposerBin,
		DefaultNPM:      defaults.NPMBin,
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

func readFirstLine(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	if sc.Scan() {
		return strings.TrimSpace(sc.Text())
	}
	return ""
}

// Detected is a version found in a project's files, plus the file it came
// from, so a consumer can tell an installed version from a declared constraint.
type Detected struct {
	Version string `json:"version"`
	Source  string `json:"source"`
}

// LaravelVersion returns the laravel/framework version for a composer-based
// site, or "" if the project is not a Laravel app. It prefers the exact
// installed version from composer.lock and falls back to the constraint in
// composer.json.
func LaravelVersion(site config.Site) string {
	if d := DetectLaravel(site); d != nil {
		return d.Version
	}
	return ""
}

// DetectLaravel is LaravelVersion with its source, or nil when not found.
func DetectLaravel(site config.Site) *Detected {
	dir := composerDir(site)
	if dir == "" {
		return nil
	}
	if v := laravelFromLock(filepath.Join(dir, "composer.lock")); v != "" {
		return &Detected{Version: v, Source: "composer.lock"}
	}
	if v := laravelFromManifest(filepath.Join(dir, "composer.json")); v != "" {
		return &Detected{Version: v, Source: "composer.json require"}
	}
	return nil
}

func laravelFromLock(path string) string {
	var lock struct {
		Packages []struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"packages"`
	}
	if !readJSON(path, &lock) {
		return ""
	}
	for _, p := range lock.Packages {
		if p.Name == "laravel/framework" {
			return strings.TrimPrefix(p.Version, "v")
		}
	}
	return ""
}

func laravelFromManifest(path string) string {
	var m struct {
		Require map[string]string `json:"require"`
	}
	if !readJSON(path, &m) {
		return ""
	}
	return m.Require["laravel/framework"]
}

// DetectPHP returns the PHP version a composer-based site targets, or nil.
// In order: a versioned PHP binary in the site's composer command (e.g.
// "/usr/bin/php7.4 composer", the PHP that actually runs on this server), the
// composer.json config.platform.php pin, then the composer.json require.php
// constraint. defaultComposerBin is the global composer_bin setting, used when
// the site has no override.
func DetectPHP(site config.Site, defaultComposerBin string) *Detected {
	dir := composerDir(site)
	if dir == "" {
		return nil
	}
	bin := site.ComposerBin
	if bin == "" {
		bin = defaultComposerBin
	}
	if v := phpFromComposerBin(bin); v != "" {
		return &Detected{Version: v, Source: "composer_bin"}
	}
	var m struct {
		Require map[string]string `json:"require"`
		Config  struct {
			Platform map[string]string `json:"platform"`
		} `json:"config"`
	}
	if !readJSON(filepath.Join(dir, "composer.json"), &m) {
		return nil
	}
	if v := strings.TrimSpace(m.Config.Platform["php"]); v != "" {
		return &Detected{Version: v, Source: "composer.json platform"}
	}
	if v := strings.TrimSpace(m.Require["php"]); v != "" {
		return &Detected{Version: v, Source: "composer.json require"}
	}
	return nil
}

var phpBinVersion = regexp.MustCompile(`^php-?(\d+(?:\.\d+)*)$`)

// phpFromComposerBin finds a versioned php binary among the words of a
// composer command, e.g. "7.4" from "/usr/bin/php7.4 /home/deploy/bin/composer".
func phpFromComposerBin(bin string) string {
	for _, word := range strings.Fields(bin) {
		if m := phpBinVersion.FindStringSubmatch(filepath.Base(word)); m != nil {
			return m[1]
		}
	}
	return ""
}

// NodeVersion returns the Node version pinned for an npm-based site, or "" if
// the project has no npm ecosystem or pins no version.
func NodeVersion(site config.Site) string {
	if d := DetectNode(site); d != nil {
		return d.Version
	}
	return ""
}

// DetectNode returns the Node version an npm-based site pins, or nil. In order:
// .nvmrc, .node-version, package.json volta.node, then package.json
// engines.node.
func DetectNode(site config.Site) *Detected {
	if site.Type != types.TypeNPM && site.Type != types.TypeBoth {
		return nil
	}
	dir := site.NPMPath
	if dir == "" {
		dir = detectEcosystemPath(site.Path, "package.json")
	}
	if dir == "" {
		return nil
	}
	for _, name := range []string{".nvmrc", ".node-version"} {
		if v := readFirstLine(filepath.Join(dir, name)); v != "" {
			return &Detected{Version: v, Source: name}
		}
	}
	var pkg struct {
		Volta struct {
			Node string `json:"node"`
		} `json:"volta"`
		Engines struct {
			Node string `json:"node"`
		} `json:"engines"`
	}
	if !readJSON(filepath.Join(dir, "package.json"), &pkg) {
		return nil
	}
	if v := strings.TrimSpace(pkg.Volta.Node); v != "" {
		return &Detected{Version: v, Source: "package.json volta"}
	}
	if v := strings.TrimSpace(pkg.Engines.Node); v != "" {
		return &Detected{Version: v, Source: "package.json engines"}
	}
	return nil
}

func composerDir(site config.Site) string {
	if site.Type != types.TypeComposer && site.Type != types.TypeBoth {
		return ""
	}
	if site.ComposerPath != "" {
		return site.ComposerPath
	}
	return detectEcosystemPath(site.Path, "composer.json")
}

func readJSON(path string, v any) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return json.Unmarshal(data, v) == nil
}

// GitBranch returns the current branch name for the git repo at the site path,
// or "" if the path is not inside a git working tree. It reads .git/HEAD
// directly (no git binary required), walking up parent directories to find the
// repository root.
func GitBranch(site config.Site) string {
	dir := site.Path
	if dir == "" {
		return ""
	}
	for {
		gitDir := filepath.Join(dir, ".git")
		if fi, err := os.Stat(gitDir); err == nil {
			// .git is a file (worktree/submodule) pointing at the real gitdir.
			if !fi.IsDir() {
				if data, err := os.ReadFile(gitDir); err == nil {
					if p := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(data)), "gitdir:")); p != "" {
						// git writes this path relative to the working tree for
						// submodules ("gitdir: ../.git/modules/x") and often for
						// linked worktrees. Resolving it against the process CWD
						// would read the wrong HEAD, or none at all.
						if !filepath.IsAbs(p) {
							p = filepath.Join(dir, p)
						}
						gitDir = p
					}
				}
			}
			if b := branchFromHEAD(filepath.Join(gitDir, "HEAD")); b != "" {
				return b
			}
			return ""
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func branchFromHEAD(head string) string {
	data, err := os.ReadFile(head)
	if err != nil {
		return ""
	}
	ref := strings.TrimSpace(string(data))
	if strings.HasPrefix(ref, "ref:") {
		ref = strings.TrimSpace(strings.TrimPrefix(ref, "ref:"))
		return strings.TrimPrefix(ref, "refs/heads/")
	}
	// Detached HEAD: show short commit hash.
	if len(ref) >= 7 {
		return ref[:7]
	}
	return ref
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
