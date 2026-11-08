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
	Schedule string `json:"schedule"`
	ReservedHoldoutQuerySeconds []float64 `json:"reserved_holdout_query_seconds"`
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
	Status                     string          `json:"status"`
	HypothesisID               string          `json:"hypothesis_id"`
	Mode                       string          `json:"mode"`
	TrainingWindows            int             `json:"training_windows"`
	Model                      string          `json:"model"`
	ModelOffsetNS              int64           `json:"model_offset_ns"`
	ModelSlopeNSPerSecond      float64         `json:"model_slope_ns_per_second"`
	TrainingRMSMS              float64         `json:"training_rms_ms"`
	HoldoutsRequested          int             `json:"holdouts_requested"`
	HoldoutsTested             int             `json:"holdouts_tested"`
	HoldoutsAvailable          int             `json:"holdouts_available"`
	HoldoutSelection           string          `json:"holdout_selection"`
	HoldoutFailures            int             `json:"holdout_failures"`
	HoldoutsWithoutControls    int             `json:"holdouts_without_controls"`
	HoldoutAbsResidualMaxMS    float64         `json:"holdout_max_abs_residual_ms"`
	HoldoutAbsResidualMedianMS float64         `json:"holdout_median_abs_residual_ms"`
	ValidHoldouts              int             `json:"valid_holdouts"`
	ControlWins                int             `json:"control_wins"`
	Result                     string          `json:"result"`
	GraphCertification         string          `json:"graph_certification"`
	ValidationAllocation string `json:"validation_allocation"`
	Windows                    []HoldoutWindow `json:"windows"`
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

