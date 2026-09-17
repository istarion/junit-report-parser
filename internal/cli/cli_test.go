package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/istarion/junit-report-parser/internal/agg"
	"github.com/istarion/junit-report-parser/internal/clock"
	"github.com/istarion/junit-report-parser/internal/discover"
	"github.com/istarion/junit-report-parser/internal/model"
	"github.com/istarion/junit-report-parser/internal/render"
)

func runRoot(t *testing.T, env *Env, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	e := &Env{Version: env.Version, Clock: env.Clock, Stdout: &out, Stderr: &errb}
	root := NewRoot(e)
	root.SetArgs(args)
	err := root.Execute()
	code := ExitCode(err)
	if err == nil {
		code = ExitOK
	} else {
		reportError(e, root, err)
	}
	return code, out.String(), errb.String()
}

func testEnv() *Env {
	return &Env{
		Version: "0.1.0",
		Clock:   clock.Fixed(time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)),
	}
}

func TestExitCodeMapping(t *testing.T) {
	tests := []struct {
		err  error
		want int
	}{
		{nil, ExitOK},
		{&TestsFailedError{}, 1},
		{&NoReportsError{Msg: "x"}, 2},
		{&UsageError{Msg: "x"}, 3},
		{&NotYetError{Cmd: "grep"}, 3},
		{&NoneParseableError{Msg: "x"}, 4},
		{&ReportWriteError{Msg: "x"}, 5},
		{&GrepNoMatchError{}, 6},
	}
	for _, tt := range tests {
		if got := ExitCode(tt.err); got != tt.want {
			t.Errorf("ExitCode(%T) = %d, want %d", tt.err, got, tt.want)
		}
	}
}

func TestVersionAndHelpExitZero(t *testing.T) {
	env := testEnv()
	code, out, _ := runRoot(t, env, "--version")
	if code != ExitOK || !strings.Contains(out, "0.1.0") {
		t.Errorf("--version: code=%d out=%q", code, out)
	}
	code, out, _ = runRoot(t, env, "--help")
	if code != ExitOK || !strings.Contains(out, "Usage:") {
		t.Errorf("--help: code=%d out=%q", code, out)
	}
}

// P5: all commands are real; `show` requires exactly one argument.
func TestShowRequiresArg(t *testing.T) {
	env := testEnv()
	code, out, errOut := runRoot(t, env, "show")
	if code != ExitUsage {
		t.Errorf("show (no arg): exit = %d, want 3", code)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	if !strings.Contains(errOut, "exactly one id-or-substring") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestUnknownFlagAndCommandAreUsageErrors(t *testing.T) {
	env := testEnv()
	code, _, errOut := runRoot(t, env, "--bogus")
	if code != ExitUsage || !strings.Contains(errOut, "unknown flag") {
		t.Errorf("--bogus: code=%d stderr=%q", code, errOut)
	}
	code, _, errOut = runRoot(t, env, "bogus-cmd")
	if code != ExitUsage {
		t.Errorf("bogus-cmd: code=%d stderr=%q", code, errOut)
	}
}

func TestFormatValidation(t *testing.T) {
	// All these fail before discovery (empty root also errors, but usage
	// errors must win — they are checked first).
	tests := []struct {
		args     []string
		wantCode int
		wantMsg  string
	}{
		{[]string{"--format", "yaml"}, ExitUsage, `invalid --format "yaml"`},
		{[]string{"--format", "yaml"}, ExitUsage, `invalid --format "yaml"`},
		{[]string{"--color", "loud"}, ExitUsage, `invalid --color "loud"`},
		{[]string{"--newer-than", "not-a-time"}, ExitUsage, "bad --newer-than value"},
		{[]string{"--status", "bogus", "summary"}, ExitUsage, "invalid --status"},
	}
	for _, tt := range tests {
		env := testEnv()
		code, _, errOut := runRoot(t, env, tt.args...)
		if code != tt.wantCode || !strings.Contains(errOut, tt.wantMsg) {
			t.Errorf("%v: code=%d stderr=%q, want code=%d msg=%q",
				tt.args, code, errOut, tt.wantCode, tt.wantMsg)
		}
	}
}

func TestNewerThanParsing(t *testing.T) {
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	t.Run("duration ago", func(t *testing.T) {
		got, err := parseNewerThan("10m", now)
		if err != nil || !got.Equal(now.Add(-10*time.Minute)) {
			t.Errorf("got %v err %v", got, err)
		}
	})
	t.Run("rfc3339", func(t *testing.T) {
		got, err := parseNewerThan("2026-09-17T10:00:00Z", now)
		if err != nil || !got.Equal(time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)) {
			t.Errorf("got %v err %v", got, err)
		}
	})
	t.Run("file path", func(t *testing.T) {
		path := t.TempDir() + "/marker.txt"
		mtime := time.Date(2026, 9, 16, 8, 0, 0, 0, time.UTC)
		if err := osWriteFile(path, []byte("x"), mtime); err != nil {
			t.Fatal(err)
		}
		got, err := parseNewerThan(path, now)
		if err != nil || !got.Equal(mtime) {
			t.Errorf("got %v err %v", got, err)
		}
	})
	t.Run("garbage", func(t *testing.T) {
		if _, err := parseNewerThan("gibberish", now); err == nil {
			t.Error("want error")
		}
	})
}

func TestIsTerminalFalseForBuffer(t *testing.T) {
	if isTerminal(&bytes.Buffer{}) {
		t.Error("buffer must not be a terminal")
	}
}

// Default command: bare invocation re-dispatches to summary (plan §10).
func TestDefaultCommandRedispatch(t *testing.T) {
	dir := t.TempDir()
	report := `<?xml version="1.0"?>
<testsuite name="K" timestamp="2026-09-17T11:00:00Z" time="0.2"><testcase name="a" classname="K" time="0.2"/></testsuite>`
	p := dir + "/app/build/test-results/test/TEST-K.xml"
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(report), 0o644); err != nil {
		t.Fatal(err)
	}
	env := testEnv()
	code, out, _ := runRoot(t, env, "--root", dir)
	if code != ExitOK || !strings.HasPrefix(out, "verdict=pass\n") {
		t.Errorf("redispatch: code=%d out=%q", code, out)
	}
	// Explicit subcommand behaves the same.
	code, out, _ = runRoot(t, env, "summary", "--root", dir)
	if code != ExitOK || !strings.HasPrefix(out, "verdict=pass\n") {
		t.Errorf("explicit summary: code=%d out=%q", code, out)
	}
}

