// Package accept implements the black-box acceptance harness (plan §11):
// each fixture is a real CLI invocation against a copied fixture tree with a
// fixed clock, compared against a masked stdout golden and an exit code.
package accept

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/istarion/junit-report-parser/internal/cli"
	"github.com/istarion/junit-report-parser/internal/clock"
)

// FixedNow is the clock used by every fixture: fixture XML timestamps are
// written as RFC3339 (absolute) so behavior is timezone-independent.
var FixedNow = time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)

// fixtureFlag allows phase-gated runs: go test ./internal/accept -fixture F01,F02.
var fixtureFlag = flag.String("fixture", "", "comma-separated fixture ids to run (default: all)")

// maskers hide time- and environment-dependent fields (plan §11: mask
// run=/age=/generatedAt; root= masks the per-run temp directory in both the
// digest and the JSON artifact shapes).
var maskers = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`root=\S+`), "root=ROOT"},
	{regexp.MustCompile(`run=\S+`), "run=TS"},
	{regexp.MustCompile(`age=\S+`), "age=AGE"},
	{regexp.MustCompile(`"root": "[^"]*"`), `"root": "ROOT"`},
	{regexp.MustCompile(`"generatedAt": "[^"]*"`), `"generatedAt": "GENERATED"`},
}

func mask(s string) string {
	for _, m := range maskers {
		s = m.re.ReplaceAllString(s, m.repl)
	}
	return s
}

