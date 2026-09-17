package agg

import (
	"time"

	"github.com/istarion/junit-report-parser/internal/model"
)

// reportRank is the dedup preference key of one report (plan §6):
// non-aggregated wins; then later suite timestamp; then later mtime; then
// lexicographically greater path — fully deterministic.
type reportRank struct {
	nonAgg bool
	ts     time.Time
	hasTS  bool
	mtime  time.Time
	path   string
}

func rankOf(r *model.Report) reportRank {
	return reportRank{
		nonAgg: !r.Aggregated,
		ts:     r.Timestamp,
		hasTS:  r.HasTimestamp,
		mtime:  r.Mtime,
		path:   r.Path,
	}
}

// better reports whether rank a is preferred over rank b.
func (a reportRank) better(b reportRank) bool {
	if a.nonAgg != b.nonAgg {
		return a.nonAgg
	}
	if a.hasTS && b.hasTS && !a.ts.Equal(b.ts) {
		return a.ts.After(b.ts)
	}
	if !a.mtime.Equal(b.mtime) {
		return a.mtime.After(b.mtime)
	}
	return a.path > b.path
}

// Dedupe collapses duplicate (classname, name) cases across reports (plan §6,
// spec §14.3), preferring case winners by the deterministic rank chain. It
// rebuilds each report's Cases in place and returns the number of collapsed
// (dropped) cases. Disabled entirely by --no-dedupe at the call site.
func Dedupe(reports []*model.Report) int {
	type key struct{ class, name string }
	best := map[key]*model.Case{}
	bestRank := map[key]reportRank{}

	for _, r := range reports {
		for _, c := range r.Cases {
			k := key{c.ClassName, c.Name}
			kr := rankOf(r)
			if _, ok := best[k]; !ok || kr.better(bestRank[k]) {
				best[k] = c
				bestRank[k] = kr
			}
		}
	}

	collapsed := 0
	for _, r := range reports {
		kept := r.Cases[:0:0]
		for _, c := range r.Cases {
			k := key{c.ClassName, c.Name}
			if best[k] == c {
				kept = append(kept, c)
			} else {
				collapsed++
			}
		}
		r.Cases = kept
	}
	return collapsed
}
