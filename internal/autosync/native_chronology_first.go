package autosync

import (
	"context"
	"fmt"
	"math"
)

func (a *NativeAnalyzer) chronologyFirst(ctx context.Context, edges []*PairEvidence, discovery []NativePair, out string, progress *nativeProgress) ([]*PairEvidence, error) {
	if len(edges) == 0 {
		return nil, nil
	}
	layout, _, err := nativeSolve(a.records, edges)
	if err != nil {
		return nil, err
	}
	// Avoid early holdout work when we do not have a plausible bracket.
	orderHint := analysisSettings(ctx).GuideRecorderOrder
	if len(earlyChronologyEstimates(a.records, layout, orderHint)) == 0 {
		return nil, nil
	}
	progress.set("Checking likely chronological matches")
	anchors, err := a.validateEdges(edges, out, "chronology-first-anchors-")
	if err != nil {
		return nil, err
	}
	verified := make(map[[2]int]*HoldoutReport, len(anchors))
	for _, edge := range anchors {
		verified[[2]int{edge.Reference, edge.Query}] = edge.Validation
	}
	for _, edge := range edges {
		edge.Validation = verified[[2]int{edge.Reference, edge.Query}]
	}
	layout, _, err = nativeSolve(a.records, anchors)
	if err != nil {
		return nil, err
	}

	estimates := earlyChronologyEstimates(a.records, layout, orderHint)
	blocked := map[int]bool{}
	for _, e := range edges {
		// Previously supported local or competing placements get broad recovery.
		blocked[e.Query], blocked[e.Reference] = true, true
	}
	targets := estimates[:0]
	for _, estimate := range estimates {
		if !blocked[estimate.Recording] {
			targets = append(targets, estimate)
		}
	}
	pairs := dateGuidedPairsForEstimates(a.records, layout, nil, discovery, targets, true)
	if len(pairs) == 0 {
		return nil, nil
	}
	found, err := a.verify(ctx, pairs, out, "chronology-first-v1")
	if err != nil {
		return nil, err
	}
	seed := map[[2]int]float64{}
	for _, pair := range pairs {
		seed[[2]int{pair.Reference, pair.Query}] = pair.Candidates[0].Offset
	}
	candidates := found[:0]
	for _, edge := range found {
		selected := edge.Selected
		if selected.Method == "calibrated_chronology_audio_search" && math.Abs(selected.Offset-seed[[2]int{edge.Reference, edge.Query}]) <= .5 {
			candidates = append(candidates, edge)
		}
	}
	checked, err := a.validateEdges(candidates, out, "chronology-first-")
	if err != nil {
		return nil, err
	}
	strong := checked[:0]
	for _, edge := range checked {
		v := edge.Validation
		if v.Excluded && v.Passed >= 3 && v.Contradictory == 0 && v.Coverage >= .6 && v.Maximum != nil && *v.Maximum <= .05 {
			strong = append(strong, edge)
		}
	}
	solved, _, err := nativeSolve(a.records, append(append([]*PairEvidence(nil), anchors...), strong...))
	if err != nil {
		return nil, err
	}
	starts := map[int]float64{}
	for _, p := range targets {
		starts[p.Recording] = p.Start
	}
	confirmed := map[int]bool{}
	for _, m := range solved {
		start, ok := starts[m.Recording]
		if ok && m.Component == 1 && m.Status == "acoustically_supported_not_certified" &&
			math.Abs(MapTime(m.Knots, -sourceOrigin(a.records[m.Recording]))-start) <= .75 {
			confirmed[m.Recording] = true
		}
	}
	outcome := strong[:0]
	for _, edge := range strong {
		if confirmed[edge.Query] {
			outcome = append(outcome, edge)
		}
	}
	detail := fmt.Sprintf("Chronology-first recovery checked %d predicted matches. %d joined the main component before broad recovery.", len(pairs), len(outcome))
	fmt.Println(detail)
	observe(ctx, ProgressEvent{Kind: "source", Detail: detail})
	return outcome, nil
}

