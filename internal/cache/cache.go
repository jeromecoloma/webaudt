// Package cache stores per-site audit results on disk and manages advisory
// locks during in-flight audit runs.
package cache

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jeromecoloma/webaudt/internal/config"
	"github.com/jeromecoloma/webaudt/internal/types"
)

// Counts is the per-severity tally for one ecosystem.
type Counts struct {
	Critical int `json:"critical"`
	High     int `json:"high"`
	Unknown  int `json:"unknown"`
	Moderate int `json:"moderate"`
	Low      int `json:"low"`
	Info     int `json:"info"`
}

// Total returns the sum across all buckets.
func (c Counts) Total() int {
	return c.Critical + c.High + c.Unknown + c.Moderate + c.Low + c.Info
}

// Worst returns the most severe bucket that has at least one finding, or
// types.SevClean when all buckets are zero.
func (c Counts) Worst() string {
	switch {
	case c.Critical > 0:
		return types.SevCritical
	case c.High > 0:
		return types.SevHigh
	case c.Unknown > 0:
		return types.SevUnknown
	case c.Moderate > 0:
		return types.SevModerate
	case c.Low > 0:
		return types.SevLow
	case c.Info > 0:
		return types.SevInfo
	default:
		return types.SevClean
	}
}

// Advisory is one entry in an ecosystem's advisories list.
type Advisory struct {
	ID       string `json:"id"`
	Package  string `json:"package"`
	Severity string `json:"severity"`
	Title    string `json:"title"`
	Affected string `json:"affected"`
}

// Ecosystem is the per-ecosystem result of an audit run.
type Ecosystem struct {
	Status     types.EcosystemStatus `json:"status"`
	AuditPath  string                `json:"audit_path,omitempty"`
	Counts     Counts                `json:"counts,omitempty"`
	Advisories []Advisory            `json:"advisories,omitempty"`
	Error      string                `json:"error,omitempty"`
}

// Entry is the full cached audit result for one site.
type Entry struct {
	SchemaVersion int       `json:"schema_version"`
	Name          string    `json:"name"`
	Path          string    `json:"path"`
	CheckedAt     int64     `json:"checked_at"`
	DurationMS    int64     `json:"duration_ms"`
	Composer      Ecosystem `json:"composer"`
	NPM           Ecosystem `json:"npm"`
}

// Worst returns the most severe bucket across both ecosystems. Returns
// types.SevError if either ecosystem failed and nothing else outranks it.
func (e Entry) Worst() string {
	w := types.SevClean
	for _, eco := range []Ecosystem{e.Composer, e.NPM} {
		if eco.Status == types.StatusErrored && types.SeverityRank(w) < types.SeverityRank(types.SevError) {
			w = types.SevError
		}
		if eco.Status == types.StatusOK {
			cw := eco.Counts.Worst()
			if cw != types.SevClean && types.SeverityRank(cw) > types.SeverityRank(w) {
				w = cw
			}
		}
	}
	return w
}

// hash is the per-site cache key — first 8 chars of sha1(path).
func hash(path string) string {
	sum := sha1.Sum([]byte(path))
	return hex.EncodeToString(sum[:])[:8]
}

// FileFor returns the cache file path for a given site path.
func FileFor(sitePath string) string {
	return filepath.Join(config.CacheDir(), "sites", hash(sitePath)+".json")
}

// LockFor returns the lock dir path for a given site path.
func LockFor(sitePath string) string {
	return filepath.Join(config.CacheDir(), "lock", hash(sitePath)+".lock")
}

// Exists reports whether a cache file is present for the given site.
func Exists(sitePath string) bool {
	_, err := os.Stat(FileFor(sitePath))
	return err == nil
}

// Read loads and decodes the cache entry for a site. Returns os.ErrNotExist when missing.
func Read(sitePath string) (*Entry, error) {
	b, err := os.ReadFile(FileFor(sitePath))
	if err != nil {
		return nil, err
	}
	var e Entry
	if err := json.Unmarshal(b, &e); err != nil {
		return nil, fmt.Errorf("decode cache: %w", err)
	}
	return &e, nil
}

// Write encodes and writes the cache entry atomically.
func Write(e *Entry) error {
	if err := os.MkdirAll(filepath.Dir(FileFor(e.Path)), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	path := FileFor(e.Path)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Delete removes the cache file for a site (no-op if missing).
func Delete(sitePath string) error {
	err := os.Remove(FileFor(sitePath))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// IsFresh reports whether a cache entry exists and is within ttl seconds of now.
// ttl of 0 means "never fresh, always refresh".
func IsFresh(sitePath string, ttl int) bool {
	if ttl <= 0 {
		return false
	}
	e, err := Read(sitePath)
	if err != nil {
		return false
	}
	age := time.Now().Unix() - e.CheckedAt
	return age < int64(ttl)
}

// AcquireLock creates the lock dir atomically and stamps it with the current PID.
// If a stale lock (owner process no longer alive) is detected, it is reclaimed.
// Returns false if the lock is held by a live process.
func AcquireLock(sitePath string) bool {
	if err := os.MkdirAll(filepath.Dir(LockFor(sitePath)), 0o755); err != nil {
		return false
	}
	if tryMkdirLock(sitePath) {
		return true
	}
	// Already exists — check the pid stamp.
	if isLockStale(sitePath) {
		_ = os.RemoveAll(LockFor(sitePath))
		return tryMkdirLock(sitePath)
	}
	return false
}

func tryMkdirLock(sitePath string) bool {
	dir := LockFor(sitePath)
	if err := os.Mkdir(dir, 0o755); err != nil {
		return false
	}
	_ = os.WriteFile(filepath.Join(dir, "pid"), []byte(strconv.Itoa(os.Getpid())), 0o644)
	return true
}

// isLockStale returns true if the lock's owner pid is missing or dead.
func isLockStale(sitePath string) bool {
	b, err := os.ReadFile(filepath.Join(LockFor(sitePath), "pid"))
	if err != nil {
		return true
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return true
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return true
	}
	// On Unix, signal 0 reports whether the process is alive.
	return p.Signal(syscall.Signal(0)) != nil
}

// ReleaseLock removes the lock dir (no-op if absent).
func ReleaseLock(sitePath string) {
	_ = os.RemoveAll(LockFor(sitePath))
}

// IsLocked reports whether an audit is in flight for the given site by a live process.
func IsLocked(sitePath string) bool {
	if _, err := os.Stat(LockFor(sitePath)); err != nil {
		return false
	}
	return !isLockStale(sitePath)
}
