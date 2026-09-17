package render

import (
	"fmt"
	"io"
	"strings"

	"github.com/istarion/junit-report-parser/internal/model"
)

// Markdown rendering (spec §12): heading + totals table, then command
// sections; traces in fenced code blocks; grep as a table. Token-lean.

func mdEscape(s string) string {
	// Markdown tables: pipes and newlines break cells.
	s = strings.ReplaceAll(s, "|", "\\|")
	return strings.ReplaceAll(s, "\n", " ")
}

func mdVerdictLine(w io.Writer, in SummaryInput) {
	fmt.Fprintf(w, "**%s** — reports=%d modules=%d run=%s age=%s stale=%t\n\n",
		verdictWord(in.Totals), in.Reports, in.Modules,
		in.RunTime.Format("2006-01-02T15:04"), model.FormatAge(in.Age), in.Stale)
	fmt.Fprintln(w, "| tests | passed | failed | errors | skipped | flaky | time |")
	fmt.Fprintln(w, "|---|---|---|---|---|---|---|")
	fmt.Fprintf(w, "| %d | %d | %d | %d | %d | %d | %s |\n\n",
		in.Totals.Tests, in.Totals.Passed, in.Totals.Failed, in.Totals.Errors,
		in.Totals.Skipped, in.Totals.Flaky, model.FormatDuration(in.Totals.Time))
}

func mdWarnings(w io.Writer, warns []model.Warning) {
	if len(warns) == 0 {
		return
	}
	fmt.Fprintln(w, "### Warnings")
	fmt.Fprintln(w)
	for _, wn := range warns {
		fmt.Fprintf(w, "- `%s` %s\n", wn.Code, mdEscape(wn.Message))
	}
	fmt.Fprintln(w)
}

func mdFence(w io.Writer, lines ...string) {
	fmt.Fprintln(w, "```")
	for _, l := range lines {
		fmt.Fprintln(w, l)
	}
	fmt.Fprintln(w, "```")
	fmt.Fprintln(w)
}

func mdModuleTable(w io.Writer, rows []ModuleRow) {
	if len(rows) == 0 {
		return
	}
	fmt.Fprintln(w, "### Modules")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "| module | tests | failed | errors | skipped | time |")
	fmt.Fprintln(w, "|---|---|---|---|---|---|")
	for _, r := range rows {
		fmt.Fprintf(w, "| %s | %d | %d | %d | %d | %s |\n",
			mdEscape(r.Name), r.Tests, r.Failures, r.Errors, r.Skipped, model.FormatDuration(r.TimeSeconds))
	}
	fmt.Fprintln(w)
}

// MDSummary writes the markdown summary: verdict, context, totals, modules,
// groups and failures.
func MDSummary(w io.Writer, in SummaryInput) {
	fmt.Fprintln(w, "## Verdict")
	fmt.Fprintln(w)
	mdVerdictLine(w, in)
	mdWarnings(w, in.Warnings)
	mdModuleTable(w, in.ModuleRows)
	if len(in.Groups) > 0 {
		fmt.Fprintln(w, "### Groups")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "| count | fingerprint | message |")
		fmt.Fprintln(w, "|---|---|---|")
		for _, g := range in.Groups {
			fmt.Fprintf(w, "| %d | %s | %s |\n", g.Count, g.Fingerprint, mdEscape(firstLine(g.Sample)))
		}
		fmt.Fprintln(w)
	}
	if len(in.Fails) > 0 {
		fmt.Fprintln(w, "### Failures")
		fmt.Fprintln(w)
		for _, fr := range in.Fails {
			lines := []string{fmt.Sprintf("%s  %s  %s", fr.Case.ID, fr.Case.Status, fr.Loc)}
			if msg := failureMessage(fr.Case); msg != "" {
				lines = append(lines, msg)
			}
			mdFence(w, lines...)
		}
	}
}

// MDFailures writes markdown failure detail with fenced traces.
func MDFailures(w io.Writer, in FailuresInput) {
	fmt.Fprintln(w, "## Failures")
	fmt.Fprintln(w)
	mdVerdictLine(w, in.Base)
	mdWarnings(w, in.Base.Warnings)
	for _, fr := range in.Fails {
		lines := []string{fmt.Sprintf("%s  %s  %s  %s", fr.Case.ID, fr.Case.Status, fr.Loc, model.FormatDuration(fr.Case.Time))}
		if msg := failureMessage(fr.Case); msg != "" {
			lines = append(lines, msg)
		}
		for _, f := range fr.Frames {
			lines = append(lines, trimAtKeyword(f))
		}
		if fr.Hidden > 0 {
			lines = append(lines, fmt.Sprintf("trunc %d frames hidden", fr.Hidden))
		}
		for _, l := range fr.OutTail {
			lines = append(lines, "out "+l)
		}
		for _, l := range fr.ErrTail {
			lines = append(lines, "err "+l)
		}
		mdFence(w, lines...)
	}
}

// MDShow writes markdown case detail.
func MDShow(w io.Writer, in ShowInput) {
	fmt.Fprintln(w, "## Cases")
	fmt.Fprintln(w)
	mdVerdictLine(w, in.Base)
	mdWarnings(w, in.Base.Warnings)
	for _, r := range in.Records {
		lines := []string{fmt.Sprintf("%s  %s  %s  %s", r.Case.ID, r.Case.Status, r.Loc, model.FormatDuration(r.Case.Time))}
		if msg := failureMessage(r.Case); msg != "" {
			lines = append(lines, msg)
		}
		for _, f := range r.Frames {
			lines = append(lines, trimAtKeyword(f))
		}
		if r.Hidden > 0 {
			lines = append(lines, fmt.Sprintf("trunc %d frames hidden", r.Hidden))
		}
		for _, l := range r.OutTail {
			lines = append(lines, "out "+l)
		}
		for _, l := range r.ErrTail {
			lines = append(lines, "err "+l)
		}
		mdFence(w, lines...)
	}
}

