package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/jeromecoloma/webaudt/internal/doctor"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check that required dependencies are installed",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out, ok := doctor.Render(Version)
			fmt.Print(out)
			if !ok {
				os.Exit(1)
			}
			return nil
		},
	}
}
