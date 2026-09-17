package cli

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/istarion/junit-report-parser/internal/agg"
	"github.com/istarion/junit-report-parser/internal/model"
	"github.com/istarion/junit-report-parser/internal/render"
	"github.com/istarion/junit-report-parser/internal/trim"
)

var statsSections = []string{"status", "duration", "slowest", "type", "module", "flaky", "skipped"}

// statsCmd builds the `stats` command (spec §8.1): status, duration,
// slowest, failure types, breakdown, flaky, skipped.
func statsCmd(env *Env, f *flags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Status, duration, slowest, failure types, breakdown",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return &UsageError{Msg: fmt.Sprintf("unexpected argument %q", args[0])}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStats(env, f)
		},
	}
	cmd.Flags().IntVar(&f.statsTop, "top", 5, "rows for slowest and failure-types (0 is invalid)")
	cmd.Flags().StringVar(&f.statsBy, "by", "module", "breakdown grouping: module|suite|package")
	cmd.Flags().StringVar(&f.statsOnly, "only", "", "sections to show (comma list from status,duration,slowest,type,module,flaky,skipped)")
	return cmd
}

func runStats(env *Env, f *flags) error {
	if f.statsTop <= 0 {
		return &UsageError{Msg: fmt.Sprintf("invalid --top %d (want >= 1)", f.statsTop)}
	}
	if f.statsBy != "module" && f.statsBy != "suite" && f.statsBy != "package" {
		return &UsageError{Msg: fmt.Sprintf("invalid --by %q (want module|suite|package)", f.statsBy)}
	}
	only := statsSections
	if f.statsOnly != "" {
		only = nil
		for _, part := range strings.Split(f.statsOnly, ",") {
			name := strings.TrimSpace(part)
			if name == "" {
				continue
			}
			if !validSection(name) {
				return &UsageError{Msg: fmt.Sprintf("unknown --only section %q (want %s)", name, strings.Join(statsSections, ","))}
			}
			only = append(only, name)
		}
	}

	p, err := prepare(env, f)
	if err != nil {
		return err
	}
	cc := newCaseCtx(p.res)

	base := render.SummaryInput{
		Root:       p.res.Root,
		RunTime:    p.res.RunTime,
		Age:        p.res.Age,
		Stale:      p.res.Stale,
		Reports:    len(p.res.Reports),
		Modules:    p.modules,
		Totals:     p.totals,
		Warnings:   p.res.Warnings,
		ModuleRows: moduleRows(p),
		UseColor:   resolveColor(f, env.Stdout),
	}
	records := statRecords(p, cc, only, f)
	b := budgetPresets["medium"]

	switch f.format {
	case "text":
		render.StatsText(env.Stdout, render.StatsInput{Base: base, Records: records})
	case "md":
		render.MDStats(env.Stdout, render.StatsInput{Base: base, Records: records})
	case "ndjson":
		render.NDJSONStats(env.Stdout, render.StatsInput{Base: base, Records: records})
	case "json":
		groups := agg.GroupFailures(p.fails, cc.fingerprint)
		if err := printCompactJSON(env, f, "stats", p, groups, cc, b); err != nil {
			return err
		}
	}

	if err := writeArtifactIfRequested(env, f, "stats", p, agg.GroupFailures(p.fails, cc.fingerprint), cc, b); err != nil {
		return err
	}
	return finalExit(p, f)
}

func validSection(s string) bool {
	for _, want := range statsSections {
		if want == s {
			return true
		}
	}
	return false
}

