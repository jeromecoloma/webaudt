package audit

import (
	"slices"
	"strings"
	"testing"

	"github.com/jeromecoloma/webaudt/internal/cache"
	"github.com/jeromecoloma/webaudt/internal/types"
)

// npm prints valid JSON with an "error" object when it cannot audit at all,
// e.g. no lockfile. That must be a failed audit, not a clean one.
func TestParseNpm_ErrorIsNotClean(t *testing.T) {
	raw := []byte(`{"error":{"code":"ENOLOCK","summary":"This command requires an existing lockfile.","detail":"..."}}`)
	_, err := ParseNpm(raw, "/x", nil)
	if err == nil || !strings.Contains(err.Error(), "ENOLOCK") {
		t.Fatalf("err = %v, want an ENOLOCK error", err)
	}
}

const npmTwoAdvisories = `{"vulnerabilities":{
  "lodash":{"name":"lodash","severity":"high","range":"<4.17.21","via":[
    {"source":1,"name":"lodash","title":"Command Injection","url":"https://github.com/advisories/GHSA-aaaa-aaaa-aaaa","severity":"high"},
    {"source":2,"name":"lodash","title":"ReDoS","url":"https://github.com/advisories/GHSA-bbbb-bbbb-bbbb","severity":"moderate"}]},
  "wrapper":{"name":"wrapper","severity":"high","range":"*","via":["lodash"]}
}}`

func TestParseNpm_LinksAndKeys(t *testing.T) {
	eco, err := ParseNpm([]byte(npmTwoAdvisories), "/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	if eco.Counts.High != 2 || eco.Counts.Total() != 2 {
		t.Errorf("counts = %+v, want 2 high", eco.Counts)
	}
	var lodash cache.Advisory
	for _, a := range eco.Advisories {
		if a.Package == "lodash" {
			lodash = a
		}
	}
	if len(lodash.Links) != 2 || lodash.Links[0] != "https://github.com/advisories/GHSA-aaaa-aaaa-aaaa" {
		t.Errorf("links = %v", lodash.Links)
	}
	want := []string{"lodash GHSA-aaaa-aaaa-aaaa", "lodash GHSA-bbbb-bbbb-bbbb"}
	if !slices.Equal(eco.Seen, want) {
		t.Errorf("seen = %v, want %v", eco.Seen, want)
	}
}

func TestParseNpm_IgnoreLowersSeverity(t *testing.T) {
	eco, err := ParseNpm([]byte(npmTwoAdvisories), "/x", NewIgnoreSet([]string{"ghsa-aaaa-aaaa-aaaa"}))
	if err != nil {
		t.Fatal(err)
	}
	// Only the moderate advisory is left, and the package that depends on
	// lodash follows it down.
	if eco.Counts.Moderate != 2 || eco.Counts.Total() != 2 {
		t.Errorf("counts = %+v, want 2 moderate", eco.Counts)
	}
	if eco.Ignored != 1 {
		t.Errorf("ignored = %d, want 1", eco.Ignored)
	}
	// The ignored advisory is still recorded as found.
	want := []string{"lodash GHSA-aaaa-aaaa-aaaa", "lodash GHSA-bbbb-bbbb-bbbb"}
	if !slices.Equal(eco.Seen, want) {
		t.Errorf("seen = %v, want %v", eco.Seen, want)
	}
}

func TestParseNpm_IgnoreAllDropsDependents(t *testing.T) {
	eco, err := ParseNpm([]byte(npmTwoAdvisories), "/x", NewIgnoreSet([]string{"GHSA-aaaa-aaaa-aaaa", "2"}))
	if err != nil {
		t.Fatal(err)
	}
	if eco.Counts.Total() != 0 || len(eco.Advisories) != 0 {
		t.Errorf("want clean, got counts %+v advisories %+v", eco.Counts, eco.Advisories)
	}
	if eco.Ignored != 2 {
		t.Errorf("ignored = %d, want 2", eco.Ignored)
	}
}

