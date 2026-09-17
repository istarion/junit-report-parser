package agg

import (
	"testing"
	"time"

	"github.com/istarion/junit-report-parser/internal/model"
)

func mkReportFull(path string, aggregated bool, ts time.Time, hasTS bool, mtime time.Time, cases ...*model.Case) *model.Report {
	for _, c := range cases {
		c.Module = path
	}
	return &model.Report{
		Path: path, Aggregated: aggregated,
		Timestamp: ts, HasTimestamp: hasTS, Mtime: mtime,
		Cases: cases,
	}
}

func TestDedupeNonAggregatedWins(t *testing.T) {
	mt := time.Date(2026, 9, 17, 11, 0, 0, 0, time.UTC)
	fresh := mt.Add(time.Minute)
	perClass := mkReportFull("TEST-A.xml", false, mt, true, mt, mkCase("A#x", "app", model.StatusPassed, 0.1))
	aggregated := mkReportFull("TEST-all.xml", true, fresh, true, fresh, mkCase("A#x", "app", model.StatusPassed, 9.9))

	collapsed := Dedupe([]*model.Report{perClass, aggregated})
	if collapsed != 1 {
		t.Fatalf("collapsed = %d, want 1", collapsed)
	}
	if len(perClass.Cases) != 1 || len(aggregated.Cases) != 0 {
		t.Errorf("per-class=%d aggregated=%d, want 1/0", len(perClass.Cases), len(aggregated.Cases))
	}
	if perClass.Cases[0].Time != 0.1 {
		t.Errorf("winner should be the per-class case, got time=%v", perClass.Cases[0].Time)
	}
}

// Plan §6 chain: later suite timestamp beats mtime beats path.
func TestDedupePreferenceChain(t *testing.T) {
	mt := time.Date(2026, 9, 17, 11, 0, 0, 0, time.UTC)
	laterTs := mt.Add(time.Hour)
	older := mkReportFull("b.xml", true, mt, true, laterTs, mkCase("A#x", "app", model.StatusPassed, 1))
	newerTS := mkReportFull("a.xml", true, laterTs, true, mt, mkCase("A#x", "app", model.StatusPassed, 2))

	if c := Dedupe([]*model.Report{older, newerTS}); c != 1 {
		t.Fatalf("collapsed = %d, want 1", c)
	}
	if len(older.Cases) != 0 || len(newerTS.Cases) != 1 {
		t.Fatalf("cases = %d/%d, want 0/1", len(older.Cases), len(newerTS.Cases))
	}
	if newerTS.Cases[0].Time != 2 {
		t.Errorf("later suite timestamp must win (time=%v)", newerTS.Cases[0].Time)
	}

	// No timestamps → later mtime wins.
	m1 := mkReportFull("p/one.xml", true, time.Time{}, false, mt, mkCase("A#y", "app", model.StatusPassed, 3))
	m2 := mkReportFull("p/two.xml", true, time.Time{}, false, mt.Add(time.Hour), mkCase("A#y", "app", model.StatusPassed, 4))
	if c := Dedupe([]*model.Report{m1, m2}); c != 1 {
		t.Fatalf("collapsed = %d, want 1", c)
	}
	if m2.Cases[0].Time != 4 {
		t.Errorf("later mtime must win (time=%v)", m2.Cases[0].Time)
	}

	// Identical ranks → lexicographically greater path wins.
	e1 := mkReportFull("same/a.xml", true, time.Time{}, false, mt, mkCase("A#z", "app", model.StatusPassed, 5))
	e2 := mkReportFull("same/b.xml", true, time.Time{}, false, mt, mkCase("A#z", "app", model.StatusPassed, 6))
	if c := Dedupe([]*model.Report{e1, e2}); c != 1 {
		t.Fatalf("collapsed = %d, want 1", c)
	}
	if e2.Cases[0].Time != 6 {
		t.Errorf("greater path must win (time=%v)", e2.Cases[0].Time)
	}
}

