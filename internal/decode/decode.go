// Package decode implements the streaming JUnit XML decoder (plan §4):
// token-driven via encoding/xml, never a whole-document Unmarshal. Captured
// output is recorded as lazy OutRef byte ranges — content is never
// accumulated. Invalid UTF-8 is sanitized to U+FFFD; declared ISO-8859-1 is
// converted. Malformed or truncated files yield an error (PARSE_ERROR at the
// call site) and the file is skipped entirely.
package decode

import (
	"bufio"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/istarion/junit-report-parser/internal/model"
)

// ErrNotXML marks files whose root element is not <testsuite>/<testsuites>
// (or that contain no XML element at all). Callers surface it as the
// verbose-only NOT_XML warning and skip the file.
var ErrNotXML = errors.New("not a junit xml report")

// DecodeFile opens path and decodes it.
func DecodeFile(path string) (*model.Report, []model.Warning, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	return Decode(path, f)
}

// Decode parses one JUnit XML report from input, attributing it to path.
// Returns the decoded report plus non-fatal warnings (ENCODING). A non-nil
// error means the file must be skipped: ErrNotXML or a parse error
// (malformed/truncated).
func Decode(path string, input io.Reader) (*model.Report, []model.Warning, error) {
	br := bufio.NewReaderSize(input, 64*1024)
	var src io.Reader = br
	if sniffEncoding(br) == "iso-8859-1" {
		src = newLatin1Reader(br)
	}
	san := newSanitizingReader(src)
	d := xml.NewDecoder(san)
	// Charset conversion already happened in the reader chain (sniffed from
	// the XML declaration); any other declared encoding falls back to the
	// sanitized stream as-is (plan §4).
	d.CharsetReader = func(charset string, r io.Reader) (io.Reader, error) { return r, nil }

	p := &parser{dec: d, path: path}
	if err := p.run(); err != nil {
		return nil, nil, err
	}
	if san.Invalid {
		p.warnings = append(p.warnings, model.Warning{
			Code: "ENCODING", Path: path,
			Message: "invalid UTF-8 bytes replaced",
		})
	}
	return &p.report, p.warnings, nil
}

type frameKind int

const (
	frSuites frameKind = iota // inside root <testsuites>
	frSuite                   // inside <testsuite> (root or nested)
	frCase                    // inside <testcase>
	frFail                    // inside <failure>/<error>: collect text
	frOut                     // inside <system-out>/<system-err>: lazy ref
	frSkip                    // unknown subtree: skip
)

type frame struct {
	kind  frameKind
	suite suiteFrame    // frSuite
	c     *caseBuild    // frCase
	fa    *failAcc      // frFail (nil for flaky*/rerun*: flag-only)
	ref   *model.OutRef // frOut
	name  string        // frOut: element name, for raw end back-compute
}

type suiteFrame struct {
	name         string
	timestamp    time.Time
	hasTimestamp bool
	out, err     model.OutRef
	cases        []*model.Case // backfilled with suite-level refs at close
}

// failAcc accumulates one failure/error element. text is raw element text,
// converted to lines once when the element closes.
type failAcc struct {
	kind  model.Status
	typ   string
	msg   string
	text  []byte
	lines []string
}

type caseBuild struct {
	c          *model.Case
	hasSkipped bool
	skipMsg    string
	firstFail  *failAcc
	firstErr   *failAcc
}

type parser struct {
	dec      *xml.Decoder
	path     string
	report   model.Report
	warnings []model.Warning
	stack    []frame
}

func (p *parser) run() error {
	// Root element validation: first element must be testsuite or testsuites.
	for {
		tok, err := p.dec.Token()
		if err == io.EOF {
			return ErrNotXML
		}
		if err != nil {
			return err
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue // ProcInst, Directive, Comment, leading whitespace
		}
		switch se.Name.Local {
		case "testsuite":
			p.stack = append(p.stack, frame{kind: frSuite, suite: parseSuiteAttrs(se)})
		case "testsuites":
			p.report.Aggregated = true // root <testsuites>: aggregated source
			p.stack = append(p.stack, frame{kind: frSuites})
		default:
			return ErrNotXML
		}
		if name := attrVal(se, "name"); name != "" {
			p.report.SuiteName = name
		}
		break
	}

	for {
		tok, err := p.dec.Token()
		if err == io.EOF {
			if len(p.stack) > 0 {
				return fmt.Errorf("truncated xml: %d unclosed element(s)", len(p.stack))
			}
			return nil
		}
		if err != nil {
			return err
		}
		off := p.dec.InputOffset() // byte offset of the end of this token

		switch t := tok.(type) {
		case xml.StartElement:
			if len(p.stack) == 0 {
				return errors.New("content after root element")
			}
			p.startElement(t, off)
		case xml.EndElement:
			if len(p.stack) == 0 {
				return errors.New("unbalanced end element")
			}
			p.endElement(t, off)
		case xml.CharData:
			if len(p.stack) > 0 {
				if top := &p.stack[len(p.stack)-1]; top.kind == frFail && top.fa != nil {
					top.fa.text = append(top.fa.text, t...)
				}
			}
			// everything else is whitespace or skipped content: never retained
		}
	}
}

