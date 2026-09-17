package agg

import (
	"testing"

	"github.com/istarion/junit-report-parser/internal/model"
)

// Locked nearest-rank percentile table (plan §6): ascending, k=ceil(p/100*n).
func TestPercentileNearestRank(t *testing.T) {
	vals := []float64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100}
	tests := []struct {
		p    float64
		want float64
	}{
		{0, 10},   // k = ceil(0) → 1
		{10, 10},  // ceil(1.0)=1
		{50, 50},  // ceil(5.0)=5 (odd n → exact middle)
		{90, 90},  // ceil(9.0)=9
		{95, 100}, // ceil(9.5)=10
		{100, 100},
	}
	for _, tt := range tests {
		if got := Percentile(vals, tt.p); got != tt.want {
			t.Errorf("Percentile(10 vals, %v) = %v, want %v", tt.p, got, tt.want)
		}
	}

	even := []float64{1, 2, 3, 4}
	// ceil(0.5*4)=2 → 2 (upper median, nearest-rank)
	if got := Percentile(even, 50); got != 2 {
		t.Errorf("median even = %v, want 2", got)
	}
	// ceil(0.9*4)=ceil(3.6)=4 → 4
	if got := Percentile(even, 90); got != 4 {
		t.Errorf("p90 even = %v, want 4", got)
	}

	if got := Percentile([]float64{7.5}, 95); got != 7.5 {
		t.Errorf("single element = %v, want 7.5", got)
	}
	if got := Percentile(nil, 50); got != 0 {
		t.Errorf("empty = %v, want 0", got)
	}
	if got := Percentile([]float64{5, 5, 5}, 90); got != 5 {
		t.Errorf("ties = %v, want 5", got)
	}
	// Unsorted input must be handled.
	if got := Percentile([]float64{30, 10, 20}, 50); got != 20 {
		t.Errorf("unsorted median = %v, want 20", got)
	}
}

func TestDurationStats(t *testing.T) {
	sum, mean, median, p90, p95, max := DurationStats([]float64{0.1, 0.2, 0.3, 6.0})
	if sum != 6.6 {
		t.Errorf("sum = %v", sum)
	}
	if mean != 1.65 {
		t.Errorf("mean = %v", mean)
	}
	if median != 0.2 || p90 != 6 || p95 != 6 || max != 6 {
		t.Errorf("median=%v p90=%v p95=%v max=%v", median, p90, p95, max)
	}
	s, m, md, p9, p95b, mx := DurationStats(nil)
	if s != 0 || m != 0 || md != 0 || p9 != 0 || p95b != 0 || mx != 0 {
		t.Errorf("empty stats = %v", []float64{s, m, md, p9, p95b, mx})
	}
}

func TestSortCanonical(t *testing.T) {
	cases := []*model.Case{
		{ID: "z#b", Module: "app", ClassName: "z", Name: "b"},
		{ID: "a#a", Module: "lib", ClassName: "a", Name: "a"},
		{ID: "a#b", Module: "app", ClassName: "a", Name: "b"},
		{ID: "a#a", Module: "app", ClassName: "a", Name: "a"},
	}
	SortCanonical(cases)
	want := []string{"app a a", "app a b", "app z b", "lib a a"}
	for i, c := range cases {
		key := c.Module + " " + c.ClassName + " " + c.Name
		if key != want[i] {
			t.Fatalf("order[%d] = %q, want %q", i, key, want[i])
		}
	}
}