func TestParseComposer_IgnoreLinksAbandoned(t *testing.T) {
	raw := []byte(`{"advisories":{"v/x":[
	  {"advisoryId":"PKSA-1","cve":"CVE-1","title":"a","link":"https://github.com/advisories/GHSA-cccc-cccc-cccc","severity":"high","affectedVersions":"*"},
	  {"advisoryId":"PKSA-2","cve":"CVE-2","title":"b","link":"https://x/2","severity":"low","affectedVersions":"*"},
	  {"advisoryId":"PKSA-3","cve":null,"title":"c","severity":"low","affectedVersions":"*"}]},
	  "abandoned":{"swiftmailer/swiftmailer":"symfony/mailer","old/pkg":null}}`)
	// Each of composer's IDs works: GHSA from the link, CVE, PKSA.
	for _, id := range []string{"GHSA-cccc-cccc-cccc", "cve-1", "PKSA-1"} {
		eco, err := ParseComposer(raw, "/x", NewIgnoreSet([]string{id}))
		if err != nil {
			t.Fatal(err)
		}
		if eco.Counts.High != 0 || eco.Counts.Low != 2 || eco.Ignored != 1 {
			t.Errorf("ignore %s: counts %+v ignored %d", id, eco.Counts, eco.Ignored)
		}
	}

	eco, err := ParseComposer(raw, "/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	if eco.Advisories[0].ID != "CVE-1" || eco.Advisories[0].Links[0] != "https://github.com/advisories/GHSA-cccc-cccc-cccc" {
		t.Errorf("first advisory = %+v, want the high one with its link", eco.Advisories[0])
	}
	want := []cache.Abandoned{{Package: "old/pkg"}, {Package: "swiftmailer/swiftmailer", Replacement: "symfony/mailer"}}
	if !slices.Equal(eco.Abandoned, want) {
		t.Errorf("abandoned = %+v, want %+v", eco.Abandoned, want)
	}
}

// The cap keeps the worst advisories, whatever order composer lists them in.
func TestParseComposer_CapKeepsWorst(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"advisories":{"v/x":[`)
	for i := 0; i < AdvisoryCap+10; i++ {
		b.WriteString(`{"advisoryId":"L","cve":"CVE-L","title":"t","affectedVersions":"*","severity":"low"},`)
	}
	b.WriteString(`{"advisoryId":"C","cve":"CVE-C","title":"t","affectedVersions":"*","severity":"critical"}]}}`)
	eco, err := ParseComposer([]byte(b.String()), "/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	if eco.Advisories[0].Severity != types.SevCritical {
		t.Errorf("first advisory = %+v, want the critical one", eco.Advisories[0])
	}
}

func TestMarkChanges(t *testing.T) {
	ok := func(keys ...string) cache.Ecosystem {
		eco := cache.Ecosystem{Status: types.StatusOK, Seen: keys}
		for _, k := range keys {
			eco.Advisories = append(eco.Advisories, cache.Advisory{ID: k, Keys: []string{k}})
			eco.Counts.High++
		}
		return eco
	}

	prev, cur := ok("a", "b"), ok("b", "c")
	markChanges(&prev, &cur)
	if !slices.Equal(cur.New, []string{"c"}) || !slices.Equal(cur.Resolved, []string{"a"}) {
		t.Errorf("new = %v resolved = %v, want [c] [a]", cur.New, cur.Resolved)
	}
	if cur.Advisories[0].New || !cur.Advisories[1].New {
		t.Errorf("New flags = %v %v, want false true", cur.Advisories[0].New, cur.Advisories[1].New)
	}

	// After a failed audit there is nothing to compare against.
	failed, cur := cache.Ecosystem{Status: types.StatusErrored}, ok("a")
	markChanges(&failed, &cur)
	if len(cur.New) != 0 || cur.Advisories[0].New {
		t.Errorf("compared against a failed audit: %+v", cur)
	}

	// Results cached before keys were recorded have findings but no keys.
	old, cur := cache.Ecosystem{Status: types.StatusOK, Counts: cache.Counts{High: 1}}, ok("a")
	markChanges(&old, &cur)
	if len(cur.New) != 0 {
		t.Errorf("compared against a cache with no keys: new = %v", cur.New)
	}

	// Ignoring an advisory does not make it fixed, and an ignored advisory
	// that first shows up is not reported as new.
	prev, cur = ok("a"), ok()
	cur.Seen, cur.IgnoredKeys = []string{"a", "b"}, []string{"a", "b"}
	markChanges(&prev, &cur)
	if len(cur.New) != 0 || len(cur.Resolved) != 0 {
		t.Errorf("ignored keys: new = %v resolved = %v, want none", cur.New, cur.Resolved)
	}

	// A clean previous audit means everything found now is new.
	clean, cur := cache.Ecosystem{Status: types.StatusOK}, ok("a")
	markChanges(&clean, &cur)
	if !slices.Equal(cur.New, []string{"a"}) {
		t.Errorf("after a clean audit: new = %v, want [a]", cur.New)
	}
}
