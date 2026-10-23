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
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

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

// ---- Ignore list ----

// IgnoreSet holds advisory IDs to leave out of results. IDs are matched
// case-insensitively. A nil set ignores nothing.
type IgnoreSet map[string]bool

// NewIgnoreSet builds an IgnoreSet from config values.
func NewIgnoreSet(ids []string) IgnoreSet {
	if len(ids) == 0 {
		return nil
	}
	s := IgnoreSet{}
	for _, id := range ids {
		if id = strings.ToUpper(strings.TrimSpace(id)); id != "" {
			s[id] = true
		}
	}
	return s
}

// matches reports whether any of the advisory's IDs is ignored.
func (s IgnoreSet) matches(ids ...string) bool {
	for _, id := range ids {
		if id != "" && s[strings.ToUpper(id)] {
			return true
		}
	}
	return false
}

// ghsaFromURL returns the GHSA ID at the end of a GitHub advisory URL, or "".
func ghsaFromURL(u string) string {
	last := u[strings.LastIndex(u, "/")+1:]
	if strings.HasPrefix(strings.ToUpper(last), "GHSA-") {
		return last
	}
	return ""
}

// ---- Composer ----

type composerAdvisory struct {
	AdvisoryID       string `json:"advisoryId"`
	CVE              string `json:"cve"`
	Title            string `json:"title"`
	Link             string `json:"link"`
	Severity         any    `json:"severity"` // string or null
	AffectedVersions string `json:"affectedVersions"`
}

type composerAdvisories map[string][]composerAdvisory

// UnmarshalJSON tolerates composer's empty-advisories shape, which is `[]` instead of `{}`.
func (c *composerAdvisories) UnmarshalJSON(b []byte) error {
	if isEmptyJSON(b) {
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

// composerAbandoned maps an abandoned package to its suggested replacement,
// which is a package name or false/null when there is none.
type composerAbandoned map[string]any

// UnmarshalJSON tolerates the empty shape `[]`, as for advisories.
func (c *composerAbandoned) UnmarshalJSON(b []byte) error {
	if isEmptyJSON(b) {
		*c = composerAbandoned{}
		return nil
	}
	m := map[string]any{}
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	*c = m
	return nil
}

func isEmptyJSON(b []byte) bool {
	trimmed := bytes.TrimSpace(b)
	return len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || bytes.Equal(trimmed, []byte("[]"))
}

type composerOutput struct {
	Advisories composerAdvisories `json:"advisories"`
	Abandoned  composerAbandoned  `json:"abandoned"`
}

// ParseComposer normalizes raw `composer audit --format=json` output into a
// cache.Ecosystem, leaving out advisories in ignore.
func ParseComposer(raw []byte, auditPath string, ignore IgnoreSet) (cache.Ecosystem, error) {
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
			id := a.CVE
			if id == "" {
				id = a.AdvisoryID
			}
			if ignore.matches(a.CVE, a.AdvisoryID, ghsaFromURL(a.Link)) {
				eco.Ignored++
				eco.IgnoredKeys = append(eco.IgnoredKeys, pkg+" "+id)
				continue
			}
			sev := normSeverity(a.Severity)
			adv := cache.Advisory{
				ID:       id,
				Package:  pkg,
				Severity: sev,
				Title:    a.Title,
				Affected: a.AffectedVersions,
				Keys:     []string{pkg + " " + id},
			}
			if a.Link != "" {
				adv.Links = []string{a.Link}
			}
			eco.Advisories = append(eco.Advisories, adv)
			incCount(&eco.Counts, sev)
		}
	}
	for pkg, repl := range out.Abandoned {
		r, _ := repl.(string)
		eco.Abandoned = append(eco.Abandoned, cache.Abandoned{Package: pkg, Replacement: r})
	}
	sort.Slice(eco.Abandoned, func(i, j int) bool { return eco.Abandoned[i].Package < eco.Abandoned[j].Package })
	finish(&eco)
	return eco, nil
}

