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
