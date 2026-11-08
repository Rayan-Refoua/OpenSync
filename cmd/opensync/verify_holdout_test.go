package main

import (
	"math"
	"testing"

	"opensync/internal/acousticverify"
	"opensync/internal/quality"
)

func TestHeldoutDisjointWindows(t *testing.T) {
	training := []TrainProbe{}
	for _, seconds := range []float64{10, 35, 60, 85} {
		training = append(training, TrainProbe{QuerySeconds: seconds, Measurement: &acousticverify.Measurement{
			QueryStartNS: int64(seconds * 1e9), WindowNS: 8_000_000_000, SearchRadiusNS: 500_000_000,
		}})
	}
	holds := disjointHoldouts(training, 8_000_000_000, 0, 110, 3)
	if len(holds) != 3 {
		t.Fatalf("expected three disjoint events: %v", holds)
	}
	for _, h := range holds {
		if h < 0 || h+8 > 110 {
			t.Fatalf("out of range: %v", h)
		}
		for _, p := range training {
			s := float64(p.Measurement.QueryStartNS) / 1e9
			if h < s+8.5 && s < h+8.5 {
				t.Fatalf("holdout %v overlaps train %v", h, s)
			}
		}
	}
}
func TestTimingModelUsesTrainingOnlyAndPenalizesComplexity(t *testing.T) {
	constant := []timeOffset{{0, 1200000000}, {10, 1200100000}, {20, 1200000000}, {30, 1200100000}}
	model, err := fitTiming(constant, quality.RockSolid)
	if err != nil {
		t.Fatal(err)
	}
	if model.name != "constant" {
		t.Fatalf("overfitted harmless jitter: %+v", model)
	}
	affine := []timeOffset{{0, 1000000000}, {10, 1002000000}, {20, 1004000000}, {30, 1006000000}}
	fit, err := fitTiming(affine, quality.RockSolid)
	if err != nil {
		t.Fatal(err)
	}
	if fit.name != "affine" || math.Abs(fit.slope-200000) > 0.1 {
		t.Fatalf("failed to detect training drift: %+v", fit)
	}
	draft, err := fitTiming(affine, quality.Draft)
	if err != nil {
		t.Fatal(err)
	}
	if draft.name != "constant" {
		t.Fatalf("draft should be constant diagnostics: %+v", draft)
	}
}
func TestHoldoutRequiresEnoughTrainingEvidence(t *testing.T) {
	if _, err := fitTiming([]timeOffset{{t: 2, offset: 1e9}}, quality.RockSolid); err == nil {
		t.Fatal("accepted one-window timing model")
	}
	if got := disjointHoldouts([]TrainProbe{{Measurement: &acousticverify.Measurement{QueryStartNS: 0}}}, 8_000_000_000, 0, 8, 3); len(got) > 0 {
		t.Fatalf("overlapping holdout should not be selected: %v", got)
	}
}

func TestFailedTrainingQueryStillExcludedFromHoldout(t *testing.T) {
	training := []TrainProbe{
		{QuerySeconds: 10, Measurement: &acousticverify.Measurement{QueryStartNS: 10_000_000_000}},
		{QuerySeconds: 30, Error: "failed acoustic probe"},
		{QuerySeconds: 50, Measurement: &acousticverify.Measurement{QueryStartNS: 50_000_000_000}},
	}
	holds := disjointHoldouts(training, 8_000_000_000, 0, 70, 3)
	for _, h := range holds {
		if h < 38.5 && h+8.5 > 30 {
			t.Fatalf("holdout %v overlaps failed training query at 30s", h)
		}
	}
}

func TestHoldoutCapacityRespectsFailedAndAdjacentTraining(t *testing.T) {
	training := []TrainProbe{
		{QuerySeconds: 10}, {QuerySeconds: 20}, {QuerySeconds: 30},
	}
	none := disjointHoldoutCandidates(training, 8_000_000_000, 10, 38)
	if len(none) != 0 {
		t.Fatalf("unexpected heldout from densely occupied region: %v", none)
	}
	available := disjointHoldoutCandidates(training, 8_000_000_000, 0, 65)
	for i, start := range available {
		if start < 0 || start+8 > 65 {
			t.Fatalf("out of bounds: %.3f", start)
		}
		if i > 0 && start < available[i-1]+8.5 {
			t.Fatalf("overlapping holdouts: %v", available)
		}
		for _, p := range training {
			if start < p.QuerySeconds+8.5 && p.QuerySeconds < start+8.5 {
				t.Fatalf("training leaked into holdout: %.3f", start)
			}
		}
	}
	got := disjointHoldouts(training, 8_000_000_000, 0, 65, 3)
	if len(got) > 3 || len(got) > len(available) {
		t.Fatalf("wrong holdout budget: %v", got)
	}
}
