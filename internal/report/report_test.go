package report

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sampleArtifact() Artifact {
	path := "app/TEST-a.xml"
	return Artifact{
		SchemaVersion: SchemaVersion,
		Tool:          Tool{Name: "junit-results", Version: "test"},
		GeneratedAt:   "2026-09-17T12:00:00Z",
		Command:       "summary",
		Root:          "/repo",
		Run:           Run{Timestamp: "2026-09-17T11:58:00Z", AgeSeconds: 120, Stale: false, Reports: 1, Modules: 1},
		Totals:        Totals{Tests: 1, Failures: 1, TimeSeconds: 0.42},
		Modules:       []Module{{Name: "app", Tests: 1, Failures: 1, TimeSeconds: 0.42}},
		Failures:      []Case{{ID: "A#x", Module: "app", Status: "failed", Trace: []string{"hdr", "at a.b(c.kt:1)"}, Flaky: true, TimeSeconds: 0.42, ReportPath: path, Loc: "c.kt:1"}},
		Groups:        []Group{{Fingerprint: "deadbeef", Count: 1, SampleMessage: "m", Tests: []string{"A#x"}}},
		Stats: Stats{
			Status:   StatusStats{Failed: 1},
			Duration: Duration{SumSeconds: 0.42, MeanSeconds: 0.42, MedianSeconds: 0.42, P90Seconds: 0.42, P95Seconds: 0.42, MaxSeconds: 0.42},
			Types:    []TypeCount{{Type: "T", Count: 1}},
			Slowest:  []Slow{{ID: "A#x", TimeSeconds: 0.42, Status: "failed"}},
			ByGroup:  []ByGroup{{Name: "app", Tests: 1, Failed: 1, TimeSeconds: 0.42}},
		},
		Cases:    casesPtr(),
		Warnings: []Warn{{Code: "PARSE_ERROR", Message: "bad.xml", Path: nil}, {Code: "STALE", Message: "old"}},
	}
}

func casesPtr() *[]Case { c := []Case{}; return &c }

// Field ORDER follows §11.1 and is deterministic (structs, not maps).
func TestMarshalFieldOrder(t *testing.T) {
	data, err := sampleArtifact().Marshal(false)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	keys := []string{
		`"schemaVersion": 1`, `"tool": {`, `"generatedAt"`, `"command"`, `"root"`,
		`"run": {`, `"totals": {`, `"modules": [`, `"failures": [`, `"groups": [`,
		`"stats": {`, `"cases":`, `"warnings": [`,
	}
	pos := -1
	for _, k := range keys {
		i := strings.Index(s, k)
		if i < 0 {
			t.Fatalf("key %s missing in output:\n%s", k, s)
		}
		if i < pos {
			t.Errorf("key %s out of order (at %d after %d)", k, i, pos)
		}
		pos = i
	}
	if !strings.HasSuffix(s, "\n") {
		t.Error("marshaled artifact must end with a newline")
	}
	// §11 warnings: path nullable.
	if !strings.Contains(s, `"path": null`) {
		t.Errorf("warnings path must serialize as null when absent:\n%s", s)
	}
	// Full fidelity: complete trace on failures.
	if !strings.Contains(s, "at a.b(c.kt:1)") {
		t.Error("trace must be carried untrimmed")
	}
	// Gate ruling: no HTML escaping — raw <, >, & per §11.1.
	a := sampleArtifact()
	a.Failures[0].Message = "expected: <3> but was: <4> & more"
	raw, err := a.Marshal(false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"expected: <3> but was: <4> & more"`) {
		t.Errorf("message must marshal with raw <, >, & (no \\u003c escapes):\n%s", raw)
	}
	if strings.Contains(string(raw), `\u003c`) || strings.Contains(string(raw), `\u0026`) {
		t.Error("HTML escapes must be disabled")
	}
}

func TestMarshalCompactVsPretty(t *testing.T) {
	pretty, err := sampleArtifact().Marshal(false)
	if err != nil {
		t.Fatal(err)
	}
	compact, err := sampleArtifact().Marshal(true)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(compact, []byte("\n")) && bytes.Contains(compact, []byte("  \"")) {
		t.Error("compact artifact must not be indented")
	}
	if !bytes.Contains(pretty, []byte("\n  \"tool\"")) {
		t.Error("pretty artifact must use 2-space indent")
	}
	var a, b map[string]any
	if err := json.Unmarshal(pretty, &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(compact, &b); err != nil {
		t.Fatal(err)
	}
	// Same facts in both shapes.
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	if !bytes.Equal(ja, jb) {
		t.Error("compact and pretty artifacts carry different facts")
	}
}

func TestCasesOmittedWhenNil(t *testing.T) {
	a := sampleArtifact()
	a.Cases = nil // --report-no-cases
	data, err := a.Marshal(false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"cases"`) {
		t.Error("nil cases must be omitted entirely")
	}
	// Non-nil empty slice stays present as [].
	a.Cases = casesPtr()
	data, err = a.Marshal(false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"cases": []`) {
		t.Error("empty cases must render as []")
	}
}

func TestWriteAtomicNoLitter(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "report.json")
	data := []byte("{\"ok\":true}\n")
	if err := Write(target, data, false, nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(data) {
		t.Errorf("content mismatch: %q", got)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "report.json" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("temp litter left behind: %v", names)
	}
}

func TestWriteRefusesExistingTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "report.json")
	seed := []byte("ORIGINAL\n")
	if err := os.WriteFile(target, seed, 0o644); err != nil {
		t.Fatal(err)
	}
	err := Write(target, []byte("{\"new\":true}\n"), false, nil)
	var refusal *ErrTargetExists
	if err == nil || !errorsAs(err, &refusal) {
		t.Fatalf("err = %v, want ErrTargetExists", err)
	}
	after, _ := os.ReadFile(target)
	if string(after) != string(seed) {
		t.Errorf("target modified on refusal: %q", after)
	}
	// Force overwrites.
	if err := Write(target, []byte("FORCED\n"), true, nil); err != nil {
		t.Fatal(err)
	}
	after, _ = os.ReadFile(target)
	if string(after) != "FORCED\n" {
		t.Errorf("force did not overwrite: %q", after)
	}
}

func TestWriteStdout(t *testing.T) {
	var buf bytes.Buffer
	if err := Write("-", []byte("hello\n"), false, &buf); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "hello\n" {
		t.Errorf("stdout write = %q", buf.String())
	}
}

// Tiny local errorsAs to keep the import list honest.
func errorsAs(err error, target **ErrTargetExists) bool {
	for err != nil {
		if e, ok := err.(*ErrTargetExists); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
