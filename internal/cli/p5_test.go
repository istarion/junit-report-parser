package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeShowTree builds a repo with one failing case that has captured output
// plus a passing case.
func writeShowTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	xmlDoc := `<?xml version="1.0" encoding="UTF-8"?>
<testsuite name="com.example.ShowTest" tests="2" failures="1" errors="0" skipped="0" timestamp="2026-09-17T11:00:00Z" time="1.2">
  <testcase name="connectRedis" classname="com.example.ShowTest" time="0.7">
    <failure message="Connection refused: connect" type="java.net.ConnectException">java.net.ConnectException: Connection refused: connect
	at com.example.ShowTest.connectRedis(ShowTest.kt:14)
	at java.base/java.lang.reflect.Method.invoke(Method.java:568)
</failure>
    <system-out><![CDATA[line1
line2
line3 timed out]]></system-out>
    <system-err>errline</system-err>
  </testcase>
  <testcase name="ping" classname="com.example.ShowTest" time="0.5"/>
</testsuite>`
	p := filepath.Join(dir, "app/build/test-results/test/TEST-com.example.ShowTest.xml")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(xmlDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestShowMatchesAndOutput(t *testing.T) {
	env := testEnv()
	dir := writeShowTree(t)
	code, out, _ := runRoot(t, env, "show", "connectRedis", "--root", dir)
	if code != ExitTestsFailed {
		t.Fatalf("exit = %d, want 1", code)
	}
	for _, want := range []string{
		"case id=com.example.ShowTest#connectRedis status=failed module=app time=0.7s loc=ShowTest.kt:14\n",
		"  msg Connection refused: connect\n",
		"  at com.example.ShowTest.connectRedis(ShowTest.kt:14)\n",
		"  out line1\n  out line2\n  out line3 timed out\n",
		"  err errline\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("show output missing %q\n--- got ---\n%s", want, out)
		}
	}
	// show must not include the framework frame beyond the trim budget.
	if strings.Contains(out, "Method.invoke") {
		t.Errorf("expected trimming to drop the framework frame:\n%s", out)
	}
}

func TestShowNoMatchesExitsSix(t *testing.T) {
	env := testEnv()
	dir := writeShowTree(t)
	code, _, _ := runRoot(t, env, "show", "no-such-test", "--root", dir)
	if code != ExitGrepNoMatch {
		t.Errorf("exit = %d, want 6 (D5)", code)
	}
}

func TestShowManyMatchesWarns(t *testing.T) {
	env := testEnv()
	dir := t.TempDir()
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?>
<testsuite name="com.example.ManyTest" timestamp="2026-09-17T11:00:00Z" time="0.1">`)
	for i := 0; i < 12; i++ {
		b.WriteString("\n  <testcase name=\"t")
		b.WriteString(string(rune('a' + i)))
		b.WriteString(`" classname="com.example.ManyTest" time="0.01"/>`)
	}
	b.WriteString("\n</testsuite>\n")
	p := filepath.Join(dir, "app/build/test-results/test/TEST-many.xml")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := runRoot(t, env, "show", "ManyTest", "--root", dir)
	if code != ExitOK {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(out, "warn MANY_MATCHES 12 matches\n") {
		t.Errorf("missing MANY_MATCHES warning:\n%s", out)
	}
	if got := strings.Count(out, "case id=com.example.ManyTest#"); got != 12 {
		t.Errorf("shown cases = %d, want 12", got)
	}
}