func TestDedupeDistinctCasesKept(t *testing.T) {
	mt := time.Date(2026, 9, 17, 11, 0, 0, 0, time.UTC)
	a := mkReportFull("TEST-A.xml", false, mt, true, mt,
		mkCase("A#x", "app", model.StatusPassed, 0.1),
		mkCase("A#y", "app", model.StatusPassed, 0.2))
	b := mkReportFull("TEST-B.xml", false, mt, true, mt,
		mkCase("B#z", "app", model.StatusPassed, 0.3))
	if c := Dedupe([]*model.Report{a, b}); c != 0 {
		t.Fatalf("collapsed = %d, want 0", c)
	}
	if len(a.Cases) != 2 || len(b.Cases) != 1 {
		t.Errorf("distinct cases must survive: %d/%d", len(a.Cases), len(b.Cases))
	}
}

// Fixture 5: aggregated <testsuites> + per-class files must not double-count.
func TestDedupeTotalsNotDoubleCounted(t *testing.T) {
	mt := time.Date(2026, 9, 17, 11, 0, 0, 0, time.UTC)
	fresh := mt.Add(time.Minute)
	perA := mkReportFull("TEST-A.xml", false, mt, true, mt, mkCase("A#x", "app", model.StatusPassed, 0.1))
	perB := mkReportFull("TEST-B.xml", false, mt, true, mt, mkCase("B#y", "app", model.StatusPassed, 0.2))
	aggregated := mkReportFull("TEST-all.xml", true, fresh, true, fresh,
		mkCase("A#x", "app", model.StatusPassed, 0.1),
		mkCase("B#y", "app", model.StatusPassed, 0.2),
		mkCase("C#only", "app", model.StatusPassed, 0.3), // only in aggregated → survives
	)

	collapsed := Dedupe([]*model.Report{perA, perB, aggregated})
	if collapsed != 2 {
		t.Fatalf("collapsed = %d, want 2", collapsed)
	}
	var all []*model.Case
	for _, r := range []*model.Report{perA, perB, aggregated} {
		all = append(all, r.Cases...)
	}
	totals := Totals(all)
	if totals.Tests != 3 {
		t.Errorf("totals.Tests = %d, want 3 (no double counting)", totals.Tests)
	}
}

func TestGroupFailures(t *testing.T) {
	cases := []*model.Case{
		mkCase("A#one", "app", model.StatusFailed, 0),
		mkCase("A#two", "app", model.StatusError, 0),
		mkCase("A#three", "app", model.StatusFailed, 0),
	}
	fp := func(c *model.Case) string {
		if c.Name == "one" || c.Name == "two" {
			return "aaaa1111"
		}
		return "bbbb2222"
	}
	groups := GroupFailures(cases, fp)
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(groups))
	}
	// count desc: aaaa1111 (2) before bbbb2222 (1)
	if groups[0].Fingerprint != "aaaa1111" || groups[0].Count != 2 {
		t.Errorf("group[0] = %+v", groups[0])
	}
	if groups[0].IDs[0] != "A#one" || groups[0].IDs[1] != "A#two" {
		t.Errorf("ids = %v", groups[0].IDs)
	}
	if groups[0].Sample.Name != "one" {
		t.Errorf("sample = %v", groups[0].Sample.Name)
	}
	if groups[1].Fingerprint != "bbbb2222" || groups[1].Count != 1 {
		t.Errorf("group[1] = %+v", groups[1])
	}

	// Tie on count → fingerprint asc.
	tie := GroupFailures(cases, func(*model.Case) string { return "zzzz" })
	_ = tie
	same := []*model.Case{mkCase("A#a", "app", model.StatusFailed, 0), mkCase("A#b", "app", model.StatusFailed, 0)}
	groupsTie := GroupFailures(same, func(c *model.Case) string {
		if c.Name == "a" {
			return "ffff0002"
		}
		return "ffff0001"
	})
	if groupsTie[0].Fingerprint != "ffff0001" {
		t.Errorf("tie order = %s first, want ffff0001", groupsTie[0].Fingerprint)
	}

	if GroupFailures(nil, fp) != nil {
		t.Error("empty input should give nil groups")
	}
}
