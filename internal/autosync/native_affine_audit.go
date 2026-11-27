package autosync

import (
	"math"
	"sort"
)

type affineAudit struct {
	Component     int     `json:"component"`
	Status        string  `json:"status"`
	Windows       int     `json:"holdout_windows,omitempty"`
	PathMedian    float64 `json:"path_median_seconds,omitempty"`
	PathMaximum   float64 `json:"path_maximum_seconds,omitempty"`
	AffineMedian  float64 `json:"affine_median_seconds,omitempty"`
	AffineMaximum float64 `json:"affine_maximum_seconds,omitempty"`
	fit           map[int]NativeLayout
}

// Reuse the anchor solver's robust affine fitter for diagnostics. Train on
// fit windows only, then score the untouched holdouts under both timelines.
func auditGlobalAffine(records []MediaRecord, layout []NativeLayout, edges []*PairEvidence, active []int) []affineAudit {
	byRecording := make([]NativeLayout, len(records))
	groups := map[int][]int{}
	for _, m := range layout {
		byRecording[m.Recording] = m
		groups[m.Component] = append(groups[m.Component], m.Recording)
	}
	byGroup := map[int][]*PairEvidence{}
	for _, i := range active {
		e := edges[i]
		component := byRecording[e.Reference].Component
		if component == byRecording[e.Query].Component {
			byGroup[component] = append(byGroup[component], e)
		}
	}
	var components []int
	for c, group := range groups {
		if len(group) > 2 && len(byGroup[c]) >= len(group) {
			components = append(components, c)
		}
	}
	sort.Ints(components)
	var results []affineAudit
	for _, c := range components {
		members := groups[c]
		audit := affineAudit{Component: c, Status: "insufficient_independent_holdouts"}
		results = append(results, auditAffineComponent(audit, records, byRecording, members, byGroup[c]))
	}
	return results
}

func auditAffineComponent(report affineAudit, records []MediaRecord, baseline []NativeLayout, members []int, edges []*PairEvidence) affineAudit {
	rows := 0
	for _, e := range edges {
		rows += len(e.Selected.Fit)
	}
	if len(members) > 24 || rows > 512 {
		report.Status = "audit_compute_limit"
		return report
	}
	for _, e := range edges {
		points := relationPoints(e, records)
		if len(points) > 2 {
			first, last := points[0], points[len(points)-1]
			for _, p := range points[1 : len(points)-1] {
				expected := first[1] + (p[0]-first[0])*(last[1]-first[1])/(last[0]-first[0])
				if math.Abs(p[1]-expected) > .008 {
					report.Status = "piecewise_timing_required"
					return report
				}
			}
		}
	}
	index := map[int]int{}
	local := make([]MediaRecord, len(members))
	for i, r := range members {
		index[r], local[i] = i, records[r]
	}
	train := make([]*PairEvidence, 0, len(edges))
	var holdouts [][]*Measurement
	for _, e := range edges {
		windows := affineAuditHoldouts(e)
		if len(windows) < 2 || len(e.Selected.Fit) < 2 {
			return report
		}
		holdouts = append(holdouts, windows)
		copy := *e
		copy.Reference, copy.Query = index[e.Reference], index[e.Query]
		copy.Validation = nil
		train = append(train, &copy)
	}
	fit := make([]NativeLayout, len(local))
	for _, m := range nativeAnchorLayout(local, train) {
		if m.ComponentSize != len(local) || !(m.Rate > 0) {
			report.Status = "affine_fit_disconnected"
			return report
		}
		fit[m.Recording] = m
	}
	// Compare in the path solver's time scale. Common zero cancels in
	// pairwise residuals, while the two solvers can choose different roots.
	scale := baseline[members[0]].Rate / fit[0].Rate
	var current, affine []float64
	for i, e := range edges {
		r, q := fit[index[e.Reference]], fit[index[e.Query]]
		for _, o := range holdouts[i] {
			qtime, rtime := o.QueryTime, o.QueryTime+o.Offset
			current = append(current, math.Abs(MapTime(baseline[e.Reference].Knots, rtime)-MapTime(baseline[e.Query].Knots, qtime)))
			affine = append(affine, scale*math.Abs(r.Start+r.Rate*rtime-q.Start-q.Rate*qtime))
		}
	}
	if len(current) == 0 {
		return report
	}
	report.Windows = len(current)
	for i, v := range affine {
		report.AffineMaximum = max(report.AffineMaximum, v)
		report.PathMaximum = max(report.PathMaximum, current[i])
	}
	report.PathMedian, report.AffineMedian = median(current), median(affine)
	report.Status = "no_clear_affine_improvement"
	if report.AffineMaximum > .025 || report.AffineMaximum > report.PathMaximum+.002 {
		report.Status = "affine_holdout_disagreement"
	} else if report.PathMedian-report.AffineMedian >= .001 {
		report.Status = "affine_improved_holdout_median"
		report.fit = make(map[int]NativeLayout, len(members))
		for i, r := range members {
			report.fit[r] = fit[i]
		}
	}
	return report
}