// --report-output embeds a tail-biased output object on failure entries.
func TestReportOutput(t *testing.T) {
	env := testEnv()
	dir := writeShowTree(t)
	base := filepath.Join(t.TempDir(), "r.json")

	// Without the flag: no output object anywhere.
	if code, _, _ := runRoot(t, env, "failures", "--report", base, "--root", dir); code != ExitTestsFailed {
		t.Fatalf("exit = %d, want 1", code)
	}
	plain, err := os.ReadFile(base)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plain), `"output"`) {
		t.Error("output must be absent without --report-output")
	}

	// With the flag: failure entries carry out/err tails.
	withOut := filepath.Join(t.TempDir(), "ro.json")
	if code, _, _ := runRoot(t, env, "failures", "--report", withOut, "--report-output", "--root", dir); code != ExitTestsFailed {
		t.Fatalf("exit = %d, want 1", code)
	}
	data, err := os.ReadFile(withOut)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Failures []struct {
			ID     string `json:"id"`
			Output *struct {
				Out string `json:"out"`
				Err string `json:"err"`
			} `json:"output"`
		} `json:"failures"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Failures) != 1 || doc.Failures[0].Output == nil {
		t.Fatalf("failure output missing: %s", data)
	}
	o := doc.Failures[0].Output
	if !strings.Contains(o.Out, "line3 timed out") || !strings.Contains(o.Err, "errline") {
		t.Errorf("output tail wrong: %+v", o)
	}
	// Tail bias: --output-lines 2 keeps only the last two out lines.
	tail := filepath.Join(t.TempDir(), "tail.json")
	if code, _, _ := runRoot(t, env, "failures", "--report", tail, "--report-output", "--output-lines", "2", "--root", dir); code != ExitTestsFailed {
		t.Fatalf("exit = %d, want 1", code)
	}
	tdata, _ := os.ReadFile(tail)
	var tdoc struct {
		Failures []struct {
			Output *struct {
				Out string `json:"out"`
			} `json:"output"`
		} `json:"failures"`
	}
	if err := json.Unmarshal(tdata, &tdoc); err != nil {
		t.Fatal(err)
	}
	if tdoc.Failures[0].Output == nil || strings.Contains(tdoc.Failures[0].Output.Out, "line1") {
		t.Errorf("tail bias not applied: %s", tdata)
	}
}

// --fail-on-stale promotes a would-be exit 0 to 1.
func TestFailOnStale(t *testing.T) {
	env := testEnv()
	dir := t.TempDir()
	// 74-day-old passing suite (relative to the harness's fixed clock? this
	// test uses testEnv's clock, so pick a timestamp well past 30m).
	xmlDoc := `<?xml version="1.0"?>
<testsuite name="Old" timestamp="2020-01-01T00:00:00Z" time="0.1">
  <testcase name="a" classname="Old" time="0.1"/>
</testsuite>`
	p := filepath.Join(dir, "app/build/test-results/test/TEST-old.xml")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(xmlDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := runRoot(t, env, "summary", "--root", dir); code != ExitOK {
		t.Fatalf("stale-only corpus without flag: exit = %d, want 0", code)
	}
	code, out, _ := runRoot(t, env, "summary", "--fail-on-stale", "--root", dir)
	if code != ExitTestsFailed {
		t.Fatalf("--fail-on-stale: exit = %d, want 1", code)
	}
	if !strings.Contains(out, "stale=true") {
		t.Errorf("expected stale=true in context:\n%s", out)
	}
}

// --budget small: groups only, zero `at` lines (fixture 22 semantics).
func TestBudgetSmallNoFrames(t *testing.T) {
	env := testEnv()
	dir := writeFailureTree(t)
	code, out, _ := runRoot(t, env, "failures", "--budget", "small", "--root", dir)
	if code != ExitTestsFailed {
		t.Fatalf("exit = %d, want 1", code)
	}
	if strings.Contains(out, "  at ") {
		t.Errorf("--budget small must emit zero at lines:\n%s", out)
	}
	if strings.Contains(out, "fail id=") {
		t.Errorf("--budget small must omit detailed fail records:\n%s", out)
	}
	if !strings.Contains(out, "group count=") {
		t.Errorf("--budget small must keep group records:\n%s", out)
	}
	// large: ungrouped with frames.
	code, out, _ = runRoot(t, env, "failures", "--budget", "large", "--root", dir)
	if code != ExitTestsFailed {
		t.Fatalf("large exit = %d", code)
	}
	if strings.Contains(out, "group count=") {
		t.Errorf("--budget large must be ungrouped:\n%s", out)
	}
	if !strings.Contains(out, "  at ") {
		t.Errorf("--budget large must keep frames:\n%s", out)
	}
}

func TestBudgetInvalid(t *testing.T) {
	env := testEnv()
	dir := writeShowTree(t)
	code, _, errOut := runRoot(t, env, "failures", "--budget", "gigantic", "--root", dir)
	if code != ExitUsage || !strings.Contains(errOut, "invalid --budget") {
		t.Errorf("exit=%d stderr=%q", code, errOut)
	}
}

// --format json|ndjson|md carry the same facts (spec §12).
func TestStdoutFormats(t *testing.T) {
	env := testEnv()
	dir := writeShowTree(t)

	t.Run("json is compact artifact minus tool.version", func(t *testing.T) {
		code, out, _ := runRoot(t, env, "summary", "--format", "json", "--root", dir)
		if code != ExitTestsFailed {
			t.Fatalf("exit = %d", code)
		}
		if strings.Contains(out, "\n  ") {
			t.Error("stdout json must be compact")
		}
		var doc map[string]any
		if err := json.Unmarshal([]byte(out), &doc); err != nil {
			t.Fatalf("not JSON: %v\n%s", err, out)
		}
		if v, ok := doc["schemaVersion"].(float64); !ok || v != 1 {
			t.Errorf("schemaVersion = %v", doc["schemaVersion"])
		}
		tool, _ := doc["tool"].(map[string]any)
		if _, hasVersion := tool["version"]; hasVersion {
			t.Errorf("D12: tool.version must be dropped from stdout json: %v", tool)
		}
		if _, hasName := tool["name"]; !hasName {
			t.Error("tool.name must remain")
		}
	})

	t.Run("ndjson header then records", func(t *testing.T) {
		code, out, _ := runRoot(t, env, "failures", "--format", "ndjson", "--root", dir)
		if code != ExitTestsFailed {
			t.Fatalf("exit = %d", code)
		}
		lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
		if len(lines) < 2 {
			t.Fatalf("expected header + records, got:\n%s", out)
		}
		var header map[string]any
		if err := json.Unmarshal([]byte(lines[0]), &header); err != nil {
			t.Fatalf("header not JSON: %v", err)
		}
		if header["verdict"] != "fail" {
			t.Errorf("header verdict = %v", header["verdict"])
		}
		if _, ok := header["totals"]; !ok {
			t.Error("header must carry totals")
		}
		for i, l := range lines {
			var obj map[string]any
			if err := json.Unmarshal([]byte(l), &obj); err != nil {
				t.Fatalf("line %d not JSON: %v", i, err)
			}
		}
	})

	t.Run("md starts with verdict heading", func(t *testing.T) {
		code, out, _ := runRoot(t, env, "failures", "--format", "md", "--root", dir)
		if code != ExitTestsFailed {
			t.Fatalf("exit = %d", code)
		}
		if !strings.HasPrefix(out, "## Failures\n") {
			t.Errorf("md must start with a heading:\n%s", out)
		}
		if !strings.Contains(out, "| tests | passed | failed | errors | skipped | flaky | time |") {
			t.Errorf("md totals table missing:\n%s", out)
		}
		if !strings.Contains(out, "```") {
			t.Errorf("md traces must be fenced:\n%s", out)
		}
	})
}

