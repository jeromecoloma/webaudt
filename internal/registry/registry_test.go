package registry

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jeromecoloma/webaudt/internal/config"
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
