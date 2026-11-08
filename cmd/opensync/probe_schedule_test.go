package main

import (
	"math"
	"testing"

	"opensync/internal/quality"
)

func TestPreReservedValidationAcrossDurationBudgets(t *testing.T) {
	preset, err := quality.Parse("rock-solid")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		max         float64
		train, held int
	}{
		{7, 1, 0}, {16, 2, 0}, {25, 2, 1}, {33.5, 2, 2}, {42, 2, 3}, {110, 10, 3},
	} {
		got := planReservedWindows(0, tc.max, preset)
		if len(got.Training) != tc.train || len(got.Holdout) != tc.held {
			t.Fatalf("max %.2f got train=%d hold=%d capacity=%d", tc.max, len(got.Training), len(got.Holdout), got.Capacity)
		}
		all := append(append([]float64{}, got.Training...), got.Holdout...)
		for _, v := range all {
			if v < 0 || v > tc.max+1e-6 {
				t.Fatalf("invalid start %v", v)
			}
			for _, u := range all {
				if math.Abs(v-u) > 1e-9 && math.Abs(v-u) < 8.5-1e-6 {
					t.Fatalf("overlapping schedule %v", all)
				}
			}
		}
	}
}
func TestPresetsPreserveTrainingAndHoldoutSeparation(t *testing.T) {
	for _, mode := range []string{"draft", "normal", "rock-solid"} {
		preset, err := quality.Parse(mode)
		if err != nil {
			t.Fatal(err)
		}
		got := planReservedWindows(4, 260, preset)
		if len(got.Training) != len(preset.ProbeFractions) || len(got.Holdout) != preset.HeldOutChecks {
			t.Fatalf("%s wrong allocation %+v", mode, got)
		}
		for _, h := range got.Holdout {
			for _, tr := range got.Training {
				if math.Abs(h-tr) < float64(preset.WindowNS)/1e9+0.5-1e-6 {
					t.Fatalf("%s validation overlaps training %.3f vs %.3f", mode, h, tr)
				}
			}
		}
	}
}

func TestAdaptiveWindowSelectsShorterForBriefOverlap(t *testing.T){
 preset,err:=quality.Parse("rock-solid")
 if err!=nil{t.Fatal(err)}
 // Predicted overlap supports ~17 seconds of query audio. Eight second
 // windows only give two slots, while 2s reserves training + 3 holdouts.
 result,ok:=chooseAdaptiveSchedule(18*16000,18*16000,0,preset)
 if !ok{t.Fatal("short overlap not investigable")}
 if result.WindowNS!=2_000_000_000{t.Fatalf("expected 2s window, got %d",result.WindowNS)}
 if len(result.Plan.Training)<2||len(result.Plan.Holdout)!=3{
  t.Fatalf("failed to reserve short-overlap evidence: %+v",result.Plan)
 }
}
func TestAdaptiveWindowRetainsEightSecondsWhenPossible(t *testing.T){
 preset,_:=quality.Parse("rock-solid")
 result,ok:=chooseAdaptiveSchedule(460*16000,460*16000,0,preset)
 if !ok||result.WindowNS!=8_000_000_000||len(result.Plan.Holdout)!=3{
  t.Fatalf("should preserve longest valid window: %+v",result)
 }
}
func TestAdaptiveWindowInsufficientOverlapNeverCertifies(t *testing.T){
 preset,_:=quality.Parse("rock-solid")
 _,ok:=chooseAdaptiveSchedule(3*16000,3*16000,0,preset)
 if ok{t.Fatal("accepted a recording without two disjoint training events")}
}
func TestAdaptiveQualityBudget(t *testing.T){
 for _,mode:=range []string{"draft","normal","rock-solid"}{
  preset,_:=quality.Parse(mode)
  result,ok:=chooseAdaptiveSchedule(40*16000,40*16000,0,preset)
  if !ok{t.Fatalf("%s failed to allocate",mode)}
  if len(result.Plan.Holdout)>preset.HeldOutChecks{t.Fatalf("%s exceeded heldout budget",mode)}
  if len(result.Plan.Training)<2{t.Fatalf("%s insufficient training",mode)}
 }
}
