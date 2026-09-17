package render

import (
	"fmt"
	"io"
	"time"
)

// DiscoverRecord is one `report` row of the discovery digest (spec §10.3).
type DiscoverRecord struct {
	Path   string // repo-relative
	Module string
	Mtime  time.Time
	Suite  string
	Cases  int
	Fail   bool // true when the report contains a failed/error case
	Source string
}

// DiscoverInput is everything the discovery digest needs.
type DiscoverInput struct {
	Base    SummaryInput
	Records []DiscoverRecord // sorted by path asc (CLI pre-sorts)
}

// DiscoverText writes the discovery digest (plan §5): standard preamble
// (run-level staleness still applies) followed by one `report` record per
// discovered file, unfiltered by --newer-than/--max-age.
func DiscoverText(w io.Writer, in DiscoverInput) {
	writePreamble(w, in.Base)
	for _, r := range in.Records {
		status := "pass"
		if r.Fail {
			status = "fail"
		}
		fmt.Fprintf(w, "report path=%s module=%s mtime=%s suite=%s cases=%d status=%s source=%s\n",
			EncodeValue(r.Path),
			EncodeValue(r.Module),
			r.Mtime.Format("2006-01-02T15:04"), // naive local (D6)
			EncodeValue(r.Suite),
			r.Cases,
			status,
			EncodeValue(r.Source),
		)
	}
}
