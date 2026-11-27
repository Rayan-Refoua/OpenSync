package autosync

import "math"

// Look for independent audio spanning a bracketed camera clip. The reference
// mapping converts timeline seconds back into the reference audio clock.
func dateGuidedPairs(records []MediaRecord, layout []NativeLayout, blocked map[int]bool, discovery []NativePair) []NativePair {
	return dateGuidedPairsForEstimates(records, layout, blocked, discovery, acousticSearchEstimates(records, layout, blocked), false)
}

func dateGuidedPairsForEstimates(records []MediaRecord, layout []NativeLayout, blocked map[int]bool, discovery []NativePair, estimates []ChronologicalPlacement, independentSource bool) []NativePair {
	priors := map[[2]int][]NativeCandidate{}
	for _, pair := range discovery {
		key := [2]int{pair.Reference, pair.Query}
		priors[key] = append(priors[key], pair.Candidates...)
	}
	var pairs []NativePair
	for _, estimate := range estimates {
		q := estimate.Recording
		if records[q].Audio == nil || estimate.Uncertainty > 2 {
			continue
		}
		bestOverlap := 0.
		var best NativePair
		for _, m := range layout {
			r := m.Recording
			if blocked[r] || r == q || m.Component != 1 || m.Status != "acoustically_supported_not_certified" || records[r].Audio == nil || len(m.Knots) < 2 || records[r].Duration < records[q].Duration {
				continue
			}
			if independentSource {
				reference, _ := SourceTrackGroup(records[r])
				query, _ := SourceTrackGroup(records[q])
				if reference == query {
					continue
				}
			}
			offset := MapTime(InverseMapping(m.Knots), estimate.Start+sourceOrigin(records[q]))
			overlap := math.Min(records[q].Duration, records[r].Duration-offset) - math.Max(0, -offset)
			if overlap < 8 || overlap <= bestOverlap {
				continue
			}
			bestOverlap = overlap
			candidate := NativeCandidate{Offset: offset, Method: "calibrated_chronology_audio_search"}
			best = NativePair{Reference: r, Query: q, Candidates: []NativeCandidate{candidate}}
			for _, other := range priors[[2]int{r, q}] {
				distinct := true
				for _, c := range best.Candidates {
					if math.Abs(c.Offset-other.Offset) < .35 {
						distinct = false
						break
					}
				}
				if distinct {
					best.Candidates = append(best.Candidates, other)
				}
			}
		}
		if bestOverlap > 0 {
			pairs = append(pairs, best)
		}
	}
	return pairs
}

// Additional estimates are search priors only. Two solved clips from the
// recorder must agree on its clock, and the predicted clip must fit between
// those anchors even when other unresolved files intervene.
func acousticSearchEstimates(records []MediaRecord, layout []NativeLayout, blocked map[int]bool) []ChronologicalPlacement {
	estimates := chronologicalPlacements(records, layout, blocked)
	seen := map[int]bool{}
	for _, p := range estimates {
		seen[p.Recording] = true
	}
	models := map[int]NativeLayout{}
	for _, m := range layout {
		models[m.Recording] = m
	}
	groups, keys := continuitySeries(records)
	for _, key := range keys {
		series := groups[key]
		prior := -1
		for i, e := range series {
			if i > 0 && e.sequence <= series[i-1].sequence {
				prior = -1
				continue
			}
			m := models[e.id]
			if m.Component != 1 || m.Status != "acoustically_supported_not_certified" || blocked[e.id] {
				continue
			}
			if prior >= 0 {
				a := series[prior].id
				left := models[a]
				for _, q := range series[prior+1 : i] {
					qm := models[q.id]
					if seen[q.id] || blocked[q.id] || qm.ComponentSize != 1 || qm.Status != "unresolved_no_supported_relation" {
						continue
					}
					var estimatesAt []float64
					for _, kind := range []string{"sidecar_creation", "container_creation", "filesystem_mtime"} {
						x, xok := recordingStamp(records[a], kind)
						y, yok := recordingStamp(records[e.id], kind)
						v, vok := recordingStamp(records[q.id], kind)
						if !xok || !yok || !vok || v <= x || v >= y {
							continue
						}
						for _, end := range []bool{false, true} {
							if end && kind != "filesystem_mtime" {
								continue
							}
							startA := MapTime(left.Knots, -sourceOrigin(records[a]))
							startC := MapTime(m.Knots, -sourceOrigin(records[e.id]))
							deltaA, deltaC := v-x, v-y
							if end {
								deltaA += records[a].Duration - records[q.id].Duration
								deltaC += records[e.id].Duration - records[q.id].Duration
							}
							pa, pc := startA+deltaA, startC+deltaC
							if math.Abs(pa-pc) > 1 {
								continue
							}
							t := (pa + pc) / 2
							if t >= startA+records[a].Duration-1 && t+records[q.id].Duration <= startC+1 {
								estimatesAt = append(estimatesAt, t)
							}
						}
					}
					if len(estimatesAt) == 0 {
						continue
					}
					lo, hi := estimatesAt[0], estimatesAt[0]
					for _, t := range estimatesAt {
						lo = math.Min(lo, t)
						hi = math.Max(hi, t)
					}
					if hi-lo <= 1 {
						estimates = append(estimates, ChronologicalPlacement{Recording: q.id, Start: (lo + hi) / 2, Uncertainty: 2, Basis: "bracketing_clock_search_only"})
					}
				}
			}
			prior = i
		}
	}
	return estimates
}
