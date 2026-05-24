package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jeromecoloma/webaudt/internal/types"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// internal/audit/ -> repo root -> tests/fixtures/
	root := filepath.Join(wd, "..", "..")
	b, err := os.ReadFile(filepath.Join(root, "tests", "fixtures", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

func TestParseComposer_Clean(t *testing.T) {
	eco, err := ParseComposer(readFixture(t, "composer-audit-clean.json"), "/site")
	if err != nil {
		t.Fatal(err)
	}
	if eco.Status != types.StatusOK {
		t.Errorf("status: got %q want ok", eco.Status)
	}
	if eco.Counts.Total() != 0 {
		t.Errorf("total: got %d want 0", eco.Counts.Total())
	}
	if eco.AuditPath != "/site" {
		t.Errorf("audit_path: got %q", eco.AuditPath)
	}
}

func TestParseComposer_Vulns(t *testing.T) {
	eco, err := ParseComposer(readFixture(t, "composer-audit-vulns.json"), "/site")
	if err != nil {
		t.Fatal(err)
	}
	if eco.Counts.Critical != 1 || eco.Counts.High != 1 || eco.Counts.Moderate != 1 {
		t.Errorf("counts: %+v", eco.Counts)
	}
	for _, a := range eco.Advisories {
		if a.Severity == "medium" {
			t.Errorf("medium leaked into normalized output: %+v", a)
		}
	}
}

func TestParseComposer_NullSeverity(t *testing.T) {
	raw := []byte(`{"advisories":{"v/x":[{"advisoryId":"A","cve":"CVE-1","title":"t","affectedVersions":"*","severity":null}]}}`)
	eco, err := ParseComposer(raw, "/x")
	if err != nil {
		t.Fatal(err)
	}
	if eco.Counts.Unknown != 1 {
		t.Errorf("unknown count: got %d want 1 (%+v)", eco.Counts.Unknown, eco.Counts)
	}
}

func TestParseComposer_AdvisoryCap(t *testing.T) {
	// Synthesize a fixture with 75 advisories.
	advs := make([]string, 0, 75)
	for i := 0; i < 75; i++ {
		advs = append(advs, `{"advisoryId":"A","cve":"CVE","title":"t","affectedVersions":"*","severity":"low"}`)
	}
	raw := []byte(`{"advisories":{"v/x":[`)
	for i, a := range advs {
		if i > 0 {
			raw = append(raw, ',')
		}
		raw = append(raw, []byte(a)...)
	}
	raw = append(raw, []byte(`]}}`)...)
	eco, err := ParseComposer(raw, "/x")
	if err != nil {
		t.Fatal(err)
	}
	if len(eco.Advisories) != AdvisoryCap {
		t.Errorf("advisories cap: got %d want %d", len(eco.Advisories), AdvisoryCap)
	}
}

func TestParseNpm_Clean(t *testing.T) {
	eco, err := ParseNpm(readFixture(t, "npm-audit-clean.json"), "/site/www")
	if err != nil {
		t.Fatal(err)
	}
	if eco.Counts.Total() != 0 {
		t.Errorf("total: %d", eco.Counts.Total())
	}
	if eco.AuditPath != "/site/www" {
		t.Errorf("audit_path: %q", eco.AuditPath)
	}
}

func TestParseNpm_Vulns(t *testing.T) {
	eco, err := ParseNpm(readFixture(t, "npm-audit-vulns.json"), "/x")
	if err != nil {
		t.Fatal(err)
	}
	if eco.Counts.High != 1 || eco.Counts.Moderate != 1 {
		t.Errorf("counts: %+v", eco.Counts)
	}
	if len(eco.Advisories) != 2 {
		t.Errorf("advisory count: %d", len(eco.Advisories))
	}
	foundProto := false
	for _, a := range eco.Advisories {
		if strings.Contains(a.Title, "Prototype pollution") {
			foundProto = true
		}
	}
	if !foundProto {
		t.Errorf("expected to find Prototype pollution title; got %+v", eco.Advisories)
	}
}

func TestNormSeverity(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{nil, types.SevUnknown},
		{"", types.SevUnknown},
		{"CRITICAL", types.SevCritical},
		{"Medium", types.SevModerate},
		{"info", types.SevInfo},
		{"weird", types.SevUnknown},
	}
	for _, c := range cases {
		if got := normSeverity(c.in); got != c.want {
			t.Errorf("normSeverity(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}
