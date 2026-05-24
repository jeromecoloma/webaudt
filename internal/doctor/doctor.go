// Package doctor checks that required external dependencies are installed
// at acceptable versions.
package doctor

import (
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/jeromecoloma/webaudt/internal/ui"
)

// Status of one tool probe.
type Status int

const (
	StatusOK Status = iota
	StatusMissing
	StatusOutdated
	StatusUnknown
)

// Result is the outcome of probing one tool.
type Result struct {
	Name    string
	Status  Status
	Version string
	MinVer  string
	Note    string // freeform extra info (e.g. yq flavor warning)
}

// Check is a single tool probe.
type Check struct {
	Name      string
	MinVer    string
	ExtractFn func() (string, error) // returns the detected version
}

// requiredChecks are the deps that must be present. The Go rewrite has no
// hard external requirements — composer/npm are only needed for sites of
// that type, and config/JSON/TOML/cache I/O are all stdlib + pure-Go libs.
// We still surface a slot here so future features can register hard deps.
var requiredChecks = []Check{}

// optionalChecks are only needed when a site of that type is registered.
var optionalChecks = []Check{
	{Name: "composer", MinVer: "2.0", ExtractFn: extractFromCmd("composer", []string{"--version"}, `(\d+\.\d+(\.\d+)?)`, strings.TrimPrefix)},
	{Name: "npm", MinVer: "7.0", ExtractFn: extractFromCmd("npm", []string{"--version"}, `(\d+\.\d+(\.\d+)?)`, strings.TrimPrefix)},
}

// extractFromCmd builds an ExtractFn that runs a command and pulls a version
// substring out of its combined stdout/stderr via regex.
func extractFromCmd(bin string, args []string, pat string, prefix func(string, string) string) func() (string, error) {
	re := regexp.MustCompile(pat)
	_ = prefix // unused, kept for symmetry with bash extractor signatures
	return func() (string, error) {
		if _, err := exec.LookPath(bin); err != nil {
			return "", err
		}
		out, _ := exec.Command(bin, args...).CombinedOutput()
		m := re.FindString(string(out))
		if m == "" {
			return "", fmt.Errorf("could not parse version from %s output", bin)
		}
		return m, nil
	}
}

// Run probes all required + optional tools and returns the results.
func Run() (required, optional []Result) {
	for _, c := range requiredChecks {
		required = append(required, runOne(c))
	}
	for _, c := range optionalChecks {
		optional = append(optional, runOne(c))
	}
	return required, optional
}

func runOne(c Check) Result {
	v, err := c.ExtractFn()
	if err != nil {
		return Result{Name: c.Name, Status: StatusMissing, MinVer: c.MinVer}
	}
	if !verGE(v, c.MinVer) {
		return Result{Name: c.Name, Status: StatusOutdated, Version: v, MinVer: c.MinVer}
	}
	return Result{Name: c.Name, Status: StatusOK, Version: v, MinVer: c.MinVer}
}

// verGE reports whether have >= want using dotted version comparison.
func verGE(have, want string) bool {
	ha := parseVer(have)
	wa := parseVer(want)
	for i := 0; i < 3; i++ {
		var h, w int
		if i < len(ha) {
			h = ha[i]
		}
		if i < len(wa) {
			w = wa[i]
		}
		if h != w {
			return h > w
		}
	}
	return true
}

func parseVer(v string) []int {
	parts := strings.Split(v, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, _ := strconv.Atoi(p)
		out = append(out, n)
	}
	return out
}

// Render returns a human-readable report. Returns ok=true if all required deps passed.
func Render(version string) (string, bool) {
	required, optional := Run()
	allOK := true
	for _, r := range required {
		if r.Status != StatusOK {
			allOK = false
		}
	}

	var b strings.Builder
	b.WriteString(ui.Banner(version))
	b.WriteString("\n\n")
	b.WriteString(ui.Heading("dependency check"))
	b.WriteString("\n\n")
	for _, r := range required {
		b.WriteString(formatLine(r))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(ui.Heading("optional (only needed for sites of that type)"))
	b.WriteString("\n\n")
	for _, r := range optional {
		b.WriteString(formatLine(r))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	if allOK {
		b.WriteString("  " + ui.Success("all required dependencies OK."))
		b.WriteString("\n")
	} else {
		b.WriteString(installHints())
		b.WriteString("\n  " + ui.Failure("one or more dependencies are missing or outdated."))
		b.WriteString("\n")
	}
	return b.String(), allOK
}

func formatLine(r Result) string {
	var mark, detail string
	switch r.Status {
	case StatusOK:
		mark = ui.Success("")
		detail = fmt.Sprintf("%s (>= %s)", r.Version, r.MinVer)
	case StatusMissing:
		mark = ui.Failure("")
		detail = fmt.Sprintf("missing (min %s)", r.MinVer)
	case StatusOutdated:
		mark = ui.Warn("")
		detail = fmt.Sprintf("%s (need %s)", r.Version, r.MinVer)
	default:
		mark = ui.Warn("")
		detail = "installed, version unparseable (min " + r.MinVer + ")"
	}
	return fmt.Sprintf("  %s %-10s  %s", strings.TrimSpace(mark), r.Name, detail)
}

func installHints() string {
	hints := []string{"", "Install hints:"}
	switch runtime.GOOS {
	case "darwin":
		hints = append(hints, "  brew install jq git", "  optional: brew install composer node")
	case "linux":
		hints = append(hints, "  Debian/Ubuntu: sudo apt install jq git", "  Arch:          sudo pacman -S jq git")
	}
	return strings.Join(hints, "\n")
}
