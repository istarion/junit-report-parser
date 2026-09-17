package render

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/istarion/junit-report-parser/internal/model"
)

func TestNDJSONSummary(t *testing.T) {
	var buf bytes.Buffer
	in := summaryIn()
	in.Groups = []GroupRecord{{Count: 2, Fingerprint: "deadbeef", Sample: "boom", IDs: []string{"C#x", "C#y"}}}
	NDJSONSummary(&buf, in)
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) < 3 {
		t.Fatalf("expected header + warn + group + fail, got:\n%s", buf.String())
	}
	var header map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &header); err != nil {
		t.Fatalf("header: %v", err)
	}
	if header["verdict"] != "fail" {
		t.Errorf("verdict = %v", header["verdict"])
	}
	for _, key := range []string{"root", "run", "totals"} {
		if _, ok := header[key]; !ok {
			t.Errorf("header missing %q", key)
		}
	}
	seen := map[string]bool{}
	for _, l := range lines[1:] {
		var obj map[string]any
		if err := json.Unmarshal([]byte(l), &obj); err != nil {
			t.Fatalf("record not JSON: %v\n%s", err, l)
		}
		if typ, _ := obj["type"].(string); typ != "" {
			seen[typ] = true
		}
	}
	for _, typ := range []string{"warn", "group", "fail", "hint"} {
		if !seen[typ] {
			t.Errorf("missing record type %q (seen %v)", typ, seen)
		}
	}
}

func TestMDSummary(t *testing.T) {
	var buf bytes.Buffer
	in := summaryIn()
	in.Groups = []GroupRecord{{Count: 2, Fingerprint: "deadbeef", Sample: "boom", IDs: []string{"C#x", "C#y"}}}
	MDSummary(&buf, in)
	out := buf.String()
	for _, want := range []string{
		"## Verdict",
		"**fail**",
		"| tests | passed | failed | errors | skipped | flaky | time |",
		"### Warnings",
		"### Groups",
		"| count | fingerprint | message |",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("md missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b[") {
		t.Error("md must never contain ANSI")
	}
}

func TestMDGrepTable(t *testing.T) {
	var buf bytes.Buffer
	MDGrep(&buf, GrepInput{
		Base: SummaryInput{Totals: model.Totals{Tests: 1, Failed: 1}},
		Records: []GrepRecord{{
			Case:  mkFail("A#x", nil).Case,
			Field: "trace",
			Line:  2,
			Lines: []string{"at a.b(C.kt:1)"},
		}},
	})
	out := buf.String()
	if !strings.HasPrefix(out, "## Matches\n") || !strings.Contains(out, "| id | module | status | field | line | text |") {
		t.Errorf("grep md shape wrong:\n%s", out)
	}
	if !strings.Contains(out, "at a.b(C.kt:1)") {
		t.Errorf("match text missing:\n%s", out)
	}
}