// Empty discovery root → exit 2 through the redispatch path.
func TestRedispatchNoReports(t *testing.T) {
	env := testEnv()
	code, _, errOut := runRoot(t, env, "--root", t.TempDir())
	if code != ExitNoReports {
		t.Errorf("code=%d, want 2", code)
	}
	if !strings.Contains(errOut, "no report files found") {
		t.Errorf("stderr=%q", errOut)
	}
}

// writeFailureTree builds a repo with two same-fingerprint connection errors
// and one assertion failure.
func writeFailureTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	xmlDoc := `<?xml version="1.0"?>
<testsuite name="com.example.CartTest" tests="3" failures="3" timestamp="2026-09-17T11:00:00Z" time="1">
  <testcase name="testCheckout" classname="com.example.CartTest" time="0.42">
    <error message="Connection refused: connect" type="java.net.ConnectException">java.net.ConnectException: Connection refused: connect
	at com.example.cart.RedisClient.connect(RedisClient.kt:31)
</error>
  </testcase>
  <testcase name="testPayment" classname="com.example.CartTest" time="0.2">
    <error message="Connection refused: connect" type="java.net.ConnectException">java.net.ConnectException: Connection refused: connect
	at com.example.cart.RedisClient.connect(RedisClient.kt:31)
</error>
  </testcase>
  <testcase name="computesTotal" classname="com.example.CartTest" time="0.38">
    <failure message="expected: &lt;3&gt; but was: &lt;4&gt;" type="org.opentest4j.AssertionFailedError">org.opentest4j.AssertionFailedError: expected: &lt;3&gt; but was: &lt;4&gt;
	at com.example.CartTest.computesTotal(CartTest.kt:42)
</failure>
  </testcase>
</testsuite>`
	p := dir + "/app/build/test-results/test/TEST-com.example.CartTest.xml"
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(xmlDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestFailuresEndToEnd(t *testing.T) {
	env := testEnv()
	dir := writeFailureTree(t)
	code, out, _ := runRoot(t, env, "failures", "--root", dir)
	if code != ExitTestsFailed {
		t.Fatalf("exit = %d, want 1", code)
	}
	for _, want := range []string{
		"verdict=fail\n",
		"totals tests=3 passed=0 failed=1 errors=2 skipped=0 flaky=0 time=1.0s\n",
		// groups first (D20), count desc
		"group count=2 fingerprint=",
		"  msg Connection refused: connect\n",
		"  tests com.example.CartTest#testCheckout,com.example.CartTest#testPayment\n",
		// then detail records with trimmed frames
		"fail id=com.example.CartTest#testCheckout status=error module=app loc=RedisClient.kt:31 type=java.net.ConnectException time=0.42s\n",
		"  at com.example.cart.RedisClient.connect(RedisClient.kt:31)\n",
		"fail id=com.example.CartTest#computesTotal status=failed module=app loc=CartTest.kt:42 type=org.opentest4j.AssertionFailedError time=0.38s\n",
		"  msg expected: <3> but was: <4>\n",
		// D19 hint (first fail record in canonical order)
		"hint junit-results show com.example.CartTest#computesTotal\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("failures output missing %q\n--- got ---\n%s", want, out)
		}
	}
	// Groups must precede fail records.
	if strings.Index(out, "group count=2") > strings.Index(out, "fail id=") {
		t.Error("group records must come before fail records")
	}
}

