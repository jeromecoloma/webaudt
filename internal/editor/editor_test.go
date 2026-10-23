package editor

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// fakeBin creates an executable named name in a fresh dir and returns the dir.
func fakeBin(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestCommand(t *testing.T) {
	cases := []struct {
		name          string
		bins          []string
		visual, edit  string
		wantExe       string
		wantArgs      []string
		wantErr       bool
		wantNoEditErr bool
	}{
		{name: "VISUAL wins", bins: []string{"code", "nano", "nvim"}, visual: "code --wait", edit: "nano", wantExe: "code", wantArgs: []string{"--wait", "/f"}},
		{name: "EDITOR next", bins: []string{"nano", "nvim"}, edit: "nano", wantExe: "nano", wantArgs: []string{"/f"}},
		{name: "nvim by default", bins: []string{"nvim", "vim"}, wantExe: "nvim", wantArgs: []string{"/f"}},
		{name: "vim without nvim", bins: []string{"vim", "vi"}, wantExe: "vim", wantArgs: []string{"/f"}},
		{name: "EDITOR not found", bins: []string{"nvim"}, edit: "missing-editor", wantErr: true},
		{name: "nothing installed", wantErr: true, wantNoEditErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("PATH", fakeBin(t, c.bins...))
			t.Setenv("VISUAL", c.visual)
			t.Setenv("EDITOR", c.edit)

			cmd, err := Command("/f")
			if c.wantErr {
				if err == nil {
					t.Fatalf("want error, got %v", cmd.Args)
				}
				if c.wantNoEditErr && !errors.Is(err, ErrNoEditor) {
					t.Errorf("err = %v, want ErrNoEditor", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := filepath.Base(cmd.Path); got != c.wantExe {
				t.Errorf("exe = %s, want %s", got, c.wantExe)
			}
			if got := cmd.Args[1:]; !slices.Equal(got, c.wantArgs) {
				t.Errorf("args = %v, want %v", got, c.wantArgs)
			}
		})
	}
}
