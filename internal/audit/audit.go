// Package audit runs `composer audit` and `npm audit`, parses the JSON output,
// and writes normalized results to the cache.
package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/jeromecoloma/webaudt/internal/cache"
	"github.com/jeromecoloma/webaudt/internal/config"
	"github.com/jeromecoloma/webaudt/internal/types"
)

// AdvisoryCap is the max number of advisories kept per ecosystem.
const AdvisoryCap = 50

// normSeverity maps composer/npm severity strings to webaudt's buckets.
// null/empty/unrecognized → "unknown"; "medium" → "moderate".
func normSeverity(raw any) string {
	var s string
	switch v := raw.(type) {
	case string:
		s = v
	case nil:
		return types.SevUnknown
	default:
		s = fmt.Sprint(v)
	}
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "":
		return types.SevUnknown
	case "medium":
		return types.SevModerate
	case types.SevCritical, types.SevHigh, types.SevModerate, types.SevLow, types.SevInfo:
		return s
	default:
		return types.SevUnknown
	}
}

// ---- Composer ----

type composerAdvisory struct {
	AdvisoryID       string `json:"advisoryId"`
	CVE              string `json:"cve"`
	Title            string `json:"title"`
	Severity         any    `json:"severity"` // string or null
	AffectedVersions string `json:"affectedVersions"`
}

type composerAdvisories map[string][]composerAdvisory

// UnmarshalJSON tolerates composer's empty-advisories shape, which is `[]` instead of `{}`.
func (c *composerAdvisories) UnmarshalJSON(b []byte) error {
	trimmed := bytes.TrimSpace(b)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || bytes.Equal(trimmed, []byte("[]")) {
		*c = composerAdvisories{}
		return nil
	}
	m := map[string][]composerAdvisory{}
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	*c = m
	return nil
}

type composerOutput struct {
	Advisories composerAdvisories `json:"advisories"`
}

// ParseComposer normalizes raw `composer audit --format=json` output into a cache.Ecosystem.
func ParseComposer(raw []byte, auditPath string) (cache.Ecosystem, error) {
	var out composerOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		return cache.Ecosystem{}, fmt.Errorf("decode composer json: %w", err)
	}

	eco := cache.Ecosystem{
		Status:    types.StatusOK,
		AuditPath: auditPath,
	}
	for pkg, advs := range out.Advisories {
		for _, a := range advs {
			sev := normSeverity(a.Severity)
			id := a.CVE
			if id == "" {
				id = a.AdvisoryID
			}
			eco.Advisories = append(eco.Advisories, cache.Advisory{
				ID:       id,
				Package:  pkg,
				Severity: sev,
				Title:    a.Title,
				Affected: a.AffectedVersions,
			})
			incCount(&eco.Counts, sev)
		}
	}
	if len(eco.Advisories) > AdvisoryCap {
		eco.Advisories = eco.Advisories[:AdvisoryCap]
	}
	return eco, nil
}

// ---- npm ----

type npmVia struct {
	Title string
	Name  string
}

func (v *npmVia) UnmarshalJSON(b []byte) error {
	// `via` entries can be either a string (package name) or an object with .title / .name.
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		v.Name = s
		return nil
	}
	var obj struct {
		Title string `json:"title"`
		Name  string `json:"name"`
	}
	if err := json.Unmarshal(b, &obj); err == nil {
		v.Title = obj.Title
		v.Name = obj.Name
		return nil
	}
	return nil // skip unrecognized shape
}

type npmVuln struct {
	Name     string   `json:"name"`
	Severity any      `json:"severity"`
	Via      []npmVia `json:"via"`
	Range    string   `json:"range"`
}

type npmMetaCounts struct {
	Critical int `json:"critical"`
	High     int `json:"high"`
	Moderate int `json:"moderate"`
	Low      int `json:"low"`
	Info     int `json:"info"`
}

type npmOutput struct {
	Vulnerabilities map[string]npmVuln `json:"vulnerabilities"`
	Metadata        struct {
		Vulnerabilities npmMetaCounts `json:"vulnerabilities"`
	} `json:"metadata"`
}

// ParseNpm normalizes raw `npm audit --json` output into a cache.Ecosystem.
func ParseNpm(raw []byte, auditPath string) (cache.Ecosystem, error) {
	var out npmOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		return cache.Ecosystem{}, fmt.Errorf("decode npm json: %w", err)
	}

	eco := cache.Ecosystem{
		Status:    types.StatusOK,
		AuditPath: auditPath,
		// Trust npm's metadata counts for the canonical buckets it tracks.
		Counts: cache.Counts{
			Critical: out.Metadata.Vulnerabilities.Critical,
			High:     out.Metadata.Vulnerabilities.High,
			Moderate: out.Metadata.Vulnerabilities.Moderate,
			Low:      out.Metadata.Vulnerabilities.Low,
			Info:     out.Metadata.Vulnerabilities.Info,
		},
	}
	unknownCount := 0
	for pkg, v := range out.Vulnerabilities {
		sev := normSeverity(v.Severity)
		name := v.Name
		if name == "" {
			name = pkg
		}
		titleParts := make([]string, 0, len(v.Via))
		for _, via := range v.Via {
			switch {
			case via.Title != "":
				titleParts = append(titleParts, via.Title)
			case via.Name != "":
				titleParts = append(titleParts, via.Name)
			}
		}
		eco.Advisories = append(eco.Advisories, cache.Advisory{
			ID:       pkg + "@" + v.Range,
			Package:  name,
			Severity: sev,
			Title:    strings.Join(titleParts, "; "),
			Affected: v.Range,
		})
		if sev == types.SevUnknown {
			unknownCount++
		}
	}
	eco.Counts.Unknown = unknownCount
	if len(eco.Advisories) > AdvisoryCap {
		eco.Advisories = eco.Advisories[:AdvisoryCap]
	}
	return eco, nil
}

