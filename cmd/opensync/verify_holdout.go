package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"

	"opensync/internal/acousticverify"
	"opensync/internal/audio"
	"opensync/internal/projectstate"
	"opensync/internal/quality"
)

type TrainProbe struct {
	QuerySeconds float64                     `json:"query_seconds"`
	Measurement  *acousticverify.Measurement `json:"measurement"`
	Error        string                      `json:"error"`
}
type TrainingReport struct {
	HypothesisID string       `json:"hypothesis_id"`
	Reference    string       `json:"reference"`
	Query        string       `json:"query"`
	QualityMode  string       `json:"quality_mode"`
	Probes       []TrainProbe `json:"probes"`
}

type HoldoutWindow struct {
	QueryStartNS      int64   `json:"query_start_ns"`
	PredictedOffsetNS int64   `json:"predicted_offset_ns"`
	MeasuredOffsetNS  int64   `json:"measured_offset_ns,omitempty"`
	ResidualMS        float64 `json:"residual_ms,omitempty"`
	Correlation       float64 `json:"correlation,omitempty"`
	ControlWins       int     `json:"control_wins"`
	ValidControls     int     `json:"valid_controls"`
	Error             string  `json:"error,omitempty"`
}
type HoldoutReport struct {
	Status                string          `json:"status"`
	HypothesisID          string          `json:"hypothesis_id"`
	Mode                  string          `json:"mode"`
	TrainingWindows       int             `json:"training_windows"`
	Model                 string          `json:"model"`
	ModelOffsetNS         int64           `json:"model_offset_ns"`
	ModelSlopeNSPerSecond float64         `json:"model_slope_ns_per_second"`
	TrainingRMSMS         float64         `json:"training_rms_ms"`
	HoldoutsRequested     int             `json:"holdouts_requested"`
	HoldoutsTested        int             `json:"holdouts_tested"`
	ValidHoldouts         int             `json:"valid_holdouts"`
	ControlWins           int             `json:"control_wins"`
	Result                string          `json:"result"`
	GraphCertification    string          `json:"graph_certification"`
	Windows               []HoldoutWindow `json:"windows"`
}

type timeOffset struct {
	t      float64
	offset float64
}
type timingModel struct {
	name      string
	intercept float64
	slope     float64
	rms       float64
}

