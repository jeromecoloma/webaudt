package registry

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jeromecoloma/webaudt/internal/config"
	"github.com/jeromecoloma/webaudt/internal/types"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGitBranch(t *testing.T) {
	t.Run("plain repo", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, ".git", "HEAD"), "ref: refs/heads/main\n")

		if got := GitBranch(config.Site{Path: dir}); got != "main" {
			t.Errorf("GitBranch = %q, want main", got)
		}
	})

	t.Run("nested dir walks up to the repo root", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/develop\n")
		nested := filepath.Join(root, "www", "app")
		if err := os.MkdirAll(nested, 0o755); err != nil {
			t.Fatal(err)
		}

		if got := GitBranch(config.Site{Path: nested}); got != "develop" {
			t.Errorf("GitBranch = %q, want develop", got)
		}
	})

	// A linked worktree / submodule has a .git *file* whose gitdir is written
	// relative to the working tree. Joining it against the process CWD instead
	// of the site dir read the wrong HEAD (usually none), so the branch silently
	// came back empty.
	t.Run("worktree with a relative gitdir", func(t *testing.T) {
		root := t.TempDir()
		realGitDir := filepath.Join(root, "repo", ".git", "worktrees", "feature")
		writeFile(t, filepath.Join(realGitDir, "HEAD"), "ref: refs/heads/feature/login\n")

		siteDir := filepath.Join(root, "feature-wt")
		writeFile(t, filepath.Join(siteDir, ".git"), "gitdir: ../repo/.git/worktrees/feature\n")

		if got := GitBranch(config.Site{Path: siteDir}); got != "feature/login" {
			t.Errorf("GitBranch = %q, want feature/login", got)
		}
	})

	t.Run("worktree with an absolute gitdir", func(t *testing.T) {
		root := t.TempDir()
		realGitDir := filepath.Join(root, "repo", ".git", "worktrees", "hotfix")
		writeFile(t, filepath.Join(realGitDir, "HEAD"), "ref: refs/heads/hotfix\n")

		siteDir := filepath.Join(root, "hotfix-wt")
		writeFile(t, filepath.Join(siteDir, ".git"), "gitdir: "+realGitDir+"\n")

		if got := GitBranch(config.Site{Path: siteDir}); got != "hotfix" {
			t.Errorf("GitBranch = %q, want hotfix", got)
		}
	})

	// Detached HEAD is reported as the short SHA, by design.
	t.Run("detached HEAD shows the short sha", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, ".git", "HEAD"), "9fceb02d0ae598e95dc970b74767f19372d61af8\n")

		if got := GitBranch(config.Site{Path: dir}); got != "9fceb02" {
			t.Errorf("GitBranch = %q, want 9fceb02", got)
		}
	})

	t.Run("not a git tree", func(t *testing.T) {
		if got := GitBranch(config.Site{Path: t.TempDir()}); got != "" {
			t.Errorf("GitBranch = %q, want \"\"", got)
		}
	})

	t.Run("empty path", func(t *testing.T) {
		if got := GitBranch(config.Site{Path: ""}); got != "" {
			t.Errorf("GitBranch = %q, want \"\"", got)
		}
	})
}

// ---- ecosystem detection ----

func TestDetectEcosystemPath(t *testing.T) {
	cases := []struct {
		name     string
		layout   []string // files to create, relative to the site dir
		manifest string
		wantSub  string // expected result, relative to the site dir; "-" means ""
	}{
		{"manifest at root", []string{"composer.json"}, "composer.json", "."},
		{"manifest under www", []string{"www/composer.json"}, "composer.json", "www"},
		{"root wins over www", []string{"composer.json", "www/composer.json"}, "composer.json", "."},
		{"package.json at root", []string{"package.json"}, "package.json", "."},
		{"package.json under www", []string{"www/package.json"}, "package.json", "www"},
		{"absent", []string{"README.md"}, "composer.json", "-"},
		{"wrong manifest present", []string{"package.json"}, "composer.json", "-"},
		// Only www/ is probed, not arbitrary depth.
		{"nested deeper than www is not found", []string{"src/app/composer.json"}, "composer.json", "-"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, f := range c.layout {
				writeFile(t, filepath.Join(dir, f), "{}")
			}

			got := detectEcosystemPath(dir, c.manifest)

			want := ""
			if c.wantSub != "-" {
				want = filepath.Join(dir, c.wantSub)
				if c.wantSub == "." {
					want = dir
				}
			}
			if got != want {
				t.Errorf("detectEcosystemPath(%s) = %q, want %q", c.manifest, got, want)
			}
		})
	}
}

// ---- site type auto-detection (webaudt add) ----