// finish records every advisory key, ignored ones included, then orders the advisories worst first
// and caps them. Sorting before the cap keeps the most severe advisories and
// makes the result stable between runs; both input formats are maps.
func finish(eco *cache.Ecosystem) {
	for _, a := range eco.Advisories {
		eco.Seen = append(eco.Seen, a.Keys...)
	}
	eco.Seen = append(eco.Seen, eco.IgnoredKeys...)
	sort.Strings(eco.Seen)
	sort.SliceStable(eco.Advisories, func(i, j int) bool {
		a, b := eco.Advisories[i], eco.Advisories[j]
		if ra, rb := types.SeverityRank(a.Severity), types.SeverityRank(b.Severity); ra != rb {
			return ra > rb
		}
		if a.Package != b.Package {
			return a.Package < b.Package
		}
		return a.ID < b.ID
	})
	if len(eco.Advisories) > AdvisoryCap {
		eco.Advisories = eco.Advisories[:AdvisoryCap]
	}
}

// ---- npm ----

// npmVia is one entry in a vulnerability's `via` list: either another
// vulnerable package it depends on (a bare name) or an advisory object.
type npmVia struct {
	Package string // set for a bare-name entry
	Source  any    // advisory number; set for an advisory object
	Title   string
	URL     string
	Sev     any
}

func (v *npmVia) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		v.Package = s
		return nil
	}
	var obj struct {
		Source   any    `json:"source"`
		Title    string `json:"title"`
		Name     string `json:"name"`
		URL      string `json:"url"`
		Severity any    `json:"severity"`
	}
	if err := json.Unmarshal(b, &obj); err == nil {
		v.Source, v.Title, v.URL, v.Sev = obj.Source, obj.Title, obj.URL, obj.Severity
		if v.Title == "" {
			v.Title = obj.Name
		}
		return nil
	}
	return nil // skip unrecognized shape
}

// isAdvisory reports whether the entry is an advisory object rather than a
// reference to another vulnerable package.
func (v npmVia) isAdvisory() bool { return v.Package == "" }

// id is the advisory's GHSA ID, or its npm source number when there is no URL.
func (v npmVia) id() string {
	if g := ghsaFromURL(v.URL); g != "" {
		return g
	}
	if v.Source != nil {
		return fmt.Sprint(v.Source)
	}
	return v.Title
}

type npmVuln struct {
	Name     string   `json:"name"`
	Severity any      `json:"severity"`
	Via      []npmVia `json:"via"`
	Range    string   `json:"range"`
}

type npmOutput struct {
	Vulnerabilities map[string]npmVuln `json:"vulnerabilities"`
	// Error is set when npm could not run the audit at all, e.g. ENOLOCK when
	// there is no lockfile. npm still prints valid JSON in that case.
	Error *struct {
		Code    string `json:"code"`
		Summary string `json:"summary"`
	} `json:"error"`
}

// ParseNpm normalizes raw `npm audit --json` output into a cache.Ecosystem,
// leaving out advisories in ignore. A package that is only vulnerable through
// ignored advisories, directly or through its dependencies, is left out too.
func ParseNpm(raw []byte, auditPath string, ignore IgnoreSet) (cache.Ecosystem, error) {
	var out npmOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		return cache.Ecosystem{}, fmt.Errorf("decode npm json: %w", err)
	}
	if out.Error != nil {
		msg := strings.TrimSpace(out.Error.Code + ": " + out.Error.Summary)
		return cache.Ecosystem{}, fmt.Errorf("npm audit failed: %s", strings.TrimPrefix(msg, ": "))
	}

	eco := cache.Ecosystem{
		Status:    types.StatusOK,
		AuditPath: auditPath,
	}

	// Drop ignored advisories, then work out which packages are still
	// vulnerable and how badly. A package is vulnerable through its own
	// advisories or through a dependency that is still vulnerable, so this
	// repeats until nothing changes.
	severity := map[string]string{}
	kept := map[string][]npmVia{}
	for pkg, v := range out.Vulnerabilities {
		sev := types.SevClean
		for _, via := range v.Via {
			if via.isAdvisory() && ignore.matches(via.id(), fmt.Sprint(via.Source)) {
				eco.Ignored++
				eco.IgnoredKeys = append(eco.IgnoredKeys, npmName(pkg, v)+" "+via.id())
				continue
			}
			kept[pkg] = append(kept[pkg], via)
			if via.isAdvisory() {
				sev = worse(sev, normSeverity(via.Sev))
			}
		}
		if len(ignore) == 0 {
			sev = normSeverity(v.Severity)
		}
		severity[pkg] = sev
	}
	for changed := len(ignore) > 0; changed; {
		changed = false
		for pkg, vias := range kept {
			for _, via := range vias {
				if via.isAdvisory() {
					continue
				}
				if dep, ok := severity[via.Package]; ok && dep != types.SevClean {
					if w := worse(severity[pkg], dep); w != severity[pkg] {
						severity[pkg] = w
						changed = true
					}
				}
			}
		}
	}

	for pkg, v := range out.Vulnerabilities {
		sev := severity[pkg]
		if sev == types.SevClean {
			continue
		}
		name := npmName(pkg, v)
		adv := cache.Advisory{
			ID:       pkg + "@" + v.Range,
			Package:  name,
			Severity: sev,
			Affected: v.Range,
		}
		titles := make([]string, 0, len(kept[pkg]))
		for _, via := range kept[pkg] {
			if !via.isAdvisory() {
				titles = append(titles, via.Package)
				continue
			}
			titles = append(titles, via.Title)
			adv.Keys = append(adv.Keys, name+" "+via.id())
			if via.URL != "" {
				adv.Links = append(adv.Links, via.URL)
			}
		}
		adv.Title = strings.Join(titles, "; ")
		eco.Advisories = append(eco.Advisories, adv)
		incCount(&eco.Counts, sev)
	}
	finish(&eco)
	return eco, nil
}

