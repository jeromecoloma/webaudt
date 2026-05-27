// Command webaudt is a terminal UI for monitoring composer/npm audit findings
// across a registered list of local sites.
package main

import (
	"fmt"
	"os"
)

// Version is the build version. Override via -ldflags "-X main.Version=..." for releases.
var Version = "0.3.0-dev"

func main() {
	root := newRootCmd()
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "webaudt:", err)
		os.Exit(1)
	}
}
