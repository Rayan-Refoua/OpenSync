package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"

	"opensync/internal/acousticverify"
	"opensync/internal/audio"
	"opensync/internal/projectstate"
	"opensync/internal/quality"
)

type probeCacheEntry struct {
	Source string `json:"source"`
	Cache  string `json:"cache"`
}

func probeHypothesis(projectPath, inventoryPath, hypothesisID string) error {
	return probeHypothesisPreset(projectPath, inventoryPath, hypothesisID, "normal")
}

func probeHypothesisPreset(projectPath, inventoryPath, hypothesisID, mode string) error {
	return probeHypothesisScheduled(projectPath, inventoryPath, hypothesisID, mode, false)
}

func probeHypothesisScheduled(projectPath, inventoryPath, hypothesisID, mode string, reserve bool) error {
	preset, err := quality.Parse(mode)
	if err != nil {
		return err
	}
	project, err := projectstate.Load(projectPath)
	if err != nil {
		return err
	}
	h, ok := project.Hypotheses[hypothesisID]
	if !ok {
		return fmt.Errorf("unknown hypothesis %q", hypothesisID)
	}
	if h.Status == projectstate.Rejected {
		return fmt.Errorf("hypothesis is rejected")
	}

	raw, err := os.ReadFile(inventoryPath)
	if err != nil {
		return err
	}
	var inventory map[string]probeCacheEntry
	if err := json.Unmarshal(raw, &inventory); err != nil {
		return err
	}

	openCache := func(id string) (*audio.Reader, error) {
		entry, ok := inventory[id]
		if !ok || entry.Cache == "" {
			return nil, fmt.Errorf("missing cache for %s", id)
		}
		source := project.Recordings[id].SourcePath
		normalize := func(s string) string {
			return strings.ToLower(strings.ReplaceAll(s, "\\", "/"))
		}
		if normalize(source) != normalize(entry.Source) {
			return nil, fmt.Errorf("source identity mismatch for %s", id)
		}
		return audio.Open(entry.Cache)
	}

	ref, err := openCache(h.Reference)
	if err != nil {
		return err
	}
	defer ref.Close()
	query, err := openCache(h.Query)
	if err != nil {
		return err
	}
	defer query.Close()

	window := float64(preset.WindowNS) / 1e9
	radius := float64(preset.SearchRadiusNS) / 1e9
	offset := float64(h.OffsetNS) / 1e9

	// Every probe must fit inside both recordings, including search margin.
	low := math.Max(0, radius-offset)
	high := math.Min(
		float64(query.Frames())/16000-window,
		float64(ref.Frames())/16000-offset-window-radius,
	)
	low += 0.25
	high -= 0.25
	if high < low {
		return fmt.Errorf("insufficient predicted overlap for bounded probes")
	}

	type controlResult struct {
		OffsetDeltaSeconds float64 `json:"offset_delta_seconds"`
		MeasuredOffsetNS   int64   `json:"measured_offset_ns,omitempty"`
		Correlation        float64 `json:"correlation,omitempty"`
		Error              string  `json:"error,omitempty"`
	}

	type outcome struct {
		QuerySeconds     float64                          `json:"query_seconds"`
		Measurement      *acousticverify.Measurement      `json:"measurement,omitempty"`
		OffsetResidualMS float64                          `json:"offset_residual_ms,omitempty"`
		Spectral         *acousticverify.SpectralEvidence `json:"spectral,omitempty"`
		ControlResults   []controlResult                  `json:"wrong_offset_controls,omitempty"`
		Error            string                           `json:"error,omitempty"`
	}

	result := struct {
		Status                      string    `json:"status"`
		HypothesisID                string    `json:"hypothesis_id"`
		Reference                   string    `json:"reference"`
		Query                       string    `json:"query"`
		PredictedS                  float64   `json:"predicted_offset_seconds"`
		QualityMode                 string    `json:"quality_mode"`
		HeldOutStatus               string    `json:"held_out_status"`
		Schedule                    string    `json:"schedule,omitempty"`
		ReservedHoldoutQuerySeconds []float64 `json:"reserved_holdout_query_seconds,omitempty"`
		AvailableDisjointRegions    int       `json:"available_disjoint_regions,omitempty"`
		Probes                      []outcome `json:"probes"`
	}{
		Status:        "DIAGNOSTIC_NOT_CERTIFIED",
		HypothesisID:  hypothesisID,
		Reference:     h.Reference,
		Query:         h.Query,
		PredictedS:    offset,
		QualityMode:   string(preset.Mode),
		HeldOutStatus: "not_yet_independently_verified",
	}

	queryTimes := make([]float64, 0, len(preset.ProbeFractions))
	if reserve {
		allocation := planReservedWindows(low, high, preset)
		queryTimes = allocation.Training
		result.Schedule = "preallocated_disjoint_v1"
		result.ReservedHoldoutQuerySeconds = allocation.Holdout
		result.AvailableDisjointRegions = allocation.Capacity
		if len(queryTimes) < 2 {
			return fmt.Errorf("insufficient disjoint training windows: %d", len(queryTimes))
		}
	} else {
		for _, fraction := range preset.ProbeFractions {
			queryTimes = append(queryTimes, low+(high-low)*fraction)
		}
	}
	for _, qtime := range queryTimes {
		p := acousticverify.Probe{
			QueryStartNS: int64(math.Round(qtime * 1e9)),
			OffsetNS:     h.OffsetNS,
			WindowNS:     preset.WindowNS,
			RadiusNS:     preset.SearchRadiusNS,
		}
		m, err := acousticverify.Measure(ref, query, p)
		entry := outcome{QuerySeconds: qtime}
		if err != nil {
			entry.Error = err.Error()
		} else {
			entry.Measurement = &m
			spectral, spectralErr := acousticverify.Corroborate(ref, query, m)
			if spectralErr != nil {
				entry.Error = "spectral: " + spectralErr.Error()
			} else {
				entry.Spectral = &spectral
			}
			entry.OffsetResidualMS = float64(m.MeasuredNS-h.OffsetNS) / 1e6

			// Deliberately wrong offsets: same query event, same
			// window and search radius, different reference regions.
			// These are negative controls, not independent holdouts.
			for _, controlOffset := range quality.ValidControls(ref.Frames(), p.QueryStartNS, h.OffsetNS, p.WindowNS, p.RadiusNS, preset.ControlOffsetsSeconds) {
				delta := (controlOffset - h.OffsetNS) / 1_000_000_000
				control := acousticverify.Probe{
					QueryStartNS: p.QueryStartNS,
					OffsetNS:     controlOffset,
					WindowNS:     p.WindowNS,
					RadiusNS:     p.RadiusNS,
				}
				cm, controlErr := acousticverify.Measure(ref, query, control)
				c := controlResult{
					OffsetDeltaSeconds: float64(delta),
				}
				if controlErr != nil {
					c.Error = controlErr.Error()
				} else {
					c.MeasuredOffsetNS = cm.MeasuredNS
					c.Correlation = cm.Correlation
				}
				entry.ControlResults = append(entry.ControlResults, c)
			}
		}
		result.Probes = append(result.Probes, entry)
	}

	return json.NewEncoder(os.Stdout).Encode(result)
}
