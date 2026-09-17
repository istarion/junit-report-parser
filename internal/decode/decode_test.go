package decode

import (
	"errors"
	"strings"
	"testing"

	"github.com/istarion/junit-report-parser/internal/model"
)

func decodeStr(t *testing.T, xmlDoc string) (*model.Report, []model.Warning, error) {
	t.Helper()
	return Decode("TEST-x.xml", strings.NewReader(xmlDoc))
}

func hasWarning(warns []model.Warning, code string) bool {
	for _, w := range warns {
		if w.Code == code {
			return true
		}
	}
	return false
}

func TestDecodeRootValidation(t *testing.T) {
	tests := []struct {
		name    string
		xmlDoc  string
		wantXML bool // ErrNotXML expected
		wantErr bool // any parse error expected
	}{
		{"testsuite root", `<testsuite name="a"><testcase name="x"/></testsuite>`, false, false},
		{"testsuites root", `<testsuites><testsuite name="a"><testcase name="x"/></testsuite></testsuites>`, false, false},
		{"xml but wrong root", `<run><testcase/></run>`, true, true},
		{"html", `<html><body>hi</body></html>`, true, true},
		{"empty file", ``, true, true},
		{"only procinst", `<?xml version="1.0"?>`, true, true},
		{"truncated", `<testsuite name="a"><testcase name="x">`, false, true},
		{"malformed", `<testsuite><testcase></testsuite>`, false, true},
		{"garbage", `this is not xml at all`, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := decodeStr(t, tt.xmlDoc)
			if tt.wantErr && err == nil {
				t.Fatal("want error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantXML && !errors.Is(err, ErrNotXML) {
				t.Fatalf("want ErrNotXML, got %v", err)
			}
		})
	}
}

func TestDecodeCaseStatusPrecedence(t *testing.T) {
	tests := []struct {
		name   string
		inner  string
		status model.Status
		flaky  bool
	}{
		{"passed", ``, model.StatusPassed, false},
		{"skipped", `<skipped/>`, model.StatusSkipped, false},
		{"skipped message", `<skipped message="why"/>`, model.StatusSkipped, false},
		{"failed", `<failure message="m" type="T"/>`, model.StatusFailed, false},
		{"error", `<error message="m" type="T"/>`, model.StatusError, false},
		{"failure beats error", `<error/><failure/>`, model.StatusFailed, false},
		{"failure beats skipped", `<skipped/><failure/>`, model.StatusFailed, false},
		{"error beats skipped", `<skipped/><error/>`, model.StatusError, false},
		{"flakyFailure only", `<flakyFailure message="fb"/>`, model.StatusPassed, true},
		{"flakyError only", `<flakyError/>`, model.StatusPassed, true},
		{"rerunFailure only", `<rerunFailure/>`, model.StatusPassed, true},
		{"flaky then failure", `<flakyFailure/><failure message="m"/>`, model.StatusFailed, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rep, _, err := decodeStr(t,
				`<testsuite name="s"><testcase name="c" classname="K" time="0.01">`+tt.inner+`</testcase></testsuite>`)
			if err != nil {
				t.Fatal(err)
			}
			if len(rep.Cases) != 1 {
				t.Fatalf("cases = %d, want 1", len(rep.Cases))
			}
			c := rep.Cases[0]
			if c.Status != tt.status {
				t.Errorf("status = %v, want %v", c.Status, tt.status)
			}
			if c.Flaky != tt.flaky {
				t.Errorf("flaky = %v, want %v", c.Flaky, tt.flaky)
			}
			if c.ID != "K#c" {
				t.Errorf("id = %q", c.ID)
			}
		})
	}
}

func TestDecodeMultipleFailuresFirstPrimary(t *testing.T) {
	rep, _, err := decodeStr(t, `<testsuite name="s">
  <testcase name="c" classname="K" time="0.2">
    <failure message="first" type="T1">trace one</failure>
    <failure message="second" type="T2">trace two</failure>
  </testcase>
</testsuite>`)
	if err != nil {
		t.Fatal(err)
	}
	f := rep.Cases[0].Failure
	if f == nil {
		t.Fatal("no failure")
	}
	if f.Message != "first" || f.Type != "T1" {
		t.Errorf("primary = %q/%q, want first/T1", f.Message, f.Type)
	}
	if len(f.Trace) != 1 || f.Trace[0] != "trace one" {
		t.Errorf("trace = %q", f.Trace)
	}
}

func TestDecodeSelfClosingFailure(t *testing.T) {
	rep, _, err := decodeStr(t, `<testsuite name="s">
  <testcase name="c" classname="K"><failure message="boom" type="X"/></testcase>
</testsuite>`)
	if err != nil {
		t.Fatal(err)
	}
	c := rep.Cases[0]
	if c.Status != model.StatusFailed {
		t.Fatalf("status = %v", c.Status)
	}
	if c.Failure.Message != "boom" || c.Failure.Type != "X" || len(c.Failure.Trace) != 0 {
		t.Errorf("failure = %+v", c.Failure)
	}
}

func TestDecodeMessageAttrPreferredAndTypeDerivation(t *testing.T) {
	rep, _, err := decodeStr(t, `<testsuite name="s">
  <testcase name="c" classname="K">
    <failure>java.net.ConnectException: Connection refused: connect
	at com.example.Redis.connect(Redis.kt:31)
</failure>
  </testcase>
</testsuite>`)
	if err != nil {
		t.Fatal(err)
	}
	f := rep.Cases[0].Failure
	if f.Message != "java.net.ConnectException: Connection refused: connect" {
		t.Errorf("message = %q (want first trace line)", f.Message)
	}
	if f.Type != "java.net.ConnectException" {
		t.Errorf("type = %q (want derived)", f.Type)
	}
}

