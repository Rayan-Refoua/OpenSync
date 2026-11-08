package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"opensync/internal/projectstate"
	"opensync/internal/quality"
)

// Recovery tries multiple discovery hypotheses, not merely multiple windows
// around the same offset. Reports are diagnostic and must not be promoted to
// verified graph edges without independent holdout and cycle evidence.
type Recovery struct {
	Status       string          `json:"status"`
	Mode         string          `json:"mode"`
	Recording    string          `json:"recording"`
	Candidates   int             `json:"candidates"`
	Investigated int             `json:"investigated"`
	Results      []RecoveryEntry `json:"results"`
}

type RecoveryEntry struct {
	HypothesisID      string  `json:"hypothesis_id"`
	Reference         string  `json:"reference"`
	Query             string  `json:"query"`
	PredictedOffsetNS int64   `json:"predicted_offset_ns"`
	Outcome           string  `json:"outcome"`
	Error             string  `json:"error,omitempty"`
	Windows           int     `json:"windows"`
	ControlWins       int     `json:"control_wins"`
	WindowsNoControls int     `json:"windows_no_controls"`
	OffsetSpreadMS    float64 `json:"offset_spread_ms"`
	MedianAbsWaveform float64 `json:"median_abs_waveform"`
	Report            string  `json:"report"`
}

type recoveryProbe struct {
	Probes []struct {
		Error       string `json:"error"`
		Measurement *struct {
			Offset      int64   `json:"measured_offset_ns"`
			Correlation float64 `json:"correlation"`
		} `json:"measurement"`
		Controls []struct {
			Error       string  `json:"error"`
			Correlation float64 `json:"correlation"`
		} `json:"wrong_offset_controls"`
	} `json:"probes"`
}

func evidenceRecovery(data []byte, entry *RecoveryEntry) error {
	var probe recoveryProbe
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	if len(probe.Probes) == 0 {
		return fmt.Errorf("empty acoustic probe result")
	}
	offsets := make([]int64, 0, len(probe.Probes))
	waves := make([]float64, 0, len(probe.Probes))
	for _, p := range probe.Probes {
		if p.Error != "" || p.Measurement == nil {
			continue
		}
		m := p.Measurement
		if math.IsNaN(m.Correlation) || math.IsInf(m.Correlation, 0) {
			return fmt.Errorf("nonfinite correlation")
		}
		entry.Windows++
		offsets = append(offsets, m.Offset)
		waves = append(waves, math.Abs(m.Correlation))
		available := 0
		won := false
		for _, c := range p.Controls {
			if c.Error != "" {
				continue
			}
			if math.IsNaN(c.Correlation) || math.IsInf(c.Correlation, 0) {
				return fmt.Errorf("nonfinite control")
			}
			available++
			if math.Abs(c.Correlation) >= math.Abs(m.Correlation) {
				won = true
			}
		}
		if available == 0 {
			entry.WindowsNoControls++
		}
		if won {
			entry.ControlWins++
		}
	}
	if entry.Windows == 0 {
		return fmt.Errorf("no successful acoustic measurement")
	}
	sort.Slice(offsets, func(i, j int) bool { return offsets[i] < offsets[j] })
	sort.Float64s(waves)
	entry.OffsetSpreadMS = float64(offsets[len(offsets)-1]-offsets[0]) / 1e6
	entry.MedianAbsWaveform = waves[len(waves)/2]
	if entry.ControlWins*2 >= entry.Windows && entry.ControlWins > 0 {
		entry.Outcome = "control_dominated"
	} else if entry.WindowsNoControls == entry.Windows {
		entry.Outcome = "controls_unavailable"
	} else {
		entry.Outcome = "candidate_needs_holdout"
	}
	return nil
}

