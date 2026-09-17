package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/istarion/junit-report-parser/internal/agg"
	"github.com/istarion/junit-report-parser/internal/model"
	"github.com/istarion/junit-report-parser/internal/render"
	"github.com/istarion/junit-report-parser/internal/trim"
)

// showCmd builds the `show` command (spec §8.1): full detail for matching
// cases, including captured output. >10 matches → warn MANY_MATCHES; zero
// matches → exit 6 (D5, documented in --help).
func showCmd(env *Env, f *flags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <id-or-substring>",
		Short: "Full detail for matching case(s), incl. captured output",
		Long: "Full detail for matching case(s).\n\n" +
			"ARG matches as a substring over ids (classname#name); a bare class\n" +
			"name matches all of its cases. More than 10 matches emit a\n" +
			"MANY_MATCHES warning and still show. Exit codes: 0 clean corpus,\n" +
			"1 failing corpus, 6 no matches (D5).",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return &UsageError{Msg: "show requires exactly one id-or-substring argument"}
			}
			f.showIDs = []string{args[0]}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runShow(env, f, cmd)
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&f.budget, "budget", "medium", "token preset: small|medium|large (explicit flags win)")
	fl.IntVar(&f.maxFrames, "max-frames", 5, "trace frames kept per case (overrides --budget)")
	fl.BoolVar(&f.fullStack, "full-stack", false, "disable trace trimming")
	fl.IntVar(&f.maxMessage, "max-message", 500, "message character cap (overrides --budget)")
	fl.StringVar(&f.showOutput, "show-output", "all", "captured output: none|on-failure|all (default all)")
	fl.IntVar(&f.outputLines, "output-lines", 30, "captured-output lines per case (tail-biased)")
	fl.BoolVar(&f.noGroup, "no-group", false, "unused for show (kept for symmetry)")
	fl.BoolVar(&f.group, "group", false, "unused for show (kept for symmetry)")
	return cmd
}

func runShow(env *Env, f *flags, cmd *cobra.Command) error {
	p, err := prepare(env, f)
	if err != nil {
		return err
	}
	cc := newCaseCtx(p.res)

	b, err := resolveBudget(f.budget)
	if err != nil {
		return err
	}
	// show implies --show-output all unless overridden.
	if b, err = b.applyOverrides(cmd, f, "all"); err != nil {
		return err
	}

	needle := f.showIDs[0]
	var matched []*model.Case
	for _, c := range append([]*model.Case(nil), p.cases...) {
		if strings.Contains(c.ID, needle) {
			matched = append(matched, c)
		}
	}
	agg.SortCanonical(matched) // (module, classname, name)

	if len(matched) > 10 {
		p.res.Warnings = append(p.res.Warnings, model.Warning{
			Code:    "MANY_MATCHES",
			Message: fmt.Sprintf("%d matches", len(matched)),
		})
	}

	records := make([]render.ShowRecord, 0, len(matched))
	for _, c := range matched {
		rec := render.ShowRecord{Case: c, Loc: cc.loc(c)}
		if c.Failure != nil {
			tr := trim.TrimTrace(c.Failure.Trace, cc.rep(c), trim.Options{
				MaxFrames: b.maxFrames,
				FullStack: f.fullStack,
				Files:     p.res.Files,
			})
			rec.Frames, rec.Hidden = tr.Frames, tr.Hidden
		}
		if b.wantsOutput(c.Status == model.StatusFailed || c.Status == model.StatusError) {
			rec.OutTail = tailLines(c.Out, b.outputLinesOrDefault())
			rec.ErrTail = tailLines(c.Err, b.outputLinesOrDefault())
		}
		records = append(records, rec)
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
		MaxMessage: b.maxMessage,
		UseColor:   resolveColor(f, env.Stdout),
	}
	in := render.ShowInput{Base: base, Records: records}

	switch f.format {
	case "text":
		render.ShowText(env.Stdout, in)
	case "md":
		render.MDShow(env.Stdout, in)
	case "ndjson":
		render.NDJSONShow(env.Stdout, in)
	case "json":
		groups := agg.GroupFailures(p.fails, cc.fingerprint)
		if err := printCompactJSON(env, f, "show", p, groups, cc, b); err != nil {
			return err
		}
	}

	if err := writeArtifactIfRequested(env, f, "show", p, agg.GroupFailures(p.fails, cc.fingerprint), cc, b); err != nil {
		return err
	}
	// D5: no matches → 6; matches + failing corpus → 1; else 0.
	if len(matched) == 0 {
		return &GrepNoMatchError{}
	}
	return finalExit(p, f)
}
