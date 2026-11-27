package autosync

import "sort"

// Holdout scheduling uses connectivity only to decide when to run a check.
// Only independently checked edges change the queue's connectivity. Cycle
// edges still receive the same checks and are returned in original order.
func scheduleNativeHoldouts(recordings, workers int, edges []*PairEvidence, reuse bool, work func(int) bool) {
	if len(edges) == 0 {
		return
	}
	parent := make([]int, recordings)
	for i := range parent {
		parent[i] = i
	}
	find := func(i int) int {
		for i != parent[i] {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	link := func(e *PairEvidence) {
		parent[find(e.Query)] = find(e.Reference)
	}
	var pending, cycles []int
	for i, edge := range edges {
		if reuse && edge.Validation != nil && edge.Validation.Status == "pass_disjoint_windows" {
			// The production work function returns this existing report directly.
			if work(i) {
				link(edge)
			}
			continue
		}
		pending = append(pending, i)
	}
	sort.SliceStable(pending, func(i, j int) bool {
		left, right := edges[pending[i]].Selected, edges[pending[j]].Selected
		if left.Confidence != right.Confidence {
			return left.Confidence > right.Confidence
		}
		return left.Span > right.Span
	})
	type completion struct {
		index  int
		passed bool
	}
	done := make(chan completion, workers)
	active := 0
	for len(pending)+len(cycles)+active > 0 {
		for active < workers && len(pending)+len(cycles) > 0 {
			// Connectivity grows monotonically, so a deferred cycle
			// cannot become a bridge during this pass.
			for len(pending) > 0 {
				e := edges[pending[0]]
				if find(e.Reference) != find(e.Query) {
					break
				}
				cycles = append(cycles, pending[0])
				pending = pending[1:]
			}
			queue := &pending
			if len(pending) == 0 {
				queue = &cycles
			}
			i := (*queue)[0]
			*queue = (*queue)[1:]
			active++
			go func() { done <- completion{i, work(i)} }()
		}
		if active > 0 {
			result := <-done
			active--
			if result.passed {
				link(edges[result.index])
			}
		}
	}
}
