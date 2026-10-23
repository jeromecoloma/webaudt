package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jeromecoloma/webaudt/internal/cache"
	"github.com/jeromecoloma/webaudt/internal/config"
	"github.com/jeromecoloma/webaudt/internal/registry"
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
			failed := false
			matched := 0
			jsonOut := []statusJSON{}
			composerBin := cfg.Settings.Resolve().ComposerBin

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
					failed = failed || entry.Failed()
				}
				if asJSON {
					if entry != nil {
						jsonOut = append(jsonOut, statusJSON{Entry: entry, Versions: detectVersions(s, composerBin)})
					}
					continue
				}
				cards = append(cards, renderStatusCard(s, entry))
			}

			if target != "" && matched == 0 {
				return fmt.Errorf("no such site: %s", target)
			}

			if asJSON {
				b, _ := json.MarshalIndent(jsonOut, "", "  ")
				fmt.Println(string(b))
				os.Exit(exitCode(worst, failed))
				return nil
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
			overall := ui.SeverityBadge(worst)
			if failed && worst != types.SevError {
				overall += ui.Dim(" · ") + ui.SeverityBadge(types.SevError)
			}
			fmt.Printf("  %s %s\n\n", ui.Dim("overall:"), overall)
			os.Exit(exitCode(worst, failed))
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Emit JSON instead of plain text")
	return cmd
}

// statusJSON is one site in `status --json`: the cached audit entry, flattened,
// plus the versions found in the project's files at the time of the call.
type statusJSON struct {
	*cache.Entry
	Versions siteVersions `json:"versions"`
}

// siteVersions holds each detected version, or null when it was not found.
type siteVersions struct {
	Laravel *registry.Detected `json:"laravel"`
	PHP     *registry.Detected `json:"php"`
	Node    *registry.Detected `json:"node"`
	Branch  *string            `json:"branch"`
}

func detectVersions(s config.Site, composerBin string) siteVersions {
	v := siteVersions{
		Laravel: registry.DetectLaravel(s),
		PHP:     registry.DetectPHP(s, composerBin),
		Node:    registry.DetectNode(s),
	}
	if b := registry.GitBranch(s); b != "" {
		v.Branch = &b
	}
	return v
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
		// Ordered slice, not a map: ranging a map gave composer/npm in a random
		// order, so the card's columns jumped around between runs.
		breakdown := ""
		for _, p := range []struct {
			label string
			eco   cache.Ecosystem
		}{
			{"composer", entry.Composer},
			{"npm", entry.NPM},
		} {
			switch p.eco.Status {
			case types.StatusOK:
				breakdown += ui.Dim(p.label+":") + " " + ui.CountsSummaryLong(p.eco.Counts) + "   "
			case types.StatusErrored:
				breakdown += ui.Dim(p.label+":") + " " + ui.SeverityBadge(types.SevError) + "   "
			}
		}
		if breakdown != "" {
			out += "      " + breakdown + "\n"
		}
		if changes := changesSummary(entry); changes != "" {
			out += "      " + changes + "\n"
		}
	}
	out += "      " + ui.Dim(s.Path) + "\n\n"
	return out
}

// changesSummary describes what changed since the previous audit and what
// else needs attention, e.g. "2 new · 1 fixed since last scan · 1 abandoned".
// Empty when there is nothing to report.
func changesSummary(entry *cache.Entry) string {
	var newN, fixed, abandoned, ignored int
	for _, eco := range []cache.Ecosystem{entry.Composer, entry.NPM} {
		newN += len(eco.New)
		fixed += len(eco.Resolved)
		abandoned += len(eco.Abandoned)
		ignored += eco.Ignored
	}
	var parts []string
	if newN > 0 {
		parts = append(parts, fmt.Sprintf("%d new", newN))
	}
	if fixed > 0 {
		parts = append(parts, fmt.Sprintf("%d fixed", fixed))
	}
	if len(parts) > 0 {
		parts[len(parts)-1] += " since last scan"
	}
	if abandoned > 0 {
		parts = append(parts, fmt.Sprintf("%d abandoned", abandoned))
	}
	if ignored > 0 {
		parts = append(parts, fmt.Sprintf("%d ignored", ignored))
	}
	return strings.Join(parts, ui.Dim(" · "))
}

// exitCode is the documented exit code for a run. A failed audit wins over
// any severity: the counts are incomplete, so scripts must not trust them.
func exitCode(worst string, failed bool) int {
	if failed {
		return 10
	}
	return severityToExitCode(worst)
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