// npmName is the package name for a vulnerabilities entry.
func npmName(key string, v npmVuln) string {
	if v.Name != "" {
		return v.Name
	}
	return key
}

// worse returns the more severe of two buckets. SevClean ranks lowest.
func worse(a, b string) string {
	if types.SeverityRank(b) > types.SeverityRank(a) {
		return b
	}
	return a
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

// auditTimeout bounds a single ecosystem audit subprocess. A hung `composer
// audit` or `npm audit` (network stall, prompt on stdin) would otherwise pin the
// site lock forever. Overridden in tests.
var auditTimeout = 120 * time.Second

// auditWaitDelay bounds how long we wait for output pipes to drain after the
// deadline kills the process. Without it, a grandchild that inherited stdout
// (composer/npm shelling out further) keeps the pipe open and Run blocks past
// the timeout — defeating it. Overridden in tests.
var auditWaitDelay = 5 * time.Second

// RunComposer executes `composer audit` in cpath and returns a normalized Ecosystem.
// Errors are returned as Ecosystem{Status:error}, not as Go errors.
// `bin` may include leading args (e.g. "/usr/bin/php7.4 /home/deploy/bin/composer")
// for multi-PHP setups.
func RunComposer(ctx context.Context, cpath, bin string, ignore IgnoreSet) cache.Ecosystem {
	exe, prefix, err := resolveBin(bin)
	if err != nil {
		return errBlock(cpath, fmt.Sprintf("composer binary not found: %s", bin))
	}
	ctx, cancel := context.WithTimeout(ctx, auditTimeout)
	defer cancel()
	args := append(prefix, "audit", "--format=json", "--no-interaction", "--locked")
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Dir = cpath
	cmd.WaitDelay = auditWaitDelay
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// composer audit exits non-zero when advisories exist — that's not a failure.
	_ = cmd.Run()

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return errBlock(cpath, fmt.Sprintf("composer audit timed out after %s", auditTimeout))
	}
	if !json.Valid(stdout.Bytes()) {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = "composer audit produced no parseable JSON"
		}
		return errBlock(cpath, truncate(msg, 1000))
	}
	eco, err := ParseComposer(stdout.Bytes(), cpath, ignore)
	if err != nil {
		return errBlock(cpath, err.Error())
	}
	return eco
}

// RunNpm executes `npm audit` in npath and returns a normalized Ecosystem.
func RunNpm(ctx context.Context, npath, bin string, ignore IgnoreSet) cache.Ecosystem {
	exe, prefix, err := resolveBin(bin)
	if err != nil {
		return errBlock(npath, fmt.Sprintf("npm binary not found: %s", bin))
	}
	ctx, cancel := context.WithTimeout(ctx, auditTimeout)
	defer cancel()
	args := append(prefix, "audit", "--json", "--audit-level=info")
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Dir = npath
	cmd.WaitDelay = auditWaitDelay
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	_ = cmd.Run()

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return errBlock(npath, fmt.Sprintf("npm audit timed out after %s", auditTimeout))
	}
	if !json.Valid(stdout.Bytes()) {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = "npm audit produced no parseable JSON"
		}
		return errBlock(npath, truncate(msg, 1000))
	}
	eco, err := ParseNpm(stdout.Bytes(), npath, ignore)
	if err != nil {
		return errBlock(npath, err.Error())
	}
	return eco
}

