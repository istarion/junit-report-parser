package render

import (
	"fmt"
	"io"
	"strings"
)

// FailuresInput is everything the failures digest needs (plan D20 shape:
// shared preamble, then group records, then detailed fail records).
type FailuresInput struct {
	Base   SummaryInput // preamble fields (Root/RunTime/Totals/Warnings/UseColor…)
	Fails  []FailRecord
	HintID string // first fail id → `hint junit-results show <id>` (D19)
}

// FailuresText writes the failures digest (spec §10.4): preamble identical
// to summary, then group records (--no-group drops them), then one detailed
// fail record per failure with msg/at/trunc continuations.
func FailuresText(w io.Writer, in FailuresInput) {
	writePreamble(w, in.Base)

	for _, g := range in.Base.Groups {
		writeGroup(w, g, in.Base.MaxMessage)
	}

	for _, fr := range in.Fails {
		c := fr.Case
		writeFailHeader(w, fr, in.Base.UseColor)
		if c.Failure != nil && c.Failure.Message != "" {
			capped, hidden := CapMessage(c.Failure.Message, in.Base.MaxMessage)
			writeMessage(w, capped) // embedded newlines → repeated msg lines
			writeTruncChars(w, hidden)
		}
		for _, f := range fr.Frames {
			// The continuation key IS the `at` keyword; strip it from the
			// frame text so the digest does not double it.
			fmt.Fprintf(w, "  at %s\n", strings.TrimPrefix(strings.TrimSpace(f), "at "))
		}
		if fr.Hidden > 0 {
			fmt.Fprintf(w, "  trunc %d frames hidden\n", fr.Hidden)
		}
		writeOutputContinuations(w, fr.OutTail, fr.ErrTail)
	}

	if in.HintID != "" {
		fmt.Fprintf(w, "hint junit-results show %s\n", EncodeValue(in.HintID))
	}
}
