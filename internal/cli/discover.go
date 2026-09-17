package cli

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/istarion/junit-report-parser/internal/agg"
	"github.com/istarion/junit-report-parser/internal/discover"
	"github.com/istarion/junit-report-parser/internal/model"
	"github.com/istarion/junit-report-parser/internal/render"
)

// discoverCmd builds the `discover` command (spec §8.1, plan §5): discovered
// report files and provenance, unfiltered by --newer-than/--max-age; the
// run-level stale flag still applies.
func discoverCmd(env *Env, f *flags) *cobra.Command {
	return &cobra.Command{
		Use:   "discover",
		Short: "Discovered report files and provenance",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return &UsageError{Msg: fmt.Sprintf("unexpected argument %q", args[0])}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiscover(env, f)
		},
	}
}

func runDiscover(env *Env, f *flags) error {
	if err := validateRunFlags(f); err != nil {
		return err
	}
	// Discovery debugging: no --newer-than filtering, no dedup, no case
	// selection — every discovered report is shown raw (plan §5).
	res, err := discover.Discover(discover.Options{
		Root:    f.root,
		Paths:   f.paths,
		MaxAge:  f.maxAge,
		Verbose: f.verbose,
		Clock:   env.Clock,
	})
	if err != nil {
		return mapDiscoverErr(err)
	}

	var all []*model.Case
	for _, r := range res.Reports {
		for _, c := range r.Cases {
			c.Module = r.Module
			all = append(all, c)
		}
	}
	p := &prepared{
		res:     res,
		fails:   agg.Failed(all),
		cases:   all,
		totals:  agg.Totals(all),
		modules: countModules(res.Reports),
		noCases: f.reportNoCases,
	}
	cc := newCaseCtx(res)

	records := make([]render.DiscoverRecord, 0, len(res.Reports))
	for _, r := range res.Reports {
		fail := false
		for _, c := range r.Cases {
			if c.Status == model.StatusFailed || c.Status == model.StatusError {
				fail = true
				break
			}
		}
		records = append(records, render.DiscoverRecord{
			Path:   r.Path,
			Module: r.Module,
			Mtime:  r.Mtime,
			Suite:  r.SuiteName,
			Cases:  len(r.Cases),
			Fail:   fail,
			Source: r.Source,
		})
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Path < records[j].Path }) // D10

	base := render.SummaryInput{
		Root:     res.Root,
		RunTime:  res.RunTime,
		Age:      res.Age,
		Stale:    res.Stale,
		Reports:  len(res.Reports),
		Modules:  p.modules,
		Totals:   p.totals,
		Warnings: res.Warnings,
		UseColor: resolveColor(f, env.Stdout),
	}
	in := render.DiscoverInput{Base: base, Records: records}
	b := budgetPresets["medium"]

	switch f.format {
	case "text":
		render.DiscoverText(env.Stdout, in)
	case "md":
		render.MDDiscover(env.Stdout, in)
	case "ndjson":
		render.NDJSONDiscover(env.Stdout, in)
	case "json":
		groups := agg.GroupFailures(p.fails, cc.fingerprint)
		if err := printCompactJSON(env, f, "discover", p, groups, cc, b); err != nil {
			return err
		}
	}

	groups := agg.GroupFailures(p.fails, cc.fingerprint)
	if err := writeArtifactIfRequested(env, f, "discover", p, groups, cc, b); err != nil {
		return err
	}
	return finalExit(p, f)
}

func countModules(reports []*model.Report) int {
	seen := map[string]bool{}
	n := 0
	for _, r := range reports {
		if !seen[r.Module] {
			seen[r.Module] = true
			n++
		}
	}
	return n
}

// mapDiscoverErr maps discovery errors to typed CLI errors.
func mapDiscoverErr(err error) error {
	if err == discover.ErrNoReports {
		return &NoReportsError{Msg: "no report files discovered"}
	}
	if err == discover.ErrNoneParseable {
		return &NoneParseableError{Msg: "reports discovered but none parseable"}
	}
	return err
}
