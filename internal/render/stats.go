package render

import (
	"fmt"
	"io"
)

// StatField is one ordered key/value pair of a stat record.
type StatField struct{ Key, Value string }

// StatRecord is one `stat kind=…` line (spec §10.3).
type StatRecord struct {
	Kind   string
	Fields []StatField
}

// StatsInput is everything the stats digest needs.
type StatsInput struct {
	Base    SummaryInput
	Records []StatRecord
}

// StatsText writes the stats digest: preamble, then stat records in the
// fixed section order (status, duration, slowest, type, module, flaky,
// skipped) already assembled and ordered by the CLI.
func StatsText(w io.Writer, in StatsInput) {
	writePreamble(w, in.Base)
	for _, r := range in.Records {
		fmt.Fprintf(w, "stat kind=%s", r.Kind)
		for _, f := range r.Fields {
			fmt.Fprintf(w, " %s=%s", f.Key, EncodeValue(f.Value))
		}
		fmt.Fprintln(w)
	}
}
