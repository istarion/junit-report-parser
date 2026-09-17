package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/istarion/junit-report-parser/internal/agg"
	"github.com/istarion/junit-report-parser/internal/discover"
	"github.com/istarion/junit-report-parser/internal/model"
	"github.com/istarion/junit-report-parser/internal/render"
	"github.com/istarion/junit-report-parser/internal/trim"
)

// summaryCmd builds the `summary` command. The root re-dispatches here as
// the default command, sharing the same flag values.
func summaryCmd(env *Env, f *flags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "summary",
		Short: "Verdict, totals, and one line per failure",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return &UsageError{Msg: fmt.Sprintf("unexpected argument %q", args[0])}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSummary(env, f)
		},
	}
	cmd.Flags().IntVar(&f.top, "top", 5, "root-cause groups / failure records shown")
	return cmd
}

// prepared is the shared pipeline result of all report-reading commands:
// discover → dedup (plan §6) → selection (D11) → totals.
type prepared struct {
	res     *discover.Result
	fails   []*model.Case // failed/error cases, canonical sort, uncapped
	cases   []*model.Case
	totals  model.Totals
	modules int
	noCases bool // --report-no-cases: omit cases[] from the artifact
}

// validateRunFlags enforces the flag checks shared by report-reading
// commands (called before any discovery work).
func validateRunFlags(f *flags) error {
	if err := validateFormat(f.format); err != nil {
		return err
	}
	if f.color != "auto" && f.color != "always" && f.color != "never" {
		return &UsageError{Msg: fmt.Sprintf("invalid --color %q (want auto|always|never)", f.color)}
	}
	return nil
}

// prepare runs the pipeline shared by summary and failures (P3+ commands
// reuse it). Dedup runs before selection so totals never double-count
// aggregated duplicates (plan §6, spec §14.3).
func prepare(env *Env, f *flags) (*prepared, error) {
	if err := validateRunFlags(f); err != nil {
		return nil, err
	}
	nowTs := now(env)

	var newerThan time.Time
	var hasNewerThan bool
	if f.newerThan != "" {
		t, err := parseNewerThan(f.newerThan, nowTs)
		if err != nil {
			return nil, err
		}
		newerThan, hasNewerThan = t, true
	}

	statuses, err := agg.ParseStatuses(f.statuses)
	if err != nil {
		return nil, &UsageError{Msg: err.Error()}
	}

	res, err := discover.Discover(discover.Options{
		Root:         f.root,
		Paths:        f.paths,
		HasNewerThan: hasNewerThan,
		NewerThan:    newerThan,
		MaxAge:       f.maxAge,
		Verbose:      f.verbose,
		Clock:        env.Clock,
	})
	if err != nil {
		switch {
		case errors.Is(err, discover.ErrNoReports):
			return nil, &NoReportsError{Msg: fmt.Sprintf("no report files found under %s", f.root)}
		case errors.Is(err, discover.ErrNoneParseable):
			return nil, &NoneParseableError{Msg: fmt.Sprintf("reports found under %s but none parseable", f.root)}
		default:
			return nil, err // e.g. bad --path glob → exit 3
		}
	}

	if !f.noDedupe {
		if n := agg.Dedupe(res.Reports); n > 0 {
			res.Warnings = append(res.Warnings, model.Warning{
				Code:    "DEDUP",
				Message: fmt.Sprintf("%d cases collapsed", n),
			})
		}
	}

	cases := agg.Select(res.Reports, agg.Selection{
		Modules:  f.modules,
		Statuses: statuses,
		Include:  f.filter,
		Exclude:  f.exclude,
	})
	modules := 0
	{
		seen := map[string]bool{}
		for _, r := range res.Reports {
			if !seen[r.Module] {
				seen[r.Module] = true
				modules++
			}
		}
	}
	return &prepared{
		res:     res,
		fails:   agg.Failed(cases),
		cases:   cases,
		totals:  agg.Totals(cases),
		modules: modules,
		noCases: f.reportNoCases,
	}, nil
}

// caseCtx resolves per-case context (owning report + file index) for the
// trim-backed computations: fingerprints, locations and trace trimming.
type caseCtx struct {
	byPath map[string]*model.Report
	files  trim.FileIndex
}

func newCaseCtx(res *discover.Result) *caseCtx {
	cc := &caseCtx{
		byPath: make(map[string]*model.Report, len(res.Reports)),
		files:  res.Files,
	}
	for _, r := range res.Reports {
		cc.byPath[r.Path] = r
	}
	return cc
}

