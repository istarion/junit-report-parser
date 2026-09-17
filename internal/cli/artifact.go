package cli

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"time"

	"github.com/istarion/junit-report-parser/internal/agg"
	"github.com/istarion/junit-report-parser/internal/model"
	"github.com/istarion/junit-report-parser/internal/report"
)

// buildArtifact assembles the §11 document from the prepared pipeline
// result. Full fidelity: complete traces, uncapped messages, uncapped
// groups. All arrays use the canonical D10 orders.
func buildArtifact(env *Env, command string, p *prepared, groups []agg.Group, cc *caseCtx, out artifactOutput) report.Artifact {
	root, err := filepath.Abs(p.res.Root)
	if err != nil {
		root = p.res.Root
	}

	art := report.Artifact{
		SchemaVersion: report.SchemaVersion,
		Tool:          report.Tool{Name: "junit-results", Version: env.Version},
		GeneratedAt:   now(env).UTC().Format(time.RFC3339),
		Command:       command,
		Root:          root,
		Run: report.Run{
			Timestamp:  p.res.RunTime.UTC().Format(time.RFC3339),
			AgeSeconds: p.res.Age.Seconds(),
			Stale:      p.res.Stale,
			Reports:    len(p.res.Reports),
			Modules:    p.modules,
		},
		Totals: report.Totals{
			Tests:       p.totals.Tests,
			Passed:      p.totals.Passed,
			Failures:    p.totals.Failed,
			Errors:      p.totals.Errors,
			Skipped:     p.totals.Skipped,
			Flaky:       p.totals.Flaky,
			TimeSeconds: p.totals.Time,
		},
		Modules:  moduleStats(p.cases),
		Failures: failureEntries(p.fails, cc, out),
		Groups:   groupEntries(groups),
		Stats:    buildStats(p, cc),
		Warnings: warnEntries(p.res.Warnings),
	}
	if p.noCases {
		return art
	}
	cases := caseEntries(p.cases, cc)
	art.Cases = &cases
	return art
}

// moduleStats aggregates per-module totals from the (filtered) cases.
func moduleStats(cases []*model.Case) []report.Module {
	idx := map[string]int{}
	var out []report.Module
	for _, c := range cases {
		i, ok := idx[c.Module]
		if !ok {
			out = append(out, report.Module{Name: c.Module})
			i = len(out) - 1
			idx[c.Module] = i
		}
		out[i].Tests++
		switch c.Status {
		case model.StatusFailed:
			out[i].Failures++
		case model.StatusError:
			out[i].Errors++
		case model.StatusSkipped:
			out[i].Skipped++
		}
		out[i].TimeSeconds += c.Time
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name }) // D10
	return out
}

func failureEntries(fails []*model.Case, cc *caseCtx, out artifactOutput) []report.Case {
	entries := make([]report.Case, 0, len(fails))
	for _, c := range fails {
		e := toArtifactCase(c, cc)
		if out.enable {
			o := &report.CaseOutput{}
			if tail := tailText(c.Out, out.lines); tail != "" {
				o.Out = tail
			}
			if tail := tailText(c.Err, out.lines); tail != "" {
				o.Err = tail
			}
			if o.Out != "" || o.Err != "" {
				e.Output = o
			}
		}
		entries = append(entries, e)
	}
	return entries
}

func caseEntries(cases []*model.Case, cc *caseCtx) []report.Case {
	sorted := append([]*model.Case(nil), cases...)
	agg.SortCanonical(sorted)
	out := make([]report.Case, 0, len(sorted))
	for _, c := range sorted {
		out = append(out, toArtifactCase(c, cc))
	}
	return out
}

func toArtifactCase(c *model.Case, cc *caseCtx) report.Case {
	out := report.Case{
		ID:          c.ID,
		Classname:   c.ClassName,
		Name:        c.Name,
		Module:      c.Module,
		Status:      c.Status.String(),
		Flaky:       c.Flaky,
		TimeSeconds: c.Time,
		ReportPath:  c.ReportPath,
		Loc:         cc.loc(c),
	}
	if c.Failure != nil {
		out.Type = c.Failure.Type
		out.Message = c.Failure.Message // full fidelity: no cap
		out.Trace = append([]string(nil), c.Failure.Trace...)
	}
	return out
}

