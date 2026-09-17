// Package model defines the decoded report vocabulary shared by all packages
// (plan §3.1). Later phases build on these types; treat the API as stable.
package model

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Status is the outcome of a single case. The declaration order is fixed and
// used for status stats ordering (plan §3.1).
type Status int

const (
	StatusPassed Status = iota
	StatusFailed
	StatusError
	StatusSkipped
)

func (s Status) String() string {
	switch s {
	case StatusPassed:
		return "passed"
	case StatusFailed:
		return "failed"
	case StatusError:
		return "error"
	case StatusSkipped:
		return "skipped"
	default:
		return fmt.Sprintf("status(%d)", int(s))
	}
}

// Statuses is the canonical fixed order for iterating statuses.
var Statuses = []Status{StatusPassed, StatusFailed, StatusError, StatusSkipped}

// ParseStatus maps a digest token ("passed", "failed", "error", "skipped")
// to a Status.
func ParseStatus(s string) (Status, bool) {
	for _, st := range Statuses {
		if st.String() == s {
			return st, true
		}
	}
	return 0, false
}

// OutRef is a lazy reference to captured output: a byte range within the
// sanitized stream of the report at Path. Content is never accumulated during
// decode (plan §4).
type OutRef struct {
	Path        string
	Start, End  int64
	Valid       bool
	IsErrStream bool // true for <system-err>, false for <system-out>
}

// Failure holds the primary failure or error detail of a case.
type Failure struct {
	Kind    Status // StatusFailed or StatusError
	Type    string // exception type attr, or derived from the first trace line
	Message string // message attr preferred; else first trace line
	Trace   []string
}

// Case is one <testcase>, identified by ID = ClassName#Name.
type Case struct {
	ID         string
	ClassName  string
	Name       string
	Module     string
	Status     Status
	Flaky      bool
	Time       float64 // seconds
	Failure    *Failure
	ReportPath string // repo-relative
	Out, Err   OutRef // case-level captured output
	SuiteName  string

	SkipMessage string // <skipped message="...">, kept for stats skipped-reasons
}

// Suite is a <testsuite> element (minimal info needed downstream).
type Suite struct {
	Name         string
	Timestamp    time.Time
	HasTimestamp bool
	Out, Err     OutRef
}

// Report is one decoded JUnit XML file.
type Report struct {
	Path   string // repo-relative
	Module string
	Source string // gradle | surefire | failsafe ("" when not derivable)

	// Aggregated is true when the file's root element was <testsuites>
	// (an aggregated report). Used by dedup preference (plan §6).
	Aggregated bool

	SuiteName    string
	Timestamp    time.Time // newest suite timestamp in the file
	HasTimestamp bool
	Mtime        time.Time // file mtime, set by discovery

	Cases []*Case
}

// Totals are always recomputed from cases, never trusted from XML attributes
// (spec §17).
type Totals struct {
	Tests   int
	Passed  int
	Failed  int
	Errors  int
	Skipped int
	Flaky   int
	Time    float64 // seconds
}

// Verdict reports whether the (filtered) set is passing: zero failed and zero
// error cases (plan D11).
func (t Totals) Verdict() bool { return t.Failed == 0 && t.Errors == 0 }

// Warning is a non-fatal condition surfaced as a `warn` digest line.
type Warning struct {
	Code    string // PARSE_ERROR, ENCODING, STALE, NOT_XML, ...
	Message string // verbatim text printed after the code
	Path    string // empty when not file-scoped
}

// FormatDuration renders seconds per plan D7: <0.1s → integer ms; <60s →
// seconds with ≤2 decimals, trailing zeros trimmed but ≥1 decimal kept;
// ≥60s → "<m>m<s>s" with unbounded minutes. Locale-independent.
func FormatDuration(sec float64) string {
	if sec < 0 {
		sec = 0
	}
	if sec < 0.1 {
		return strconv.FormatInt(int64(math.Round(sec*1000)), 10) + "ms"
	}
	if sec < 60 {
		s := strconv.FormatFloat(sec, 'f', 2, 64)
		s = strings.TrimRight(s, "0")
		if strings.HasSuffix(s, ".") {
			s += "0"
		}
		return s + "s"
	}
	total := int64(math.Round(sec))
	return strconv.FormatInt(total/60, 10) + "m" + strconv.FormatInt(total%60, 10) + "s"
}

// FormatAge renders a duration per plan D8: <60s → "42s"; <60m → "4m";
// <24h → "3h" or "3h12m"; else "74d".
func FormatAge(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return strconv.FormatInt(int64(d.Seconds()), 10) + "s"
	case d < time.Hour:
		return strconv.FormatInt(int64(d.Minutes()), 10) + "m"
	case d < 24*time.Hour:
		h := int64(d / time.Hour)
		m := int64(d/time.Minute) % 60
		if m > 0 {
			return strconv.FormatInt(h, 10) + "h" + strconv.FormatInt(m, 10) + "m"
		}
		return strconv.FormatInt(h, 10) + "h"
	default:
		return strconv.FormatInt(int64(d/(24*time.Hour)), 10) + "d"
	}
}
