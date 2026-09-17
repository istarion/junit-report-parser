//go:build perf

// Perf smoke test (plan §11): generate a large corpus and run summary
// end-to-end. Soft assertions only — CI machines vary; the numbers are
// logged against the spec §18 budgets (1000 reports / 50k cases < 300 ms,
// < 50 MB RSS).
package accept

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/istarion/junit-report-parser/internal/cli"
	"github.com/istarion/junit-report-parser/internal/clock"
)

func TestPerf(t *testing.T) {
	const (
		reports   = 1000
		casesEach = 50
	)
	root := t.TempDir()
	genStart := time.Now()
	if err := genPerfTree(root, reports, casesEach); err != nil {
		t.Fatal(err)
	}
	t.Logf("generated %d reports / %d cases in %v", reports, reports*casesEach, time.Since(genStart))

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	env := &cli.Env{
		Version: "perf",
		Clock:   clock.Real(),
		Stdout:  io.Discard,
		Stderr:  io.Discard,
	}
	rootCmd := cli.NewRoot(env)
	rootCmd.SetArgs([]string{"summary", "--root", root})
	start := time.Now()
	err := rootCmd.Execute()
	elapsed := time.Since(start)

	runtime.ReadMemStats(&after)
	rssSys := after.Sys / (1 << 20)
	heapLive := after.HeapAlloc / (1 << 20)
	// Also measure the truly-live heap after a collection: HeapAlloc without
	// GC includes not-yet-collected garbage from the run.
	runtime.GC()
	var settled runtime.MemStats
	runtime.ReadMemStats(&settled)
	heapSettled := settled.HeapAlloc / (1 << 20)
	t.Logf("summary: %v wall, live heap %d MB (settled %d MB), Sys %d MB (spec §18: <300ms, <50MB)",
		elapsed, heapLive, heapSettled, rssSys)
	// The generated corpus intentionally contains failures, so exit 1 is the
	// expected outcome (0 would also be fine for a clean corpus).
	if code := cli.ExitCode(err); code != 1 && code != 0 {
		t.Fatalf("summary exit = %d (err %v)", code, err)
	}
	// Soft assertions: generous multiples to avoid CI flake while still
	// catching an order-of-magnitude regression.
	if elapsed > 5*time.Second {
		t.Errorf("summary wall time %v exceeds the loose 5s guard", elapsed)
	}
	if rssSys > 400*(1<<20) {
		t.Errorf("Sys memory %d MB exceeds the loose 400 MB guard", rssSys)
	}
}

// genPerfTree writes reports×casesEach cases across per-module Gradle-style
// report files, with a mix of statuses and one multi-frame failure per
// report.
func genPerfTree(root string, reports, casesEach int) error {
	for r := 0; r < reports; r++ {
		module := fmt.Sprintf("mod%03d", r%10)
		dir := filepath.Join(root, module, "build", "test-results", "test")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		var b bytes.Buffer
		b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
		fmt.Fprintf(&b, "<testsuite name=\"com.example.T%04d\" tests=\"%d\" failures=\"1\" errors=\"0\" skipped=\"1\" timestamp=\"2026-09-17T11:58:00Z\" time=\"1.0\">\n", r, casesEach)
		for c := 0; c < casesEach; c++ {
			switch {
			case c == 0:
				fmt.Fprintf(&b, "  <testcase name=\"t%04d\" classname=\"com.example.T%04d\" time=\"0.01\">\n", c, r)
				fmt.Fprintf(&b, "    <failure message=\"perf failure %d\" type=\"org.opentest4j.AssertionFailedError\">boom\n", r)
				fmt.Fprintf(&b, "\tat com.example.T%04d.m(T%04d.kt:%d)\n", r, r, 10+c)
				b.WriteString("\tat java.base/java.lang.reflect.Method.invoke(Method.java:568)\n")
				b.WriteString("</failure>\n  </testcase>\n")
			case c == 1:
				fmt.Fprintf(&b, "  <testcase name=\"t%04d\" classname=\"com.example.T%04d\" time=\"0.01\"><skipped message=\"flaky env\"/></testcase>\n", c, r)
			default:
				fmt.Fprintf(&b, "  <testcase name=\"t%04d\" classname=\"com.example.T%04d\" time=\"0.01\"/>\n", c, r)
			}
		}
		b.WriteString("</testsuite>\n")
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("TEST-com.example.T%04d.xml", r)), b.Bytes(), 0o644); err != nil {
			return err
		}
	}
	return nil
}
