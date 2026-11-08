package main

import (
	"math"
	"sort"

	"opensync/internal/quality"
)

type reservedSchedule struct {
	Training []float64
	Holdout  []float64
	Capacity int
}

// planReservedWindows allocates non-overlapping acoustic regions before
// inspecting their content. It uses the entire valid predicted-overlap range,
// allowing at least two training events before reserving other slots.
// Neither measurements nor correlations are consulted when choosing holdouts.
// A short source may simply have insufficient room, and is never certified.
func planReservedWindows(low, high float64, preset quality.Preset) reservedSchedule {
	result := reservedSchedule{Training: []float64{}, Holdout: []float64{}}
	window := float64(preset.WindowNS) / 1e9
	if window <= 0 || high < low || math.IsNaN(low) || math.IsNaN(high) {
		return result
	}
	const guard = 0.5
	starts := []float64{}
	for t := low; t <= high+1e-8; t += window + guard {
		starts = append(starts, t)
		if len(starts) > 100000 {
			break
		}
	}
	result.Capacity = len(starts)
	if len(starts) == 0 {
		return result
	}
	trainBudget := len(preset.ProbeFractions)
	if trainBudget < 2 {
		trainBudget = 2
	}
	holdBudget := preset.HeldOutChecks
	if len(starts) < 2 {
		holdBudget = 0
	} else if holdBudget > len(starts)-2 {
		holdBudget = len(starts) - 2
	}
	if holdBudget < 0 {
		holdBudget = 0
	}
	// Spread reserved slots over the full overlap; prefer interior gaps
	// when ample room exists to preserve training coverage at both ends.
	reserved := map[int]bool{}
	for i := 0; i < holdBudget; i++ {
		idx := int(math.Floor((float64(i) + 0.5) * float64(len(starts)) / float64(holdBudget)))
		if idx >= len(starts) {
			idx = len(starts) - 1
		}
		for reserved[idx] {
			idx = (idx + 1) % len(starts)
		}
		reserved[idx] = true
	}
	remaining := []float64{}
	for idx, t := range starts {
		if reserved[idx] {
			result.Holdout = append(result.Holdout, t)
		} else {
			remaining = append(remaining, t)
		}
	}
	sort.Float64s(result.Holdout)
	if len(remaining) <= trainBudget {
		result.Training = remaining
		return result
	}
	// Uniform sample: no repeated regions and no holdout reuse.
	for i := 0; i < trainBudget; i++ {
		idx := int(math.Floor((float64(i) + 0.5) * float64(len(remaining)) / float64(trainBudget)))
		if idx >= len(remaining) {
			idx = len(remaining) - 1
		}
		result.Training = append(result.Training, remaining[idx])
	}
	return result
}

type adaptiveSchedule struct {
	WindowNS int64
	Low float64
	High float64
	Plan reservedSchedule
}

// chooseAdaptiveSchedule examines only clip durations and the proposed offset.
// It does not inspect audio samples, correlation or verification residuals.
// Prefer the longest window providing the preset's requested independent
// holdouts alongside at least two training regions; otherwise retain the
// longest usable window with the greatest available validation budget.
func chooseAdaptiveSchedule(queryFrames, refFrames int64, offsetNS int64, preset quality.Preset) (adaptiveSchedule, bool) {
	offset:=float64(offsetNS)/1e9
	radius:=float64(preset.SearchRadiusNS)/1e9
	candidates:=[]int64{preset.WindowNS,4_000_000_000,2_000_000_000}
	var best adaptiveSchedule
	for _,windowNS:=range candidates {
		if windowNS>preset.WindowNS||windowNS<=0{continue}
		window:=float64(windowNS)/1e9
		low:=math.Max(0,radius-offset)+0.25
		high:=math.Min(float64(queryFrames)/16000-window,
			float64(refFrames)/16000-offset-window-radius)-0.25
		p:=preset
		p.WindowNS=windowNS
		plan:=planReservedWindows(low,high,p)
		if len(plan.Training)<2{continue}
		candidate:=adaptiveSchedule{WindowNS:windowNS,Low:low,High:high,Plan:plan}
		if best.WindowNS==0 {best=candidate}
		if len(plan.Holdout)>=preset.HeldOutChecks{return candidate,true}
		if len(plan.Holdout)>len(best.Plan.Holdout) {best=candidate}
	}
	return best,best.WindowNS!=0
}