func TestResolve_TypeDetection(t *testing.T) {
	cases := []struct {
		name     string
		layout   []string
		optType  string
		wantType types.SiteType
		wantErr  string // substring; "" means success
	}{
		{"composer only", []string{"composer.json"}, "", types.TypeComposer, ""},
		{"npm only", []string{"package.json"}, "", types.TypeNPM, ""},
		{"both", []string{"composer.json", "package.json"}, "", types.TypeBoth, ""},
		{"both, npm under www", []string{"composer.json", "www/package.json"}, "", types.TypeBoth, ""},
		{"neither", []string{"README.md"}, "", "", "no composer.json or package.json"},

		// Explicit --type must still find the manifest it implies.
		{"explicit composer, manifest present", []string{"composer.json"}, "composer", types.TypeComposer, ""},
		{"explicit composer, no manifest", []string{"package.json"}, "composer", "", "no composer.json detected"},
		{"explicit npm, no manifest", []string{"composer.json"}, "npm", "", "no package.json detected"},
		{"explicit both, only one manifest", []string{"composer.json"}, "both", "", "requires both manifests"},
		{"bogus type", []string{"composer.json"}, "yarn", "", "must be composer|npm|both"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, f := range c.layout {
				writeFile(t, filepath.Join(dir, f), "{}")
			}

			cfg := &config.File{}
			res, err := Resolve(cfg, AddOptions{Path: dir, Type: c.optType})

			if c.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got type %q", c.wantErr, res.Type)
				}
				if !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("error = %v, want it to contain %q", err, c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if res.Type != c.wantType {
				t.Errorf("type = %q, want %q", res.Type, c.wantType)
			}
		})
	}
}

func TestResolve_PathsAndName(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "composer.json"), "{}")
	writeFile(t, filepath.Join(dir, "www", "package.json"), "{}")

	res, err := Resolve(&config.File{}, AddOptions{Path: dir})
	if err != nil {
		t.Fatal(err)
	}

	if res.Type != types.TypeBoth {
		t.Errorf("type = %q, want both", res.Type)
	}
	if res.ComposerPath != dir {
		t.Errorf("composer_path = %q, want %q", res.ComposerPath, dir)
	}
	if want := filepath.Join(dir, "www"); res.NPMPath != want {
		t.Errorf("npm_path = %q, want %q", res.NPMPath, want)
	}
	if res.Name != filepath.Base(dir) {
		t.Errorf("name = %q, want the dir basename %q", res.Name, filepath.Base(dir))
	}
	if res.AbsPath != dir {
		t.Errorf("abs_path = %q, want %q", res.AbsPath, dir)
	}
}

// A name collision must be resolved, not silently produce a duplicate.
func TestResolve_UniqueName(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "composer.json"), "{}")
	base := filepath.Base(dir)

	cfg := &config.File{
		Sites: []config.Site{{Name: base, Path: "/somewhere/else", Type: types.TypeComposer}},
	}

	res, err := Resolve(cfg, AddOptions{Path: dir})
	if err != nil {
		t.Fatal(err)
	}
	if res.Name != base+"-2" {
		t.Errorf("name = %q, want %q", res.Name, base+"-2")
	}
}

func TestResolve_DuplicatePath(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "composer.json"), "{}")

	cfg := &config.File{
		Sites: []config.Site{{Name: "already-here", Path: dir, Type: types.TypeComposer}},
	}

	_, err := Resolve(cfg, AddOptions{Path: dir})

	var dup *DuplicatePathError
	if !errors.As(err, &dup) {
		t.Fatalf("err = %v, want a *DuplicatePathError", err)
	}
	if dup.ExistingName != "already-here" {
		t.Errorf("ExistingName = %q", dup.ExistingName)
	}
}

func TestResolve_Rejects(t *testing.T) {
	t.Run("empty path", func(t *testing.T) {
		if _, err := Resolve(&config.File{}, AddOptions{}); err == nil {
			t.Error("expected an error for an empty path")
		}
	})

	t.Run("nonexistent path", func(t *testing.T) {
		_, err := Resolve(&config.File{}, AddOptions{Path: filepath.Join(t.TempDir(), "nope")})
		if err == nil || !strings.Contains(err.Error(), "does not exist") {
			t.Errorf("err = %v, want a does-not-exist error", err)
		}
	})

	t.Run("file, not a directory", func(t *testing.T) {
		dir := t.TempDir()
		f := filepath.Join(dir, "composer.json")
		writeFile(t, f, "{}")

		_, err := Resolve(&config.File{}, AddOptions{Path: f})
		if err == nil || !strings.Contains(err.Error(), "not a directory") {
			t.Errorf("err = %v, want a not-a-directory error", err)
		}
	})
}

