package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/istarion/junit-report-parser/internal/agg"
	"github.com/istarion/junit-report-parser/internal/model"
	"github.com/istarion/junit-report-parser/internal/render"
	"github.com/istarion/junit-report-parser/internal/trim"
)

// failuresCmd builds the `failures` command (plan D20): shared preamble,
// then group records, then detailed fail records with msg/at/trunc.
func failuresCmd(env *Env, f *flags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "failures",
		Short: "Failure/error detail with trimmed traces",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return &UsageError{Msg: fmt.Sprintf("unexpected argument %q", args[0])}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFailures(env, f, cmd)
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&f.budget, "budget", "medium", "token preset: small|medium|large (explicit flags win)")
	fl.IntVar(&f.maxFrames, "max-frames", 5, "trace frames kept per failure (overrides --budget)")
	fl.BoolVar(&f.fullStack, "full-stack", false, "disable trace trimming")
	fl.IntVar(&f.maxMessage, "max-message", 500, "message character cap (overrides --budget)")
	fl.StringVar(&f.showOutput, "show-output", "none", "captured output: none|on-failure|all")
	fl.IntVar(&f.outputLines, "output-lines", 30, "captured-output lines per case (tail-biased)")
	fl.BoolVar(&f.noGroup, "no-group", false, "do not collapse failures by fingerprint")
	fl.BoolVar(&f.group, "group", false, "force fingerprint grouping on")
	return cmd
}

// runFailures implements the failures pipeline (D20 + §13 budgets): groups
// first (unless --no-group / large), then detailed fail records.
func runFailures(env *Env, f *flags, cmd *cobra.Command) error {
	p, err := prepare(env, f)
	if err != nil {
		return err
	}
	cc := newCaseCtx(p.res)

	b, err := resolveBudget(f.budget)
	if err != nil {
		return err
	}
	if b, err = b.applyOverrides(cmd, f, "none"); err != nil {
		return err
	}

	allGroups := agg.GroupFailures(p.fails, cc.fingerprint)
	digestGroups := allGroups
	if !b.group {
		digestGroups = nil
	}

	records := make([]render.FailRecord, 0, len(p.fails))
	if !b.groupsOnly {
		for _, c := range p.fails {
			rec := render.FailRecord{Case: c, Loc: cc.loc(c)}
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
	}

	hintID := ""
	if len(p.fails) > 0 {
		hintID = p.fails[0].ID // D19
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
		MaxMessage: b.maxMessage,
		UseColor:   resolveColor(f, env.Stdout),
	}
	in := render.FailuresInput{Base: base, Fails: records, HintID: hintID}

	switch f.format {
	case "text":
		render.FailuresText(env.Stdout, in)
	case "md":
		render.MDFailures(env.Stdout, in)
	case "ndjson":
		render.NDJSONFailures(env.Stdout, in)
	case "json":
		if err := printCompactJSON(env, f, "failures", p, allGroups, cc, b); err != nil {
			return err
		}
	}

	if err := writeArtifactIfRequested(env, f, "failures", p, allGroups, cc, b); err != nil {
		return err
	}
	return finalExit(p, f)
}
