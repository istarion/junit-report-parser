package render

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/istarion/junit-report-parser/internal/model"
)

// GroupRecord is one fingerprint-collapsed failure group (spec §10.3, D20).
type GroupRecord struct {
	Count       int
	Fingerprint string
	Sample      string   // representative failure message (uncapped)
	IDs         []string // member test ids in canonical order
}

// FailRecord is one detailed failure/error row with its location and
// trimmed trace. Loc is computed by the CLI (via trim classification).
type FailRecord struct {
	Case    *model.Case
	Loc     string
	Frames  []string // trimmed frames (`at` continuations)
	Hidden  int      // frames dropped by trimming (drives `trunc`)
	OutTail []string // captured-output tail lines (`out` continuations)
	ErrTail []string // captured-error tail lines (`err` continuations)
}

// MaxMessageRunes is the medium-budget message cap (spec §13); applied as a
// plain default until budget presets arrive (P5).
const MaxMessageRunes = 500

// CapMessage cuts msg to max runes (0 or less = unlimited) and reports how
// many runes were hidden.
func CapMessage(msg string, max int) (string, int) {
	if max <= 0 {
		return msg, 0
	}
	r := []rune(msg)
	if len(r) <= max {
		return msg, 0
	}
	return string(r[:max]), len(r) - max
}

// TestsContinuation joins ids per plan D18: capped at 10, then ",+N more".
func TestsContinuation(ids []string) string {
	const max = 10
	if len(ids) <= max {
		return strings.Join(ids, ",")
	}
	return strings.Join(ids[:max], ",") + ",+" + strconv.Itoa(len(ids)-max) + " more"
}

// writeGroup emits one `group` record with msg/tests continuations (§10.4).
func writeGroup(w io.Writer, g GroupRecord, maxMessage int) {
	fmt.Fprintf(w, "group count=%d fingerprint=%s\n", g.Count, g.Fingerprint)
	sample, hidden := CapMessage(firstLine(g.Sample), maxMessage)
	writeMessage(w, sample)
	writeTruncChars(w, hidden)
	fmt.Fprintf(w, "  tests %s\n", TestsContinuation(g.IDs))
}

// firstLine returns the first line of s.
func firstLine(s string) string {
	return strings.SplitN(strings.TrimRight(s, "\r\n"), "\n", 2)[0]
}

// writeMessage emits `  msg <text>` continuation lines. Embedded newlines
// become additional msg lines (§10.2).
func writeMessage(w io.Writer, text string) {
	text = strings.TrimRight(text, "\r\n")
	for _, line := range strings.Split(text, "\n") {
		fmt.Fprintf(w, "  msg %s\n", strings.TrimSuffix(line, "\r"))
	}
}

// writeTruncChars reports message-cap overruns by reusing the `trunc` line
// class (§10.3) with a chars unit.
func writeTruncChars(w io.Writer, hidden int) {
	if hidden > 0 {
		fmt.Fprintf(w, "  trunc %d chars hidden\n", hidden)
	}
}

// writeOutputContinuations emits `out`/`err` tail lines (§10.4). Text is
// verbatim; no escaping (spec §10.2).
func writeOutputContinuations(w io.Writer, outTail, errTail []string) {
	for _, l := range outTail {
		fmt.Fprintf(w, "  out %s\n", strings.TrimSuffix(l, "\r"))
	}
	for _, l := range errTail {
		fmt.Fprintf(w, "  err %s\n", strings.TrimSuffix(l, "\r"))
	}
}

// writeFailHeader emits the `fail` record line (shared by summary/failures).
func writeFailHeader(w io.Writer, fr FailRecord, useColor bool) {
	c := fr.Case
	typ := ""
	if c.Failure != nil {
		typ = c.Failure.Type
	}
	line := fmt.Sprintf("fail id=%s status=%s module=%s loc=%s type=%s time=%s",
		EncodeValue(c.ID),
		c.Status,
		EncodeValue(c.Module),
		EncodeValue(fr.Loc),
		EncodeValue(typ),
		model.FormatDuration(c.Time),
	)
	writeColored(w, useColor, ansiRed, line)
}