func earlyChronologyEstimates(records []MediaRecord, layout []NativeLayout, orderHint ...bool) []ChronologicalPlacement {
	models := map[int]NativeLayout{}
	for _, m := range layout {
		models[m.Recording] = m
	}
	start := func(i int) float64 {
		return MapTime(models[i].Knots, -sourceOrigin(records[i]))
	}
	groups, keys := continuitySeries(records)
	var estimates []ChronologicalPlacement
	for _, key := range keys {
		series := groups[key]
		unique := true
		for i := 1; i < len(series); i++ {
			if series[i].sequence <= series[i-1].sequence {
				unique = false
				break
			}
		}
		if !unique {
			continue
		}
		for i := 1; i+1 < len(series); i++ {
			a, b, c := series[i-1], series[i], series[i+1]
			if a.sequence+1 != b.sequence || b.sequence+1 != c.sequence {
				continue
			}
			left, target, right := models[a.id], models[b.id], models[c.id]
			if !chronologyEarlyAnchor(left) || !chronologyEarlyAnchor(right) || target.ComponentSize != 1 ||
				target.Status != "unresolved_no_supported_relation" || records[b.id].Audio == nil {
				continue
			}

			group, _ := SourceTrackGroup(records[b.id])
			ag, _ := SourceTrackGroup(records[a.id])
			cg, _ := SourceTrackGroup(records[c.id])
			if group != ag || group != cg {
				continue
			}
			ids := [3]int{a.id, b.id, c.id}
			suspect := false
			for _, id := range ids {
				_, _, basis := detectSourceTrack(records[id])
				suspect = suspect || basis == "transcoded"
			}
			if suspect {
				continue
			}
			lo := MapTime(left.Knots, records[a.id].Duration)
			hi := start(c.id) - records[b.id].Duration
			if hi < lo {
				continue
			}
			predicted, ok := corroborateSearchDates(records, ids, start(a.id), start(c.id))
			basis := "corroborated_bracketing_dates"
			if (!ok || predicted < lo-.25 || predicted > hi+.25) && len(orderHint) > 0 && orderHint[0] && hi-lo <= .5 {
				predicted, ok = (lo+hi)/2, true
				basis = "tight_recorder_order_acoustic_search_only"
			}
			if !ok || predicted < lo-.25 || predicted > hi+.25 {
				continue
			}
			estimates = append(estimates, ChronologicalPlacement{
				Recording: b.id, Start: math.Max(lo, math.Min(hi, predicted)), Uncertainty: 1,
				Basis: basis, Anchors: []int{a.id, c.id},
			})
		}
	}
	return estimates
}

func chronologyEarlyAnchor(m NativeLayout) bool {
	return m.Component == 1 && m.ComponentSize > 1 && m.Status == "acoustically_supported_not_certified" &&
		len(m.Knots) >= 2 && m.Error <= .05
}

func corroborateSearchDates(records []MediaRecord, ids [3]int, left, right float64) (float64, bool) {
	predict := func(kind string, end bool) (float64, bool) {
		var stamps [3]float64
		for i, id := range ids {
			t, ok := recordingStamp(records[id], kind)
			if !ok {
				return 0, false
			}
			if end {
				t -= records[id].Duration
			}
			stamps[i] = t
		}
		if stamps[0] >= stamps[1] || stamps[1] >= stamps[2] {
			return 0, false
		}
		a := left + (stamps[1] - stamps[0])
		b := right + (stamps[1] - stamps[2])
		return (a + b) / 2, math.Abs(a-b) <= .75
	}
	created, ok := predict("sidecar_creation", false)
	if !ok {
		created, ok = predict("container_creation", false)
	}
	if !ok {
		return 0, false
	}
	matches := false
	for _, end := range []bool{false, true} {
		modified, ok := predict("filesystem_mtime", end)
		if !ok {
			continue
		}
		if math.Abs(created-modified) > .75 {
			return 0, false
		}
		matches = true
	}
	return created, matches
}
