package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubBin puts an executable named `name` on PATH that prints `output`.
func stubBin(t *testing.T, dir, name, output string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\necho '"+output+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// isolatedPath points PATH at an empty temp dir, so only the stubs we install
// are visible — a real composer/npm on the dev machine can't skew the result.
func isolatedPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	return dir
}

func TestRunOne_OK(t *testing.T) {
	dir := isolatedPath(t)
	stubBin(t, dir, "composer", "Composer version 2.7.1 2024-02-09")

	got := runOne(Check{
		Name:      "composer",
		MinVer:    "2.0",
		ExtractFn: extractFromCmd("composer", []string{"--version"}, `(\d+\.\d+(\.\d+)?)`),
	})

	if got.Status != StatusOK {
		t.Errorf("status = %v, want StatusOK", got.Status)
	}
	if got.Version != "2.7.1" {
		t.Errorf("version = %q, want 2.7.1", got.Version)
	}
}

func TestRunOne_Missing(t *testing.T) {
	isolatedPath(t) // nothing installed

	got := runOne(Check{
		Name:      "composer",
		MinVer:    "2.0",
		ExtractFn: extractFromCmd("composer", []string{"--version"}, `(\d+\.\d+(\.\d+)?)`),
	})

	if got.Status != StatusMissing {
		t.Errorf("status = %v, want StatusMissing", got.Status)
	}
}

func TestRunOne_Outdated(t *testing.T) {
	dir := isolatedPath(t)
	stubBin(t, dir, "npm", "6.14.18")

	got := runOne(Check{
		Name:      "npm",
		MinVer:    "7.0",
		ExtractFn: extractFromCmd("npm", []string{"--version"}, `(\d+\.\d+(\.\d+)?)`),
	})

	if got.Status != StatusOutdated {
		t.Errorf("status = %v, want StatusOutdated", got.Status)
	}
	if got.Version != "6.14.18" {
		t.Errorf("version = %q", got.Version)
	}
}

// An installed tool whose version we cannot parse must report Unknown, not
// Missing. Previously every extract error collapsed to Missing, so doctor told
// the user to install a composer that was sitting right there on PATH.
func TestRunOne_InstalledButUnparseable(t *testing.T) {
	dir := isolatedPath(t)
	stubBin(t, dir, "composer", "some wrapper printed no version at all")

	got := runOne(Check{
		Name:      "composer",
		MinVer:    "2.0",
		ExtractFn: extractFromCmd("composer", []string{"--version"}, `(\d+\.\d+(\.\d+)?)`),
	})

	if got.Status != StatusUnknown {
		t.Fatalf("status = %v, want StatusUnknown", got.Status)
	}

	line := formatLine(got)
	if want := "installed, version unparseable"; !strings.Contains(line, want) {
		t.Errorf("formatLine = %q, want it to contain %q", line, want)
	}
}

func TestVerGE(t *testing.T) {
	cases := []struct {
		have, want string
		ok         bool
	}{
		{"2.7.1", "2.0", true},
		{"2.0", "2.0", true},
		{"1.10.0", "2.0", false},
		{"6.14.18", "7.0", false},
		{"10.2.4", "7.0", true}, // numeric, not lexicographic
		{"2.0.1", "2.0.0", true},
	}
	for _, c := range cases {
		if got := verGE(c.have, c.want); got != c.ok {
			t.Errorf("verGE(%q, %q) = %v, want %v", c.have, c.want, got, c.ok)
		}
	}
}
