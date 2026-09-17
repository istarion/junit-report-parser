package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

// flags holds parsed flag values for one root invocation, owned by the
// NewRoot closure (not package-global, so tests can build parallel trees).
type flags struct {
	root      string
	paths     []string
	format    string
	report    string
	maxAge    time.Duration
	newerThan string
	color     string
	noColor   bool
	verbose   bool
	noDedupe  bool

	reportCompact bool
	reportForce   bool
	reportNoCases bool
	reportOutput  bool
	failOnStale   bool

	modules  []string
	statuses string
	filter   []string
	exclude  []string

	top         int
	maxFrames   int
	fullStack   bool
	maxMessage  int
	noGroup     bool
	group       bool
	budget      string
	showOutput  string
	outputLines int

	showIDs []string

	grepPatterns      []string
	grepPositional    []string
	grepIn            string
	grepIgnoreCase    bool
	grepFixed         bool
	grepInvert        bool
	grepContext       int
	grepIds           bool
	grepCount         bool
	grepMaxMatches    int
	grepMaxLineLength int

	statsTop  int
	statsBy   string
	statsOnly string
}

// NewRoot builds the full command tree (plan §10): persistent global and
// selection flags on the root, default-command redispatch to summary, and
// typed errors mapped to exit codes by cli.ExitCode.
func NewRoot(env *Env) *cobra.Command {
	f := &flags{top: 5}

	root := &cobra.Command{
		Use:     "junit-results",
		Short:   "Turn JUnit XML reports into an agent-readable digest",
		Version: env.Version,
		// The auto-generated `completion` command is not part of the
		// specified interface (spec §8.1).
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
		// Default command: the root re-dispatches to summary (plan §10).
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSummary(env, f)
		},
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return &UsageError{Msg: fmt.Sprintf("unknown command %q", args[0])}
			}
			return nil
		},
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	// Cobra's own output (--version, --help, usage) goes to the env streams
	// so tests and the harness capture it like real stdout/stderr.
	root.SetOut(env.Stdout)
	root.SetErr(env.Stderr)
	// All flag parse failures are usage errors (exit 3).
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return &UsageError{Msg: err.Error()}
	})

	addGlobalFlags(root, f)
	addSelectionFlags(root, f)

	root.AddCommand(
		summaryCmd(env, f),
		failuresCmd(env, f),
		grepCmd(env, f),
		statsCmd(env, f),
		showCmd(env, f),
		listCmd(env, f),
		discoverCmd(env, f),
	)
	annotateExitCodes(root)
	return root
}

// exitCodeNote documents the exit-code contract (spec §16) on every command
// so `--help` states the grep/show exit-6 convention loudly (D5).
const exitCodeNote = "\n\nExit codes: 0 ok; 1 tests failed (or grep/show matched a failing corpus); " +
	"2 no reports found; 3 invalid usage; 4 reports found but none parseable; " +
	"5 report write refused/failed; 6 no match (grep and show; plan D5). " +
	"Note: grep/show use exit 6 for 'no match', so exit 1 uniformly means the corpus has failures."

// annotateExitCodes appends the exit-code contract to every command's help.
func annotateExitCodes(root *cobra.Command) {
	apply := func(c *cobra.Command) {
		c.Long = c.Long + exitCodeNote
	}
	apply(root)
	for _, c := range root.Commands() {
		if c.Name() == "help" {
			continue
		}
		apply(c)
	}
}

// addGlobalFlags registers the persistent global flags summary owns this
// phase (spec §8.2). Flags owned by later phases (--report-force,
// --report-compact, --report-no-cases, --report-output, --show-output,
// --no-dedupe, --fail-on-stale, --source) are registered by those phases.
// --report is registered now but refuses to run (exit 3).
func addGlobalFlags(root *cobra.Command, f *flags) {
	pf := root.PersistentFlags()
	pf.StringVar(&f.root, "root", ".", "discovery root directory")
	pf.StringArrayVar(&f.paths, "path", nil, "explicit report path/glob (repeatable; disables discovery)")
	pf.StringVar(&f.format, "format", "text", "stdout format: text|json|ndjson|md")
	pf.StringVar(&f.report, "report", "", "write the JSON artifact to FILE ('-' = stdout)")
	pf.BoolVar(&f.reportCompact, "report-compact", false, "write minified JSON instead of pretty")
	pf.BoolVar(&f.reportForce, "report-force", false, "overwrite an existing report target")
	pf.BoolVar(&f.reportNoCases, "report-no-cases", false, "omit the per-case array from the report")
	pf.BoolVar(&f.reportOutput, "report-output", false, "include captured output in the report")
	pf.DurationVar(&f.maxAge, "max-age", 30*time.Minute, "staleness threshold")
	pf.StringVar(&f.newerThan, "newer-than", "", "only reports newer than DURATION-AGO | RFC3339 | FILE mtime")
	pf.StringVar(&f.color, "color", "auto", "when to use ANSI color: auto|always|never")
	pf.BoolVar(&f.noColor, "no-color", false, "disable ANSI color (alias for --color never)")
	pf.BoolVar(&f.verbose, "verbose", false, "include non-fatal detail in warn lines")
	pf.BoolVar(&f.noDedupe, "no-dedupe", false, "disable dedup of duplicated aggregated cases (spec §14.3)")
	pf.BoolVar(&f.failOnStale, "fail-on-stale", false, "promote a stale run to exit 1")
}

// addSelectionFlags registers the shared selection flags (spec §8.3), applied
// before totals (plan D11).
func addSelectionFlags(root *cobra.Command, f *flags) {
	pf := root.PersistentFlags()
	pf.StringArrayVar(&f.modules, "module", nil, "restrict to module (repeatable, exact or substring)")
	pf.StringVar(&f.statuses, "status", "", "restrict to statuses: passed,failed,error,skipped")
	pf.StringArrayVar(&f.filter, "filter", nil, "only ids containing SUBSTR")
	pf.StringArrayVar(&f.exclude, "exclude", nil, "exclude ids containing SUBSTR")
}

// notYet registers a placeholder command: exits 3 with
// "junit-results <cmd>: not implemented yet" (plan §10).
func notYet(name string) *cobra.Command {
	return &cobra.Command{
		Use:   name,
		Short: "not implemented yet (planned milestone)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return &NotYetError{Cmd: name}
		},
	}
}
