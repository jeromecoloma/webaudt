// Package editor opens a file in the user's terminal editor.
package editor

import (
	"errors"
	"os"
	"os/exec"
	"strings"
)

// fallbacks are tried in order when neither $VISUAL nor $EDITOR is set.
var fallbacks = []string{"nvim", "vim", "vi"}

// ErrNoEditor is returned when no editor could be found.
var ErrNoEditor = errors.New("no editor found: set $EDITOR, or install nvim or vim")

// Command returns a command that opens path in the user's editor: $VISUAL,
// then $EDITOR, then the first of nvim, vim or vi on PATH. The variables may
// include arguments, e.g. EDITOR="code --wait". stdin, stdout and stderr are
// left unset for the caller to attach.
func Command(path string) (*exec.Cmd, error) {
	for _, env := range []string{"VISUAL", "EDITOR"} {
		if fields := strings.Fields(os.Getenv(env)); len(fields) > 0 {
			exe, err := exec.LookPath(fields[0])
			if err != nil {
				return nil, errors.New("$" + env + " is set to " + fields[0] + ", which was not found")
			}
			return exec.Command(exe, append(fields[1:], path)...), nil
		}
	}
	for _, name := range fallbacks {
		if exe, err := exec.LookPath(name); err == nil {
			return exec.Command(exe, path), nil
		}
	}
	return nil, ErrNoEditor
}
