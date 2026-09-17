package agg

import (
	"sort"

	"github.com/istarion/junit-report-parser/internal/model"
)

// Group is one fingerprint-collapsed failure group (plan §6, §7; spec §10.3).
type Group struct {
	Fingerprint string
	Count       int
	Sample      *model.Case // representative case (canonical order, first)
	IDs         []string    // member ids in canonical order (module, class, name)
}

// GroupFailures groups failed/error cases by the fingerprint function (wired
// by the CLI to internal/trim; agg stays trim-agnostic). Groups are ordered
// count desc, then fingerprint asc (plan D10); member ids keep the canonical
// (module, classname, name) order of the input.
func GroupFailures(failed []*model.Case, fingerprint func(*model.Case) string) []Group {
	if len(failed) == 0 {
		return nil
	}
	byFp := map[string]*Group{}
	var order []string
	for _, c := range failed {
		fp := fingerprint(c)
		g, ok := byFp[fp]
		if !ok {
			g = &Group{Fingerprint: fp, Sample: c}
			byFp[fp] = g
			order = append(order, fp)
		}
		g.Count++
		g.IDs = append(g.IDs, c.ID)
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := byFp[order[i]], byFp[order[j]]
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		return a.Fingerprint < b.Fingerprint
	})
	groups := make([]Group, 0, len(order))
	for _, fp := range order {
		groups = append(groups, *byFp[fp])
	}
	return groups
}
