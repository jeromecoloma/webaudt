// Package types defines the core domain types shared across webaudt.
package types

// CacheSchemaVersion is bumped when the on-disk cache format changes
// in a way that older versions can't read.
const CacheSchemaVersion = 2

// Severity buckets, ordered worst → least severe.
const (
	SevCritical = "critical"
	SevHigh     = "high"
	SevUnknown  = "unknown" // advisory present but composer/npm gave no severity rating
	SevModerate = "moderate"
	SevLow      = "low"
	SevInfo     = "info"
	SevClean    = "clean"
	SevError    = "error"
	SevNever    = "never"
	SevRunning  = "running"
)

// SeverityRank gives a numeric rank for comparing buckets. Higher = worse.
func SeverityRank(s string) int {
	switch s {
	case SevCritical:
		return 60
	case SevHigh:
		return 50
	case SevUnknown:
		return 40
	case SevModerate:
		return 30
	case SevLow:
		return 20
	case SevInfo:
		return 10
	case SevError:
		return 5 // surfaces as a problem, ranked below info so any real finding outranks it
	default:
		return 0
	}
}

// SiteType is which ecosystems are audited for a site.
type SiteType string

const (
	TypeComposer SiteType = "composer"
	TypeNPM      SiteType = "npm"
	TypeBoth     SiteType = "both"
)

// EcosystemStatus is the per-ecosystem result of an audit run.
type EcosystemStatus string

const (
	StatusOK            EcosystemStatus = "ok"
	StatusNotApplicable EcosystemStatus = "not_applicable"
	StatusErrored       EcosystemStatus = "error"
)
