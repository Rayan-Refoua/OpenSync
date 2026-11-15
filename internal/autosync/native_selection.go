package autosync

import (
	"math"
	"sort"
	"sync"
)

type eventEntry struct {
	reference, component int
	position             float64
	profile              *NativeProfile
}

func selectNativeEdges(records []MediaRecord, base []*PairEvidence, profiles []NativeProfilePair) ([]*PairEvidence, []map[string]any) {
	base, rejected := rejectExcerptBridges(records, base)
	anchorsByPair := map[[2]int]*PairEvidence{}
	anchorOrder := [][2]int{}
	putAnchor := func(e *PairEvidence) {
		key := [2]int{e.Reference, e.Query}
		if _, ok := anchorsByPair[key]; !ok {
			anchorOrder = append(anchorOrder, key)
		}
		anchorsByPair[key] = e
	}
	for _, e := range base {
		if math.Min(records[e.Reference].Duration, records[e.Query].Duration) >= 30 {
			putAnchor(e)
		}
	}
	for _, pair := range profiles {
		var valid []*NativeProfile
		for _, p := range pair.Profiles {
			if p.Status == "validated_relation" {
				valid = append(valid, p)
			}
		}
		if len(valid) == 1 && math.Min(records[pair.Reference].Duration, records[pair.Query].Duration) >= 30 {
			putAnchor(edgeFromProfile(pair, valid[0], records))
		}
	}
	anchors := []*PairEvidence{}
	for _, key := range anchorOrder {
		e := anchorsByPair[key]
		if len(e.Selected.Knots) <= 2 {
			anchors = append(anchors, e)
		}
	}
	anchors, _ = rejectExcerptBridges(records, anchors)
	byID := func(layout []NativeLayout) map[int]NativeLayout {
		result := map[int]NativeLayout{}
		for _, m := range layout {
			result[m.Recording] = m
		}
		return result
	}
	layout := byID(nativeAnchorLayout(records, anchors))
	contexts := map[int]map[int]NativeLayout{}
	counts := map[int]int{}
	for _, p := range profiles {
		counts[p.Query] += len(p.Profiles)
	}
	qs := []int{}
	for q := range counts {
		qs = append(qs, q)
	}
	sort.Ints(qs)
	contextResults := make([]map[int]NativeLayout, len(qs))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for worker := 0; worker < nativeWorkers(len(qs)); worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				q := qs[index]
				if records[q].Duration >= 30 && counts[q] > 1 {
					independent := []*PairEvidence{}
					for _, e := range anchors {
						if e.Query != q && e.Reference != q {
							independent = append(independent, e)
						}
					}
					contextResults[index] = byID(nativeAnchorLayout(records, independent))
				}
			}
		}()
	}
	for i := range qs {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	for i, result := range contextResults {
		if result != nil {
			contexts[qs[i]] = result
		}
	}
	contextFor := func(q int) map[int]NativeLayout {
		if m, ok := contexts[q]; ok {
			return m
		}
		return layout
	}
	candidates := map[int][]eventEntry{}
	for _, pair := range profiles {
		anchor := contextFor(pair.Query)[pair.Reference]
		if anchor.ComponentSize < 2 {
			continue
		}
		for _, p := range pair.Profiles {
			if p.Prior.Strength == 0 && p.Status != "validated_relation" && p.Status != "corroboration_required" && p.Status != "event_supported_timing_unresolved" {
				continue
			}
			mid := records[pair.Query].Duration / 2
			offset := p.Prior.Offset + p.Prior.Slope*mid
			if p.Model != nil {
				offset = p.Model.predict(mid)
			}
			position := anchor.Start + anchor.Rate*(mid+offset) - mid
			candidates[pair.Query] = append(candidates[pair.Query], eventEntry{pair.Reference, anchor.Component, position, p})
		}
	}
	uniqueRefs := func(group []eventEntry) int { return corroboratingReferences(group, records) }
	strength := func(group []eventEntry) float64 {
		v := 0.
		for _, e := range group {
			v = math.Max(v, e.profile.Prior.Strength)
		}
		return v
	}
	extent := func(group []eventEntry) float64 {
		maximum := 0.
		for _, e := range group {
			lo, hi := math.Inf(1), math.Inf(-1)
			for _, role := range [][]*Measurement{e.profile.Training, e.profile.Selection, e.profile.Audit} {
				for _, o := range role {
					if o.Supported {
						lo = math.Min(lo, o.QueryTime)
						hi = math.Max(hi, o.QueryTime)
					}
				}
			}
			maximum = math.Max(maximum, hi-lo)
		}
		return maximum
	}
	decisions := map[int][]eventEntry{}
	report := []map[string]any{}
	for _, q := range qs {
		entries := append([]eventEntry(nil), candidates[q]...)
		sort.SliceStable(entries, func(i, j int) bool {
			a, b := entries[i], entries[j]
			if a.component != b.component {
				return a.component < b.component
			}
			if a.position != b.position {
				return a.position < b.position
			}
			return a.reference < b.reference
		})
		groups := [][]eventEntry{}
		for _, e := range entries {
			if len(groups) == 0 || groups[len(groups)-1][0].component != e.component || e.position-groups[len(groups)-1][0].position > .15 {
				groups = append(groups, nil)
			}
			groups[len(groups)-1] = append(groups[len(groups)-1], e)
		}
		if len(groups) < 2 {
			continue
		}
		sort.SliceStable(groups, func(i, j int) bool {
			a, b := uniqueRefs(groups[i]), uniqueRefs(groups[j])
			if a != b {
				return a > b
			}
			return strength(groups[i]) > strength(groups[j])
		})
		var winner []eventEntry
		if uniqueRefs(groups[0]) >= 2 && uniqueRefs(groups[0]) > uniqueRefs(groups[1]) {
			winner = groups[0]
		} else {
			ranked := append([][]eventEntry(nil), groups...)
			sort.SliceStable(ranked, func(i, j int) bool { return extent(ranked[i]) > extent(ranked[j]) })
			if extent(ranked[0]) >= 60 && extent(ranked[0]) >= 3*math.Max(extent(ranked[1]), 1) {
				winner = ranked[0]
			}
		}
		decisions[q] = winner
		alternatives := []map[string]any{}
		for _, g := range groups {
			refs := map[int]bool{}
			positions := []float64{}
			for _, e := range g {
				refs[e.reference] = true
				positions = append(positions, e.position)
			}
			ids := []int{}
			for r := range refs {
				ids = append(ids, r)
			}
			sort.Ints(ids)
			alternatives = append(alternatives, map[string]any{"component": g[0].component, "position_seconds": median(positions), "references": ids})
		}
		status := "unresolved_competing_events"
		if winner != nil {
			status = "corroborated_alternative"
		}
		report = append(report, map[string]any{"recording": q, "status": status, "alternatives": alternatives})
	}
	permitted := func(e *PairEvidence) bool {
		winner, ok := decisions[e.Query]
		if !ok {
			return true
		}
		if winner == nil {
			return false
		}
		anchor := contextFor(e.Query)[e.Reference]
		mid := records[e.Query].Duration / 2
		position := anchor.Start + anchor.Rate*MapTime(relationPoints(e, records), mid) - mid
		positions := []float64{}
		for _, w := range winner {
			positions = append(positions, w.position)
		}
		return anchor.Component == winner[0].component && math.Abs(position-median(positions)) <= .15
	}
	selected := map[[2]int]*PairEvidence{}
	for _, e := range base {
		if permitted(e) {
			selected[[2]int{e.Reference, e.Query}] = e
		}
	}
	for _, pair := range profiles {
		allowed := []*PairEvidence{}
		for _, p := range pair.Profiles {
			corroborated := false
			if p.Status == "corroboration_required" {
				var own *eventEntry
				for i, e := range candidates[pair.Query] {
					if e.reference == pair.Reference && e.profile == p {
						copy := candidates[pair.Query][i]
						own = &copy
						break
					}
				}
				if own != nil {
					group := []eventEntry{}
					for _, e := range candidates[pair.Query] {
						if e.component == own.component && math.Abs(e.position-own.position) <= .15 {
							group = append(group, e)
						}
					}
					corroborated = uniqueRefs(group) >= 2
				}
			}
			if p.Status == "validated_relation" || corroborated {
				e := edgeFromProfile(pair, p, records)
				if permitted(e) {
					allowed = append(allowed, e)
				}
			}
		}
		key := [2]int{pair.Reference, pair.Query}
		if len(allowed) == 1 {
			selected[key] = allowed[0]
		} else if len(allowed) > 1 {
			delete(selected, key)
		}
	}
	keys := [][2]int{}
	for k := range selected {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i][0] != keys[j][0] {
			return keys[i][0] < keys[j][0]
		}
		return keys[i][1] < keys[j][1]
	})
	result := []*PairEvidence{}
	for _, k := range keys {
		e := selected[k]
		blocked := false
		for _, node := range []int{e.Reference, e.Query} {
			if w, ok := decisions[node]; ok && w == nil {
				blocked = true
			}
		}
		if !blocked {
			result = append(result, e)
		}
	}
	result, more := rejectExcerptBridges(records, result)
	seen := map[[2]int]bool{}
	for _, r := range append(rejected, more...) {
		key := [2]int{r["reference"].(int), r["query"].(int)}
		if !seen[key] {
			report = append(report, r)
			seen[key] = true
		}
	}
	result, componentDecisions := corroborateComponents(records, result, profiles)
	report = append(report, componentDecisions...)
	return result, report
}
