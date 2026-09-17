package render

import (
	"bytes"
	"testing"
	"time"

	"github.com/istarion/junit-report-parser/internal/model"
)

// EncodeValue goldens lock the §10.2 record-line value encoding.
func TestEncodeValue(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"plain", "plain"},
		{"app-1.v2", "app-1.v2"},
		{"", `""`},
		{"two words", `"two words"`},
		{`say "hi"`, `"say \"hi\""`},
		{`back\slash`, `"back\\slash"`},
		{"tab\tsep", "\"tab\tsep\""}, // no \t escape in grammar: verbatim inside quotes
		{"new\nline", "\"new\nline\""},
		{"ünïcode", "ünïcode"},
	}
	for _, tt := range tests {
		if got := EncodeValue(tt.in); got != tt.want {
			t.Errorf("EncodeValue(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestAbbreviateHome(t *testing.T) {
	home := "/home/testuser"
	t.Setenv("HOME", home)
	tests := []struct {
		in, want string
	}{
		{home + "/IdeaProjects/msg-stat", "~/IdeaProjects/msg-stat"},
		{home, "~"},
		{home + "x/y", home + "x/y"}, // prefix but not path boundary
		{"/tmp/elsewhere", "/tmp/elsewhere"},
	}
	for _, tt := range tests {
		if got := AbbreviateHome(tt.in); got != tt.want {
			t.Errorf("AbbreviateHome(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func mkFail(id string, trace []string) FailRecord {
	return FailRecord{
		Case: &model.Case{
			ID: id, ClassName: "C", Name: id, Module: "app", Time: 0.11,
			Status:  model.StatusFailed,
			Failure: &model.Failure{Kind: model.StatusFailed, Type: "T", Message: "boom\nsecond line", Trace: trace},
		},
		Loc: "FooTest.kt:42",
	}
}

func summaryIn() SummaryInput {
	runTs := time.Date(2026, 9, 17, 9, 12, 0, 0, time.UTC)
	return SummaryInput{
		Root:    "/home/testuser/proj",
		RunTime: runTs,
		Age:     4 * time.Minute,
		Reports: 2,
		Modules: 1,
		Totals:  model.Totals{Tests: 3, Passed: 2, Failed: 1, Time: 0.13},
		Warnings: []model.Warning{
			{Code: "PARSE_ERROR", Path: "bad.xml", Message: "bad.xml"},
		},
		Fails: []FailRecord{
			mkFail("computesTotal", []string{
				"org.opentest4j.AssertionFailedError: expected: <3> but was: <4>",
				"at com.example.FooTest.computesTotal(FooTest.kt:42)",
			}),
		},
	}
}

func TestSummaryTextFailGolden(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")
	var buf bytes.Buffer
	SummaryText(&buf, summaryIn())
	got := buf.String()
	expect := []string{
		"verdict=fail\n",
		"root=~/proj run=2026-09-17T09:12 age=4m reports=2 modules=1 stale=false\n",
		"totals tests=3 passed=2 failed=1 errors=0 skipped=0 flaky=0 time=0.13s\n",
		"warn PARSE_ERROR bad.xml\n",
		"fail id=computesTotal status=failed module=app loc=FooTest.kt:42 type=T time=0.11s\n",
		"  msg boom\n",
		"hint junit-results failures\n",
	}
	for _, e := range expect {
		if !bytes.Contains(buf.Bytes(), []byte(e)) {
			t.Errorf("output missing %q\n--- got ---\n%s", e, got)
		}
	}
	if bytes.Contains(buf.Bytes(), []byte("second line")) {
		t.Error("summary msg must carry only the first line of the message")
	}
}

func TestSummaryTextPassNoHint(t *testing.T) {
	var buf bytes.Buffer
	in := SummaryInput{
		Root:    "/p",
		RunTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Age:     74 * 24 * time.Hour,
		Stale:   true,
		Totals:  model.Totals{Tests: 1, Passed: 1, Time: 0.05},
		Warnings: []model.Warning{
			{Code: "STALE", Message: "newest report is 74d old"},
		},
	}
	SummaryText(&buf, in)
	got := buf.String()
	want := "verdict=pass\n" +
		"root=/p run=2026-01-01T00:00 age=74d reports=0 modules=0 stale=true\n" +
		"totals tests=1 passed=1 failed=0 errors=0 skipped=0 flaky=0 time=50ms\n" +
		"warn STALE newest report is 74d old\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	if bytes.Contains(buf.Bytes(), []byte("hint ")) {
		t.Error("pass verdict must not emit hint")
	}
}

func TestSummaryTextColorPlumbing(t *testing.T) {
	var buf bytes.Buffer
	in := summaryIn()
	in.UseColor = false
	SummaryText(&buf, in)
	if bytes.Contains(buf.Bytes(), []byte("\x1b[")) {
		t.Error("UseColor=false must emit no ANSI")
	}

	buf.Reset()
	in.UseColor = true
	SummaryText(&buf, in)
	if !bytes.Contains(buf.Bytes(), []byte("\x1b[31mverdict=fail\x1b[0m")) {
		t.Error("UseColor=true should color the verdict line")
	}
}