func (m timingModel) predict(t float64) float64 { return m.intercept + m.slope*t }
func fitTiming(samples []timeOffset, mode quality.Mode) (timingModel, error) {
	if len(samples) < 2 {
		return timingModel{}, fmt.Errorf("need at least two valid training windows")
	}
	sorted := make([]float64, len(samples))
	for i, p := range samples {
		sorted[i] = p.offset
	}
	sort.Float64s(sorted)
	median := sorted[len(sorted)/2]
	if len(sorted)%2 == 0 {
		median = (sorted[len(sorted)/2-1] + sorted[len(sorted)/2]) / 2
	}
	best := timingModel{name: "constant", intercept: median}
	rms := func(m timingModel) float64 {
		sum := 0.0
		for _, p := range samples {
			d := p.offset - m.predict(p.t)
			sum += d * d
		}
		return math.Sqrt(sum / float64(len(samples)))
	}
	best.rms = rms(best)
	if mode == quality.Draft || len(samples) < 3 {
		return best, nil
	}
	meanT, meanO := 0.0, 0.0
	for _, p := range samples {
		meanT += p.t
		meanO += p.offset
	}
	meanT /= float64(len(samples))
	meanO /= float64(len(samples))
	covariance, variance := 0.0, 0.0
	for _, p := range samples {
		dt := p.t - meanT
		covariance += dt * (p.offset - meanO)
		variance += dt * dt
	}
	if variance <= 0 {
		return best, nil
	}
	slope := covariance / variance
	affine := timingModel{name: "affine", intercept: meanO - slope*meanT, slope: slope}
	affine.rms = rms(affine)
	// Complexity penalty is evaluated ONLY on training data. Holdouts cannot
	// influence model selection. The penalty avoids fitting sampling noise.
	penalty := 250000.0 // 0.25 ms per training observation in RMS units
	if affine.rms+penalty < best.rms {
		return affine, nil
	}
	return best, nil
}
func disjointHoldouts(training []TrainProbe, windowNS int64, availableStart, availableEnd float64, count int) []float64 {
	if count == 0 {
		return nil
	}
	used := make([]float64, 0)
	window := float64(windowNS) / 1e9
	for _, p := range training {
		if p.Measurement != nil {
			used = append(used, float64(p.Measurement.QueryStartNS)/1e9)
		}
	}
	// Candidate positions are midpoints of disjoint gaps between training
	// intervals. Entire holdout window must stay outside every train window.
	bounds := append([]float64{availableStart}, used...)
	sort.Float64s(bounds)
	positions := make([]float64, 0)
	for i := 0; i < len(bounds); i++ {
		end := availableEnd
		if i+1 < len(bounds) {
			end = bounds[i+1]
		}
		start := bounds[i]
		if i > 0 {
			start += window
		}
		if end-start >= window+1 {
			candidate := start + (end-start-window)/2
			separated := true
			for _, u := range used {
				if candidate < u+window+0.5 && u < candidate+window+0.5 {
					separated = false
					break
				}
			}
			if separated {
				positions = append(positions, candidate)
			}
		}
	}
	// Pick spread-out windows rather than repeatedly querying one gap.
	result := make([]float64, 0, count)
	for len(result) < count && len(positions) > 0 {
		index := len(positions) / 2
		result = append(result, positions[index])
		positions = append(positions[:index], positions[index+1:]...)
	}
	sort.Float64s(result)
	return result
}
func verifyHoldout(projectPath, indexPath, trainPath, mode, output string) error {
	preset, err := quality.Parse(mode)
	if err != nil {
		return err
	}
	p, err := projectstate.Load(projectPath)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(trainPath)
	if err != nil {
		return err
	}
	var training TrainingReport
	if err := json.Unmarshal(raw, &training); err != nil {
		return err
	}
	h, ok := p.Hypotheses[training.HypothesisID]
	if !ok || h.Status == projectstate.Rejected || h.Reference != training.Reference || h.Query != training.Query {
		return fmt.Errorf("training report hypothesis identity mismatch")
	}
	if training.QualityMode != string(preset.Mode) {
		return fmt.Errorf("training mode differs from requested mode")
	}
	indexBytes, err := os.ReadFile(indexPath)
	if err != nil {
		return err
	}
	var index map[string]probeCacheEntry
	if err := json.Unmarshal(indexBytes, &index); err != nil {
		return err
	}
	open := func(id string) (*audio.Reader, error) {
		item, exists := index[id]
		if !exists || item.Cache == "" {
			return nil, fmt.Errorf("missing cache %q", id)
		}
		norm := func(s string) string { return strings.ToLower(strings.ReplaceAll(s, "\\", "/")) }
		if norm(item.Source) != norm(p.Recordings[id].SourcePath) {
			return nil, fmt.Errorf("cache identity mismatch for %q", id)
		}
		return audio.Open(item.Cache)
	}
	ref, err := open(h.Reference)
	if err != nil {
		return err
	}
	defer ref.Close()
	query, err := open(h.Query)
	if err != nil {
		return err
	}
	defer query.Close()
	if len(training.Probes) == 0 {
		return fmt.Errorf("empty training report")
	}
	data := make([]timeOffset, 0, len(training.Probes))
	for _, probe := range training.Probes {
		if probe.Measurement == nil || probe.Error != "" {
			continue
		}
		m := probe.Measurement
		if m.WindowNS != preset.WindowNS || m.SearchRadiusNS != preset.SearchRadiusNS {
			return fmt.Errorf("training probe parameters mismatch")
		}
		if math.Abs(float64(m.QueryStartNS)/1e9-probe.QuerySeconds) > 0.002 {
			return fmt.Errorf("training event position mismatch")
		}
		data = append(data, timeOffset{t: float64(m.QueryStartNS) / 1e9, offset: float64(m.MeasuredNS)})
	}
	model, err := fitTiming(data, preset.Mode)
	if err != nil {
		return err
	}
	report := HoldoutReport{
		Status: "HELDOUT_DIAGNOSTIC_NOT_GRAPH_CERTIFIED", HypothesisID: h.ID,
		Mode: string(preset.Mode), TrainingWindows: len(data), Model: model.name,
		ModelOffsetNS:         int64(math.Round(model.intercept)),
		ModelSlopeNSPerSecond: model.slope,
		TrainingRMSMS:         model.rms / 1e6,
		HoldoutsRequested:     preset.HeldOutChecks,
		GraphCertification:    "not_checked", Windows: []HoldoutWindow{},
	}
	// A candidate holdout must fit the predicted reference region and be
	// temporally disjoint from training events. No model is refitted on holdout.
	windowSeconds := float64(preset.WindowNS) / 1e9
	radiusSeconds := float64(preset.SearchRadiusNS) / 1e9
	low := math.Max(0, radiusSeconds-float64(h.OffsetNS)/1e9) + 0.25
	high := math.Min(float64(query.Frames())/16000-windowSeconds,
		float64(ref.Frames())/16000-float64(h.OffsetNS)/1e9-windowSeconds-radiusSeconds) - 0.25
	positions := disjointHoldouts(training.Probes, preset.WindowNS, low, high, preset.HeldOutChecks)
	report.HoldoutsTested = len(positions)
	for _, t := range positions {
		predicted := int64(math.Round(model.predict(t)))
		item := HoldoutWindow{QueryStartNS: int64(math.Round(t * 1e9)), PredictedOffsetNS: predicted}
		m, err := acousticverify.Measure(ref, query, acousticverify.Probe{
			QueryStartNS: item.QueryStartNS, OffsetNS: predicted, WindowNS: preset.WindowNS, RadiusNS: preset.SearchRadiusNS,
		})
		if err != nil {
			item.Error = err.Error()
		} else {
			item.MeasuredOffsetNS = m.MeasuredNS
			item.ResidualMS = float64(m.MeasuredNS-predicted) / 1e6
			item.Correlation = m.Correlation
			for _, wrong := range quality.ValidControls(ref.Frames(), item.QueryStartNS, predicted, preset.WindowNS, preset.SearchRadiusNS, preset.ControlOffsetsSeconds) {
				control, err := acousticverify.Measure(ref, query, acousticverify.Probe{
					QueryStartNS: item.QueryStartNS, OffsetNS: wrong, WindowNS: preset.WindowNS, RadiusNS: preset.SearchRadiusNS,
				})
				if err != nil {
					continue
				}
				item.ValidControls++
				if math.Abs(control.Correlation) >= math.Abs(m.Correlation) {
					item.ControlWins++
					report.ControlWins++
				}
			}
			report.ValidHoldouts++
		}
		report.Windows = append(report.Windows, item)
	}
	report.Result = "insufficient_independent_events"
	if report.HoldoutsRequested == 0 {
		report.Result = "preset_does_not_request_holdouts"
	}
	if report.HoldoutsRequested > 0 && report.ValidHoldouts == report.HoldoutsRequested {
		report.Result = "heldout_measurements_available_review_required"
		if report.ControlWins > 0 {
			report.Result = "heldout_competing_offsets_detected"
		}
	}
	outputBytes, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	outputBytes = append(outputBytes, '\n')
	if err := exclusiveCreatePreview(output, outputBytes); err != nil {
		return err
	}
	fmt.Printf("STATUS: %s\nMODEL: %s\nTRAINING: %d\nHELDOUT: %d/%d\nCONTROL_WINS: %d\nRESULT: %s\nREPORT: %s\n", report.Status, report.Model, report.TrainingWindows, report.ValidHoldouts, report.HoldoutsRequested, report.ControlWins, report.Result, output)
	return nil
}
