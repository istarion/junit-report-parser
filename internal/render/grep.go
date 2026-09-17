package render

import (
	"fmt"
	"io"
	"strings"

	"github.com/istarion/junit-report-parser/internal/model"
)

// GrepRecord is one `match` row (spec §10.3): the record line identifies the
// hit; Lines are the verbatim `text` continuations (the matched line sits at
// its position when context is emitted; empty for inverted selections).
type GrepRecord struct {
	Case  *model.Case
	Field string
	Line  int
	Lines []string
}

// GrepInput is everything the grep digest needs. IdsOnly switches to the
// D17 bare-ids output (no preamble, no records).
type GrepInput struct {
	Base       SummaryInput
	Records    []GrepRecord
	IdsOnly    bool
	CountOnly  bool
	MatchCount int // printed by -c
	MatchedIDs []string
}

// GrepText writes the grep digest.
func GrepText(w io.Writer, in GrepInput) {
	if in.IdsOnly {
		for _, id := range in.MatchedIDs {
			fmt.Fprintln(w, id) // bare ids: no header, no records (D17)
		}
		return
	}
	writePreamble(w, in.Base)
	if in.CountOnly {
		fmt.Fprintf(w, "matches %d\n", in.MatchCount) // single line (D17)
		return
	}
	for _, r := range in.Records {
		line := fmt.Sprintf("match id=%s module=%s status=%s field=%s line=%d",
			EncodeValue(r.Case.ID),
			EncodeValue(r.Case.Module),
			r.Case.Status,
			r.Field,
			r.Line,
		)
		writeColored(w, in.Base.UseColor, ansiRed, line)
		for _, l := range r.Lines {
			// Continuations are verbatim (§10.2); no escaping.
			fmt.Fprintf(w, "  text %s\n", strings.TrimSuffix(l, "\r"))
		}
	}
}
