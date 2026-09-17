// Package cli wires the cobra command tree, flag surface, exit-code mapping
// and command implementations (plan §10).
package cli

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/istarion/junit-report-parser/internal/clock"
)

// Process exit codes (spec §16 + plan D5).
const (
	ExitOK            = 0 // parsed, no failures/errors (grep: ≥1 match)
	ExitTestsFailed   = 1 // ≥1 failure or error
	ExitNoReports     = 2 // no report files found
	ExitUsage         = 3 // invalid usage, bad flag, bad value, not implemented
	ExitNoneParseable = 4 // reports found but none parseable
	ExitReportWrite   = 5 // report write refused/failed (unused until P3)
	ExitGrepNoMatch   = 6 // grep: no matches (unused until P4)
)

// Env bundles what command implementations need: streams, clock, version.
type Env struct {
	Version string
	Clock   clock.Clock
	Stdout  io.Writer
	Stderr  io.Writer
}

// UsageError: invalid usage, unknown flag, bad flag value → exit 3.
type UsageError struct{ Msg string }

func (e *UsageError) Error() string { return e.Msg }

// NotYetError: command/feature not implemented in this phase → exit 3.
type NotYetError struct{ Cmd string }

func (e *NotYetError) Error() string {
	return fmt.Sprintf("junit-results %s: not implemented yet", e.Cmd)
}

// TestsFailedError: parsed; ≥1 failure or error → exit 1.
type TestsFailedError struct{}

func (*TestsFailedError) Error() string { return "tests failed" }

// NoReportsError: no report files found/selected → exit 2.
type NoReportsError struct{ Msg string }

func (e *NoReportsError) Error() string { return e.Msg }

// NoneParseableError: reports found but none parseable → exit 4.
type NoneParseableError struct{ Msg string }

func (e *NoneParseableError) Error() string { return e.Msg }

// ReportWriteError → exit 5 (reserved for --report, P3).
type ReportWriteError struct{ Msg string }

func (e *ReportWriteError) Error() string { return e.Msg }

// GrepNoMatchError → exit 6 (reserved for grep, P4; plan D5).
type GrepNoMatchError struct{}

func (*GrepNoMatchError) Error() string { return "no matches" }

// ExitCode maps a RunE error to the process exit code (plan §10).
func ExitCode(err error) int {
	switch err.(type) {
	case nil:
		return ExitOK
	case *TestsFailedError:
		return ExitTestsFailed
	case *NoReportsError:
		return ExitNoReports
	case *UsageError, *NotYetError:
		return ExitUsage
	case *NoneParseableError:
		return ExitNoneParseable
	case *ReportWriteError:
		return ExitReportWrite
	case *GrepNoMatchError:
		return ExitGrepNoMatch
	default:
		return ExitUsage
	}
}

// Execute builds the command tree with the real environment, runs it, and
// returns the process exit code. Error text goes to stderr (root has
// SilenceErrors/SilenceUsage); usage errors additionally print usage.
func Execute(version string, clk clock.Clock) int {
	env := &Env{
		Version: version,
		Clock:   clk,
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
	}
	root := NewRoot(env)
	err := root.Execute()
	if err == nil {
		return ExitOK
	}
	reportError(env, root, err)
	return ExitCode(err)
}

// reportError writes the standard stderr presentation of a RunE error.
// Tests call it too, so they observe exactly what Execute would print.
func reportError(env *Env, root *cobra.Command, err error) {
	switch e := err.(type) {
	case *NotYetError:
		fmt.Fprintln(env.Stderr, e.Error())
	case *TestsFailedError:
		// Silent: the digest on stdout already carries verdict=fail and the
		// exit code carries the outcome.
	case *UsageError:
		fmt.Fprintf(env.Stderr, "junit-results: %s\n", e.Msg)
		fmt.Fprintln(env.Stderr, root.UsageString())
	default:
		if msg := err.Error(); msg != "" {
			fmt.Fprintf(env.Stderr, "junit-results: %s\n", msg)
		}
	}
}

// isTerminal reports whether w is a character device (TTY). Non-file
// writers (buffers in tests) are never terminals, so piped runs and the
// acceptance harness never see ANSI (spec §10.4).
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// now is a tiny helper so command code reads the injected clock.
func now(env *Env) time.Time { return env.Clock.Now() }
