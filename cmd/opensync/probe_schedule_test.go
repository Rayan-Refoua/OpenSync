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
