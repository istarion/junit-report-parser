package accept

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/istarion/junit-report-parser/internal/cli"
	"github.com/istarion/junit-report-parser/internal/clock"
)

// TestBigOutputLazy (fixture 8): a ~5 MB system-out must stay out of default
// output and the artifact unless --report-output is given, and the default
// run must complete well inside the documented budget (spec §18: 1000
// reports/50k cases < 300 ms is the real target; this single-file guard is
// deliberately loose).
func TestBigOutputLazy(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping big-output test in -short mode")
	}
	root := t.TempDir()
	if err := generateBigOut(root); err != nil {
		t.Fatal(err)
	}
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	dir := t.TempDir()
	report := filepath.Join(dir, "r.json")

	env := &cli.Env{Version: "test", Clock: clock.Fixed(FixedNow), Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	rootCmd := cli.NewRoot(env)
	rootCmd.SetArgs([]string{"summary", "--root", root, "--report", report})
	start := time.Now()
	err := rootCmd.Execute()
	elapsed := time.Since(start)

	if code := cli.ExitCode(err); code != 0 {
		t.Fatalf("exit = %d (err %v)", code, err)
	}
	if elapsed > 5*time.Second {
		t.Errorf("default run took %v, want < 5s", elapsed)
	}
	out := env.Stdout.(*bytes.Buffer).String() //nolint:errcheck
	if strings.Contains(out, markerBigOut) {
		t.Fatal("captured output leaked into default stdout")
	}
	art, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(art), markerBigOut) || strings.Contains(string(art), `"output"`) {
		t.Fatal("captured output present in artifact without --report-output")
	}
	t.Logf("big-output default run: %v (RSS %d KB)", elapsed, mem.Sys/1024)
}