// RunFixture executes one fixture by id (e.g. "F01") against the caller's t.
func RunFixture(t *testing.T, id string) {
	t.Helper()
	dir := filepath.Join("testdata", id)
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("fixture %s missing: %v", id, err)
	}

	// Copy the fixture tree so mtimes can be set per-run.
	tmp := t.TempDir()
	rootCopy := filepath.Join(tmp, "root")
	if err := copyDir(filepath.Join(dir, "root"), rootCopy); err != nil {
		t.Fatal(err)
	}

	// Set mtimes relative to the fixed clock (plan §11: os.Chtimes).
	age := time.Minute
	if b, err := os.ReadFile(filepath.Join(dir, "mtime_age")); err == nil {
		d, err := time.ParseDuration(strings.TrimSpace(string(b)))
		if err != nil {
			t.Fatalf("fixture %s: bad mtime_age %q: %v", id, b, err)
		}
		age = d
	}
	mt := FixedNow.Add(-age)
	if err := chtimesAll(rootCopy, mt); err != nil {
		t.Fatal(err)
	}

	// generate: fixtures that need large or synthetic trees write them at
	// test start via a checked-in generator (no multi-MB files in git).
	if b, err := os.ReadFile(filepath.Join(dir, "generate")); err == nil {
		if err := generate(strings.TrimSpace(string(b)), rootCopy); err != nil {
			t.Fatalf("fixture %s: generate: %v", id, err)
		}
		if err := chtimesAll(rootCopy, mt); err != nil {
			t.Fatal(err)
		}
	}

	// Command line: first non-empty, non-comment line of `cmd`, plus --root.
	cmdRaw, err := os.ReadFile(filepath.Join(dir, "cmd"))
	if err != nil {
		t.Fatalf("fixture %s: %v", id, err)
	}
	var fields []string
	for _, line := range strings.Split(string(cmdRaw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields = strings.Fields(line)
		break
	}
	if len(fields) == 0 {
		t.Fatalf("fixture %s: empty cmd", id)
	}

	// @REPORT@ placeholder: --report targets a per-run temp path.
	reportPath := filepath.Join(tmp, "report.json")
	for i := range fields {
		fields[i] = strings.ReplaceAll(fields[i], "@REPORT@", reportPath)
	}
	args := append(fields, "--root", rootCopy)

	// report_seed: pre-place bytes at the @REPORT@ target; the fixture then
	// asserts refusal by verifying the bytes survive the run unchanged.
	seed, hasSeed := readOptional(filepath.Join(dir, "report_seed"))
	if hasSeed {
		if err := os.WriteFile(reportPath, seed, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	wantExit := 0
	if b, err := os.ReadFile(filepath.Join(dir, "want_exit")); err == nil {
		if _, err := fmt.Sscanf(strings.TrimSpace(string(b)), "%d", &wantExit); err != nil {
			t.Fatalf("fixture %s: bad want_exit: %v", id, err)
		}
	}

	// run_twice: the fixture runs two identical invocations; stdout must be
	// byte-identical after masking (fixture 20, spec §16 determinism).
	runs := 1
	if _, err := os.Stat(filepath.Join(dir, "run_twice")); err == nil {
		runs = 2
	}

	var first string
	var elapsed time.Duration
	for run := 0; run < runs; run++ {
		env := &cli.Env{
			Version: "test",
			Clock:   clock.Fixed(FixedNow),
			Stdout:  &outBuffer{},
			Stderr:  &errBuffer{},
		}
		root := cli.NewRoot(env)
		root.SetArgs(args)
		stdout := env.Stdout.(*outBuffer)
		stderr := env.Stderr.(*errBuffer)
		start := time.Now()
		runErr := root.Execute()
		if run == 0 {
			elapsed = time.Since(start)
		}
		gotExit := cli.ExitCode(runErr)

		if gotExit != wantExit {
			t.Fatalf("exit = %d, want %d\nstdout:\n%s\nstderr:\n%s",
				gotExit, wantExit, stdout.String(), stderr.String())
		}
		// Fixture 13: piped stdout must never carry ANSI escapes (the
		// harness always pipes, so this guards every command).
		if strings.ContainsRune(stdout.String(), '\x1b') {
			t.Errorf("ANSI escape emitted on piped stdout:\n%q", stdout.String())
		}
		masked := mask(stdout.String())
		if run == 0 {
			first = masked
		} else if masked != first {
			t.Errorf("run %d stdout differs (fixture 20 determinism)\n--- run 1 ---\n%s\n--- run %d ---\n%s",
				run+1, first, run+1, masked)
		}
	}

	// max_seconds: loose wall-time guard (fixtures 8/18 budgets).
	if b, err := os.ReadFile(filepath.Join(dir, "max_seconds")); err == nil {
		var max float64
		if _, err := fmt.Sscanf(strings.TrimSpace(string(b)), "%f", &max); err != nil {
			t.Fatalf("fixture %s: bad max_seconds: %v", id, err)
		}
		if elapsed.Seconds() > max {
			t.Errorf("wall time %.2fs exceeds %.2fs budget", elapsed.Seconds(), max)
		}
		t.Logf("wall time: %.3fs (budget %.1fs)", elapsed.Seconds(), max)
	}

	// Stdout golden (masked).
	want, err := os.ReadFile(filepath.Join(dir, "want_stdout.golden"))
	if err != nil {
		t.Fatalf("fixture %s: %v", id, err)
	}
	gotMasked := mask(first)
	wantMasked := mask(string(want))
	if gotMasked != wantMasked {
		t.Errorf("stdout mismatch\n--- got ---\n%s\n--- want ---\n%s", gotMasked, wantMasked)
	}

	// Report artifact assertions.
	if hasSeed {
		after, err := os.ReadFile(reportPath)
		if err != nil {
			t.Fatalf("fixture %s: seeded report vanished: %v", id, err)
		}
		if string(after) != string(seed) {
			t.Errorf("fixture %s: seeded report was modified (want byte-identical)", id)
		}
	}
	goldenReport, hasReport := readOptional(filepath.Join(dir, "want_report.json"))
	if !hasSeed {
		if data, err := os.ReadFile(reportPath); err == nil {
			// A report was written: structural invariants always apply; the
			// golden (when present) is compared byte-exactly after masking.
			checkReport(t, id, data, goldenReport, hasReport, fields)
		} else if hasReport {
			t.Fatalf("fixture %s: report artifact missing: %v", id, err)
		}
	}
}

// checkReport validates the written artifact: well-formed JSON, schema
// version, cases presence, output-presence matching --report-output, and
// optionally byte-exact equality against the (masked) golden.
func checkReport(t *testing.T, id string, data, golden []byte, hasGolden bool, args []string) {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("fixture %s: report is not valid JSON: %v", id, err)
	}
	if v, ok := doc["schemaVersion"].(float64); !ok || v != 1 {
		t.Errorf("fixture %s: schemaVersion = %v, want 1", id, doc["schemaVersion"])
	}
	noCases, reportOutput := false, false
	for _, a := range args {
		if a == "--report-no-cases" {
			noCases = true
		}
		if a == "--report-output" {
			reportOutput = true
		}
	}
	_, hasCases := doc["cases"]
	if !noCases && !hasCases {
		t.Errorf("fixture %s: cases array missing from report", id)
	}
	if noCases && hasCases {
		t.Errorf("fixture %s: cases must be omitted with --report-no-cases", id)
	}
	if hasOutput := strings.Contains(string(data), `"output"`); hasOutput && !reportOutput {
		t.Errorf("fixture %s: captured output present without --report-output", id)
	}
	if hasGolden && mask(string(data)) != mask(string(golden)) {
		t.Errorf("fixture %s: report mismatch\n--- got ---\n%s\n--- want ---\n%s",
			id, data, golden)
	}
}

func readOptional(path string) ([]byte, bool) {
	b, err := os.ReadFile(path)
	return b, err == nil
}

type outBuffer struct{ b strings.Builder }

func (o *outBuffer) Write(p []byte) (int, error) { return o.b.Write(p) }
func (o *outBuffer) String() string              { return o.b.String() }

type errBuffer struct{ b strings.Builder }

func (e *errBuffer) Write(p []byte) (int, error) { return e.b.Write(p) }
func (e *errBuffer) String() string              { return e.b.String() }

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

func chtimesAll(dir string, t time.Time) error {
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		return os.Chtimes(path, t, t)
	})
}

// Selected returns the fixture ids chosen by -fixture (or the provided full
// list when the flag is empty).
func Selected(all []string) []string {
	if *fixtureFlag == "" {
		return all
	}
	var out []string
	for _, want := range strings.Split(*fixtureFlag, ",") {
		want = strings.TrimSpace(want)
		for _, id := range all {
			if id == want {
				out = append(out, id)
			}
		}
	}
	return out
}
