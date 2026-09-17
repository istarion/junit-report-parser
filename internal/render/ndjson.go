package render

import (
	"bytes"
	"encoding/json"
	"io"
	"strconv"

	"github.com/istarion/junit-report-parser/internal/model"
)

// NDJSON emits one JSON object per line (spec §12): line 1 is the header,
// then one object per emitted record in emission order (partial-safe).
// Key order is fixed by structs; HTML escaping is off for readability.

func writeJSONLine(w io.Writer, v any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return // encoding of our own structs cannot fail meaningfully
	}
	_, _ = w.Write(buf.Bytes())
}

type ndHeader struct {
	Verdict string   `json:"verdict"`
	Root    string   `json:"root"`
	Run     ndRun    `json:"run"`
	Totals  ndTotals `json:"totals"`
}

type ndRun struct {
	Timestamp  string  `json:"timestamp"`
	AgeSeconds float64 `json:"ageSeconds"`
	Stale      bool    `json:"stale"`
	Reports    int     `json:"reports"`
	Modules    int     `json:"modules"`
}

type ndTotals struct {
	Tests       int     `json:"tests"`
	Passed      int     `json:"passed"`
	Failures    int     `json:"failures"`
	Errors      int     `json:"errors"`
	Skipped     int     `json:"skipped"`
	Flaky       int     `json:"flaky"`
	TimeSeconds float64 `json:"timeSeconds"`
}

