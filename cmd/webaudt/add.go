package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/jeromecoloma/webaudt/internal/config"
	"github.com/jeromecoloma/webaudt/internal/registry"
	"github.com/jeromecoloma/webaudt/internal/types"
	"github.com/jeromecoloma/webaudt/internal/ui"
)

func newAddCmd() *cobra.Command {
	opts := registry.AddOptions{}
	cmd := &cobra.Command{
		Use:   "add <path>",
		Short: "Register a site",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Path = args[0]
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			res, err := registry.Resolve(cfg, opts)
			if err != nil {
				if dup, ok := err.(*registry.DuplicatePathError); ok {
					renderDuplicate(dup)
					os.Exit(1)
				}
				return err
			}
			renderResolution(res)
			if !opts.AssumeYes {
				if !confirm("Continue?") {
					fmt.Println("Cancelled.")
					return nil
				}
			}
			if err := registry.Apply(cfg, res); err != nil {
				return err
			}
			fmt.Println()
			fmt.Printf("  %s\n", ui.Success("registered "+ui.Name(res.Name)))
			fmt.Printf("    %s %s\n\n", ui.Dim("edit later:"), ui.Dim(config.ConfigFile()))
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.Name, "name", "", "Override display name")
	cmd.Flags().StringVar(&opts.Type, "type", "", "Ecosystem type: composer|npm|both")
	cmd.Flags().StringVar(&opts.ComposerPath, "composer-path", "", "Explicit composer audit path")
	cmd.Flags().StringVar(&opts.NPMPath, "npm-path", "", "Explicit npm audit path")
	cmd.Flags().StringVar(&opts.ComposerBin, "composer-bin", "", "Override composer binary for this site")
	cmd.Flags().StringVar(&opts.NPMBin, "npm-bin", "", "Override npm binary for this site")
	cmd.Flags().BoolVarP(&opts.AssumeYes, "yes", "y", false, "Skip the confirmation prompt")
	return cmd
}

func renderResolution(r *registry.Resolution) {
	fmt.Println()
	fmt.Println(ui.Heading("register site"))
	fmt.Println()
	label := func(s string) string { return lipgloss.NewStyle().Bold(true).Render(s) }
	fmt.Printf("  %s  %s\n", label("name:          "), ui.Accent(r.Name))
	fmt.Printf("  %s  %s\n", label("path:          "), r.AbsPath)
	fmt.Printf("  %s  %s\n", label("type:          "), lipgloss.NewStyle().Foreground(lipgloss.Color("226")).Render(string(r.Type)))
	if r.ComposerPath == r.AbsPath && r.NPMPath == r.AbsPath {
		fmt.Printf("  %s  %s\n", label("audit path:    "), r.AbsPath)
	} else {
		if r.Type == types.TypeComposer || r.Type == types.TypeBoth {
			fmt.Printf("  %s  %s\n", label("composer audit:"), nonEmpty(r.ComposerPath))
		}
		if r.Type == types.TypeNPM || r.Type == types.TypeBoth {
			fmt.Printf("  %s  %s\n", label("npm audit:     "), nonEmpty(r.NPMPath))
		}
	}
	if r.Type == types.TypeComposer || r.Type == types.TypeBoth {
		fmt.Printf("  %s  %s\n", label("composer bin:  "), nonEmpty(r.ComposerBin))
	}
	if r.Type == types.TypeNPM || r.Type == types.TypeBoth {
		fmt.Printf("  %s  %s\n", label("npm bin:       "), nonEmpty(r.NPMBin))
		if r.NVMRC != "" {
			fmt.Printf("  %s  %s\n", label(".nvmrc:        "), r.NVMRC+" (set via nvm before refreshing)")
		}
	}
	fmt.Printf("\n  %s\n\n", ui.Dim("↳ edit any of these later in: "+config.ConfigFile()))
}

func renderDuplicate(d *registry.DuplicatePathError) {
	fmt.Println()
	body := lipgloss.JoinVertical(lipgloss.Left,
		ui.Warn("site already registered"),
		"",
		"  name:  "+ui.Accent(d.ExistingName),
		"  path:  "+d.Path,
	)
	if ui.ColorEnabled() {
		fmt.Println(lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("208")).
			Padding(1, 2).
			Render(body))
	} else {
		fmt.Println(body)
	}
	fmt.Println("  next:")
	fmt.Printf("    %s  %s\n", ui.Dim("remove:"), ui.Accent("webaudt rm "+d.ExistingName))
	fmt.Printf("    %s  %s\n\n", ui.Dim("or edit:"), config.ConfigFile())
}

func nonEmpty(s string) string {
	if s == "" {
		return "(not found)"
	}
	return s
}

// confirm prompts the user [y/N]; returns true on yes.
func confirm(prompt string) bool {
	fmt.Printf("  %s [y/N] ", prompt)
	r := bufio.NewReader(os.Stdin)
	line, _ := r.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes"
}
