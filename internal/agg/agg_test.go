package agg

import (
	"testing"
	"time"

	"github.com/istarion/junit-report-parser/internal/model"
)

func mkCase(id, module string, st model.Status, t float64) *model.Case {
	cls, name := id, id
	if i := indexByte(id, '#'); i >= 0 {
		cls, name = id[:i], id[i+1:]
	}
	return &model.Case{ID: id, ClassName: cls, Name: name, Module: module, Status: st, Time: t}
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func mkReport(module string, cases ...*model.Case) *model.Report {
	for _, c := range cases {
		c.Module = module
	}
	return &model.Report{Module: module, Path: module + "/x.xml", Cases: cases}
}

func TestTotals(t *testing.T) {
	cases := []*model.Case{
		mkCase("A#p1", "app", model.StatusPassed, 0.1),
		mkCase("A#p2", "app", model.StatusPassed, 0.2),
		mkCase("A#f1", "app", model.StatusFailed, 0.3),
		mkCase("A#e1", "app", model.StatusError, 0.4),
		mkCase("A#s1", "app", model.StatusSkipped, 0),
		mkCase("A#p3", "app", model.StatusPassed, 0.5),
	}
	cases[5].Flaky = true
	got := Totals(cases)
	want := model.Totals{Tests: 6, Passed: 3, Failed: 1, Errors: 1, Skipped: 1, Flaky: 1, Time: 1.5}
	if got != want {
		t.Errorf("Totals = %+v, want %+v", got, want)
	}
	if !Totals(nil).Verdict() {
		t.Error("empty totals should pass")
	}
}

// D11: selection filters the universe BEFORE totals; verdict reflects the
// filtered set.
func TestSelectBeforeTotals(t *testing.T) {
	reports := []*model.Report{
		mkReport("app",
			mkCase("A#ok", "app", model.StatusPassed, 0.1),
			mkCase("A#bad", "app", model.StatusFailed, 0.2),
		),
		mkReport("lib",
			mkCase("B#bad", "lib", model.StatusFailed, 0.3),
			mkCase("B#ok", "lib", model.StatusPassed, 0.4),
		),
	}
	tests := []struct {
		name       string
		sel        Selection
		wantTests  int
		wantFailed int
		wantPass   bool
	}{
		{"no filter", Selection{}, 4, 2, false},
		{"module app", Selection{Modules: []string{"app"}}, 2, 1, false},
		{"module exact", Selection{Modules: []string{"lib"}}, 2, 1, false},
		{"module substring", Selection{Modules: []string{"ib"}}, 2, 1, false},
		{"status passed", Selection{Statuses: []model.Status{model.StatusPassed}}, 2, 0, true},
		{"filter id", Selection{Include: []string{"A#"}}, 2, 1, false},
		{"exclude bad", Selection{Exclude: []string{"bad"}}, 2, 0, true},
		{"module+exclude", Selection{Modules: []string{"app"}, Exclude: []string{"bad"}}, 1, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Totals(Select(reports, tt.sel))
			if got.Tests != tt.wantTests || got.Failed != tt.wantFailed || got.Verdict() != tt.wantPass {
				t.Errorf("totals = %+v, want tests=%d failed=%d pass=%v",
					got, tt.wantTests, tt.wantFailed, tt.wantPass)
			}
		})
	}
}

func TestParseStatuses(t *testing.T) {
	tests := []struct {
		in     string
		want   int
		hasErr bool
	}{
		{"", 0, false},
		{"passed", 1, false},
		{"passed,failed", 2, false},
		{" passed , error ", 2, false},
		{"bogus", 0, true},
		{"passed,bogus", 0, true},
	}
	for _, tt := range tests {
		got, err := ParseStatuses(tt.in)
		if (err != nil) != tt.hasErr {
			t.Errorf("ParseStatuses(%q) err = %v, wantErr %v", tt.in, err, tt.hasErr)
			continue
		}
		if err == nil && len(got) != tt.want {
			t.Errorf("ParseStatuses(%q) = %v, want %d statuses", tt.in, got, tt.want)
		}
	}
}

func TestFailedSorted(t *testing.T) {
	cases := []*model.Case{
		mkCase("Z#zz", "app", model.StatusFailed, 0),
		mkCase("A#aa", "lib", model.StatusError, 0),
		mkCase("A#mm", "app", model.StatusFailed, 0),
		mkCase("A#aa", "app", model.StatusPassed, 0),
		mkCase("B#aa", "app", model.StatusSkipped, 0),
		mkCase("A#aa", "app", model.StatusError, 0),
	}
	got := Failed(cases)
	if len(got) != 4 {
		t.Fatalf("Failed len = %d, want 4", len(got))
	}
	type key struct{ m, c, n string }
	var gotKeys []key
	for _, c := range got {
		gotKeys = append(gotKeys, key{c.Module, c.ClassName, c.Name})
	}
	wantKeys := []key{
		{"app", "A", "aa"},
		{"app", "A", "mm"},
		{"app", "Z", "zz"},
		{"lib", "A", "aa"},
	}
	for i := range wantKeys {
		if gotKeys[i] != wantKeys[i] {
			t.Fatalf("order = %v, want %v", gotKeys, wantKeys)
		}
	}
}

// Guard: module is stamped from the report during Select.
func TestSelectStampsModule(t *testing.T) {
	c := mkCase("A#x", "", model.StatusPassed, 0)
	c.Module = ""
	rep := &model.Report{Module: "app", Cases: []*model.Case{c}, Timestamp: time.Time{}}
	got := Select([]*model.Report{rep}, Selection{})
	if len(got) != 1 || got[0].Module != "app" {
		t.Errorf("module not stamped: %+v", got)
	}
}
