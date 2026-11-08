package quality

import (
	"fmt"
	"strings"
)

// Mode controls the amount of acoustic investigation, not whether an
// unsupported match can be treated as verified.
type Mode string

const (
	Draft Mode = "draft"
	Normal Mode = "normal"
	RockSolid Mode = "rock-solid"
)

type Preset struct {
	Mode Mode `json:"mode"`
	Label string `json:"label"`
	Description string `json:"description"`
	ProbeFractions []float64 `json:"probe_fractions"`
	SearchRadiusNS int64 `json:"search_radius_ns"`
	WindowNS int64 `json:"window_ns"`
	ControlOffsetsSeconds []int64 `json:"control_offsets_seconds"`
	HeldOutChecks int `json:"held_out_checks"`
	DriftModel string `json:"drift_model"`
}

func Parse(input string) (Preset, error) {
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "draft":
		return Preset{
			Mode: Draft, Label: "Draft",
			Description: "Fast approximate discovery, sparse checks, unresolved matches retained",
			ProbeFractions: []float64{0.15, 0.50, 0.85},
			SearchRadiusNS: 500_000_000, WindowNS: 8_000_000_000,
			ControlOffsetsSeconds: []int64{-15, 15, -5, 5},
			HeldOutChecks: 0, DriftModel: "constant_diagnostic",
		}, nil
	case "normal", "standard":
		return Preset{
			Mode: Normal, Label: "Normal",
			Description: "Adaptive matching with broader event sampling and basic drift investigation",
			ProbeFractions: []float64{0.10, 0.30, 0.50, 0.70, 0.90},
			SearchRadiusNS: 500_000_000, WindowNS: 8_000_000_000,
			ControlOffsetsSeconds: []int64{-15, 15, -5, 5, -30, 30},
			HeldOutChecks: 1, DriftModel: "constant_and_affine_candidate",
		}, nil
	case "rock-solid", "rocksolid", "sock-solid":
		return Preset{
			Mode: RockSolid, Label: "Rock Solid",
			Description: "Dense validation, held-out evidence, competing offsets and nonlinear drift investigation",
			ProbeFractions: []float64{0.05, 0.15, 0.25, 0.35, 0.45, 0.55, 0.65, 0.75, 0.85, 0.95},
			SearchRadiusNS: 500_000_000, WindowNS: 8_000_000_000,
			ControlOffsetsSeconds: []int64{-15, 15, -5, 5, -30, 30, -60, 60},
			HeldOutChecks: 3, DriftModel: "constant_affine_smooth_piecewise_candidates",
		}, nil
	default:
		return Preset{}, fmt.Errorf("unknown quality mode %q (draft, normal, rock-solid)", input)
	}
}

// ValidControls selects alternative offsets that fit the same query window
// and both bounded search margins inside the reference recording. It does
// not silently interpret an unavailable control as evidence for a match.
func ValidControls(referenceFrames int64, queryStartNS int64, predictedOffsetNS int64, windowNS int64, radiusNS int64, offsetsSeconds []int64) []int64 {
	const sampleRate int64 = 16000
	toFrames := func(ns int64) int64 { return (ns*sampleRate + 500_000_000) / 1_000_000_000 }
	qstart := toFrames(queryStartNS)
	window := toFrames(windowNS)
	radius := toFrames(radiusNS)
	result := make([]int64, 0, 2)
	for _, delta := range offsetsSeconds {
		offset := predictedOffsetNS + delta*1_000_000_000
		expected := qstart + toFrames(offset)
		if expected-radius < 0 || expected+window+radius+32 >= referenceFrames {
			continue
		}
		result = append(result, offset)
		if len(result) == 2 {
			break
		}
	}
	return result
}