// Skipped cases are counted and never appear as failures (fixture 4).
func TestSkippedNotInFailRecords(t *testing.T) {
	env := testEnv()
	dir := t.TempDir()
	xmlDoc := `<?xml version="1.0"?>
<testsuite name="com.example.SkipTest" timestamp="2026-09-17T11:00:00Z" time="0.3">
  <testcase name="runs" classname="com.example.SkipTest" time="0.3"/>
  <testcase name="needsEnv" classname="com.example.SkipTest" time="0"><skipped message="not ready"/></testcase>
  <testcase name="manual" classname="com.example.SkipTest" time="0"><skipped/></testcase>
</testsuite>`
	p := filepath.Join(dir, "app/build/test-results/test/TEST-skip.xml")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(xmlDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := runRoot(t, env, "failures", "--root", dir)
	if code != ExitOK {
		t.Fatalf("exit = %d, want 0", code)
	}
	if strings.Contains(out, "fail id=") || strings.Contains(out, "group count=") {
		t.Errorf("skipped cases must not appear as failures:\n%s", out)
	}
	if !strings.Contains(out, "totals tests=3 passed=1 failed=0 errors=0 skipped=2") {
		t.Errorf("skipped not counted:\n%s", out)
	}
}

// resolveBudget sanity: preset values and invalid names.
func TestResolveBudget(t *testing.T) {
	small, err := resolveBudget("small")
	if err != nil || !small.groupsOnly || small.maxFrames != 0 || small.maxMessage != 200 || small.showOutput != "none" {
		t.Errorf("small = %+v err %v", small, err)
	}
	large, err := resolveBudget("large")
	if err != nil || large.group || large.maxFrames != 20 || large.maxMessage != 2000 || large.showOutput != "on-failure" {
		t.Errorf("large = %+v err %v", large, err)
	}
	if _, err := resolveBudget("nope"); err == nil {
		t.Error("invalid budget must error")
	}
}
