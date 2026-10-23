package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/jeromecoloma/webaudt/internal/config"
	"github.com/jeromecoloma/webaudt/internal/editor"
	"github.com/jeromecoloma/webaudt/internal/ui"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Work with config.toml",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "edit",
		Short: "Open config.toml in $VISUAL, $EDITOR, nvim or vim",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := config.EnsureFile()
			if err != nil {
				return err
			}
			ed, err := editor.Command(path)
			if err != nil {
				return err
			}
			ed.Stdin, ed.Stdout, ed.Stderr = os.Stdin, os.Stdout, os.Stderr
			if err := ed.Run(); err != nil {
				return fmt.Errorf("editor: %w", err)
			}
			if _, err := config.Load(); err != nil {
				return fmt.Errorf("config.toml was saved but is not valid: %w\nRun `webaudt config edit` again to fix it", err)
			}
			fmt.Printf("  %s\n\n", ui.Success("config.toml is valid"))
			return nil
		},
	})
	return cmd
}
