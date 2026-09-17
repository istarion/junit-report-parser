// Command junit-results turns raw JUnit XML reports into an agent-readable
// digest on stdout (spec: junit-results-design.md).
package main

import (
	"os"

	"github.com/istarion/junit-report-parser/internal/cli"
	"github.com/istarion/junit-report-parser/internal/clock"
)

// version is injected at build time: -ldflags "-X main.version=$(VERSION)".
var version = "0.1.0"

func main() {
	os.Exit(cli.Execute(version, clock.Real()))
}