// RunSite runs both ecosystems (per type) for one site and writes the cache.
// Acquires a per-site lock; returns immediately with errLocked if already in flight.
var errLocked = errors.New("audit already in flight for this site")

func RunSite(ctx context.Context, settings config.Resolved, site config.Site) error {
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

	ignore := NewIgnoreSet(site.Ignore)
	start := time.Now()
	composerEco := naBlock()
	npmEco := naBlock()

	switch site.Type {
	case types.TypeComposer:
		composerEco = RunComposer(ctx, site.ResolvedComposerPath(), composerBin, ignore)
	case types.TypeNPM:
		npmEco = RunNpm(ctx, site.ResolvedNPMPath(), npmBin, ignore)
	case types.TypeBoth:
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			composerEco = RunComposer(ctx, site.ResolvedComposerPath(), composerBin, ignore)
		}()
		go func() {
			defer wg.Done()
			npmEco = RunNpm(ctx, site.ResolvedNPMPath(), npmBin, ignore)
		}()
		wg.Wait()
	}

	if prev, err := cache.Read(site.Path); err == nil {
		markChanges(&prev.Composer, &composerEco)
		markChanges(&prev.NPM, &npmEco)
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

// markChanges compares cur with the previous result for the same ecosystem
// and records which advisories are new and which were fixed. It only compares
// two successful audits: after a failed one, everything would look new.
func markChanges(prev, cur *cache.Ecosystem) {
	if prev.Status != types.StatusOK || cur.Status != types.StatusOK {
		return
	}
	// Results cached before advisory keys were recorded have findings but no
	// keys. Comparing against them would mark every advisory as new.
	if len(prev.Seen) == 0 && prev.Counts.Total() > 0 {
		return
	}
	before := make(map[string]bool, len(prev.Seen))
	for _, k := range prev.Seen {
		before[k] = true
	}
	ignored := make(map[string]bool, len(cur.IgnoredKeys))
	for _, k := range cur.IgnoredKeys {
		ignored[k] = true
	}
	now := make(map[string]bool, len(cur.Seen))
	for _, k := range cur.Seen {
		now[k] = true
		if !before[k] && !ignored[k] {
			cur.New = append(cur.New, k)
		}
	}
	for _, k := range prev.Seen {
		if !now[k] {
			cur.Resolved = append(cur.Resolved, k)
		}
	}
	for i, a := range cur.Advisories {
		for _, k := range a.Keys {
			if !before[k] {
				cur.Advisories[i].New = true
				break
			}
		}
	}
}

// RunMany runs audits for many sites with a parallelism cap.
// Errors per site are returned in a map keyed by site name. errLocked is
// surfaced as a skip (not a failure).
func RunMany(ctx context.Context, settings config.Resolved, sites []config.Site) map[string]error {
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

// resolveBin splits a bin string into its executable and any leading args.
// Accepts plain names ("composer"), absolute paths ("/usr/bin/composer"),
// paths containing spaces ("/Users/x/Library/Application Support/Herd/bin/composer"),
// and wrapper forms like "/usr/bin/php7.4 /home/deploy/bin/composer" used on
// multi-PHP boxes.
//
// Resolution order:
//  1. Try the entire string as a single path (handles spaces in the path).
//  2. Fall back to whitespace-splitting (handles wrapper forms).
func resolveBin(bin string) (string, []string, error) {
	trimmed := strings.TrimSpace(bin)
	if trimmed == "" {
		return "", nil, exec.ErrNotFound
	}
	if exe, err := exec.LookPath(trimmed); err == nil {
		return exe, nil, nil
	}
	parts := strings.Fields(trimmed)
	if len(parts) == 0 {
		return "", nil, exec.ErrNotFound
	}
	exe, err := exec.LookPath(parts[0])
	if err != nil {
		return "", nil, err
	}
	return exe, parts[1:], nil
}

// truncate caps s at n runes, appending an ellipsis if it had to cut.
// Rune-based, not byte-based: slicing bytes can land mid-rune and produce
// invalid UTF-8, and these strings are subprocess stderr, which is arbitrary
// text (non-ASCII package names, localized error messages).
func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}
