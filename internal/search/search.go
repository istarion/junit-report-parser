// Package search implements grep semantics over the decoded model (plan §9,
// spec §15): field-scoped pattern search with RE2 regexes or literals,
// inversion, context, caps — and streamed out/err re-reads that are never
// retained. Enumeration order: (module, classname, name), then ascending
// line number (D10).
package search

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/istarion/junit-report-parser/internal/model"
)

// Field names (spec §15.1).
const (
	FieldID      = "id"
	FieldClass   = "class"
	FieldName    = "name"
	FieldType    = "type"
	FieldMessage = "message"
	FieldTrace   = "trace"
	FieldOut     = "out"
	FieldErr     = "err"
)

var defaultFields = []string{FieldID, FieldClass, FieldName, FieldType, FieldMessage, FieldTrace}
var validFields = map[string]bool{
	FieldID: true, FieldClass: true, FieldName: true, FieldType: true,
	FieldMessage: true, FieldTrace: true, FieldOut: true, FieldErr: true,
}

// ParseFields validates a --in value: comma-separated field names, "all"
// adds out/err to the defaults. Unknown names are a usage error.
func ParseFields(in string) ([]string, error) {
	in = strings.TrimSpace(in)
	if in == "" {
		return append([]string(nil), defaultFields...), nil
	}
	seen := map[string]bool{}
	var out []string
	for _, part := range strings.Split(in, ",") {
		name := strings.TrimSpace(part)
		if name == "" {
			continue
		}
		if name == "all" {
			for _, f := range append(append([]string(nil), defaultFields...), FieldOut, FieldErr) {
				if !seen[f] {
					seen[f] = true
					out = append(out, f)
				}
			}
			continue
		}
		if !validFields[name] {
			return nil, fmt.Errorf("unknown --in field %q (want id,class,name,type,message,trace,out,err or all)", name)
		}
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	if len(out) == 0 {
		return append([]string(nil), defaultFields...), nil
	}
	return out, nil
}

// Options for Compile.
type Options struct {
	Patterns      []string
	Fixed         bool // -F: literal instead of regex
	IgnoreCase    bool // -i
	Invert        bool // -v: select cases with NO match
	Fields        []string
	Context       int                                    // -C, 0..5, trace/out/err only
	MaxMatches    int                                    // cap on match records (<=0 → unlimited here; CLI normalizes)
	MaxLineLength int                                    // truncate emitted lines with … (<=0 → unlimited)
	ReadOut       func(ref model.OutRef) (string, error) // out/err re-reads
}

// Matcher is the compiled pattern set.
type Matcher struct {
	res  []*regexp.Regexp
	opts Options
}

// Compile compiles the pattern set (OR-combined at match time) and returns
// a Matcher bound to the search options. Any bad regex is a usage error;
// -F literals are QuoteMeta'd.
func Compile(opts Options) (*Matcher, error) {
	if len(opts.Patterns) == 0 {
		return nil, fmt.Errorf("no pattern given (use -e or a positional argument)")
	}
	m := &Matcher{opts: opts}
	for _, p := range opts.Patterns {
		if opts.Fixed {
			p = regexp.QuoteMeta(p)
		}
		if opts.IgnoreCase {
			p = "(?i)" + p
		}
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("bad pattern %q: %v", opts.Patterns[len(m.res)], err)
		}
		m.res = append(m.res, re)
	}
	return m, nil
}

// Match is one hit (or, when inverted, one selected case).
type Match struct {
	Case  *model.Case
	Field string   // id|class|name|type|message|trace|out|err; "-" when inverted
	Line  int      // 1-based within the field text; 0 when inverted
	Lines []string // text lines to emit as `text` continuations (matched line included at its position)
}

