package cli

import (
	"github.com/istarion/junit-report-parser/internal/agg"
	"github.com/istarion/junit-report-parser/internal/render"
)

// moduleRows converts the module aggregate for markdown tables (§12).
func moduleRows(p *prepared) []render.ModuleRow {
	ms := moduleStats(p.cases)
	out := make([]render.ModuleRow, 0, len(ms))
	for _, m := range ms {
		out = append(out, render.ModuleRow{
			Name: m.Name, Tests: m.Tests, Failures: m.Failures,
			Errors: m.Errors, Skipped: m.Skipped, TimeSeconds: m.TimeSeconds,
		})
	}
	return out
}

// artifactOutput controls --report-output / --format json output embedding.
type artifactOutput struct {
	enable bool
	lines  int
}

// finalExit applies the shared verdict/staleness exit policy: failures or
// errors → 1; otherwise --fail-on-stale promotes a stale run to 1.
func finalExit(p *prepared, f *flags) error {
	if !p.totals.Verdict() {
		return &TestsFailedError{}
	}
	if f.failOnStale && p.res.Stale {
		return &TestsFailedError{}
	}
	return nil
}

// printCompactJSON emits the compact §11 document on stdout for --format
// json (D12: same shape as --report, minus tool.version). Independent of
// --report; cases follow --report-no-cases.
func printCompactJSON(env *Env, f *flags, cmdName string, p *prepared, groups []agg.Group, cc *caseCtx, b budget) error {
	art := buildArtifact(env, cmdName, p, groups, cc, artifactOutput{enable: f.reportOutput, lines: b.outputLinesOrDefault()})
	art.Tool.Version = "" // D12: stdout json drops tool.version
	data, err := art.Marshal(true)
	if err != nil {
		return err
	}
	_, err = env.Stdout.Write(data)
	return err
}
