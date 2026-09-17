package render

import (
	"fmt"
	"io"
	"strings"

	"github.com/istarion/junit-report-parser/internal/model"
)

// ShowRecord is one `case` row of the show digest (spec §10.3) with full
// detail: message, trimmed trace and tail-biased captured output.
type ShowRecord struct {
	Case    *model.Case
	Loc     string
	Frames  []string
	Hidden  int
	OutTail []string
	ErrTail []string
}

// ShowInput is everything the show digest needs.
type ShowInput struct {
	Base    SummaryInput
	Records []ShowRecord
}

// ShowText writes the show digest: preamble, then one `case` record per
// matching case with msg/at/out/err continuations.
func ShowText(w io.Writer, in ShowInput) {
	writePreamble(w, in.Base)
	for _, r := range in.Records {
		c := r.Case
		line := fmt.Sprintf("case id=%s status=%s module=%s time=%s loc=%s",
			EncodeValue(c.ID),
			c.Status,
			EncodeValue(c.Module),
			model.FormatDuration(c.Time),
			EncodeValue(r.Loc),
		)
		writeColored(w, in.Base.UseColor, ansiRed, line)
		if c.Failure != nil && c.Failure.Message != "" {
			capped, hidden := CapMessage(c.Failure.Message, in.Base.MaxMessage)
			writeMessage(w, capped) // embedded newlines → repeated msg lines
			writeTruncChars(w, hidden)
		}
		for _, f := range r.Frames {
			fmt.Fprintf(w, "  at %s\n", strings.TrimPrefix(strings.TrimSpace(f), "at "))
		}
		if r.Hidden > 0 {
			fmt.Fprintf(w, "  trunc %d frames hidden\n", r.Hidden)
		}
		writeOutputContinuations(w, r.OutTail, r.ErrTail)
	}
}