// disjointHoldoutCandidates produces deterministic, non-overlapping,
// unmeasured test windows. Failed training attempts reserve their full span.
// Query times must not overlap any source region examined for training.
func disjointHoldoutCandidates(training []TrainProbe, windowNS int64, availableStart, availableEnd float64) []float64 {
	window := float64(windowNS) / 1e9
	if window <= 0 || availableEnd-availableStart < window {
		return nil
	}
	const guard = 0.5
	used := make([]float64, 0, len(training))
	for _, p := range training {
		if math.IsNaN(p.QuerySeconds) || math.IsInf(p.QuerySeconds, 0) {
			return nil
		}
		used = append(used, p.QuerySeconds)
	}
	sort.Float64s(used)
	// Build free intervals using the union of training windows expanded by
	// 0.5s each side. This avoids reuse of a previously inspected event.
	cursor := availableStart
	result := make([]float64, 0)
	addGap := func(start, end float64) {
		for start+window <= end+1e-9 {
			result = append(result, start)
			start += window + guard
		}
	}
	for _, t := range used {
		left := t - guard
		right := t + window + guard
		if left > cursor {
			addGap(cursor, math.Min(left, availableEnd))
		}
		if right > cursor {
			cursor = right
		}
		if cursor >= availableEnd {
			break
		}
	}
	if cursor < availableEnd {
		addGap(cursor, availableEnd)
	}
	return result
}
func disjointHoldouts(training []TrainProbe, windowNS int64, availableStart, availableEnd float64, count int) []float64 {
	if count <= 0 {
		return nil
	}
	candidates := disjointHoldoutCandidates(training, windowNS, availableStart, availableEnd)
	if len(candidates) <= count {
		return candidates
	}
	// Uniform distribution across eligible events; never repeat a candidate.
	result := make([]float64, 0, count)
	for i := 0; i < count; i++ {
		index := int(math.Floor((float64(i) + 0.5) * float64(len(candidates)) / float64(count)))
		if index >= len(candidates) {
			index = len(candidates) - 1
		}
		result = append(result, candidates[index])
	}
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
	report.ValidationAllocation="post_training_disjoint_legacy"
	report.HoldoutsAvailable = len(disjointHoldoutCandidates(training.Probes, preset.WindowNS, low, high))
	if training.Schedule!="" {
		if training.Schedule!="preallocated_disjoint_v1" {return fmt.Errorf("unknown reservation schedule %q",training.Schedule)}
		report.ValidationAllocation=training.Schedule
		positions=append([]float64(nil),training.ReservedHoldoutQuerySeconds...)
		report.HoldoutsAvailable=len(positions)
		if len(positions)>preset.HeldOutChecks{return fmt.Errorf("reserved more holdouts than preset")}
		for i,t:=range positions {
			if math.IsNaN(t)||math.IsInf(t,0)||t<low-0.001||t>high+0.001 {
				return fmt.Errorf("reserved window out of bounds")
			}
			if i>0&&t<positions[i-1]+windowSeconds+0.5-0.001{return fmt.Errorf("reserved windows overlap or unsorted")}
			for _,train:=range training.Probes {
				if t<train.QuerySeconds+windowSeconds+0.5 &&
					train.QuerySeconds<t+windowSeconds+0.5 {
					return fmt.Errorf("reserved validation overlaps training at %.3fs",t)
				}
			}
		}
	}
	report.HoldoutsTested = len(positions)
	report.HoldoutSelection = "disjoint_windows_available"
	if training.Schedule=="preallocated_disjoint_v1"{report.HoldoutSelection="pre_reserved_before_training"}
	if report.HoldoutsAvailable < report.HoldoutsRequested {
		report.HoldoutSelection = "insufficient_disjoint_audio_duration"
	}
	absResiduals := make([]float64, 0, len(positions))
	for _, t := range positions {
		predicted := int64(math.Round(model.predict(t)))
		item := HoldoutWindow{QueryStartNS: int64(math.Round(t * 1e9)), PredictedOffsetNS: predicted}
		m, err := acousticverify.Measure(ref, query, acousticverify.Probe{
			QueryStartNS: item.QueryStartNS, OffsetNS: predicted, WindowNS: preset.WindowNS, RadiusNS: preset.SearchRadiusNS,
		})
		if err != nil {
			item.Error = err.Error()
			report.HoldoutFailures++
		} else {
			item.MeasuredOffsetNS = m.MeasuredNS
			item.ResidualMS = float64(m.MeasuredNS-predicted) / 1e6
			absResiduals = append(absResiduals, math.Abs(item.ResidualMS))
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
			if item.ValidControls == 0 {
				report.HoldoutsWithoutControls++
			}
			report.ValidHoldouts++
		}
		report.Windows = append(report.Windows, item)
	}
	if len(absResiduals) > 0 {
		sort.Float64s(absResiduals)
		report.HoldoutAbsResidualMaxMS = absResiduals[len(absResiduals)-1]
		report.HoldoutAbsResidualMedianMS = absResiduals[len(absResiduals)/2]
		if len(absResiduals)%2 == 0 {
			report.HoldoutAbsResidualMedianMS = (absResiduals[len(absResiduals)/2-1] + absResiduals[len(absResiduals)/2]) / 2
		}
	}
	report.Result = "insufficient_independent_events"
	if report.HoldoutsAvailable < report.HoldoutsRequested {
		report.Result = "insufficient_disjoint_audio_duration"
	} else if report.HoldoutFailures > 0 {
		report.Result = "holdout_measurement_failed"
	}
	if report.HoldoutsRequested == 0 {
		report.Result = "preset_does_not_request_holdouts"
	}
	if report.HoldoutsRequested > 0 && report.ValidHoldouts == report.HoldoutsRequested {
		report.Result = "heldout_measurements_available_review_required"
		if report.ControlWins > 0 {
			report.Result = "heldout_competing_offsets_detected"
		} else if report.HoldoutsWithoutControls > 0 {
			report.Result = "heldout_controls_unavailable"
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
	fmt.Printf("STATUS: %s\nMODEL: %s\nTRAINING: %d\nHELDOUT: %d/%d\nAVAILABLE_WINDOWS: %d\nSELECTION: %s\nMEASUREMENT_FAILURES: %d\nCONTROLS_UNAVAILABLE: %d\nMEDIAN_ABS_RESIDUAL_MS: %.3f\nMAX_ABS_RESIDUAL_MS: %.3f\nCONTROL_WINS: %d\nRESULT: %s\nREPORT: %s\n", report.Status, report.Model, report.TrainingWindows, report.ValidHoldouts, report.HoldoutsRequested, report.HoldoutsAvailable, report.HoldoutSelection, report.HoldoutFailures, report.HoldoutsWithoutControls, report.HoldoutAbsResidualMedianMS, report.HoldoutAbsResidualMaxMS, report.ControlWins, report.Result, output)
	return nil
}
