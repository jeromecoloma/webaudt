package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/jeromecoloma/webaudt/internal/cache"
	"github.com/jeromecoloma/webaudt/internal/config"
	"github.com/jeromecoloma/webaudt/internal/types"
	"github.com/jeromecoloma/webaudt/internal/ui"
)

func newStatusCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "status [name]",
		Short: "Print audit summary for one or all sites",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			var target string
			if len(args) > 0 {
				target = args[0]
			}

			worst := types.SevClean
			matched := 0
			var jsonOut []*cache.Entry

			cards := []string{}
			for _, s := range cfg.Sites {
				if target != "" && s.Name != target {
					continue
				}
				matched++
				entry, _ := cache.Read(s.Path) // missing → nil
				if entry != nil {
					w := entry.Worst()
					if types.SeverityRank(w) > types.SeverityRank(worst) {
						worst = w
					}
				}
				if asJSON {
					if entry != nil {
						jsonOut = append(jsonOut, entry)
					}
					continue
				}
				cards = append(cards, renderStatusCard(s, entry))
			}

			if asJSON {
				b, _ := json.MarshalIndent(jsonOut, "", "  ")
				fmt.Println(string(b))
				os.Exit(severityToExitCode(worst))
				return nil
			}

			if target != "" && matched == 0 {
				return fmt.Errorf("no such site: %s", target)
			}

			noun := "sites"
			if matched == 1 {
				noun = "site"
			}
			fmt.Println(ui.Heading(fmt.Sprintf("audit status (%d %s)", matched, noun)))
			fmt.Println()
			if len(cards) == 0 {
				fmt.Println("  (no sites registered)")
				fmt.Println()
			} else {
				for _, c := range cards {
					fmt.Print(c)
				}
			}
			fmt.Printf("  %s %s\n\n", ui.Dim("overall:"), ui.SeverityBadge(worst))
			os.Exit(severityToExitCode(worst))
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Emit JSON instead of plain text")
	return cmd
}

func renderStatusCard(s config.Site, entry *cache.Entry) string {
	worst := types.SevNever
	statusLine := ui.Dim("never checked")
	if entry != nil {
		worst = entry.Worst()
		statusLine = ui.SeverityBadge(worst) + " " + ui.Dim("· "+ui.RelativeTime(entry.CheckedAt))
	}
	out := fmt.Sprintf("  %s  %s  %s\n",
		ui.StatusIcon(worst),
		ui.Name(s.Name),
		statusLine,
	)
	if entry != nil {
		breakdown := ""
		for label, eco := range map[string]cache.Ecosystem{"composer": entry.Composer, "npm": entry.NPM} {
			switch eco.Status {
			case types.StatusOK:
				breakdown += ui.Dim(label+":") + " " + ui.CountsSummaryLong(eco.Counts) + "   "
			case types.StatusErrored:
				breakdown += ui.Dim(label+":") + " " + ui.SeverityBadge(types.SevError) + "   "
			}
		}
		if breakdown != "" {
			out += "      " + breakdown + "\n"
		}
	}
	out += "      " + ui.Dim(s.Path) + "\n\n"
	return out
}

// severityToExitCode maps a worst-severity bucket to the documented exit code:
// 0 clean, 1 moderate/low/info/unknown, 2 high, 3 critical, 10 error.
func severityToExitCode(sev string) int {
	switch sev {
	case types.SevCritical:
		return 3
	case types.SevHigh:
		return 2
	case types.SevUnknown, types.SevModerate, types.SevLow, types.SevInfo:
		return 1
	case types.SevError:
		return 10
	default:
		return 0
	}
}
