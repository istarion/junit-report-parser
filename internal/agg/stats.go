package agg

import (
	"math"
	"sort"

	"github.com/istarion/junit-report-parser/internal/model"
)

// Percentile returns the nearest-rank percentile (plan §6): ascending sort,
// k = ceil(p/100 * n), 1-based. Empty set → 0.
func Percentile(vals []float64, p float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	s := append([]float64(nil), vals...)
	sort.Float64s(s)
	k := int(math.Ceil(p / 100 * float64(len(s))))
	if k < 1 {
		k = 1
	}
	if k > len(s) {
		k = len(s)
	}
	return s[k-1]
}

// DurationStats summarizes case durations in seconds: sum, mean, median
// (= nearest-rank p50), p90, p95 and max. Empty set → all zeros.
func DurationStats(times []float64) (sum, mean, median, p90, p95, max float64) {
	for _, v := range times {
		sum += v
		if v > max {
			max = v
		}
	}
	if len(times) == 0 {
		return 0, 0, 0, 0, 0, 0
	}
	mean = sum / float64(len(times))
	median = Percentile(times, 50)
	p90 = Percentile(times, 90)
	p95 = Percentile(times, 95)
	return sum, mean, median, p90, p95, max
}

// SortCanonical orders cases by (module, classname, name) in place (plan D10).
func SortCanonical(cases []*model.Case) {
	sort.SliceStable(cases, func(i, j int) bool {
		a, b := cases[i], cases[j]
		if a.Module != b.Module {
			return a.Module < b.Module
		}
		if a.ClassName != b.ClassName {
			return a.ClassName < b.ClassName
		}
		return a.Name < b.Name
	})
}