func TestFailuresNoGroupAndMaxFrames(t *testing.T) {
	env := testEnv()
	dir := writeFailureTree(t)
	code, out, _ := runRoot(t, env, "failures", "--no-group", "--max-frames", "1", "--root", dir)
	if code != ExitTestsFailed {
		t.Fatalf("exit = %d, want 1", code)
	}
	if strings.Contains(out, "group count=") {
		t.Error("--no-group must drop group records")
	}
	if strings.Contains(out, "  trunc ") {
		t.Errorf("--max-frames 1 with 1-frame traces must not emit trunc; got:\n%s", out)
	}
	// Trim happens: the connection traces have exactly 1 frame each.
	if strings.Count(out, "  at com.example.cart.RedisClient.connect(RedisClient.kt:31)\n") != 2 {
		t.Errorf("expected both connection frames kept:\n%s", out)
	}
}

// --report artifact wiring: pretty default, refusal, compact, no-cases, "-".
func TestReportFlagWiring(t *testing.T) {
	dir := writeFailureTree(t)
	report := filepath.Join(t.TempDir(), "r.json")

	env := testEnv()
	code, _, _ := runRoot(t, env, "summary", "--report", report, "--root", dir)
	if code != ExitTestsFailed {
		t.Fatalf("exit = %d, want 1", code)
	}
	data, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "\n  \"tool\"") {
		t.Error("default report must be pretty-printed with 2-space indent")
	}

	// Existing target without --report-force → exit 5.
	code, _, errOut := runRoot(t, env, "summary", "--report", report, "--root", dir)
	if code != ExitReportWrite {
		t.Fatalf("refusal exit = %d, want 5", code)
	}
	if !strings.Contains(errOut, "report target exists") {
		t.Errorf("stderr = %q", errOut)
	}

	// --report-compact: minified, still valid JSON.
	compact := filepath.Join(t.TempDir(), "c.json")
	code, _, _ = runRoot(t, env, "summary", "--report", compact, "--report-compact", "--root", dir)
	if code != ExitTestsFailed {
		t.Fatalf("exit = %d, want 1", code)
	}
	cd, err := os.ReadFile(compact)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(cd), "\n  ") {
		t.Error("compact report must not be indented")
	}

	// --report-no-cases: cases key omitted (needs --report-force: file exists
	// only when re-running the same path, so use a fresh path here).
	noc := filepath.Join(t.TempDir(), "nc.json")
	code, _, _ = runRoot(t, env, "summary", "--report", noc, "--report-no-cases", "--root", dir)
	if code != ExitTestsFailed {
		t.Fatalf("exit = %d, want 1", code)
	}
	nc, err := os.ReadFile(noc)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(nc), "\"cases\"") {
		t.Error("--report-no-cases must omit cases")
	}

	// "-" → pretty JSON on stdout.
	code, out, _ := runRoot(t, env, "summary", "--report", "-", "--root", dir)
	if code != ExitTestsFailed {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(out, "\"schemaVersion\": 1") {
		t.Errorf("--report - must print the artifact to stdout; got:\n%s", out)
	}
}

