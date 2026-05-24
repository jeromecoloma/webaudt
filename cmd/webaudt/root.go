package main

import (
	"github.com/spf13/cobra"
)

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "webaudt",
		Short: "Composer + npm audit monitor for local sites",
		Long: `webaudt is a terminal UI for monitoring composer and npm audit findings
across a registered list of local sites.

Run with no arguments to open the TUI.`,
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUI()
		},
	}
	cmd.AddCommand(
		newAddCmd(),
		newRmCmd(),
		newListCmd(),
		newRefreshCmd(),
		newStatusCmd(),
		newDoctorCmd(),
	)
	return cmd
}
