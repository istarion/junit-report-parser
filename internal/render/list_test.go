package render

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/istarion/junit-report-parser/internal/model"
)

func discoverBase() SummaryInput {
	return SummaryInput{
		Root:    "/p",
		RunTime: time.Date(2026, 9, 17, 11, 58, 0, 0, time.UTC),
		Age:     2 * time.Minute,
		Reports: 2,
		Modules: 2,
		Totals:  model.Totals{Tests: 3, Passed: 2, Failed: 1, Time: 0.6},
	}
}

// Discover records: §10.3 fields + source label, path asc, naive-local mtime.
func TestDiscoverText(t *testing.T) {
	in := DiscoverInput{
		Base: discoverBase(),
		Records: []DiscoverRecord{
			{
				Path:   "app/build/test-results/test/TEST-A.xml",
				Module: "app",
				Mtime:  time.Date(2026, 9, 17, 11, 5, 0, 0, time.UTC),
				Suite:  "com.example.A",
				Cases:  2,
				Fail:   true,
				Source: "gradle",
			},
			{
				Path:   "svc/target/surefire-reports/TEST-B.xml",
				Module: "svc",
				Mtime:  time.Date(2026, 9, 17, 11, 6, 30, 0, time.UTC),
				Suite:  "com.example.B",
				Cases:  1,
				Source: "surefire",
			},
		},
	}
	var buf bytes.Buffer
	DiscoverText(&buf, in)
	want := `verdict=fail
root=/p run=2026-09-17T11:58 age=2m reports=2 modules=2 stale=false
totals tests=3 passed=2 failed=1 errors=0 skipped=0 flaky=0 time=0.6s
report path=app/build/test-results/test/TEST-A.xml module=app mtime=2026-09-17T11:05 suite=com.example.A cases=2 status=fail source=gradle
report path=svc/target/surefire-reports/TEST-B.xml module=svc mtime=2026-09-17T11:06 suite=com.example.B cases=1 status=pass source=surefire
`
	if buf.String() != want {
		t.Errorf("discover digest:\n%s\nwant:\n%s", buf.String(), want)
	}
}

// List records: case id status module time loc.
func TestListText(t *testing.T) {
	in := ListInput{
		Base: SummaryInput{
			Root:    "/p",
			RunTime: time.Date(2026, 9, 17, 11, 58, 0, 0, time.UTC),
			Age:     2 * time.Minute,
			Reports: 1,
			Modules: 1,
			Totals:  model.Totals{Tests: 2, Passed: 1, Failed: 1, Time: 0.52},
		},
		Records: []ListRecord{
			{Case: &model.Case{ID: "A#a", ClassName: "A", Name: "a", Module: "app",
				Status: model.StatusPassed, Time: 0.1}, Loc: "-"},
			{Case: &model.Case{ID: "A#b", ClassName: "A", Name: "b", Module: "app",
				Status: model.StatusFailed, Time: 0.42,
				Failure: &model.Failure{Trace: []string{"at A.b(B.kt:5)"}},
			}, Loc: "B.kt:5"},
		},
	}
	var buf bytes.Buffer
	ListText(&buf, in)
	want := `verdict=fail
root=/p run=2026-09-17T11:58 age=2m reports=1 modules=1 stale=false
totals tests=2 passed=1 failed=1 errors=0 skipped=0 flaky=0 time=0.52s
case id=A#a status=passed module=app time=0.1s loc=-
case id=A#b status=failed module=app time=0.42s loc=B.kt:5
hint junit-results failures
`
	if buf.String() != want {
		t.Errorf("list digest:\n%s\nwant:\n%s", buf.String(), want)
	}
}

func TestListTextQuotedLoc(t *testing.T) {
	in := ListInput{
		Base: SummaryInput{
			RunTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			Totals:  model.Totals{Tests: 1, Passed: 1},
		},
		Records: []ListRecord{
			{Case: &model.Case{ID: "weird id", Module: "m", Status: model.StatusPassed}, Loc: "at frame with spaces"},
		},
	}
	var buf bytes.Buffer
	ListText(&buf, in)
	if !strings.Contains(buf.String(), `id="weird id"`) || !strings.Contains(buf.String(), `loc="at frame with spaces"`) {
		t.Errorf("value encoding broken:\n%s", buf.String())
	}
}