func groupEntries(groups []agg.Group) []report.Group {
	out := make([]report.Group, 0, len(groups))
	for _, g := range groups {
		sample := ""
		if g.Sample != nil && g.Sample.Failure != nil {
			sample = g.Sample.Failure.Message // full fidelity: no cap, no D18 cap on tests
		}
		out = append(out, report.Group{
			Fingerprint:   g.Fingerprint,
			Count:         g.Count,
			SampleMessage: sample,
			Tests:         append([]string(nil), g.IDs...),
		})
	}
	return out
}

func buildStats(p *prepared, cc *caseCtx) report.Stats {
	times := make([]float64, 0, len(p.cases))
	var st report.StatusStats
	typeCounts := map[string]int{}
	for _, c := range p.cases {
		times = append(times, c.Time)
		switch c.Status {
		case model.StatusPassed:
			st.Passed++
		case model.StatusFailed:
			st.Failed++
		case model.StatusError:
			st.Error++
		case model.StatusSkipped:
			st.Skipped++
		}
		if c.Failure != nil && c.Failure.Type != "" {
			typeCounts[c.Failure.Type]++
		}
	}
	sum, mean, median, p90, p95, max := agg.DurationStats(times)

	types := make([]report.TypeCount, 0, len(typeCounts))
	for ty, n := range typeCounts {
		types = append(types, report.TypeCount{Type: ty, Count: n})
	}
	sort.Slice(types, func(i, j int) bool { // count desc, type asc (D10)
		if types[i].Count != types[j].Count {
			return types[i].Count > types[j].Count
		}
		return types[i].Type < types[j].Type
	})

	slowest := make([]report.Slow, 0, len(p.cases))
	for _, c := range p.cases {
		slowest = append(slowest, report.Slow{ID: c.ID, TimeSeconds: c.Time, Status: c.Status.String()})
	}
	sort.Slice(slowest, func(i, j int) bool { // time desc, id asc (D10)
		if slowest[i].TimeSeconds != slowest[j].TimeSeconds {
			return slowest[i].TimeSeconds > slowest[j].TimeSeconds
		}
		return slowest[i].ID < slowest[j].ID
	})

	byGroup := moduleStats(p.cases) // artifact byGroup = module grouping
	bg := make([]report.ByGroup, 0, len(byGroup))
	for _, m := range byGroup {
		bg = append(bg, report.ByGroup{Name: m.Name, Tests: m.Tests, Failed: m.Failures, TimeSeconds: m.TimeSeconds})
	}

	return report.Stats{
		Status:   st,
		Duration: report.Duration{SumSeconds: sum, MeanSeconds: mean, MedianSeconds: median, P90Seconds: p90, P95Seconds: p95, MaxSeconds: max},
		Types:    types,
		Slowest:  slowest,
		ByGroup:  bg,
	}
}

func warnEntries(warns []model.Warning) []report.Warn {
	out := make([]report.Warn, 0, len(warns))
	for _, w := range warns {
		var path *string
		if w.Path != "" {
			p := w.Path
			path = &p
		}
		out = append(out, report.Warn{Code: w.Code, Message: w.Message, Path: path})
	}
	return out
}

// writeArtifactIfRequested marshals and writes the §11 artifact when
// --report was given. All failures map to exit 5 (ReportWriteError).
func writeArtifactIfRequested(env *Env, f *flags, command string, p *prepared, groups []agg.Group, cc *caseCtx, b budget) error {
	if f.report == "" {
		return nil
	}
	art := buildArtifact(env, command, p, groups, cc, artifactOutput{
		enable: f.reportOutput,
		lines:  b.outputLinesOrDefault(),
	})
	data, err := art.Marshal(f.reportCompact)
	if err != nil {
		return &ReportWriteError{Msg: fmt.Sprintf("report marshal: %v", err)}
	}
	var out io.Writer = env.Stdout // used only for "-" targets
	if err := report.Write(f.report, data, f.reportForce, out); err != nil {
		return &ReportWriteError{Msg: err.Error()}
	}
	return nil
}
