package render

import (
	"fmt"
	"io"

	"github.com/istarion/junit-report-parser/internal/model"
)

// ListRecord is one `case` row of the list digest (spec §10.3).
type ListRecord struct {
	Case *model.Case
	Loc  string // precomputed by the CLI via trim
}

// ListInput is everything the list digest needs.
type ListInput struct {
	Base    SummaryInput
	Records []ListRecord
}

// ListText writes the flat case list: preamble, then `case` records sorted
// (module, classname, name) — the CLI pre-sorts.
func ListText(w io.Writer, in ListInput) {
	writePreamble(w, in.Base)
	for _, r := range in.Records {
		line := fmt.Sprintf("case id=%s status=%s module=%s time=%s loc=%s",
			EncodeValue(r.Case.ID),
			r.Case.Status,
			EncodeValue(r.Case.Module),
			model.FormatDuration(r.Case.Time),
			EncodeValue(r.Loc),
		)
		if r.Case.Status == model.StatusFailed || r.Case.Status == model.StatusError {
			writeColored(w, in.Base.UseColor, ansiRed, line)
		} else {
			fmt.Fprintln(w, line)
		}
	}
	if !in.Base.Totals.Verdict() {
		fmt.Fprintln(w, "hint junit-results failures")
	}
}
