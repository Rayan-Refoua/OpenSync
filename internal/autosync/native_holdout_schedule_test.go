package autosync

import (
	"reflect"
	"testing"
)

func TestHoldoutPriorityKeepsAlternativesAfterFalseBridge(t *testing.T) {
	edge := func(ref, query int, confidence float64) *PairEvidence {
		return &PairEvidence{Reference: ref, Query: query, Selected: &CandidateEvidence{Confidence: confidence}}
	}
	edges := []*PairEvidence{
		edge(0, 1, .9), edge(1, 2, .8), edge(0, 2, .7), edge(2, 3, .6), edge(0, 3, 1),
	}
	edges[4].Reference, edges[4].Query = 0, 1
	edges[4].Validation = &HoldoutReport{Status: "pass_disjoint_windows"}
	var seen []int
	scheduleNativeHoldouts(4, 1, edges, true, func(i int) bool {
		seen = append(seen, i)
		return i != 1
	})
	if !reflect.DeepEqual(seen, []int{4, 1, 2, 3, 0}) {
		t.Fatalf("cached edge, bridge checks and corroboration dispatched in order %v", seen)
	}
}

func TestAutomaticTimingNeedsWorkloadAndIndependentSupport(t *testing.T) {
	policy := AnalysisSettings{TimingProfiles: "auto"}
	prior := NativePrior{Offset: 20, Strength: .8}
	var records []MediaRecord
	var pairs []NativePriorPair
	var holdouts []*PairEvidence
	for i := 0; i < 10; i++ {
		records = append(records, MediaRecord{Duration: 400}, MediaRecord{Duration: 360})
		pairs = append(pairs, NativePriorPair{Reference: 2 * i, Query: 2*i + 1, Candidates: []NativePrior{prior}})
		holdouts = append(holdouts, &PairEvidence{Reference: 2 * i, Query: 2*i + 1, Validation: &HoldoutReport{
			Status: "pass_disjoint_windows", Excluded: true, Observations: []*Measurement{
				{Passes: true, QueryTime: 50, Offset: 20},
				{Passes: true, QueryTime: 180, Offset: 20},
				{Passes: true, QueryTime: 310, Offset: 20},
			},
		}})
	}
	if got := adaptiveTimingPairs(policy, records, pairs[:1], holdouts[:1]); got != nil {
		t.Fatalf("small workload used adaptive timing: %v", got)
	}
	if got := adaptiveTimingPairs(policy, records, pairs, nil); got != nil {
		t.Fatalf("unvalidated pairs used adaptive timing: %v", got)
	}
	holdouts[0].Validation.Observations[1].Offset += .020
	got := adaptiveTimingPairs(policy, records, pairs, holdouts)
	if len(got) != len(pairs) || got[0] {
		t.Fatalf("disagreeing holdout did not keep dense timing: %v", got)
	}
	for i, adaptive := range got[1:] {
		if !adaptive {
			t.Fatalf("supported long pair %d did not use adaptive timing", i+1)
		}
	}
}