type ndWarn struct {
	Type    string `json:"type"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ndGroup struct {
	Type        string   `json:"type"`
	Count       int      `json:"count"`
	Fingerprint string   `json:"fingerprint"`
	Message     string   `json:"msg"`
	Tests       []string `json:"tests"`
}

type ndFail struct {
	Type        string   `json:"type"`
	ID          string   `json:"id"`
	Status      string   `json:"status"`
	Module      string   `json:"module"`
	Loc         string   `json:"loc"`
	ErrorType   string   `json:"errorType,omitempty"`
	TimeSeconds float64  `json:"timeSeconds"`
	Message     string   `json:"message,omitempty"`
	At          []string `json:"at,omitempty"`
	Trunc       int      `json:"trunc,omitempty"`
	Out         []string `json:"out,omitempty"`
	Err         []string `json:"err,omitempty"`
}

type ndCase struct {
	Type        string   `json:"type"`
	ID          string   `json:"id"`
	Status      string   `json:"status"`
	Module      string   `json:"module"`
	TimeSeconds float64  `json:"timeSeconds"`
	Loc         string   `json:"loc"`
	Message     string   `json:"message,omitempty"`
	At          []string `json:"at,omitempty"`
	Trunc       int      `json:"trunc,omitempty"`
	Out         []string `json:"out,omitempty"`
	Err         []string `json:"err,omitempty"`
}

type ndMatch struct {
	Type   string   `json:"type"`
	ID     string   `json:"id"`
	Module string   `json:"module"`
	Status string   `json:"status"`
	Field  string   `json:"field"`
	Line   int      `json:"line"`
	Text   []string `json:"text,omitempty"`
}

type ndReport struct {
	Type   string `json:"type"`
	Path   string `json:"path"`
	Module string `json:"module"`
	Mtime  string `json:"mtime"`
	Suite  string `json:"suite"`
	Cases  int    `json:"cases"`
	Status string `json:"status"`
	Source string `json:"source"`
}

type ndHint struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ndStatLine flattens a stat record so kind-specific fields are top-level.
type ndStatLine map[string]any

func ndHeaderObj(in SummaryInput) ndHeader {
	return ndHeader{
		Verdict: verdictWord(in.Totals),
		Root:    in.Root,
		Run: ndRun{
			Timestamp:  in.RunTime.UTC().Format(rfc3339),
			AgeSeconds: in.Age.Seconds(),
			Stale:      in.Stale,
			Reports:    in.Reports,
			Modules:    in.Modules,
		},
		Totals: ndTotals{
			Tests: in.Totals.Tests, Passed: in.Totals.Passed,
			Failures: in.Totals.Failed, Errors: in.Totals.Errors,
			Skipped: in.Totals.Skipped, Flaky: in.Totals.Flaky,
			TimeSeconds: in.Totals.Time,
		},
	}
}

func verdictWord(t model.Totals) string {
	if t.Verdict() {
		return "pass"
	}
	return "fail"
}

func ndWarnLines(w io.Writer, warns []model.Warning) {
	for _, wn := range warns {
		writeJSONLine(w, ndWarn{Type: "warn", Code: wn.Code, Message: wn.Message})
	}
}

// anyValue keeps machine-friendly numbers where the string is numeric.
func anyValue(s string) any {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return s
}

// NDJSONSummary emits the summary record stream.
func NDJSONSummary(w io.Writer, in SummaryInput) {
	writeJSONLine(w, ndHeaderObj(in))
	ndWarnLines(w, in.Warnings)
	for _, g := range in.Groups {
		writeJSONLine(w, ndGroup{Type: "group", Count: g.Count, Fingerprint: g.Fingerprint, Message: firstLine(g.Sample), Tests: g.IDs})
	}
	for _, fr := range in.Fails {
		writeJSONLine(w, ndFail{
			Type: "fail", ID: fr.Case.ID, Status: fr.Case.Status.String(),
			Module: fr.Case.Module, Loc: fr.Loc, ErrorType: failureType(fr.Case),
			TimeSeconds: fr.Case.Time, Message: failureMessage(fr.Case),
		})
	}
	if !in.Totals.Verdict() {
		writeJSONLine(w, ndHint{Type: "hint", Text: "junit-results failures"})
	}
}

// NDJSONFailures emits the detailed record stream with traces.
func NDJSONFailures(w io.Writer, in FailuresInput) {
	writeJSONLine(w, ndHeaderObj(in.Base))
	ndWarnLines(w, in.Base.Warnings)
	for _, g := range in.Base.Groups {
		writeJSONLine(w, ndGroup{Type: "group", Count: g.Count, Fingerprint: g.Fingerprint, Message: firstLine(g.Sample), Tests: g.IDs})
	}
	for _, fr := range in.Fails {
		writeJSONLine(w, ndFail{
			Type: "fail", ID: fr.Case.ID, Status: fr.Case.Status.String(),
			Module: fr.Case.Module, Loc: fr.Loc, ErrorType: failureType(fr.Case),
			TimeSeconds: fr.Case.Time, Message: failureMessage(fr.Case),
			At: stripAt(fr.Frames), Trunc: fr.Hidden, Out: fr.OutTail, Err: fr.ErrTail,
		})
	}
	if in.HintID != "" {
		writeJSONLine(w, ndHint{Type: "hint", Text: "junit-results show " + in.HintID})
	}
}

// NDJSONShow emits one case object per match with full detail.
func NDJSONShow(w io.Writer, in ShowInput) {
	writeJSONLine(w, ndHeaderObj(in.Base))
	ndWarnLines(w, in.Base.Warnings)
	for _, r := range in.Records {
		writeJSONLine(w, ndCase{
			Type: "case", ID: r.Case.ID, Status: r.Case.Status.String(),
			Module: r.Case.Module, TimeSeconds: r.Case.Time, Loc: r.Loc,
			Message: failureMessage(r.Case), At: stripAt(r.Frames), Trunc: r.Hidden,
			Out: r.OutTail, Err: r.ErrTail,
		})
	}
}

// NDJSONList emits one case object per selected case.
func NDJSONList(w io.Writer, in ListInput) {
	writeJSONLine(w, ndHeaderObj(in.Base))
	ndWarnLines(w, in.Base.Warnings)
	for _, r := range in.Records {
		writeJSONLine(w, ndCase{
			Type: "case", ID: r.Case.ID, Status: r.Case.Status.String(),
			Module: r.Case.Module, TimeSeconds: r.Case.Time, Loc: r.Loc,
			Message: failureMessage(r.Case),
		})
	}
}

// NDJSONDiscover emits one report object per discovered file.
func NDJSONDiscover(w io.Writer, in DiscoverInput) {
	writeJSONLine(w, ndHeaderObj(in.Base))
	ndWarnLines(w, in.Base.Warnings)
	for _, r := range in.Records {
		status := "pass"
		if r.Fail {
			status = "fail"
		}
		writeJSONLine(w, ndReport{
			Type: "report", Path: r.Path, Module: r.Module,
			Mtime: r.Mtime.Format(rfc3339), Suite: r.Suite,
			Cases: r.Cases, Status: status, Source: r.Source,
		})
	}
}

// NDJSONStats emits one stat object per record (fields flattened).
func NDJSONStats(w io.Writer, in StatsInput) {
	writeJSONLine(w, ndHeaderObj(in.Base))
	ndWarnLines(w, in.Base.Warnings)
	for _, r := range in.Records {
		obj := ndStatLine{"type": "stat", "kind": r.Kind}
		for _, f := range r.Fields {
			obj[f.Key] = anyValue(f.Value)
		}
		writeJSONLine(w, obj)
	}
}

// NDJSONGrep emits one match object per record.
func NDJSONGrep(w io.Writer, in GrepInput) {
	writeJSONLine(w, ndHeaderObj(in.Base))
	ndWarnLines(w, in.Base.Warnings)
	for _, r := range in.Records {
		writeJSONLine(w, ndMatch{
			Type: "match", ID: r.Case.ID, Module: r.Case.Module,
			Status: r.Case.Status.String(), Field: r.Field, Line: r.Line, Text: r.Lines,
		})
	}
}

const rfc3339 = "2006-01-02T15:04:05Z07:00"

func failureType(c *model.Case) string {
	if c.Failure != nil {
		return c.Failure.Type
	}
	return ""
}

func failureMessage(c *model.Case) string {
	if c.Failure != nil {
		return c.Failure.Message
	}
	return ""
}

func stripAt(frames []string) []string {
	if len(frames) == 0 {
		return nil
	}
	out := make([]string, 0, len(frames))
	for _, f := range frames {
		out = append(out, trimAtKeyword(f))
	}
	return out
}

func trimAtKeyword(frame string) string {
	f := frame
	for len(f) > 0 && (f[0] == ' ' || f[0] == '\t') {
		f = f[1:]
	}
	if len(f) >= 3 && f[:3] == "at " {
		return f[3:]
	}
	return f
}