// MDList writes markdown case list.
func MDList(w io.Writer, in ListInput) {
	fmt.Fprintln(w, "## Cases")
	fmt.Fprintln(w)
	mdVerdictLine(w, in.Base)
	mdWarnings(w, in.Base.Warnings)
	fmt.Fprintln(w, "| id | status | module | time | loc |")
	fmt.Fprintln(w, "|---|---|---|---|---|")
	for _, r := range in.Records {
		fmt.Fprintf(w, "| %s | %s | %s | %s | %s |\n",
			mdEscape(r.Case.ID), r.Case.Status, mdEscape(r.Case.Module),
			model.FormatDuration(r.Case.Time), mdEscape(r.Loc))
	}
}

// MDDiscover writes markdown report provenance.
func MDDiscover(w io.Writer, in DiscoverInput) {
	fmt.Fprintln(w, "## Reports")
	fmt.Fprintln(w)
	mdVerdictLine(w, in.Base)
	mdWarnings(w, in.Base.Warnings)
	fmt.Fprintln(w, "| path | module | mtime | suite | cases | status | source |")
	fmt.Fprintln(w, "|---|---|---|---|---|---|---|")
	for _, r := range in.Records {
		status := "pass"
		if r.Fail {
			status = "fail"
		}
		fmt.Fprintf(w, "| %s | %s | %s | %s | %d | %s | %s |\n",
			mdEscape(r.Path), mdEscape(r.Module), r.Mtime.Format("2006-01-02T15:04"),
			mdEscape(r.Suite), r.Cases, status, r.Source)
	}
}

// MDStats writes markdown stats: status/duration/slowest tables plus the
// remaining stat records.
func MDStats(w io.Writer, in StatsInput) {
	fmt.Fprintln(w, "## Stats")
	fmt.Fprintln(w)
	mdVerdictLine(w, in.Base)
	mdWarnings(w, in.Base.Warnings)
	mdModuleTable(w, in.Base.ModuleRows)

	var statusRows, slowestRows, other []StatRecord
	for _, r := range in.Records {
		switch r.Kind {
		case "status":
			statusRows = append(statusRows, r)
		case "slowest":
			slowestRows = append(slowestRows, r)
		case "duration":
			// inline line below
			other = append(other, r)
		default:
			other = append(other, r)
		}
	}
	if len(statusRows) > 0 {
		fmt.Fprintln(w, "| status | count | share |")
		fmt.Fprintln(w, "|---|---|---|")
		for _, r := range statusRows {
			f := statFields(r)
			fmt.Fprintf(w, "| %s | %s | %s |\n", f["status"], f["count"], f["share"])
		}
		fmt.Fprintln(w)
	}
	for _, r := range in.Records {
		if r.Kind != "duration" {
			continue
		}
		f := statFields(r)
		fmt.Fprintf(w, "sum=%s mean=%s median=%s p90=%s p95=%s max=%s\n\n",
			f["sum"], f["mean"], f["median"], f["p90"], f["p95"], f["max"])
	}
	if len(slowestRows) > 0 {
		fmt.Fprintln(w, "| rank | id | time | status |")
		fmt.Fprintln(w, "|---|---|---|---|")
		for _, r := range slowestRows {
			f := statFields(r)
			fmt.Fprintf(w, "| %s | %s | %s | %s |\n", f["rank"], mdEscape(f["id"]), f["time"], f["status"])
		}
		fmt.Fprintln(w)
	}
	if len(other) > 0 {
		fmt.Fprintln(w, "```")
		for _, r := range other {
			fmt.Fprintf(w, "stat kind=%s", r.Kind)
			for _, f := range r.Fields {
				fmt.Fprintf(w, " %s=%s", f.Key, f.Value)
			}
			fmt.Fprintln(w)
		}
		fmt.Fprintln(w, "```")
	}
}

func statFields(r StatRecord) map[string]string {
	m := make(map[string]string, len(r.Fields))
	for _, f := range r.Fields {
		m[f.Key] = f.Value
	}
	return m
}

// MDGrep writes markdown matches as a table.
func MDGrep(w io.Writer, in GrepInput) {
	fmt.Fprintln(w, "## Matches")
	fmt.Fprintln(w)
	mdVerdictLine(w, in.Base)
	mdWarnings(w, in.Base.Warnings)
	if in.CountOnly {
		fmt.Fprintf(w, "matches %d\n", in.MatchCount)
		return
	}
	if in.IdsOnly {
		for _, id := range in.MatchedIDs {
			fmt.Fprintln(w, id)
		}
		return
	}
	fmt.Fprintln(w, "| id | module | status | field | line | text |")
	fmt.Fprintln(w, "|---|---|---|---|---|---|")
	for _, r := range in.Records {
		fmt.Fprintf(w, "| %s | %s | %s | %s | %d | %s |\n",
			mdEscape(r.Case.ID), mdEscape(r.Case.Module), r.Case.Status,
			r.Field, r.Line, mdEscape(strings.Join(r.Lines, " ⏎ ")))
	}
}