// candidate ordering uses discovery information only as a work scheduler.
// A higher discovery vote count is not proof of correctness.
func recoveryCandidates(p *projectstate.Project, filename string) ([]projectstate.Hypothesis, error) {
	var matches []string
	for id, r := range p.Recordings {
		if strings.EqualFold(filepath.Base(strings.ReplaceAll(r.SourcePath, "\\", "/")), filename) || id == filename {
			matches = append(matches, id)
		}
	}
	if len(matches) != 1 {
		return nil, fmt.Errorf("recording %q matches %d project entries", filename, len(matches))
	}
	id := matches[0]
	candidates := make([]projectstate.Hypothesis, 0)
	for _, h := range p.Hypotheses {
		if h.Reference != id && h.Query != id || h.Status == projectstate.Rejected {
			continue
		}
		candidates = append(candidates, h)
	}
	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		var aw, bw, av, bv int
		if a.Discovery != nil {
			aw = a.Discovery.SupportWindows
			av = a.Discovery.Votes
		}
		if b.Discovery != nil {
			bw = b.Discovery.SupportWindows
			bv = b.Discovery.Votes
		}
		if aw != bw {
			return aw > bw
		}
		if av != bv {
			return av > bv
		}
		return a.ID < b.ID
	})
	return candidates, nil
}

func recoveryLimit(mode quality.Mode) int {
	switch mode {
	case quality.Draft:
		return 3
	case quality.Normal:
		return 12
	default:
		return 40
	}
}

func recoverRecording(projectPath, inventoryPath, filename, mode, outDir string) error {
	preset, err := quality.Parse(mode)
	if err != nil {
		return err
	}
	p, err := projectstate.Load(projectPath)
	if err != nil {
		return err
	}
	candidates, err := recoveryCandidates(p, filename)
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		return fmt.Errorf("no discovery hypothesis for %q", filename)
	}
	if err := os.Mkdir(outDir, 0700); err != nil {
		return fmt.Errorf("output directory must be new: %w", err)
	}
	report := Recovery{Status: "ALTERNATIVES_UNVERIFIED", Mode: string(preset.Mode), Recording: filename, Candidates: len(candidates), Results: []RecoveryEntry{}}
	limit := recoveryLimit(preset.Mode)
	if len(candidates) < limit {
		limit = len(candidates)
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	for i, h := range candidates[:limit] {
		entry := RecoveryEntry{HypothesisID: h.ID, Reference: h.Reference, Query: h.Query, PredictedOffsetNS: h.OffsetNS}
		entry.Report = fmt.Sprintf("candidate-%03d.json", i+1)
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		command := exec.CommandContext(ctx, executable, "probe", projectPath, inventoryPath, h.ID, string(preset.Mode))
		var stderr bytes.Buffer
		command.Stderr = &stderr
		data, callErr := command.Output()
		cancel()
		if callErr != nil {
			entry.Outcome = "probe_failed"
			entry.Error = strings.TrimSpace(stderr.String())
			if entry.Error == "" {
				entry.Error = callErr.Error()
			}
			data, _ = json.Marshal(map[string]string{"error": entry.Error})
		} else if err := evidenceRecovery(data, &entry); err != nil {
			entry.Outcome = "invalid_evidence"
			entry.Error = err.Error()
		}
		if err := os.WriteFile(filepath.Join(outDir, entry.Report), data, 0600); err != nil {
			return err
		}
		report.Results = append(report.Results, entry)
		report.Investigated++
		fmt.Fprintf(os.Stderr, "RECOVERY %d/%d: %s (%s)\n", i+1, limit, h.ID, entry.Outcome)
	}
	sort.SliceStable(report.Results, func(i, j int) bool {
		a, b := report.Results[i], report.Results[j]
		order := func(s string) int {
			switch s {
			case "candidate_needs_holdout":
				return 0
			case "controls_unavailable":
				return 1
			case "control_dominated":
				return 2
			default:
				return 3
			}
		}
		if order(a.Outcome) != order(b.Outcome) {
			return order(a.Outcome) < order(b.Outcome)
		}
		if a.WindowsNoControls != b.WindowsNoControls {
			return a.WindowsNoControls < b.WindowsNoControls
		}
		if a.ControlWins != b.ControlWins {
			return a.ControlWins < b.ControlWins
		}
		if a.OffsetSpreadMS != b.OffsetSpreadMS {
			return a.OffsetSpreadMS < b.OffsetSpreadMS
		}
		return a.HypothesisID < b.HypothesisID
	})
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(outDir, "recovery.json"), data, 0600); err != nil {
		return err
	}
	fmt.Printf("STATUS: %s\nMODE: %s\nCANDIDATES: %d\nINVESTIGATED: %d\nOUTPUT: %s\n", report.Status, report.Mode, report.Candidates, report.Investigated, filepath.Join(outDir, "recovery.json"))
	return nil
}