func (m *Matcher) matchesLine(s string) bool {
	for _, re := range m.res {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

// Search enumerates matches over the selected cases in canonical order.
// ReadOut must return the DECODED text of an OutRef span (the CLI wires
// decode.ReadRange + entity decoding; search stays storage-agnostic).
// out/err content is streamed, never retained beyond the match.
func (m *Matcher) Search(cases []*model.Case) ([]Match, error) {
	var matches []Match
	var inverted []*Match
	for _, c := range cases {
		caseMatched := false
		for _, field := range m.opts.Fields {
			lines, err := m.fieldLines(c, field)
			if err != nil {
				return nil, err
			}
			if lines == nil {
				continue
			}
			for i, line := range lines {
				if !m.matchesLine(line) {
					continue
				}
				caseMatched = true
				matches = append(matches, Match{
					Case:  c,
					Field: field,
					Line:  i + 1,
					Lines: m.contextBlock(field, lines, i),
				})
			}
		}
		if m.opts.Invert && !caseMatched {
			inverted = append(inverted, &Match{Case: c, Field: "-", Line: 0})
		}
	}

	if m.opts.Invert {
		// The selected universe IS the non-matching cases; regular hits are
		// dropped entirely.
		matches = deref(inverted)
	}
	m.sortMatches(matches)
	if m.opts.MaxMatches > 0 && len(matches) > m.opts.MaxMatches {
		matches = matches[:m.opts.MaxMatches]
	}
	return matches, nil
}

func deref(ms []*Match) []Match {
	out := make([]Match, 0, len(ms))
	for _, m := range ms {
		out = append(out, *m)
	}
	return out
}

var fieldOrder = map[string]int{}

func (m *Matcher) sortMatches(ms []Match) {
	sort.SliceStable(ms, func(i, j int) bool {
		a, b := &ms[i], &ms[j]
		ac, bc := a.Case, b.Case
		if ac.Module != bc.Module {
			return ac.Module < bc.Module
		}
		if ac.ClassName != bc.ClassName {
			return ac.ClassName < bc.ClassName
		}
		if ac.Name != bc.Name {
			return ac.Name < bc.Name
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return m.fieldRank(a.Field) < m.fieldRank(b.Field)
	})
}

// fieldRank breaks same-line ties by the searched-field order (defaults read
// id, class, name, …); unknown fields sort last, lexicographically.
func (m *Matcher) fieldRank(field string) int {
	for i, f := range m.opts.Fields {
		if f == field {
			return i
		}
	}
	return len(m.opts.Fields)
}

// contextBlock returns the emission block: lines [i-C, i+C] with the
// matched line at its position (trace/out/err only, per spec §15).
func (m *Matcher) contextBlock(field string, lines []string, i int) []string {
	c := m.opts.Context
	if c <= 0 || !contextable(field) {
		return []string{m.truncate(lines[i])}
	}
	lo := i - c
	if lo < 0 {
		lo = 0
	}
	hi := i + c + 1
	if hi > len(lines) {
		hi = len(lines)
	}
	block := make([]string, 0, hi-lo)
	for _, l := range lines[lo:hi] {
		block = append(block, m.truncate(l))
	}
	return block
}

func contextable(field string) bool {
	return field == FieldTrace || field == FieldOut || field == FieldErr
}

func (m *Matcher) truncate(s string) string {
	max := m.opts.MaxLineLength
	if max <= 0 || len([]rune(s)) <= max {
		return s
	}
	return string([]rune(s)[:max]) + "…"
}

// fieldLines returns the searched field's text split into lines (nil when
// the field is absent for this case). For out/err, ReadOut yields the
// already-decoded span text (CDATA stripped, entities resolved).
func (m *Matcher) fieldLines(c *model.Case, field string) ([]string, error) {
	var text string
	switch field {
	case FieldID:
		text = c.ID
	case FieldClass:
		text = c.ClassName
	case FieldName:
		text = c.Name
	case FieldType:
		if c.Failure != nil {
			text = c.Failure.Type
		}
	case FieldMessage:
		if c.Failure != nil {
			text = c.Failure.Message
		}
	case FieldTrace:
		if c.Failure != nil {
			text = strings.Join(c.Failure.Trace, "\n")
		}
	case FieldOut, FieldErr:
		ref := c.Out
		if field == FieldErr {
			ref = c.Err
		}
		if !ref.Valid {
			return nil, nil
		}
		if m.opts.ReadOut == nil {
			return nil, fmt.Errorf("out/err search requires an output reader")
		}
		raw, err := m.opts.ReadOut(ref)
		if err != nil {
			return nil, fmt.Errorf("re-reading %s of %s: %w", field, c.ID, err)
		}
		text = raw
	default:
		return nil, nil
	}
	if text == "" {
		return nil, nil
	}
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	return lines, nil
}