func (cc *caseCtx) rep(c *model.Case) *model.Report { return cc.byPath[c.ReportPath] }

func (cc *caseCtx) fingerprint(c *model.Case) string {
	return trim.FingerprintOf(c, cc.rep(c), cc.files)
}

func (cc *caseCtx) loc(c *model.Case) string { return trim.Loc(c, cc.rep(c), cc.files) }

// groupRecords converts agg groups to renderer records.
func groupRecords(groups []agg.Group) []render.GroupRecord {
	if len(groups) == 0 {
		return nil
	}
	out := make([]render.GroupRecord, 0, len(groups))
	for _, g := range groups {
		sample := ""
		if g.Sample != nil && g.Sample.Failure != nil {
			sample = g.Sample.Failure.Message
		}
		out = append(out, render.GroupRecord{
			Count:       g.Count,
			Fingerprint: g.Fingerprint,
			Sample:      sample,
			IDs:         g.IDs,
		})
	}
	return out
}

// runSummary implements the summary pipeline: prepare → groups (capped at
// --top) → fail records (capped at --top) → render → artifact → verdict exit.
func runSummary(env *Env, f *flags) error {
	p, err := prepare(env, f)
	if err != nil {
		return err
	}
	useColor := resolveColor(f, env.Stdout)
	cc := newCaseCtx(p.res)

	allGroups := agg.GroupFailures(p.fails, cc.fingerprint)
	digestGroups := allGroups
	if len(digestGroups) > f.top {
		digestGroups = digestGroups[:f.top]
	}
	fails := p.fails
	if len(fails) > f.top {
		fails = fails[:f.top]
	}
	records := make([]render.FailRecord, 0, len(fails))
	for _, c := range fails {
		records = append(records, render.FailRecord{Case: c, Loc: cc.loc(c)})
	}

	base := render.SummaryInput{
		Root:       p.res.Root,
		RunTime:    p.res.RunTime,
		Age:        p.res.Age,
		Stale:      p.res.Stale,
		Reports:    len(p.res.Reports),
		Modules:    p.modules,
		Totals:     p.totals,
		Warnings:   p.res.Warnings,
		Groups:     groupRecords(digestGroups),
		Fails:      records,
		MaxMessage: render.MaxMessageRunes, // medium budget default until P5 presets
		ModuleRows: moduleRows(p),
		UseColor:   useColor,
	}
	b := budgetPresets["medium"] // summary has no --budget flag
	switch f.format {
	case "text":
		render.SummaryText(env.Stdout, base)
	case "md":
		render.MDSummary(env.Stdout, base)
	case "ndjson":
		render.NDJSONSummary(env.Stdout, base)
	case "json":
		if err := printCompactJSON(env, f, "summary", p, allGroups, cc, b); err != nil {
			return err
		}
	}

	if err := writeArtifactIfRequested(env, f, "summary", p, allGroups, cc, b); err != nil {
		return err
	}
	return finalExit(p, f)
}

// validateFormat enforces the stdout format set (spec §8.2/§12).
func validateFormat(format string) error {
	switch format {
	case "text", "json", "ndjson", "md":
		return nil
	default:
		return &UsageError{Msg: fmt.Sprintf("invalid --format %q (want text|json|ndjson|md)", format)}
	}
}

// parseNewerThan accepts duration-ago (relative to now), RFC3339, or a file
// path whose mtime is used (spec §8.2, plan §5).
func parseNewerThan(s string, nowTs time.Time) (time.Time, error) {
	if d, err := time.ParseDuration(s); err == nil {
		return nowTs.Add(-d), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if fi, err := os.Stat(s); err == nil {
		return fi.ModTime(), nil
	}
	return time.Time{}, &UsageError{
		Msg: fmt.Sprintf("bad --newer-than value %q (want duration-ago, RFC3339, or file path)", s),
	}
}

// resolveColor implements --color auto|always|never + --no-color alias:
// color only when stdout is a TTY (auto) or --color always; piped output is
// never colored (plan D9, spec §10.4). The caller validates the flag value.
func resolveColor(f *flags, stdout io.Writer) bool {
	if f.noColor || f.color == "never" {
		return false
	}
	if f.color == "always" {
		return true
	}
	return isTerminal(stdout)
}
