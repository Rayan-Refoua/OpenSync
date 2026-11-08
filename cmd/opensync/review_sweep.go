package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SweepReview deliberately does not certify matches. The wrong-offset
// comparisons are controls for competing locations, not independent proof.
type SweepReview struct {
	Status                string           `json:"status"`
	TotalRecordings       int              `json:"total_recordings"`
	Attempted             int              `json:"attempted"`
	Completed             int              `json:"completed"`
	Failed                int              `json:"failed"`
	ControlWarning        int              `json:"control_warning"`
	NoControlWarning      int              `json:"no_control_warning"`
	MissingControlWindows int              `json:"missing_control_windows"`
	Rows                  []SweepReviewRow `json:"rows"`
}

type SweepReviewRow struct {
	HypothesisID           string  `json:"hypothesis_id"`
	Reference              string  `json:"reference"`
	Query                  string  `json:"query"`
	Classification         string  `json:"classification"`
	Failure                string  `json:"failure,omitempty"`
	CompleteWindows        int     `json:"complete_windows"`
	ControlOutperformed    int     `json:"control_outperformed_windows"`
	WindowsMissingControls int     `json:"windows_missing_controls"`
	OffsetSpreadMS         float64 `json:"offset_spread_ms"`
	MedianAbsWaveform      float64 `json:"median_abs_waveform_correlation"`
	MedianMeanSpectral     float64 `json:"median_mean_spectral_correlation"`
	MedianOffsetNS         int64   `json:"median_offset_ns"`
}

type sweepList struct {
	TotalRecordings int `json:"total_recordings"`
	Results         []struct {
		HypothesisID string `json:"hypothesis_id"`
		Reference    string `json:"reference"`
		Query        string `json:"query"`
		Outcome      string `json:"outcome"`
		Report       string `json:"report"`
	} `json:"results"`
}

type sweepProbe struct {
	Reference    string `json:"reference"`
	Query        string `json:"query"`
	HypothesisID string `json:"hypothesis_id"`
	Error        string `json:"error"`
	Probes       []struct {
		Error       string `json:"error"`
		Measurement *struct {
			MeasuredNS  int64   `json:"measured_offset_ns"`
			Correlation float64 `json:"correlation"`
		} `json:"measurement"`
		Spectral *struct {
			Bands []float64 `json:"band_correlation"`
		} `json:"spectral"`
		Controls []struct {
			Error       string  `json:"error"`
			Correlation float64 `json:"correlation"`
		} `json:"wrong_offset_controls"`
	} `json:"probes"`
}

