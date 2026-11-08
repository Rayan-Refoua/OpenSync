package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"

	"opensync/internal/projectstate"
)

// Diagnostic probe reports are retained separately from the project.
// They are measurements, not certified acoustic correspondences.
type measuredReport struct {
	Status       string `json:"status"`
	HypothesisID string `json:"hypothesis_id"`
	Reference    string `json:"reference"`
	Query        string `json:"query"`
	Probes       []struct {
		Error       string `json:"error"`
		Measurement *struct {
			QueryStartNS int64   `json:"query_start_ns"`
			ReferenceNS  int64   `json:"reference_ns"`
			MeasuredNS   int64   `json:"measured_offset_ns"`
			Correlation  float64 `json:"correlation"`
			WindowNS     int64   `json:"window_ns"`
			Method       string  `json:"method"`
		} `json:"measurement"`
		Spectral *struct {
			Bands  [3]float64 `json:"band_correlation"`
			Method string     `json:"method"`
		} `json:"spectral"`
		Controls []struct {
			Delta       float64 `json:"offset_delta_seconds"`
			Correlation float64 `json:"correlation"`
			Error       string  `json:"error"`
		} `json:"wrong_offset_controls"`
	} `json:"probes"`
}

func exportReport(templatePath, projectPath, reportPath, outputPath string) error {
	project, err := projectstate.Load(projectPath)
	if err != nil {
		return err
	}

	data, err := os.ReadFile(reportPath)
	if err != nil {
		return err
	}

	var r measuredReport
	if err := json.Unmarshal(data, &r); err != nil {
		return err
	}
	h, exists := project.Hypotheses[r.HypothesisID]
	if !exists || h.Status == projectstate.Rejected ||
		r.Reference != h.Reference || r.Query != h.Query ||
		r.Status != "DIAGNOSTIC_NOT_CERTIFIED" {
		return fmt.Errorf("probe report does not match an eligible project hypothesis")
	}
	if len(r.Probes) < 3 {
		return fmt.Errorf("insufficient probe measurements")
	}

	offsets := make([]int64, 0, len(r.Probes))
	var previousQuery int64 = -1

	for i, probe := range r.Probes {
		m := probe.Measurement
		if probe.Error != "" || m == nil || probe.Spectral == nil ||
			m.WindowNS <= 0 || m.Method == "" ||
			probe.Spectral.Method == "" ||
			m.QueryStartNS <= previousQuery ||
			len(probe.Controls) != 2 ||
			m.ReferenceNS-m.QueryStartNS != m.MeasuredNS ||
			!finiteReport(m.Correlation) {
			return fmt.Errorf("incomplete or inconsistent probe %d", i)
		}

		ref := project.Recordings[h.Reference]
		query := project.Recordings[h.Query]
		if m.QueryStartNS < 0 || m.ReferenceNS < 0 ||
			m.QueryStartNS > query.DurationNS ||
			m.ReferenceNS > ref.DurationNS ||
			m.WindowNS > query.DurationNS-m.QueryStartNS ||
			m.WindowNS > ref.DurationNS-m.ReferenceNS {
			return fmt.Errorf("probe %d outside source recording", i)
		}

		for _, value := range probe.Spectral.Bands {
			if !finiteReport(value) || value < -1 || value > 1 {
				return fmt.Errorf("invalid spectral correlation in probe %d", i)
			}
		}

		for _, control := range probe.Controls {
			if control.Error != "" || !finiteReport(control.Delta) ||
				!finiteReport(control.Correlation) ||
				control.Correlation < -1 || control.Correlation > 1 {
				return fmt.Errorf("invalid negative control in probe %d", i)
			}
		}

		previousQuery = m.QueryStartNS
		offsets = append(offsets, m.MeasuredNS)
	}

	sort.Slice(offsets, func(i, j int) bool { return offsets[i] < offsets[j] })
	median := offsets[len(offsets)/2]
	if len(offsets)%2 == 0 {
		a, b := offsets[len(offsets)/2-1], offsets[len(offsets)/2]
		median = a/2 + b/2 + (a%2+b%2)/2
	}

	// This is a diagnostic estimate, not a clock-drift certification.
	// No arbitrary correlation threshold or graph admission occurs.
	if err := exportMeasured(
		templatePath, projectPath, r.HypothesisID,
		strconv.FormatInt(median, 10), outputPath,
	); err != nil {
		return err
	}

	fmt.Printf("PROBE_COUNT: %d\n", len(offsets))
	fmt.Printf("OFFSET_SPREAD_MS: %.3f\n",
		float64(offsets[len(offsets)-1]-offsets[0])/1e6)
	fmt.Println("SOURCE: SAVED_DIAGNOSTIC_PROBE_REPORT")
	return nil
}

func finiteReport(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