func TestDecodeNestedSuites(t *testing.T) {
	rep, _, err := decodeStr(t, `<testsuites>
  <testsuite name="outer" timestamp="2026-09-17T11:00:00">
    <testsuite name="inner" timestamp="2026-09-17T11:05:00">
      <testcase name="a" classname="A" time="0.1"/>
    </testsuite>
    <testcase name="b" classname="B" time="0.2"/>
  </testsuite>
</testsuites>`)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Cases) != 2 {
		t.Fatalf("cases = %d, want 2", len(rep.Cases))
	}
	if rep.SuiteName != "outer" {
		t.Errorf("suite name = %q", rep.SuiteName)
	}
	// Newest suite timestamp wins at report level.
	if !rep.HasTimestamp || rep.Timestamp.Format("15:04:05") != "11:05:00" {
		t.Errorf("timestamp = %v (want 11:05:00)", rep.Timestamp)
	}
	for _, c := range rep.Cases {
		if c.SuiteName == "" {
			t.Errorf("case %s: empty SuiteName", c.ID)
		}
	}
}

func TestDecodeOutRefsLazy(t *testing.T) {
	const xmlDoc = `<testsuite name="s" timestamp="2026-09-17T11:00:00">
  <system-out><![CDATA[suite stdout]]></system-out>
  <system-err>suite stderr</system-err>
  <testcase name="a" classname="A">
    <failure message="m"/>
    <system-out>case out</system-out>
  </testcase>
  <testcase name="b" classname="B"/>
</testsuite>`
	rep, _, err := decodeStr(t, xmlDoc)
	if err != nil {
		t.Fatal(err)
	}
	a, b := rep.Cases[0], rep.Cases[1]

	// Offsets must point at raw content spans within the document.
	checkRef := func(name string, ref model.OutRef, wantText string) {
		t.Helper()
		if !ref.Valid {
			t.Fatalf("%s: ref not valid", name)
		}
		if ref.Start <= 0 || ref.End <= ref.Start || ref.End > int64(len(xmlDoc)) {
			t.Fatalf("%s: bad range [%d,%d) doc len %d", name, ref.Start, ref.End, len(xmlDoc))
		}
		span := xmlDoc[ref.Start:ref.End]
		if !strings.Contains(span, wantText) {
			t.Fatalf("%s: span %q does not contain %q", name, span, wantText)
		}
	}
	checkRef("case a out", a.Out, "case out")
	checkRef("suite out (fallback to b)", b.Out, "suite stdout")
	checkRef("suite err (fallback to b)", b.Err, "suite stderr")
	// Case-level system-out wins over the suite-level ref: a and b must not
	// share the same span.
	if a.Out.Start == b.Out.Start {
		t.Errorf("case-level out did not take precedence: both start at %d", a.Out.Start)
	}
	// a has no case-level err → suite-level err is the fallback (spec §17).
	checkRef("suite err (fallback to a)", a.Err, "suite stderr")
}

func TestDecodeOutRefEmptySelfClosing(t *testing.T) {
	rep, _, err := decodeStr(t, `<testsuite name="s">
  <testcase name="a" classname="A"><system-out/></testcase>
</testsuite>`)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Cases[0].Out.Valid {
		t.Error("self-closing system-out must not be valid")
	}
}

func TestDecodeEncodingWarningAndLatin1(t *testing.T) {
	doc := `<?xml version="1.0" encoding="ISO-8859-1"?>
<testsuite name="caf<e9>"><testcase name="t" classname="K"><failure message="caf<e9> boom"/></testcase></testsuite>`
	doc = strings.NewReplacer("<e9>", "\xe9").Replace(doc)
	rep, warns, err := decodeStr(t, doc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rep.Cases[0].Failure.Message, "café") {
		t.Errorf("latin-1 not decoded: %q", rep.Cases[0].Failure.Message)
	}
	// A cleanly converted declared charset has no invalid UTF-8 left in the
	// final stream, so no ENCODING warning fires (fixture-10 behavior —
	// undeclared invalid bytes — is covered by TestDecodeInvalidUTF8...).
	if len(warns) != 0 {
		t.Errorf("warnings = %+v, want none", warns)
	}
}

func TestDecodeInvalidUTF8WarnsButParses(t *testing.T) {
	xmlDoc := `<testsuite name="s"><testcase name="a" classname="K"><failure message="bad <ff> byte"/></testcase></testsuite>`
	xmlDoc = strings.NewReplacer("<ff>", "\xff").Replace(xmlDoc)
	rep, warns, err := decodeStr(t, xmlDoc)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Cases) != 1 {
		t.Fatal("case lost")
	}
	if !strings.Contains(rep.Cases[0].Failure.Message, "\uFFFD") {
		t.Errorf("message = %q, want U+FFFD", rep.Cases[0].Failure.Message)
	}
	if !hasWarning(warns, "ENCODING") {
		t.Errorf("warnings = %+v, want ENCODING", warns)
	}
}

func TestDecodeCommaDecimalTime(t *testing.T) {
	rep, _, err := decodeStr(t, `<testsuite name="s">
  <testcase name="a" classname="K" time="0,42"/>
</testsuite>`)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Cases[0].Time != 0.42 {
		t.Errorf("time = %v", rep.Cases[0].Time)
	}
}