func incCount(c *cache.Counts, sev string) {
	switch sev {
	case types.SevCritical:
		c.Critical++
	case types.SevHigh:
		c.High++
	case types.SevUnknown:
		c.Unknown++
	case types.SevModerate:
		c.Moderate++
	case types.SevLow:
		c.Low++
	case types.SevInfo:
		c.Info++
	}
}

// ---- Runners ----

// errBlock builds an errored Ecosystem with the given message.
func errBlock(auditPath, msg string) cache.Ecosystem {
	return cache.Ecosystem{
		Status:    types.StatusErrored,
		AuditPath: auditPath,
		Error:     msg,
	}
}

func naBlock() cache.Ecosystem {
	return cache.Ecosystem{Status: types.StatusNotApplicable}
}

// RunComposer executes `composer audit` in cpath and returns a normalized Ecosystem.
// Errors are returned as Ecosystem{Status:error}, not as Go errors.
func RunComposer(ctx context.Context, cpath, bin string) cache.Ecosystem {
	if _, err := exec.LookPath(bin); err != nil {
		return errBlock(cpath, fmt.Sprintf("composer binary not found: %s", bin))
	}
	cmd := exec.CommandContext(ctx, bin, "audit", "--format=json", "--no-interaction", "--locked")
	cmd.Dir = cpath
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// composer audit exits non-zero when advisories exist — that's not a failure.
	_ = cmd.Run()

	if !json.Valid(stdout.Bytes()) {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = "composer audit produced no parseable JSON"
		}
		return errBlock(cpath, truncate(msg, 1000))
	}
	eco, err := ParseComposer(stdout.Bytes(), cpath)
	if err != nil {
		return errBlock(cpath, err.Error())
	}
	return eco
}

// RunNpm executes `npm audit` in npath and returns a normalized Ecosystem.
func RunNpm(ctx context.Context, npath, bin string) cache.Ecosystem {
	if _, err := exec.LookPath(bin); err != nil {
		return errBlock(npath, fmt.Sprintf("npm binary not found: %s", bin))
	}
	cmd := exec.CommandContext(ctx, bin, "audit", "--json", "--audit-level=info")
	cmd.Dir = npath
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	_ = cmd.Run()

	if !json.Valid(stdout.Bytes()) {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = "npm audit produced no parseable JSON"
		}
		return errBlock(npath, truncate(msg, 1000))
	}
	eco, err := ParseNpm(stdout.Bytes(), npath)
	if err != nil {
		return errBlock(npath, err.Error())
	}
	return eco
}

// RunSite runs both ecosystems (per type) for one site and writes the cache.
// Acquires a per-site lock; returns immediately with errLocked if already in flight.
var errLocked = errors.New("audit already in flight for this site")

func RunSite(ctx context.Context, settings config.Settings, site config.Site) error {
	if !site.IsEnabled() {
		return nil
	}
	if !cache.AcquireLock(site.Path) {
		return errLocked
	}
	defer cache.ReleaseLock(site.Path)

	composerBin := site.ComposerBin
	if composerBin == "" {
		composerBin = settings.ComposerBin
	}
	npmBin := site.NPMBin
	if npmBin == "" {
		npmBin = settings.NPMBin
	}

	start := time.Now()
	composerEco := naBlock()
	npmEco := naBlock()

	switch site.Type {
	case types.TypeComposer:
		composerEco = RunComposer(ctx, site.ResolvedComposerPath(), composerBin)
	case types.TypeNPM:
		npmEco = RunNpm(ctx, site.ResolvedNPMPath(), npmBin)
	case types.TypeBoth:
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			composerEco = RunComposer(ctx, site.ResolvedComposerPath(), composerBin)
		}()
		go func() {
			defer wg.Done()
			npmEco = RunNpm(ctx, site.ResolvedNPMPath(), npmBin)
		}()
		wg.Wait()
	}

	entry := &cache.Entry{
		SchemaVersion: types.CacheSchemaVersion,
		Name:          site.Name,
		Path:          site.Path,
		CheckedAt:     time.Now().Unix(),
		DurationMS:    time.Since(start).Milliseconds(),
		Composer:      composerEco,
		NPM:           npmEco,
	}
	return cache.Write(entry)
}

// RunMany runs audits for many sites with a parallelism cap.
// Errors per site are returned in a map keyed by site name. errLocked is
// surfaced as a skip (not a failure).
func RunMany(ctx context.Context, settings config.Settings, sites []config.Site) map[string]error {
	parallel := settings.ParallelAudits
	if parallel < 1 {
		parallel = 1
	}
	sem := make(chan struct{}, parallel)
	var (
		mu   sync.Mutex
		errs = make(map[string]error)
		wg   sync.WaitGroup
	)
	for _, s := range sites {
		s := s
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if err := RunSite(ctx, settings, s); err != nil && !errors.Is(err, errLocked) {
				mu.Lock()
				errs[s.Name] = err
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return errs
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
