// Command webaudt is a terminal UI for monitoring composer/npm audit findings
// across a registered list of local sites.
package main

import (
	"fmt"
	"os"
)

// Version is the build version. Releases stamp it via
// -ldflags "-X main.Version=<git tag>" (goreleaser, and install.sh's
// build-from-source path). "dev" is the honest answer for an unstamped build —
// hardcoding a release number here meant every local build claimed to be
// whatever the last bump commit said, and the constant drifted from the tags.
var Version = "dev"

func main() {
	root := newRootCmd()
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "webaudt:", err)
		os.Exit(1)
	}
}
