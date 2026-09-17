package discover

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/istarion/junit-report-parser/internal/clock"
	"github.com/istarion/junit-report-parser/internal/model"
)

var fixedNow = time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)

func write(t *testing.T, path, content string, mtime time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

func suiteXML(name, ts string, cases string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<testsuite name="` + name + `" timestamp="` + ts + `" tests="9" failures="0">
` + cases + `</testsuite>
`
}

const passCase = `  <testcase name="ok" classname="C" time="0.1"/>
`

// opts builds default Options rooted at dir with a fixed clock.
func opts(dir string) Options {
	return Options{
		Root:   dir,
		MaxAge: 30 * time.Minute,
		Clock:  clock.Fixed(fixedNow),
	}
}

func reportPaths(rs []*model.Report) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Path)
	}
	return out
}

func moduleNames(rs []*model.Report) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Module)
	}
	return out
}

func TestDiscoverFindsGradleAndMavenReports(t *testing.T) {
	dir := t.TempDir()
	old := fixedNow.Add(-time.Minute)
	fresh := localNaive(fixedNow.Add(-time.Minute))
	write(t, filepath.Join(dir, "app/build/test-results/test/TEST-A.xml"),
		suiteXML("A", fresh, passCase), old)
	write(t, filepath.Join(dir, "svc/target/surefire-reports/TEST-B.xml"),
		suiteXML("B", localNaive(fixedNow.Add(-30*time.Second)), passCase), old)
	write(t, filepath.Join(dir, "it/target/failsafe-reports/TEST-C.xml"),
		suiteXML("C", localNaive(fixedNow.Add(-15*time.Second)), passCase), old)
	write(t, filepath.Join(dir, "app/src/main/java/com/example/Thing.java"),
		"class Thing {}\n", old)
	// Noise that must be ignored.
	write(t, filepath.Join(dir, "docs/readme.xml"), `<html/>`, old)
	write(t, filepath.Join(dir, "app/build/tmp/notes.xml"), `<junk/>`, old)
	write(t, filepath.Join(dir, ".git/hidden.xml"), suiteXML("H", fresh, passCase), old)

	res, err := Discover(opts(dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Reports) != 3 {
		t.Fatalf("reports = %d (%v), want 3", len(res.Reports), reportPaths(res.Reports))
	}
	// Path-sorted merge order (plan D10).
	if got := moduleNames(res.Reports); got[0] != "app" || got[1] != "it" || got[2] != "svc" {
		t.Errorf("modules = %v", got)
	}
	sources := map[string]string{}
	for _, r := range res.Reports {
		sources[r.Module] = r.Source
	}
	if sources["app"] != "gradle" || sources["svc"] != "surefire" || sources["it"] != "failsafe" {
		t.Errorf("sources = %v", sources)
	}
	if !res.RunHasTimestamp {
		t.Fatal("run time must come from suite timestamps")
	}
	// Newest suite timestamp (C) is the run time; age ≈ 15s, fresh.
	if want := 15 * time.Second; res.Age < want-2*time.Second || res.Age > want+2*time.Second {
		t.Errorf("age = %v, want ~%v", res.Age, want)
	}
	if res.Stale {
		t.Error("fresh reports must not be stale")
	}
	if !res.Files.Has("Thing.java") {
		t.Errorf("file index missing Thing.java (len=%d)", res.Files.Len())
	}
}

// localNaive renders an instant as a naive timestamp in the local zone, the
// format Gradle/Maven write and decode parses with ParseInLocation(local).
func localNaive(t time.Time) string {
	return t.In(time.Local).Format("2006-01-02T15:04:05")
}

func TestDiscoverSkipsBinaryDir(t *testing.T) {
	dir := t.TempDir()
	old := fixedNow.Add(-time.Minute)
	write(t, filepath.Join(dir, "app/build/test-results/test/TEST-A.xml"),
		suiteXML("A", "2026-09-17T11:59:00", passCase), old)
	write(t, filepath.Join(dir, "app/build/test-results/test/binary/output.old"),
		"not xml", old)
	write(t, filepath.Join(dir, "app/build/test-results/binary/results.xml"),
		suiteXML("BIN", "2026-09-17T11:59:00", passCase), old)

	res, err := Discover(opts(dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Reports) != 1 {
		t.Fatalf("reports = %d, want 1 (binary/ skipped)", len(res.Reports))
	}
}

func TestDiscoverModuleFallbacks(t *testing.T) {
	dir := t.TempDir()
	old := fixedNow.Add(-time.Minute)
	write(t, filepath.Join(dir, "build/test-results/test/TEST-R.xml"),
		suiteXML("R", "2026-09-17T11:59:00", passCase), old)

	res, err := Discover(opts(dir))
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Reports[0].Module; got != filepath.Base(dir) {
		t.Errorf("module = %q, want repo root base %q", got, filepath.Base(dir))
	}
}

func TestDiscoverStalenessAndNewerThan(t *testing.T) {
	dir := t.TempDir()
	staleTs := localNaive(fixedNow.Add(-74 * 24 * time.Hour))
	write(t, filepath.Join(dir, "app/build/test-results/test/TEST-OLD.xml"),
		suiteXML("OLD", staleTs, passCase), fixedNow.Add(-74*24*time.Hour))

	t.Run("stale flag and warning", func(t *testing.T) {
		res, err := Discover(opts(dir))
		if err != nil {
			t.Fatal(err)
		}
		if !res.Stale {
			t.Error("want stale")
		}
		var staleWarn bool
		for _, w := range res.Warnings {
			if w.Code == "STALE" && w.Message == "newest report is 74d old" {
				staleWarn = true
			}
		}
		if !staleWarn {
			t.Errorf("warnings = %+v", res.Warnings)
		}
	})

	t.Run("newer-than filters all → exit 2 semantics", func(t *testing.T) {
		o := opts(dir)
		o.HasNewerThan = true
		o.NewerThan = fixedNow.Add(-10 * time.Minute)
		_, err := Discover(o)
		if err == nil || err != ErrNoReports {
			t.Errorf("err = %v, want ErrNoReports", err)
		}
	})

	t.Run("newer-than rfc3339 keeps fresh", func(t *testing.T) {
		dir2 := t.TempDir()
		write(t, filepath.Join(dir2, "app/build/test-results/test/TEST-NEW.xml"),
			suiteXML("NEW", localNaive(fixedNow.Add(-30*time.Minute)), passCase), fixedNow.Add(-time.Minute))
		o := opts(dir2)
		o.HasNewerThan = true
		o.NewerThan = fixedNow.Add(-2 * time.Hour)
		res, err := Discover(o)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Reports) != 1 {
			t.Fatalf("reports = %d, want 1", len(res.Reports))
		}
	})
}

// Mtime fallback when the XML has no timestamp attribute.
func TestDiscoverMtimeFallback(t *testing.T) {
	dir := t.TempDir()
	old := fixedNow.Add(-2 * time.Hour)
	write(t, filepath.Join(dir, "app/build/test-results/test/TEST-A.xml"),
		`<testsuite name="A"><testcase name="ok" classname="C"/></testsuite>`, old)

	res, err := Discover(opts(dir))
	if err != nil {
		t.Fatal(err)
	}
	if res.RunHasTimestamp {
		t.Error("must fall back to mtime")
	}
	if res.Age != time.Hour && res.Age != time.Hour-time.Second {
		// Chtimes granularity: exactly 2h minus age rounding.
		if res.Age < time.Hour || res.Age > 2*time.Hour {
			t.Errorf("age = %v, want ~1h..2h from 2h-old mtime vs 30m max-age", res.Age)
		}
	}
	if !res.Stale {
		t.Error("2h-old mtime must be stale with 30m max-age")
	}
}

func TestDiscoverPathBypass(t *testing.T) {
	dir := t.TempDir()
	old := fixedNow.Add(-time.Minute)
	// A report OUTSIDE any REPORT_DIR: only --path can reach it.
	write(t, filepath.Join(dir, "custom/TEST-X.xml"),
		suiteXML("X", "2026-09-17T11:00:00", passCase), old)

	o := opts(dir)
	o.Paths = []string{filepath.Join(dir, "custom", "TEST-*.xml")}
	res, err := Discover(o)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Reports) != 1 || res.Reports[0].SuiteName != "X" {
		t.Fatalf("reports = %+v", res.Reports)
	}
}

func TestDiscoverNoneParseableVsNoReports(t *testing.T) {
	dir := t.TempDir()
	old := fixedNow.Add(-time.Minute)
	write(t, filepath.Join(dir, "app/build/test-results/test/TEST-BAD.xml"),
		`<testsuite><testcase></testsuite>`, old)

	t.Run("all unparseable → exit 4 semantics", func(t *testing.T) {
		_, err := Discover(opts(dir))
		if err == nil || err != ErrNoneParseable {
			t.Errorf("err = %v, want ErrNoneParseable", err)
		}
	})

	t.Run("empty tree → exit 2 semantics", func(t *testing.T) {
		_, err := Discover(opts(t.TempDir()))
		if err == nil || err != ErrNoReports {
			t.Errorf("err = %v, want ErrNoReports", err)
		}
	})
}

func TestDiscoverNotXMLVerboseOnly(t *testing.T) {
	dir := t.TempDir()
	old := fixedNow.Add(-time.Minute)
	write(t, filepath.Join(dir, "app/build/test-results/test/TEST-A.xml"),
		suiteXML("A", "2026-09-17T11:59:00", passCase), old)
	write(t, filepath.Join(dir, "app/build/test-results/test/NOT-REPORT.xml"),
		`<coverage/>`, old)

	o := opts(dir)
	res, err := Discover(o)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range res.Warnings {
		if w.Code == "NOT_XML" {
			t.Fatal("NOT_XML must be suppressed without --verbose")
		}
	}
	o.Verbose = true
	res, err = Discover(o)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, w := range res.Warnings {
		if w.Code == "NOT_XML" {
			found = true
		}
	}
	if !found {
		t.Errorf("warnings = %+v, want NOT_XML under --verbose", res.Warnings)
	}
}

func TestDiscoverParallelWorkersDeterministic(t *testing.T) {
	dir := t.TempDir()
	old := fixedNow.Add(-time.Minute)
	for _, m := range []string{"a", "b", "c", "d", "e"} {
		write(t, filepath.Join(dir, m, "build/test-results/test/TEST-"+m+".xml"),
			suiteXML(m, "2026-09-17T11:59:00", passCase), old)
	}
	first, err := Discover(opts(dir))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		o := opts(dir)
		o.Workers = 4
		res, err := Discover(o)
		if err != nil {
			t.Fatal(err)
		}
		for j := range res.Reports {
			if res.Reports[j].Path != first.Reports[j].Path {
				t.Fatalf("nondeterministic order at %d: %s != %s",
					j, res.Reports[j].Path, first.Reports[j].Path)
			}
		}
	}
}

func TestModuleAndSourceTable(t *testing.T) {
	repo := "/repo"
	tests := []struct {
		path       string
		wantModule string
		wantSource string
	}{
		{"/repo/app/build/test-results/test/TEST-a.xml", "app", "gradle"},
		{"/repo/app/build/test-results/unit/TEST-a.xml", "app", "gradle"},
		{"/repo/app/sub/build/test-results/test/TEST-a.xml", "app/sub", "gradle"},
		{"/repo/build/test-results/test/TEST-a.xml", "", "gradle"},
		{"/repo/svc/target/surefire-reports/TEST-a.xml", "svc", "surefire"},
		{"/repo/it/target/failsafe-reports/TEST-a.xml", "it", "failsafe"},
		{"/repo/custom/x.xml", "custom", ""},
	}
	for _, tt := range tests {
		module, source := moduleAndSource(repo, tt.path)
		if module != tt.wantModule || source != tt.wantSource {
			t.Errorf("moduleAndSource(%q) = %q,%q; want %q,%q",
				tt.path, module, source, tt.wantModule, tt.wantSource)
		}
	}
}
