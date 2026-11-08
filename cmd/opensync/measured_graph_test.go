package main

import (
	"testing"

	"opensync/internal/acousticverify"
)

func TestMeasuredGraphReportRequiresHeldoutIndependentWindows(t *testing.T) {
	train := TrainingReport{HypothesisID: "h1", Reference: "a", Query: "b",
		QualityMode: "rock-solid", Schedule: "preallocated_disjoint_v1",
		ReservedHoldoutQuerySeconds: []float64{10, 20},
		Probes:                      []TrainProbe{{QuerySeconds: 0}},
	}
	hold := HoldoutReport{Status: "HELDOUT_DIAGNOSTIC_NOT_GRAPH_CERTIFIED",
		HypothesisID: "h1", Mode: "rock-solid", Model: "constant",
		GraphCertification:   "not_checked",
		ValidationAllocation: "preallocated_disjoint_v1",
		EffectiveWindowNS:    2e9, HoldoutsRequested: 2, HoldoutsTested: 2,
		HoldoutsAvailable: 2, ValidHoldouts: 2,
		Windows: []HoldoutWindow{
			{QueryStartNS: 10e9, MeasuredOffsetNS: 52711000000, ValidControls: 2,
				Spectral: &acousticverify.SpectralEvidence{BandCorrelation: [3]float64{0.1, 0.2, 0.3}}},
			{QueryStartNS: 20e9, MeasuredOffsetNS: 52711500000, ValidControls: 2},
		}}
	c, err := candidateFromReports(train, hold, "holdout.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Events) != 2 || len(c.Events[0].BandCorrelation) != 3 || len(c.Events[1].BandCorrelation) != 0 {
		t.Fatalf("band/event provenance wrong: %+v", c.Events)
	}
	train.Probes = append(train.Probes, TrainProbe{QuerySeconds: 10})
	if _, err := candidateFromReports(train, hold, "holdout.json"); err == nil {
		t.Fatal("reused training event accepted as independent holdout")
	}
	train.Probes = train.Probes[:1]
	hold.Windows[0].ValidControls = 0
	if _, err := candidateFromReports(train, hold, "holdout.json"); err == nil {
		t.Fatal("unchecked negative controls accepted")
	}
	hold.Windows[0].ValidControls = 2
	hold.Model = "affine"
	c, err = candidateFromReports(train, hold, "holdout.json")
	if err != nil || c.Model != "affine" {
		t.Fatalf("affine evidence lost: %+v %v", c, err)
	}
}
