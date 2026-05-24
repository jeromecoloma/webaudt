package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jeromecoloma/webaudt/internal/config"
	"github.com/jeromecoloma/webaudt/internal/registry"
	"github.com/jeromecoloma/webaudt/internal/ui"
)

func newRmCmd() *cobra.Command {
	var assumeYes bool
	cmd := &cobra.Command{
		Use:     "rm <name>",
		Aliases: []string{"remove"},
		Short:   "Remove a registered site",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if _, ok := cfg.SiteByName(name); !ok {
				return fmt.Errorf("no such site: %s", name)
			}
			if !assumeYes {
				if !confirm(fmt.Sprintf("Remove site '%s' and its cached audit?", name)) {
					fmt.Println("Cancelled.")
					return nil
				}
			}
			if err := registry.Remove(cfg, name); err != nil {
				return err
			}
			fmt.Printf("  %s\n\n", ui.Success("removed "+ui.Name(name)))
			return nil
		},
	}
	cmd.Flags().BoolVarP(&assumeYes, "yes", "y", false, "Skip the confirmation prompt")
	return cmd
}
