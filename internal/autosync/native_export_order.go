package autosync

import "strings"

// Only reorder disconnected blocks. A cycle leaves the remaining blocks in
// their deterministic fallback order. No successor link sets a time offset.
func orderUnmatchedComponents(records []MediaRecord, placements []NativePlacement, components []int) []int {
	groups, keys := continuitySeries(records)
	byRecording := map[int]int{}
	for _, p := range placements {
		if strings.HasPrefix(p.Status, "unresolved") {
			byRecording[p.Recording] = p.Component
		}
	}
	dependencies := map[int]map[int]bool{}
	for _, key := range keys {
		series := groups[key]
		for i := 1; i < len(series); i++ {
			a, b := series[i-1], series[i]
			if b.sequence == a.sequence || (i > 1 && series[i-2].sequence == a.sequence) || (i+1 < len(series) && series[i+1].sequence == b.sequence) {
				continue
			}
			x, xok := byRecording[a.id]
			y, yok := byRecording[b.id]
			if !xok || !yok || x == y {
				continue
			}
			if dependencies[y] == nil {
				dependencies[y] = map[int]bool{}
			}
			dependencies[y][x] = true
		}
	}
	ordered := make([]int, 0, len(components))
	placed := map[int]bool{}
	for len(ordered) < len(components) {
		found := false
		for _, c := range components {
			if placed[c] || len(dependencies[c]) != 0 {
				continue
			}
			ordered = append(ordered, c)
			placed[c] = true
			found = true
			for _, d := range dependencies {
				delete(d, c)
			}
			break
		}
		if !found {
			for _, c := range components {
				if !placed[c] {
					ordered = append(ordered, c)
				}
			}
			break
		}
	}
	return ordered
}