func (p *parser) startElement(se xml.StartElement, off int64) {
	top := &p.stack[len(p.stack)-1]
	switch top.kind {
	case frSuites, frSuite:
		switch se.Name.Local {
		case "testsuite":
			sf := parseSuiteAttrs(se)
			if p.report.SuiteName == "" && sf.name != "" {
				// Root <testsuites> carries no name; the first named suite serves.
				p.report.SuiteName = sf.name
			}
			p.stack = append(p.stack, frame{kind: frSuite, suite: sf})
		case "testcase":
			cb := &caseBuild{c: parseCaseAttrs(se)}
			cb.c.SuiteName = top.suite.name
			top.suite.cases = append(top.suite.cases, cb.c)
			p.stack = append(p.stack, frame{kind: frCase, c: cb})
		case "system-out", "system-err":
			p.pushOut(se, off)
		default:
			p.stack = append(p.stack, frame{kind: frSkip}) // properties, etc.
		}
	case frCase:
		c := top.c
		switch se.Name.Local {
		case "failure", "error":
			fa := parseFailAttrs(se, topElementStatus(se.Name.Local))
			p.stack = append(p.stack, frame{kind: frFail, fa: fa})
		case "flakyFailure", "rerunFailure", "flakyError", "rerunError":
			c.c.Flaky = true // flag only; does not affect status (plan §4)
			p.stack = append(p.stack, frame{kind: frFail})
		case "skipped":
			c.hasSkipped = true
			c.skipMsg = attrVal(se, "message") // kept for stats skipped-reasons
			p.stack = append(p.stack, frame{kind: frSkip})
		case "system-out", "system-err":
			p.pushOut(se, off)
		default:
			p.stack = append(p.stack, frame{kind: frSkip})
		}
	default: // frFail, frOut, frSkip: nested content is discarded
		p.stack = append(p.stack, frame{kind: frSkip})
	}
}

func (p *parser) pushOut(se xml.StartElement, off int64) {
	ref := &model.OutRef{
		Path:        p.path,
		Start:       off, // just past the start tag
		Valid:       true,
		IsErrStream: se.Name.Local == "system-err",
	}
	p.stack = append(p.stack, frame{kind: frOut, ref: ref, name: se.Name.Local})
}

func (p *parser) endElement(se xml.EndElement, off int64) {
	top := p.stack[len(p.stack)-1]
	p.stack = p.stack[:len(p.stack)-1]

	switch top.kind {
	case frSuite:
		p.closeSuite(&top.suite)
	case frCase:
		p.finalizeCase(top.c)
	case frFail:
		if top.fa == nil {
			return // flaky*/rerun*: flag recorded at start, text never kept
		}
		fa := top.fa
		fa.lines = splitTrace(fa.text)
		fa.text = nil
		if len(p.stack) == 0 {
			return
		}
		cb := p.stack[len(p.stack)-1].c
		switch fa.kind {
		case model.StatusFailed:
			if cb.firstFail == nil { // first <failure> is primary (plan §4)
				cb.firstFail = fa
			}
		case model.StatusError:
			if cb.firstErr == nil {
				cb.firstErr = fa
			}
		}
	case frOut:
		// Content end = start of the raw end tag. InputOffset() is past
		// "</name>"; back-computing keeps escaped entities inside the range
		// and excludes the end tag itself.
		rawEnd := off - int64(len(top.name)+3)
		top.ref.End = rawEnd
		if rawEnd <= top.ref.Start {
			top.ref.Valid = false // empty or self-closing
		}
		if len(p.stack) == 0 {
			return
		}
		parent := &p.stack[len(p.stack)-1]
		switch parent.kind {
		case frCase:
			if top.ref.IsErrStream {
				parent.c.c.Err = *top.ref
			} else {
				parent.c.c.Out = *top.ref
			}
		case frSuite:
			if top.ref.IsErrStream {
				parent.suite.err = *top.ref
			} else {
				parent.suite.out = *top.ref
			}
		}
	}
}