// statRecords: section order, --by breakdowns, --only gating, D21 rows.
func TestStatRecords(t *testing.T) {
	cases := []*model.Case{
		{ID: "A#p1", ClassName: "com.example.A", Name: "p1", Module: "app", Status: model.StatusPassed, Time: 0.1},
		{ID: "A#p2", ClassName: "com.example.A", Name: "p2", Module: "app", Status: model.StatusPassed, Time: 0.2},
		{ID: "A#f1", ClassName: "com.example.A", Name: "f1", Module: "app", Status: model.StatusFailed, Time: 0.3,
			Failure: &model.Failure{Type: "com.example.BoomOne"}},
		{ID: "B#e1", ClassName: "com.example.B", Name: "e1", Module: "lib", Status: model.StatusError, Time: 6.0,
			Failure: &model.Failure{Type: "com.example.BoomTwo"}},
		{ID: "B#s1", ClassName: "com.example.B", Name: "s1", Module: "lib", Status: model.StatusSkipped, SkipMessage: "not ready"},
		{ID: "B#fl1", ClassName: "com.example.B", Name: "fl1", Module: "lib", Status: model.StatusPassed, Time: 0.05, Flaky: true},
	}
	p := &prepared{cases: cases, fails: agg.Failed(cases), totals: agg.Totals(cases), modules: 2}
	cc := newCaseCtx(&discover.Result{})
	f := &flags{statsTop: 5, statsBy: "module"}

	recs := statRecords(p, cc, statsSections, f)
	var kinds []string
	for _, r := range recs {
		kinds = append(kinds, r.Kind)
	}
	wantKinds := []string{"status", "status", "status", "status", "duration", "slowest", "slowest", "slowest", "slowest", "slowest", "type", "type", "module", "module", "flaky", "skipped"}
	if strings.Join(kinds, ",") != strings.Join(wantKinds, ",") {
		t.Fatalf("kinds = %v\nwant %v", kinds, wantKinds)
	}

	// Spot-check status shares and duration values (times: 0,.05,.1,.2,.3,6).
	dur := recs[4]
	if got := renderFields(dur); got["sum"] != "6.65s" || got["mean"] != "1.11s" || got["median"] != "0.1s" || got["p90"] != "6.0s" || got["p95"] != "6.0s" || got["max"] != "6.0s" {
		t.Errorf("duration = %v", got)
	}
	st0 := renderFields(recs[0])
	if st0["status"] != "passed" || st0["count"] != "3" || st0["share"] != "50.0%" {
		t.Errorf("status passed = %v", st0)
	}
	st1 := renderFields(recs[1])
	if st1["share"] != "16.7%" {
		t.Errorf("status failed share = %v", st1)
	}
	// type tie (count 1 each) → type asc
	if renderFields(recs[10])["type"] != "com.example.BoomOne" || renderFields(recs[11])["type"] != "com.example.BoomTwo" {
		t.Errorf("type order wrong: %v %v", renderFields(recs[10]), renderFields(recs[11]))
	}
	// module rows name asc (D21)
	if renderFields(recs[12])["name"] != "app" || renderFields(recs[13])["name"] != "lib" {
		t.Errorf("module rows = %v %v", renderFields(recs[12]), renderFields(recs[13]))
	}
	// skipped reason
	if renderFields(recs[len(recs)-1])["reason"] != "not ready" {
		t.Errorf("skipped = %v", renderFields(recs[len(recs)-1]))
	}

	// --by suite: names are classnames, name asc.
	f2 := &flags{statsTop: 5, statsBy: "suite"}
	var suiteNames []string
	for _, r := range statRecords(p, cc, []string{"module"}, f2) {
		suiteNames = append(suiteNames, renderFields(r)["name"])
	}
	if strings.Join(suiteNames, ",") != "com.example.A,com.example.B" {
		t.Errorf("suite breakdown = %v", suiteNames)
	}

	// --by package: everything before the last dot.
	f3 := &flags{statsTop: 5, statsBy: "package"}
	var pkg []string
	for _, r := range statRecords(p, cc, []string{"module"}, f3) {
		pkg = append(pkg, renderFields(r)["name"])
	}
	if strings.Join(pkg, ",") != "com.example" {
		t.Errorf("package breakdown = %v", pkg)
	}

	// --only gating with fixed output order.
	recs2 := statRecords(p, cc, []string{"flaky", "status"}, f) // --only order must not matter
	if len(recs2) != 5 || recs2[0].Kind != "status" || recs2[4].Kind != "flaky" {
		t.Errorf("--only gating: %v", kindsOf(recs2))
	}

	// --top applies to slowest and type.
	f4 := &flags{statsTop: 1, statsBy: "module"}
	recs3 := statRecords(p, cc, []string{"slowest", "type"}, f4)
	if len(recs3) != 2 || renderFields(recs3[0])["rank"] != "1" || renderFields(recs3[0])["id"] != "B#e1" {
		t.Errorf("--top 1 slowest: %+v", recs3)
	}
}

func renderFields(r render.StatRecord) map[string]string {
	m := map[string]string{}
	for _, f := range r.Fields {
		m[f.Key] = f.Value
	}
	return m
}

func kindsOf(rs []render.StatRecord) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Kind)
	}
	return out
}
