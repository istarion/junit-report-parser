package render

import (
	"fmt"
	"io"
	"time"

	"github.com/istarion/junit-report-parser/internal/model"
)

// ANSI palette: minimal, applied only when color is enabled (auto + TTY, or
// --color always). Never emitted when piped.
const (
	ansiReset  = "\x1b[0m"
	ansiGreen  = "\x1b[32m"
	ansiRed    = "\x1b[31m"
	ansiYellow = "\x1b[33m"
)

// ModuleRow is one module-aggregate row for markdown tables (§12).
type ModuleRow struct {
	Name        string
	Tests       int
	Failures    int
	Errors      int
	Skipped     int
	TimeSeconds float64
}

// SummaryInput carries the digest preamble (header/context/totals/warn) plus
// the record sections. It is shared by summary and failures rendering.
type SummaryInput struct {
	Root     string
	RunTime  time.Time
	Age      time.Duration
	Stale    bool
	Reports  int
	Modules  int
	Totals   model.Totals
	Warnings []model.Warning

	Groups     []GroupRecord // fingerprint-collapsed groups (D20)
	Fails      []FailRecord  // sorted, already capped by --top; Loc precomputed
	MaxMessage int           // message cap in runes; 0 = unlimited
	ModuleRows []ModuleRow   // markdown modules table (md format only)

	UseColor bool
}

// writePreamble emits the fixed head of every digest: verdict, context,
// totals, warn* (spec §10.1 emission order).
func writePreamble(w io.Writer, in SummaryInput) {
	if in.Totals.Verdict() {
		writeColored(w, in.UseColor, ansiGreen, "verdict=pass")
	} else {
		writeColored(w, in.UseColor, ansiRed, "verdict=fail")
	}

	fmt.Fprintf(w, "root=%s run=%s age=%s reports=%d modules=%d stale=%t\n",
		EncodeValue(AbbreviateHome(in.Root)),
		in.RunTime.Format("2006-01-02T15:04"), // naive local, no offset (D6)
		model.FormatAge(in.Age),
		in.Reports, in.Modules, in.Stale,
	)

	fmt.Fprintf(w, "totals tests=%d passed=%d failed=%d errors=%d skipped=%d flaky=%d time=%s\n",
		in.Totals.Tests, in.Totals.Passed, in.Totals.Failed, in.Totals.Errors,
		in.Totals.Skipped, in.Totals.Flaky, model.FormatDuration(in.Totals.Time),
	)

	for _, wn := range in.Warnings {
		writeColored(w, in.UseColor, ansiYellow, "warn "+wn.Code+" "+wn.Message)
	}
}

// SummaryText writes the text digest (spec §10): header, context, totals,
// warn*, group records (before fail lines, D20), fail records (msg
// continuation, locked in P1), hint* (D19).
func SummaryText(w io.Writer, in SummaryInput) {
	writePreamble(w, in)

	for _, g := range in.Groups {
		writeGroup(w, g, in.MaxMessage)
	}

	for _, fr := range in.Fails {
		writeFailHeader(w, fr, in.UseColor)
		if fr.Case.Failure != nil && fr.Case.Failure.Message != "" {
			// Locked decision (P1): summary fail records carry the FIRST
			// line of the failure message as `msg`.
			capped, hidden := CapMessage(firstLine(fr.Case.Failure.Message), in.MaxMessage)
			writeMessage(w, capped)
			writeTruncChars(w, hidden)
		}
	}

	if !in.Totals.Verdict() {
		fmt.Fprintln(w, "hint junit-results failures")
	}
}

func writeColored(w io.Writer, useColor bool, code, s string) {
	if useColor {
		fmt.Fprintf(w, "%s%s%s\n", code, s, ansiReset)
		return
	}
	fmt.Fprintln(w, s)
}
