package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jeromecoloma/webaudt/internal/cache"
	"github.com/jeromecoloma/webaudt/internal/config"
	"github.com/jeromecoloma/webaudt/internal/types"
	"github.com/jeromecoloma/webaudt/internal/ui"
)

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List registered sites",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			n := len(cfg.Sites)
			if n == 0 {
				fmt.Println(ui.Heading("registered sites"))
				fmt.Println()
				fmt.Println("  (none yet — try: webaudt add /path/to/site)")
				fmt.Println()
				return nil
			}
			fmt.Println(ui.Heading(fmt.Sprintf("registered sites (%d)", n)))
			fmt.Println()
			for _, s := range cfg.Sites {
				renderSiteCard(s)
			}
			return nil
		},
	}
}

func renderSiteCard(s config.Site) {
	worst := types.SevNever
	last := "never checked"
	if cache.Exists(s.Path) {
		entry, err := cache.Read(s.Path)
		if err == nil {
			worst = entry.Worst()
			last = ui.RelativeTime(entry.CheckedAt)
		}
	}
	typeLabel := string(s.Type)
	if s.Type == types.TypeBoth {
		typeLabel = "composer+npm"
	}
	statusLine := ui.Dim("never checked")
	if worst != types.SevNever {
		statusLine = ui.SeverityBadge(worst) + " " + ui.Dim("· "+last)
	}
	fmt.Printf("  %s  %s  %s  %s\n", ui.StatusIcon(worst), ui.Name(s.Name), ui.Dim(typeLabel), statusLine)
	fmt.Printf("      %s\n\n", ui.Dim(s.Path))
}