// closeSuite merges suite info upward: the report-level newest timestamp and
// backfill of suite-level captured output onto this suite's cases (spec §17:
// case-level is preferred, suite-level is the fallback).
func (p *parser) closeSuite(sf *suiteFrame) {
	if sf.hasTimestamp && (!p.report.HasTimestamp || sf.timestamp.After(p.report.Timestamp)) {
		p.report.Timestamp = sf.timestamp
		p.report.HasTimestamp = true
	}
	if sf.out.Valid || sf.err.Valid {
		// Backfill suite-level refs onto cases of THIS suite only when they
		// have none of their own (spec §17: case-level preferred). Runs even
		// for the root suite (empty stack after pop).
		for _, c := range sf.cases {
			if sf.out.Valid && !c.Out.Valid {
				c.Out = sf.out
			}
			if sf.err.Valid && !c.Err.Valid {
				c.Err = sf.err
			}
		}
	}
	if len(p.stack) == 0 {
		return // root suite: nothing above to merge into
	}
	parent := &p.stack[len(p.stack)-1]
	if parent.kind == frSuite {
		parent.suite.cases = append(parent.suite.cases, sf.cases...)
		if sf.out.Valid && !parent.suite.out.Valid {
			parent.suite.out = sf.out
		}
		if sf.err.Valid && !parent.suite.err.Valid {
			parent.suite.err = sf.err
		}
	}
}

func (p *parser) finalizeCase(cb *caseBuild) {
	c := cb.c
	// Status precedence: failure > error > skipped > passed (spec §17).
	switch {
	case cb.firstFail != nil:
		c.Status = model.StatusFailed
		c.Failure = buildFailure(cb.firstFail)
	case cb.firstErr != nil:
		c.Status = model.StatusError
		c.Failure = buildFailure(cb.firstErr)
	case cb.hasSkipped:
		c.Status = model.StatusSkipped
		c.SkipMessage = cb.skipMsg
	default:
		c.Status = model.StatusPassed
	}
	if c.ClassName != "" {
		c.ID = c.ClassName + "#" + c.Name
	} else {
		c.ID = c.Name
	}
	c.ReportPath = p.path
	p.report.Cases = append(p.report.Cases, c)
}

func buildFailure(fa *failAcc) *model.Failure {
	f := &model.Failure{Kind: fa.kind, Type: fa.typ, Message: fa.msg, Trace: fa.lines}
	if f.Message == "" && len(f.Trace) > 0 {
		f.Message = f.Trace[0] // message attr preferred; else first trace line
	}
	if f.Type == "" && len(f.Trace) > 0 {
		f.Type = firstTraceToken(f.Trace[0])
	}
	return f
}

// parseSuiteAttrs reads <testsuite> attributes. Counts (tests/failures/…)
// are deliberately ignored: totals are recomputed from cases (spec §17).
func parseSuiteAttrs(se xml.StartElement) suiteFrame {
	sf := suiteFrame{name: attrVal(se, "name")}
	if raw := attrVal(se, "timestamp"); raw != "" {
		if t, ok := parseTimestamp(raw); ok {
			sf.timestamp, sf.hasTimestamp = t, true
		}
	}
	return sf
}

func parseCaseAttrs(se xml.StartElement) *model.Case {
	c := &model.Case{
		Name:      attrVal(se, "name"),
		ClassName: attrVal(se, "classname"),
	}
	if t, err := parseSeconds(attrVal(se, "time")); err == nil {
		c.Time = t
	}
	return c
}

func parseFailAttrs(se xml.StartElement, kind model.Status) *failAcc {
	return &failAcc{
		kind: kind,
		typ:  attrVal(se, "type"),
		msg:  attrVal(se, "message"),
	}
}

func topElementStatus(local string) model.Status {
	if local == "error" {
		return model.StatusError
	}
	return model.StatusFailed
}

// splitTrace splits raw element text into trace lines: \r trimmed, leading
// and trailing empty lines dropped, interior lines preserved.
func splitTrace(raw []byte) []string {
	lines := strings.Split(string(raw), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// firstTraceToken derives a failure type from the first trace line when the
// type attribute is absent: the token before the first colon of an exception
// header ("java.net.ConnectException: Connection refused" →
// "java.net.ConnectException"). Frame lines yield "".
func firstTraceToken(line string) string {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "at ") {
		return ""
	}
	tok := line
	if i := strings.IndexByte(line, ':'); i >= 0 {
		tok = line[:i]
	}
	tok = strings.TrimSpace(tok)
	if tok == "" || strings.ContainsAny(tok, " \t") {
		return ""
	}
	return tok
}

// parseTimestamp parses the suite `timestamp` attribute: RFC3339 (explicit
// zone) first, then naive layouts interpreted in the local zone (Gradle and
// Maven write local wall-clock times).
func parseTimestamp(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, true
	}
	for _, layout := range []string{
		"2006-01-02T15:04:05.999999999",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
	} {
		if t, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// parseSeconds parses a `time` attribute in seconds. A comma decimal
// separator is tolerated (some Maven locales).
func parseSeconds(raw string) (float64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, errors.New("empty time")
	}
	return strconv.ParseFloat(strings.ReplaceAll(raw, ",", "."), 64)
}

func attrVal(se xml.StartElement, name string) string {
	for _, a := range se.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}