// Keep the production reference's clock and origin and bound every proposed
// movement. Each accepted edge must still agree with its own observations.
func applyGlobalAffine(layout []NativeLayout, edges []*PairEvidence, graph map[string]any, audits []affineAudit) {
	active, _ := graph["active_edges"].([]int)
	roots, _ := graph["reference_recordings"].([]int)
	rejected, _ := graph["rejected_edges"].([]map[string]any)
	byRecording := make(map[int]int, len(layout))
	for i, m := range layout {
		byRecording[m.Recording] = i
	}
	applied := false
	for i := range audits {
		audit := &audits[i]
		if len(audit.fit) == 0 {
			continue
		}
		conflicted := false
		for _, item := range rejected {
			k, ok := item["edge"].(int)
			if ok && k >= 0 && k < len(edges) {
				_, ref := audit.fit[edges[k].Reference]
				_, query := audit.fit[edges[k].Query]
				conflicted = conflicted || ref || query
			}
		}
		if conflicted {
			audit.Status = "affine_conflicting_graph"
			continue
		}
		for _, root := range roots {
			if _, ok := audit.fit[root]; !ok {
				continue
			}
			if candidate := guardedAffineMappings(layout, byRecording, root, audit.fit, edges, active); candidate != nil {
				for r, m := range candidate {
					layout[byRecording[r]] = m
				}
				audit.Status = "affine_applied"
				applied = true
			} else {
				audit.Status = "affine_placement_guard"
			}
			break
		}
	}
	if !applied {
		return
	}
	checks := make([]map[string]any, 0, len(active))
	maximum := 0.
	for _, k := range active {
		e := edges[k]
		obs := edgeObservations(e)
		residual := affineMappingResidual(layout[byRecording[e.Reference]].Knots, layout[byRecording[e.Query]].Knots, obs)
		checks = append(checks, map[string]any{"edge": k, "maximum_residual_seconds": residual, "observations": len(obs)})
		maximum = max(maximum, residual)
	}
	graph["global_evidence_residuals"], graph["maximum_global_residual_seconds"] = checks, maximum
}

func guardedAffineMappings(layout []NativeLayout, index map[int]int, root int, fit map[int]NativeLayout, edges []*PairEvidence, active []int) map[int]NativeLayout {
	base := layout[index[root]]
	scale := base.Rate / fit[root].Rate
	candidate := make(map[int]NativeLayout, len(fit))
	for r, affine := range fit {
		old := layout[index[r]]
		start := base.Start + scale*(affine.Start-fit[root].Start)
		rate := scale * affine.Rate
		if start < -1e-8 || math.Abs(rate-1) > .002 {
			return nil
		}
		start = max(0, start)
		points := make([]TimePoint, len(old.Knots))
		first, last := old.Knots[0], old.Knots[len(old.Knots)-1]
		for i, p := range old.Knots {
			linear := first[1] + (p[0]-first[0])*((last[1]-first[1])/(last[0]-first[0]))
			value := start + p[0]*rate
			if math.Abs(p[1]-linear) > .001 || math.Abs(p[1]-value) > .020 {
				return nil
			}
			points[i] = TimePoint{p[0], value}
		}
		if r == root {
			candidate[r] = old
			continue
		}
		old.Start, old.Rate, old.Knots = start, rate, points
		candidate[r] = old
	}
	var beforeHoldout, afterHoldout []float64
	for _, k := range active {
		e := edges[k]
		r, rok := candidate[e.Reference]
		q, qok := candidate[e.Query]
		if !rok || !qok {
			continue
		}
		obs := edgeObservations(e)
		before := affineMappingResidual(layout[index[e.Reference]].Knots, layout[index[e.Query]].Knots, obs)
		after := affineMappingResidual(r.Knots, q.Knots, obs)
		if after > .025 || after > before+.002 {
			return nil
		}
		for _, o := range affineAuditHoldouts(e) {
			qt, rt := o.QueryTime, o.QueryTime+o.Offset
			beforeHoldout = append(beforeHoldout, math.Abs(MapTime(layout[index[e.Reference]].Knots, rt)-MapTime(layout[index[e.Query]].Knots, qt)))
			afterHoldout = append(afterHoldout, math.Abs(MapTime(r.Knots, rt)-MapTime(q.Knots, qt)))
		}
		r.Error, q.Error = max(r.Error, after), max(q.Error, after)
		candidate[e.Reference], candidate[e.Query] = r, q
	}
	if len(beforeHoldout) == 0 || median(beforeHoldout)-median(afterHoldout) < .001 {
		return nil
	}
	return candidate
}

func affineMappingResidual(ref, query []TimePoint, obs []*Measurement) float64 {
	maximum := 0.
	for _, o := range obs {
		maximum = max(maximum, math.Abs(MapTime(ref, o.QueryTime+o.Offset)-MapTime(query, o.QueryTime)))
	}
	return maximum
}

func affineAuditHoldouts(e *PairEvidence) []*Measurement {
	if v := e.Validation; v != nil && v.Status == "pass_disjoint_windows" && v.Excluded && v.Contradictory == 0 && v.Passed >= 2 {
		var windows []*Measurement
		for _, m := range v.Observations {
			if m.AcousticallySupported || m.Passes {
				windows = append(windows, m)
			}
		}
		return windows
	}
	// A timing-profile edge also has separate audit windows. The edge's
	// presentation report merges selection and audit, so read the audit
	// role directly to avoid scoring windows used for model selection.
	if p := e.Profile; p != nil && p.Status == "validated_relation" && p.AuditResult != nil && p.AuditResult.Passes {
		var windows []*Measurement
		for _, m := range p.Audit {
			if m.Supported && m.Precise {
				windows = append(windows, m)
			}
		}
		return windows
	}
	return nil
}
