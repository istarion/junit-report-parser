// Package agg implements case selection and totals aggregation (plan §6).
// Selection flags filter the case universe BEFORE totals; the verdict
// reflects the filtered set (plan D11). Totals are always recomputed from
// cases, never from XML attributes (spec §17). Dedup arrives in P2.
package agg

import (
	"fmt"
	"sort"
	"strings"

	"github.com/istarion/junit-report-parser/internal/model"
)

// Selection carries the shared selection flags (spec §8.3). Empty fields
// select everything.
type Selection struct {
	Modules  []string       // --module (repeatable): exact or substring, OR-combined
	Statuses []model.Status // --status; nil = all
	Include  []string       // --filter substrings over id (OR-combined)
	Exclude  []string       // --exclude substrings over id (OR-combined)
}

// ParseStatuses parses a --status value like "passed,failed,error,skipped".
func ParseStatuses(s string) ([]model.Status, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	var out []model.Status
	for _, part := range strings.Split(s, ",") {
		st, ok := model.ParseStatus(strings.TrimSpace(part))
		if !ok {
			return nil, fmt.Errorf("invalid --status value %q (want passed|failed|error|skipped)", part)
		}
		out = append(out, st)
	}
	return out, nil
}

// Select flattens the selected cases of all reports in report order, applying
// the selection filters (plan D11: applied before totals/verdict).
func Select(reports []*model.Report, sel Selection) []*model.Case {
	var out []*model.Case
	for _, r := range reports {
		for _, c := range r.Cases {
			c.Module = r.Module
			if sel.matches(c) {
				out = append(out, c)
			}
		}
	}
	return out
}

func (s Selection) matches(c *model.Case) bool {
	if len(s.Modules) > 0 && !s.matchModule(c.Module) {
		return false
	}
	if len(s.Statuses) > 0 && !containsStatus(s.Statuses, c.Status) {
		return false
	}
	if len(s.Include) > 0 && !anySubstring(s.Include, c.ID) {
		return false
	}
	if len(s.Exclude) > 0 && anySubstring(s.Exclude, c.ID) {
		return false
	}
	return true
}

func anySubstring(list []string, s string) bool {
	for _, sub := range list {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func (s Selection) matchModule(m string) bool {
	for _, want := range s.Modules {
		if m == want || strings.Contains(m, want) {
			return true
		}
	}
	return false
}

func containsStatus(list []model.Status, st model.Status) bool {
	for _, s := range list {
		if s == st {
			return true
		}
	}
	return false
}

// Totals recomputes totals from the selected cases (spec §17).
func Totals(cases []*model.Case) model.Totals {
	var t model.Totals
	t.Tests = len(cases)
	for _, c := range cases {
		switch c.Status {
		case model.StatusPassed:
			t.Passed++
		case model.StatusFailed:
			t.Failed++
		case model.StatusError:
			t.Errors++
		case model.StatusSkipped:
			t.Skipped++
		}
		if c.Flaky {
			t.Flaky++
		}
		t.Time += c.Time
	}
	return t
}

// Failed returns the failed/error cases sorted canonically:
// (module, classname, name) (plan D10).
func Failed(cases []*model.Case) []*model.Case {
	var out []*model.Case
	for _, c := range cases {
		if c.Status == model.StatusFailed || c.Status == model.StatusError {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Module != b.Module {
			return a.Module < b.Module
		}
		if a.ClassName != b.ClassName {
			return a.ClassName < b.ClassName
		}
		return a.Name < b.Name
	})
	return out
}
