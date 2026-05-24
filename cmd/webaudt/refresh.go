package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/jeromecoloma/webaudt/internal/audit"
	"github.com/jeromecoloma/webaudt/internal/cache"
	"github.com/jeromecoloma/webaudt/internal/config"
	"github.com/jeromecoloma/webaudt/internal/types"
	"github.com/jeromecoloma/webaudt/internal/ui"
)

func newRefreshCmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "refresh [names...]",
		Short: "Re-run audits (stale by default, --all forces every site)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			targets := selectRefreshTargets(cfg, args, all)
			if len(targets) == 0 {
				fmt.Println("webaudt: nothing to refresh.")
				return nil
			}
			fmt.Println(ui.Heading(fmt.Sprintf("refreshing %d site(s)", len(targets))))
			fmt.Println()
			for _, s := range targets {
				fmt.Printf("  %s %s\n", ui.Accent("↻"), s.Name)
			}
			errs := audit.RunMany(context.Background(), cfg.Settings, targets)
			fmt.Println()
			for name, err := range errs {
				fmt.Printf("  %s %s: %s\n", ui.Failure(""), name, err)
			}
			fmt.Printf("  %s done\n\n", ui.Success(""))
			os.Exit(refreshExitCode(cfg, targets))
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "Force-refresh every site (ignore TTL)")
	return cmd
}

// selectRefreshTargets picks the sites to refresh. If names are given, they
// always refresh (TTL ignored). If --all is passed, every site refreshes.
// Otherwise, only stale sites refresh.
func selectRefreshTargets(cfg *config.File, names []string, all bool) []config.Site {
	if len(names) > 0 {
		out := make([]config.Site, 0, len(names))
		for _, n := range names {
			s, ok := cfg.SiteByName(n)
			if !ok {
				fmt.Fprintf(os.Stderr, "webaudt: skipping unknown site: %s\n", n)
				continue
			}
			out = append(out, s)
		}
		return out
	}
	if all {
		return cfg.Sites
	}
	out := make([]config.Site, 0, len(cfg.Sites))
	for _, s := range cfg.Sites {
		if !cache.IsFresh(s.Path, cfg.Settings.CacheTTL) {
			out = append(out, s)
		}
	}
	return out
}

// refreshExitCode returns the severity-based exit code for the post-refresh state.
func refreshExitCode(cfg *config.File, targets []config.Site) int {
	worst := types.SevClean
	for _, s := range targets {
		e, err := cache.Read(s.Path)
		if err != nil {
			continue
		}
		w := e.Worst()
		if types.SeverityRank(w) > types.SeverityRank(worst) {
			worst = w
		}
	}
	return severityToExitCode(worst)
}
