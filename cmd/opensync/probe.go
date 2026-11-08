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
)

type probeCacheEntry struct {
	Source string `json:"source"`
	Cache  string `json:"cache"`
}

func probeHypothesis(projectPath, inventoryPath, hypothesisID string) error {
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

	const window = 8.0
	const radius = 0.5
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
		Status       string    `json:"status"`
		HypothesisID string    `json:"hypothesis_id"`
		Reference    string    `json:"reference"`
		Query        string    `json:"query"`
		PredictedS   float64   `json:"predicted_offset_seconds"`
		Probes       []outcome `json:"probes"`
	}{
		Status:       "DIAGNOSTIC_NOT_CERTIFIED",
		HypothesisID: hypothesisID,
		Reference:    h.Reference,
		Query:        h.Query,
		PredictedS:   offset,
	}

	for _, fraction := range []float64{0.10, 0.50, 0.90} {
		qtime := low + (high-low)*fraction
		p := acousticverify.Probe{
			QueryStartNS: int64(math.Round(qtime * 1e9)),
			OffsetNS:     h.OffsetNS,
			WindowNS:     8_000_000_000,
			RadiusNS:     500_000_000,
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
			for _, delta := range []int64{-15, 15} {
				controlOffset := h.OffsetNS + delta*1_000_000_000
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