func TestDetectLaravel(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  *Detected
	}{
		{
			name: "lock wins over manifest",
			files: map[string]string{
				"composer.json": `{"require":{"laravel/framework":"^12.0"}}`,
				"composer.lock": `{"packages":[{"name":"laravel/framework","version":"v12.33.0"}]}`,
			},
			want: &Detected{Version: "12.33.0", Source: "composer.lock"},
		},
		{
			name:  "manifest constraint without a lock",
			files: map[string]string{"composer.json": `{"require":{"laravel/framework":"^11.0"}}`},
			want:  &Detected{Version: "^11.0", Source: "composer.json require"},
		},
		{
			name:  "not a laravel app",
			files: map[string]string{"composer.json": `{"require":{"php":"^8.2"}}`},
			want:  nil,
		},
		{
			name:  "unreadable lock falls back to manifest",
			files: map[string]string{"composer.json": `{"require":{"laravel/framework":"^10.0"}}`, "composer.lock": `{not json`},
			want:  &Detected{Version: "^10.0", Source: "composer.json require"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, body := range c.files {
				writeFile(t, filepath.Join(dir, name), body)
			}
			got := DetectLaravel(config.Site{Path: dir, Type: types.TypeComposer})
			assertDetected(t, got, c.want)
		})
	}
}

func TestDetectLaravel_NpmOnlySite(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "composer.lock"), `{"packages":[{"name":"laravel/framework","version":"v12.0.0"}]}`)
	if got := DetectLaravel(config.Site{Path: dir, Type: types.TypeNPM}); got != nil {
		t.Errorf("npm-only site = %+v, want nil", got)
	}
}

func TestDetectPHP(t *testing.T) {
	manifest := `{"require":{"php":"^8.2"},"config":{"platform":{"php":"8.3.12"}}}`
	cases := []struct {
		name       string
		manifest   string
		siteBin    string
		defaultBin string
		want       *Detected
	}{
		{"versioned php in site composer_bin", manifest, "/usr/bin/php7.4 /home/deploy/bin/composer", "composer", &Detected{Version: "7.4", Source: "composer_bin"}},
		{"versioned php in default composer_bin", manifest, "", "/usr/bin/php8.1 /usr/local/bin/composer", &Detected{Version: "8.1", Source: "composer_bin"}},
		{"hyphenated php binary", manifest, "/opt/php-8.2/bin/php-8.2 composer", "composer", &Detected{Version: "8.2", Source: "composer_bin"}},
		{"unversioned bin falls back to platform", manifest, "/usr/bin/composer", "composer", &Detected{Version: "8.3.12", Source: "composer.json platform"}},
		{"require constraint when no platform", `{"require":{"php":"^8.2"}}`, "", "composer", &Detected{Version: "^8.2", Source: "composer.json require"}},
		{"nothing declared", `{"require":{}}`, "", "composer", nil},
		{"composer path named php is not a version", `{}`, "/home/php/bin/composer", "composer", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, "composer.json"), c.manifest)
			site := config.Site{Path: dir, Type: types.TypeBoth, ComposerBin: c.siteBin}
			assertDetected(t, DetectPHP(site, c.defaultBin), c.want)
		})
	}
}

func TestDetectPHP_NoComposerProject(t *testing.T) {
	site := config.Site{Path: t.TempDir(), Type: types.TypeComposer, ComposerBin: "/usr/bin/php7.4 composer"}
	if got := DetectPHP(site, "composer"); got != nil {
		t.Errorf("site without composer.json = %+v, want nil", got)
	}
}

func TestDetectNode(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  *Detected
	}{
		{"nvmrc wins", map[string]string{".nvmrc": "20\n", ".node-version": "18", "package.json": `{"engines":{"node":">=16"}}`}, &Detected{Version: "20", Source: ".nvmrc"}},
		{"node-version", map[string]string{".node-version": "v18.19.0\n", "package.json": `{}`}, &Detected{Version: "v18.19.0", Source: ".node-version"}},
		{"volta before engines", map[string]string{"package.json": `{"volta":{"node":"22.1.0"},"engines":{"node":">=20"}}`}, &Detected{Version: "22.1.0", Source: "package.json volta"}},
		{"engines", map[string]string{"package.json": `{"engines":{"node":">=20"}}`}, &Detected{Version: ">=20", Source: "package.json engines"}},
		{"blank nvmrc falls through", map[string]string{".nvmrc": "\n", "package.json": `{"engines":{"node":"^18"}}`}, &Detected{Version: "^18", Source: "package.json engines"}},
		{"nothing pinned", map[string]string{"package.json": `{"name":"x"}`}, nil},
		{"no package.json", map[string]string{}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, body := range c.files {
				writeFile(t, filepath.Join(dir, name), body)
			}
			assertDetected(t, DetectNode(config.Site{Path: dir, Type: types.TypeBoth}), c.want)
			if c.want != nil && NodeVersion(config.Site{Path: dir, Type: types.TypeBoth}) != c.want.Version {
				t.Errorf("NodeVersion disagrees with DetectNode")
			}
		})
	}
}

func assertDetected(t *testing.T, got, want *Detected) {
	t.Helper()
	switch {
	case want == nil && got != nil:
		t.Errorf("got %+v, want nil", *got)
	case want != nil && got == nil:
		t.Errorf("got nil, want %+v", *want)
	case want != nil && *got != *want:
		t.Errorf("got %+v, want %+v", *got, *want)
	}
}
