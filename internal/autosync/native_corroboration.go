package autosync

import (
	"math"
	"sort"
)

// Time-separated evidence is necessary, not proof of statistical independence.
// Multiple microphones, channels and overlapping windows of one event count
// once. This gate must never be used to label a relationship certified.
func distinctRegions(windows []TimePoint) int {
	w := append([]TimePoint(nil), windows...)
	sort.Slice(w, func(i, j int) bool {
		if w[i][1] != w[j][1] {
			return w[i][1] < w[j][1]
		}
		return w[i][0] < w[j][0]
	})
	count, end := 0, math.Inf(-1)
	for _, v := range w {
		if v[0] > end+.04 && v[1] > v[0] {
			count++
			end = v[1]
		}
	}
	return count
}

func profileRegions(p *NativeProfile) []TimePoint {
	var windows []TimePoint
	for _, role := range [][]*Measurement{p.Training, p.Selection, p.Audit} {
		for _, o := range role {
			if o == nil || !o.Supported || o.Window <= 0 {
				continue
			}
			predicted := p.Prior.Offset + p.Prior.Slope*o.QueryTime
			if p.Model != nil {
				predicted = p.Model.predict(o.QueryTime)
			}
			if math.Abs(o.Offset-predicted) <= math.Max(.025, p.Limit) {
				windows = append(windows, TimePoint{o.QueryTime - o.Window/2, o.QueryTime + o.Window/2})
			}
		}
	}
	return windows
}

func corroboratingReferences(group []eventEntry, records []MediaRecord) int {
	seen := map[string][]TimePoint{}
	for _, e := range group {
		// Exact duplicate PCM must not manufacture an additional observer.
		key := records[e.reference].CacheSHA
		if key == "" {
			key = "recording:" + stringValue(e.reference)
		}
		seen[key] = append(seen[key], profileRegions(e.profile)...)
	}
	count := 0
	for _, w := range seen {
		if distinctRegions(w) >= 2 {
			count++
		}
	}
	return count
}

// A late, bounded component pass reuses already audited pair models. It does
// not decode audio, invent a clock model or turn graph agreement into an audit.
func corroborateComponents(records []MediaRecord, edges []*PairEvidence, profiles []NativeProfilePair) ([]*PairEvidence, []map[string]any) {
	eligible := 0
	for _, pair := range profiles {
		for _, p := range pair.Profiles {
			if p.Status == "corroboration_required" && p.Model != nil && p.AuditResult != nil && p.AuditResult.Passes {
				eligible++
			}
		}
	}
	if eligible < 2 {
		return edges, nil
	}
	layout, _, err := nativeSolve(records, edges)
	if err != nil {
		return edges, nil
	}
	byID := make(map[int]NativeLayout, len(layout))
	for _, l := range layout {
		byID[l.Recording] = l
	}
	type candidate struct {
		edge    *PairEvidence
		points  []TimePoint // component B -> component A, including timing bends
		windows []TimePoint // evidence in component B
		a, b    int
	}
	buckets := map[[2]int][]candidate{}
	for _, pair := range profiles {
		r, q := byID[pair.Reference], byID[pair.Query]
		if r.Component == q.Component || r.ComponentSize < 2 || q.ComponentSize < 2 {
			continue
		}
		for _, p := range pair.Profiles {
			if p.Status != "corroboration_required" || p.Model == nil || p.AuditResult == nil || !p.AuditResult.Passes || len(p.Range) != 2 {
				continue
			}
			e := edgeFromProfile(pair, p, records)
			relation := relationPoints(e, records)
			// Compose in source space first, then change the domain to the
			// established component clock. Preserve all breakpoints.
			composed := ComposeMapping(r.Knots, relation, records[pair.Query].Duration)
			points := make([]TimePoint, len(composed))
			for i, point := range composed {
				points[i] = TimePoint{MapTime(q.Knots, point[0]), point[1]}
			}
			// Include query-component knots too; its clock may be nonlinear.
			for _, point := range q.Knots {
				points = append(points, TimePoint{point[1], MapTime(composed, point[0])})
			}
			sort.Slice(points, func(i, j int) bool { return points[i][0] < points[j][0] })
			unique := points[:0]
			for _, point := range points {
				if len(unique) == 0 || point[0] > unique[len(unique)-1][0] {
					unique = append(unique, point)
				}
			}
			points = unique
			var windows []TimePoint
			for _, w := range profileRegions(p) {
				windows = append(windows, TimePoint{MapTime(q.Knots, w[0]), MapTime(q.Knots, w[1])})
			}
			a, b, ca, cb := pair.Reference, pair.Query, r.Component, q.Component
			if ca > cb {
				for i, w := range windows {
					windows[i] = TimePoint{MapTime(points, w[0]), MapTime(points, w[1])}
				}
				points = InverseMapping(points)
				a, b = b, a
				ca, cb = cb, ca
			}
			buckets[[2]int{ca, cb}] = append(buckets[[2]int{ca, cb}], candidate{e, points, windows, a, b})
		}
	}
	keys := make([][2]int, 0, len(buckets))
	for key := range buckets {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i][0] != keys[j][0] {
			return keys[i][0] < keys[j][0]
		}
		return keys[i][1] < keys[j][1]
	})
	result := append([]*PairEvidence(nil), edges...)
	var reports []map[string]any
	for _, key := range keys {
		cs := buckets[key]
		status := "deferred_insufficient_distributed_evidence"
		left, right := map[string]bool{}, map[string]bool{}
		identity := func(i int) string {
			if records[i].CacheSHA != "" {
				return records[i].CacheSHA
			}
			return "recording:" + stringValue(i)
		}
		var windows []TimePoint
		for _, c := range cs {
			left[identity(c.a)] = true
			right[identity(c.b)] = true
			windows = append(windows, c.windows...)
		}
		compatible := true
		if len(cs) > 64 {
			status = "deferred_component_hypothesis_budget"
			compatible = false
		} else {
			for i, a := range cs {
				for _, b := range cs[:i] {
					// Test every measured interval endpoint, not just an offset at
					// the midpoint: equal midpoints can conceal incompatible drift.
					for _, w := range append(append([]TimePoint(nil), a.windows...), b.windows...) {
						for _, x := range w {
							if math.Abs(MapTime(a.points, x)-MapTime(b.points, x)) > .05 {
								compatible = false
								status = "deferred_competing_component_transforms"
							}
						}
					}
				}
			}
		}
		regions := distinctRegions(windows)
		if compatible && len(left) >= 2 && len(right) >= 2 && regions >= 3 {
			status = "distributed_component_support_not_certified"
			sort.SliceStable(cs, func(i, j int) bool {
				a, b := cs[i].edge, cs[j].edge
				if a.Reference != b.Reference {
					return a.Reference < b.Reference
				}
				if a.Query != b.Query {
					return a.Query < b.Query
				}
				if a.Selected.Confidence != b.Selected.Confidence {
					return a.Selected.Confidence > b.Selected.Confidence
				}
				return a.Selected.Offset < b.Selected.Offset
			})
			seen := map[[2]int]bool{}
			for _, c := range cs {
				key := [2]int{c.edge.Reference, c.edge.Query}
				if !seen[key] {
					result = append(result, c.edge)
					seen[key] = true
				}
			}
		}
		reports = append(reports, map[string]any{"components": key, "status": status, "candidate_count": len(cs), "distinct_left_recordings": len(left), "distinct_right_recordings": len(right), "separated_regions": regions, "independent_certification": false})
	}
	return result, reports
}