func medianReview(vals []float64) float64 {
	s := append([]float64(nil), vals...)
	sort.Float64s(s)
	if len(s)%2 != 0 {
		return s[len(s)/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}

// reviewSweepFolder examines saved acoustic evidence only. It never opens
// original media and never modifies projects or Premiere placements.
func reviewSweepFolder(folder string) (SweepReview, error) {
	raw, err := os.ReadFile(filepath.Join(folder, "summary.json"))
	if err != nil {
		return SweepReview{}, err
	}
	var summary sweepList
	if err := json.Unmarshal(raw, &summary); err != nil {
		return SweepReview{}, err
	}
	if len(summary.Results) == 0 {
		return SweepReview{}, fmt.Errorf("empty sweep")
	}
	a := SweepReview{Status: "EVIDENCE_TRIAGE_NOT_CERTIFICATION", TotalRecordings: summary.TotalRecordings, Attempted: len(summary.Results), Rows: make([]SweepReviewRow, 0, len(summary.Results))}
	for _, result := range summary.Results {
		// Reports may have been generated on Windows and reviewed on Linux.
		base := filepath.Base(strings.ReplaceAll(result.Report, "\\", "/"))
		if base == "." || base == "" || strings.Contains(base, "..") || !strings.HasPrefix(base, "probe-") || !strings.HasSuffix(base, ".json") {
			return SweepReview{}, fmt.Errorf("unsafe probe report name %q", result.Report)
		}
		data, err := os.ReadFile(filepath.Join(folder, base))
		if err != nil {
			return SweepReview{}, fmt.Errorf("%s: %w", base, err)
		}
		var p sweepProbe
		if err := json.Unmarshal(data, &p); err != nil {
			return SweepReview{}, fmt.Errorf("%s: %w", base, err)
		}
		row := SweepReviewRow{HypothesisID: result.HypothesisID, Reference: result.Reference, Query: result.Query}
		if result.Outcome == "probe_failed" {
			row.Classification = "probe_failed"
			row.Failure = p.Error
			a.Failed++
			a.Rows = append(a.Rows, row)
			continue
		}
		if p.HypothesisID != result.HypothesisID || len(p.Probes) != 3 {
			return SweepReview{}, fmt.Errorf("%s: report identity/windows mismatch", base)
		}
		var offsets []int64
		var wave, spectral []float64
		for _, window := range p.Probes {
			if window.Error != "" || window.Measurement == nil || window.Spectral == nil || len(window.Spectral.Bands) != 3 {
				continue
			}
			m := window.Measurement
			if math.IsNaN(m.Correlation) || math.IsInf(m.Correlation, 0) {
				return SweepReview{}, fmt.Errorf("%s: invalid waveform correlation", base)
			}
			mean := 0.0
			for _, b := range window.Spectral.Bands {
				if math.IsNaN(b) || math.IsInf(b, 0) {
					return SweepReview{}, fmt.Errorf("%s: invalid spectral evidence", base)
				}
				mean += b / 3
			}
			offsets = append(offsets, m.MeasuredNS)
			wave = append(wave, math.Abs(m.Correlation))
			spectral = append(spectral, mean)
			row.CompleteWindows++
			validControls := 0
			controlOutperformed := false
			for _, c := range window.Controls {
				if c.Error != "" {
					continue
				}
				if math.IsNaN(c.Correlation) || math.IsInf(c.Correlation, 0) {
					return SweepReview{}, fmt.Errorf("%s: invalid control", base)
				}
				validControls++
				if math.Abs(c.Correlation) >= math.Abs(m.Correlation) {
					controlOutperformed = true
				}
			}
			if validControls == 0 {
				row.WindowsMissingControls++
				a.MissingControlWindows++
			}
			if controlOutperformed {
				row.ControlOutperformed++
			}
		}
		if len(offsets) == 0 {
			row.Classification = "incomplete_evidence"
		} else {
			sort.Slice(offsets, func(i, j int) bool { return offsets[i] < offsets[j] })
			row.OffsetSpreadMS = float64(offsets[len(offsets)-1]-offsets[0]) / 1e6
			row.MedianOffsetNS = offsets[len(offsets)/2]
			row.MedianAbsWaveform = medianReview(wave)
			row.MedianMeanSpectral = medianReview(spectral)
			// Two or more independently sampled windows failing a relative
			// negative-control comparison warrant explicit review. This is
			// not a waveform magnitude threshold or a verification gate.
			if row.ControlOutperformed >= 2 {
				row.Classification = "negative_control_warning"
				a.ControlWarning++
			} else {
				row.Classification = "requires_independent_validation"
				a.NoControlWarning++
			}
		}
		a.Completed++
		a.Rows = append(a.Rows, row)
	}
	// Give potential false matches a stable, visible place at the top.
	sort.SliceStable(a.Rows, func(i, j int) bool {
		x, y := a.Rows[i], a.Rows[j]
		order := func(c string) int {
			switch c {
			case "negative_control_warning":
				return 0
			case "probe_failed":
				return 1
			case "incomplete_evidence":
				return 2
			default:
				return 3
			}
		}
		if order(x.Classification) != order(y.Classification) {
			return order(x.Classification) < order(y.Classification)
		}
		if x.ControlOutperformed != y.ControlOutperformed {
			return x.ControlOutperformed > y.ControlOutperformed
		}
		return x.OffsetSpreadMS > y.OffsetSpreadMS
	})
	return a, nil
}

func reviewSweepCommand(folder, output string) error {
	a, err := reviewSweepFolder(folder)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	f, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err != nil {
		f.Close()
		os.Remove(output)
		return err
	}
	if err = f.Close(); err != nil {
		os.Remove(output)
		return err
	}
	fmt.Println("STATUS:", a.Status)
	fmt.Println("ATTEMPTED:", a.Attempted)
	fmt.Println("COMPLETE:", a.Completed)
	fmt.Println("CONTROL_WARNINGS:", a.ControlWarning)
	fmt.Println("REQUIRES_VALIDATION:", a.NoControlWarning)
	fmt.Println("PROBE_FAILURES:", a.Failed)
	fmt.Println("OUTPUT:", output)
	return nil
}