// statRecords builds the stat records in fixed section order (D10/D21: all
// breakdowns use `stat kind=module` rows sorted by name asc). only is a set
// of section names; the output order is always statsSections order.
func statRecords(p *prepared, cc *caseCtx, only []string, f *flags) []render.StatRecord {
	var out []render.StatRecord
	sections := statsSections
	if only != nil {
		sections = nil
		want := map[string]bool{}
		for _, s := range only {
			want[s] = true
		}
		for _, s := range statsSections {
			if want[s] {
				sections = append(sections, s)
			}
		}
	}
	total := float64(p.totals.Tests)
	if total == 0 {
		total = 1 // all-zero shares instead of NaN
	}
	for _, section := range sections {
		switch section {
		case "status":
			for _, s := range model.Statuses {
				count := statusCount(p.totals, s)
				share := float64(count) / float64(p.totals.Tests) * 100
				if p.totals.Tests == 0 {
					share = 0
				}
				out = append(out, render.StatRecord{Kind: "status", Fields: []render.StatField{
					{Key: "status", Value: s.String()},
					{Key: "count", Value: strconv.Itoa(count)},
					{Key: "share", Value: strconv.FormatFloat(share, 'f', 1, 64) + "%"},
				}})
			}
		case "duration":
			times := caseTimes(p.cases)
			sum, mean, median, p90, p95, max := agg.DurationStats(times)
			out = append(out, render.StatRecord{Kind: "duration", Fields: []render.StatField{
				{Key: "sum", Value: model.FormatDuration(sum)},
				{Key: "mean", Value: model.FormatDuration(mean)},
				{Key: "median", Value: model.FormatDuration(median)},
				{Key: "p90", Value: model.FormatDuration(p90)},
				{Key: "p95", Value: model.FormatDuration(p95)},
				{Key: "max", Value: model.FormatDuration(max)},
			}})
		case "slowest":
			slowest := append([]*model.Case(nil), p.cases...)
			sort.SliceStable(slowest, func(i, j int) bool { // time desc, id asc (D10)
				if slowest[i].Time != slowest[j].Time {
					return slowest[i].Time > slowest[j].Time
				}
				return slowest[i].ID < slowest[j].ID
			})
			if len(slowest) > f.statsTop {
				slowest = slowest[:f.statsTop]
			}
			for i, c := range slowest {
				out = append(out, render.StatRecord{Kind: "slowest", Fields: []render.StatField{
					{Key: "rank", Value: strconv.Itoa(i + 1)},
					{Key: "id", Value: c.ID},
					{Key: "time", Value: model.FormatDuration(c.Time)},
					{Key: "status", Value: c.Status.String()},
				}})
			}
		case "type":
			type typeCount struct {
				typ   string
				count int
			}
			counts := map[string]int{}
			var types []typeCount
			for _, c := range p.cases {
				if c.Failure == nil || c.Failure.Type == "" {
					continue
				}
				if counts[c.Failure.Type] == 0 {
					types = append(types, typeCount{typ: c.Failure.Type})
				}
				counts[c.Failure.Type]++
			}
			sort.Slice(types, func(i, j int) bool { // count desc, type asc (D10)
				if counts[types[i].typ] != counts[types[j].typ] {
					return counts[types[i].typ] > counts[types[j].typ]
				}
				return types[i].typ < types[j].typ
			})
			if len(types) > f.statsTop {
				types = types[:f.statsTop]
			}
			for _, tc := range types {
				out = append(out, render.StatRecord{Kind: "type", Fields: []render.StatField{
					{Key: "count", Value: strconv.Itoa(counts[tc.typ])},
					{Key: "type", Value: tc.typ},
				}})
			}
		case "module":
			for _, row := range breakdown(p.cases, f.statsBy) {
				out = append(out, render.StatRecord{Kind: "module", Fields: []render.StatField{
					{Key: "name", Value: row.name},
					{Key: "tests", Value: strconv.Itoa(row.tests)},
					{Key: "failed", Value: strconv.Itoa(row.failed)},
					{Key: "errors", Value: strconv.Itoa(row.errors)},
					{Key: "skipped", Value: strconv.Itoa(row.skipped)},
					{Key: "time", Value: model.FormatDuration(row.time)},
				}})
			}
		case "flaky":
			var flaky []*model.Case
			for _, c := range p.cases {
				if c.Flaky {
					flaky = append(flaky, c)
				}
			}
			agg.SortCanonical(flaky) // (module, classname, name)
			for _, c := range flaky {
				out = append(out, render.StatRecord{Kind: "flaky", Fields: []render.StatField{
					{Key: "id", Value: c.ID},
					{Key: "time", Value: model.FormatDuration(c.Time)},
				}})
			}
		case "skipped":
			reasons := map[string]int{}
			var order []string
			for _, c := range p.cases {
				if c.Status != model.StatusSkipped {
					continue
				}
				reason := c.SkipMessage
				if reason == "" {
					reason = "(none)"
				}
				if reasons[reason] == 0 {
					order = append(order, reason)
				}
				reasons[reason]++
			}
			sort.Slice(order, func(i, j int) bool { // count desc, reason asc (D10)
				if reasons[order[i]] != reasons[order[j]] {
					return reasons[order[i]] > reasons[order[j]]
				}
				return order[i] < order[j]
			})
			for _, reason := range order {
				out = append(out, render.StatRecord{Kind: "skipped", Fields: []render.StatField{
					{Key: "count", Value: strconv.Itoa(reasons[reason])},
					{Key: "reason", Value: reason},
				}})
			}
		}
	}
	_ = cc
	return out
}

func statusCount(t model.Totals, s model.Status) int {
	switch s {
	case model.StatusPassed:
		return t.Passed
	case model.StatusFailed:
		return t.Failed
	case model.StatusError:
		return t.Errors
	case model.StatusSkipped:
		return t.Skipped
	}
	return 0
}

func caseTimes(cases []*model.Case) []float64 {
	times := make([]float64, 0, len(cases))
	for _, c := range cases {
		times = append(times, c.Time)
	}
	return times
}

type breakdownRow struct {
	name    string
	tests   int
	failed  int
	errors  int
	skipped int
	time    float64
}

// breakdown groups cases per --by (D21): module | suite (classname) |
// package (classname package, "(default)" for the default package); rows
// sorted by name asc.
func breakdown(cases []*model.Case, by string) []breakdownRow {
	key := func(c *model.Case) string {
		switch by {
		case "suite":
			return c.ClassName
		case "package":
			return trim.PackageOf(c.ClassName)
		default:
			return c.Module
		}
	}
	idx := map[string]int{}
	var rows []breakdownRow
	for _, c := range cases {
		name := key(c)
		i, ok := idx[name]
		if !ok {
			rows = append(rows, breakdownRow{name: name})
			i = len(rows) - 1
			idx[name] = i
		}
		rows[i].tests++
		switch c.Status {
		case model.StatusFailed:
			rows[i].failed++
		case model.StatusError:
			rows[i].errors++
		case model.StatusSkipped:
			rows[i].skipped++
		}
		rows[i].time += c.Time
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].name < rows[j].name })
	return rows
}
