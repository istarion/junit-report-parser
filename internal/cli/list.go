package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/istarion/junit-report-parser/internal/agg"
	"github.com/istarion/junit-report-parser/internal/model"
	"github.com/istarion/junit-report-parser/internal/render"
)

// listCmd builds the `list` command (spec §8.1): flat case list with status.
func listCmd(env *Env, f *flags) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Flat case list with status",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return &UsageError{Msg: fmt.Sprintf("unexpected argument %q", args[0])}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(env, f)
		},
	}
}

// runList: prepare → canonical sort → render → artifact → verdict exit.
// Selection flags apply (D11); the list mirrors the filtered universe.
func runList(env *Env, f *flags) error {
	p, err := prepare(env, f)
	if err != nil {
		return err
	}
	cc := newCaseCtx(p.res)

	sorted := append([]*model.Case(nil), p.cases...)
	agg.SortCanonical(sorted)
	records := make([]render.ListRecord, 0, len(sorted))
	for _, c := range sorted {
		records = append(records, render.ListRecord{Case: c, Loc: cc.loc(c)})
	}

	base := render.SummaryInput{
		Root:     p.res.Root,
		RunTime:  p.res.RunTime,
		Age:      p.res.Age,
		Stale:    p.res.Stale,
		Reports:  len(p.res.Reports),
		Modules:  p.modules,
		Totals:   p.totals,
		Warnings: p.res.Warnings,
		UseColor: resolveColor(f, env.Stdout),
	}
	in := render.ListInput{Base: base, Records: records}
	b := budgetPresets["medium"]

	switch f.format {
	case "text":
		render.ListText(env.Stdout, in)
	case "md":
		render.MDList(env.Stdout, in)
	case "ndjson":
		render.NDJSONList(env.Stdout, in)
	case "json":
		if err := printCompactJSON(env, f, "list", p, nil, cc, b); err != nil {
			return err
		}
	}

	if err := writeArtifactIfRequested(env, f, "list", p, nil, cc, b); err != nil {
		return err
	}
	return finalExit(p, f)
}
