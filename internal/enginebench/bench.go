// Package enginebench evaluates engine output against independently generated
// labels. A candidate list is not an automatic match/reject decision.
package enginebench

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"opensync/internal/acousticverify"
	"opensync/internal/audio"
	"opensync/internal/researchmatch"
	"opensync/internal/syntheticbench"
)

const PrimaryMode = "2ms-integrated-8ms"
const OffsetToleranceMS = 4.0

type Options struct {
	Output       string
	Profile      string
	RealManifest string
	Progress     io.Writer
}

type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"` // pass, fail, blocked, observation
	Detail string `json:"detail"`
}

type FileIdentity struct {
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type Pair struct {
	Name           string  `json:"name"`
	Reference      string  `json:"reference"`
	Query          string  `json:"query"`
	Label          string  `json:"label"` // unreviewed, matched, unrelated, ambiguous
	ValidOffsetsNS []int64 `json:"valid_offsets_ns,omitempty"`
	Evidence       string  `json:"evidence,omitempty"`
	ToleranceMS    float64 `json:"tolerance_ms,omitempty"`
}

type RealManifest struct {
	Version int    `json:"version"`
	Pairs   []Pair `json:"pairs"`
}

type Run struct {
	Dataset              string               `json:"dataset"`
	Scenario             string               `json:"scenario"`
	Configuration        string               `json:"configuration"`
	Label                string               `json:"label"`
	ExpectedOffsetsNS    []int64              `json:"expected_offsets_ns"`
	Peaks                []researchmatch.Peak `json:"peaks"`
	TopErrorMS           *float64             `json:"top_error_ms"`
	AlternativesFound    int                  `json:"alternatives_found"`
	AlternativesExpected int                  `json:"alternatives_expected"`
	ToleranceMS          float64              `json:"tolerance_ms"`
	ElapsedSeconds       float64              `json:"elapsed_seconds"`
	AllocatedBytes       uint64               `json:"allocated_bytes"`
	Checks               []Check              `json:"checks"`
	Error                string               `json:"error,omitempty"`
}

type TimingPoint struct {
	QuerySeconds       float64 `json:"query_seconds"`
	Role               string  `json:"role"`
	ExpectedOffsetNS   int64   `json:"expected_offset_ns"`
	MeasuredOffsetNS   int64   `json:"measured_offset_ns"`
	MeasurementErrorMS float64 `json:"measurement_error_ms"`
	PredictionErrorMS  float64 `json:"prediction_error_ms"`
	Error              string  `json:"error,omitempty"`
}

type TimingRun struct {
	Fixture           syntheticbench.TimingFixture `json:"fixture"`
	Scope             string                       `json:"scope"`
	EstimatedDriftPPM float64                      `json:"estimated_drift_ppm"`
	Points            []TimingPoint                `json:"points"`
	Checks            []Check                      `json:"checks"`
}

type Report struct {
	Version              int               `json:"version"`
	Status               string            `json:"status"`
	RegressionStatus     string            `json:"regression_status"`
	ReadinessStatus      string            `json:"readiness_status"`
	Profile              string            `json:"profile"`
	PrimaryConfiguration string            `json:"primary_configuration"`
	StartedUTC           string            `json:"started_utc"`
	ElapsedSeconds       float64           `json:"elapsed_seconds"`
	GoVersion            string            `json:"go_version"`
	Platform             string            `json:"platform"`
	Build                map[string]string `json:"build"`
	Seeds                []int64           `json:"seeds"`
	Inputs               []FileIdentity    `json:"inputs"`
	Runs                 []Run             `json:"runs"`
	Timing               []TimingRun       `json:"timing"`
	Checks               []Check           `json:"checks"`
	Limitations          []string          `json:"limitations"`
}

func check(name string, ok bool, detail string) Check {
	status := "fail"
	if ok {
		status = "pass"
	}
	return Check{Name: name, Status: status, Detail: detail}
}

// Score never feeds truth into the matcher. Recall and top-one accuracy are
// separate requirements; finding the right answer at rank 12 is not top-one success.
func Score(run *Run, silence bool) {
	run.Checks = []Check{}
	run.TopErrorMS = nil
	run.AlternativesFound = 0
	run.AlternativesExpected = 0
	if run.Error != "" {
		run.Checks = append(run.Checks, check("measurement_completed", false, run.Error))
		return
	}
	run.Checks = append(run.Checks, check("measurement_completed", true, "PCM decoded and search completed"))
	if run.Label == "unreviewed" {
		run.Checks = append(run.Checks, Check{"real_ground_truth", "observation", "No independent label; candidates are not counted as correct or incorrect"})
		return
	}
	if run.Label == "unrelated" {
		if silence {
			run.Checks = append(run.Checks, check("silence_has_no_candidates", len(run.Peaks) == 0, fmt.Sprintf("%d candidates", len(run.Peaks))))
		}
		run.Checks = append(run.Checks, Check{"automatic_negative_rejection", "blocked", "Engine returns discovery candidates only; no calibrated accept/reject decision exists"})
		return
	}
	run.AlternativesExpected = len(run.ExpectedOffsetsNS)
	run.AlternativesFound = 0
	for _, expected := range run.ExpectedOffsetsNS {
		for _, peak := range run.Peaks {
			if math.Abs(float64(peak.OffsetNS)-float64(expected))/1e6 <= run.ToleranceMS {
				run.AlternativesFound++
				break
			}
		}
	}
	if len(run.Peaks) > 0 && len(run.ExpectedOffsetsNS) > 0 {
		errMS := math.Inf(1)
		for _, expected := range run.ExpectedOffsetsNS {
			errMS = math.Min(errMS, math.Abs(float64(run.Peaks[0].OffsetNS)-float64(expected))/1e6)
		}
		run.TopErrorMS = &errMS
	}
	run.Checks = append(run.Checks,
		check("top_one_offset", run.TopErrorMS != nil && *run.TopErrorMS <= run.ToleranceMS, fmt.Sprintf("top candidate must be within %.3f ms of valid truth", run.ToleranceMS)),
		check("all_valid_alternatives", run.AlternativesExpected > 0 && run.AlternativesFound == run.AlternativesExpected, fmt.Sprintf("%d/%d valid alignments found", run.AlternativesFound, run.AlternativesExpected)))
	if run.Label == "ambiguous" {
		run.Checks = append(run.Checks, Check{"automatic_ambiguity_decision", "blocked", "Retaining alternatives is tested; engine does not yet produce an ambiguity decision"})
	}
}

var modes = []struct {
	name           string
	hop, integrate int
}{{"8ms", 128, 1}, {"2ms", 32, 1}, {PrimaryMode, 32, 4}}

func evaluatePair(dataset string, pair Pair, silence bool) []Run {
	rows := []Run{}
	for _, mode := range modes {
		row := Run{Dataset: dataset, Scenario: pair.Name, Configuration: mode.name, Label: pair.Label, ExpectedOffsetsNS: pair.ValidOffsetsNS, ToleranceMS: pair.ToleranceMS, Peaks: []researchmatch.Peak{}}
		start := time.Now()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		func() {
			ref, err := audio.Open(pair.Reference)
			if err != nil {
				row.Error = err.Error()
				return
			}
			defer ref.Close()
			q, err := audio.Open(pair.Query)
			if err != nil {
				row.Error = err.Error()
				return
			}
			defer q.Close()
			cfg := researchmatch.Dream()
			cfg.HopSamples = mode.hop
			cfg.IntegrationFrames = mode.integrate
			result, err := researchmatch.Match(ref, q, cfg)
			if err != nil {
				row.Error = err.Error()
				return
			}
			row.Peaks = result.Peaks
		}()
		runtime.ReadMemStats(&after)
		row.AllocatedBytes = after.TotalAlloc - before.TotalAlloc
		row.ElapsedSeconds = time.Since(start).Seconds()
		Score(&row, silence)
		rows = append(rows, row)
	}
	return rows
}

func Identify(path string) (FileIdentity, error) {
	f, err := os.Open(path)
	if err != nil {
		return FileIdentity{}, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return FileIdentity{}, err
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return FileIdentity{}, err
	}
	return FileIdentity{absolute, n, hex.EncodeToString(h.Sum(nil))}, nil
}

func LoadReal(path string) (RealManifest, error) {
	f, err := os.Open(path)
	if err != nil {
		return RealManifest{}, err
	}
	defer f.Close()
	decoder := json.NewDecoder(f)
	decoder.DisallowUnknownFields()
	var manifest RealManifest
	if err := decoder.Decode(&manifest); err != nil {
		return manifest, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return manifest, fmt.Errorf("manifest must contain exactly one JSON object")
	}
	if manifest.Version != 1 || len(manifest.Pairs) == 0 {
		return manifest, fmt.Errorf("invalid real manifest version or empty pairs")
	}
	names := map[string]bool{}
	for i := range manifest.Pairs {
		p := &manifest.Pairs[i]
		if p.Name == "" || names[p.Name] || p.Reference == "" || p.Query == "" {
			return manifest, fmt.Errorf("invalid/duplicate pair %q", p.Name)
		}
		names[p.Name] = true
		for _, mediaPath := range []*string{&p.Reference, &p.Query} {
			if !filepath.IsAbs(*mediaPath) {
				*mediaPath = filepath.Join(filepath.Dir(path), *mediaPath)
			}
			*mediaPath, err = filepath.Abs(*mediaPath)
			if err != nil {
				return manifest, err
			}
		}
		if p.Reference == p.Query {
			return manifest, fmt.Errorf("pair %q uses identical files", p.Name)
		}
		switch p.Label {
		case "unreviewed":
			if len(p.ValidOffsetsNS) > 0 || p.Evidence != "" || p.ToleranceMS != 0 {
				return manifest, fmt.Errorf("unreviewed pair %q must not contain truth", p.Name)
			}
		case "matched", "ambiguous":
			if strings.TrimSpace(p.Evidence) == "" || p.ToleranceMS <= 0 || p.ToleranceMS > 20 || len(p.ValidOffsetsNS) == 0 || (p.Label == "matched" && len(p.ValidOffsetsNS) != 1) || (p.Label == "ambiguous" && len(p.ValidOffsetsNS) < 2) {
				return manifest, fmt.Errorf("pair %q needs evidence, valid offsets, and tolerance (0,20] ms", p.Name)
			}
		case "unrelated":
			if strings.TrimSpace(p.Evidence) == "" || len(p.ValidOffsetsNS) > 0 || p.ToleranceMS != 0 {
				return manifest, fmt.Errorf("unrelated pair %q needs independent evidence and no offsets", p.Name)
			}
		default:
			return manifest, fmt.Errorf("invalid label for %q", p.Name)
		}
		for j, offset := range p.ValidOffsetsNS {
			if offset < -86400000000000 || offset > 86400000000000 {
				return manifest, fmt.Errorf("offset outside supported one-day review range")
			}
			for _, other := range p.ValidOffsetsNS[:j] {
				if math.Abs(float64(offset)-float64(other))/1e6 <= 2*p.ToleranceMS {
					return manifest, fmt.Errorf("pair %q has duplicate/indistinguishable alternatives", p.Name)
				}
			}
		}
	}
	return manifest, nil
}

func runTiming(dir string, fixture syntheticbench.TimingFixture) TimingRun {
	r := TimingRun{Fixture: fixture, Scope: "local_measurement_component_with_known_initial_search_neighborhood; not end_to_end_drift_correction", Points: []TimingPoint{}, Checks: []Check{}}
	ref, err := audio.Open(filepath.Join(dir, fixture.Reference))
	if err != nil {
		r.Checks = append(r.Checks, check("open", false, err.Error()))
		return r
	}
	defer ref.Close()
	q, err := audio.Open(filepath.Join(dir, fixture.Query))
	if err != nil {
		r.Checks = append(r.Checks, check("open", false, err.Error()))
		return r
	}
	defer q.Close()
	// Fit from two early observations; later regions are not used to choose
	// the model or search center. The affine harness fit is NOT engine fitting.
	times := []float64{2, float64(fixture.DurationSeconds) * 0.2, float64(fixture.DurationSeconds) * 0.5, float64(fixture.DurationSeconds) * 0.75, float64(fixture.DurationSeconds) - 3}
	intercept, slope := float64(fixture.OffsetNS), 0.0
	trainingOK := true
	for i, t := range times {
		center := intercept + slope*(t+0.5)
		radius := int64(1e9)
		if i >= 2 {
			radius = 100000000
		}
		point := TimingPoint{QuerySeconds: t, Role: "validation", ExpectedOffsetNS: fixture.OffsetNS + int64(math.Round((t+0.5)*fixture.DriftPPM*1000))}
		if i < 2 {
			point.Role = "training"
		}
		if !trainingOK && i >= 2 {
			point.Error = "training failed; no predicted search center"
		} else {
			measurement, e := acousticverify.Measure(ref, q, acousticverify.Probe{QueryStartNS: int64(t * 1e9), OffsetNS: int64(math.Round(center)), WindowNS: 1e9, RadiusNS: radius})
			if e != nil {
				point.Error = e.Error()
				if i < 2 {
					trainingOK = false
				}
			} else {
				point.MeasuredOffsetNS = measurement.MeasuredNS
				point.MeasurementErrorMS = math.Abs(float64(point.MeasuredOffsetNS-point.ExpectedOffsetNS)) / 1e6
				point.PredictionErrorMS = math.Abs(center-float64(point.ExpectedOffsetNS)) / 1e6
			}
		}
		r.Points = append(r.Points, point)
		if i == 1 && trainingOK {
			slope = float64(r.Points[1].MeasuredOffsetNS-r.Points[0].MeasuredOffsetNS) / (times[1] - times[0])
			intercept = float64(r.Points[0].MeasuredOffsetNS) - slope*(times[0]+0.5)
			r.EstimatedDriftPPM = slope / 1000
		}
	}
	maxMeasure, maxPredict := 0.0, 0.0
	complete := true
	for _, p := range r.Points {
		if p.Error != "" {
			complete = false
		}
		maxMeasure = math.Max(maxMeasure, p.MeasurementErrorMS)
		if p.Role == "validation" {
			maxPredict = math.Max(maxPredict, p.PredictionErrorMS)
		}
	}
	r.Checks = append(r.Checks, check("all_local_measurements", complete, "five distinct regions including late recording"), check("local_timing_accuracy", complete && maxMeasure <= 1, fmt.Sprintf("max %.3f ms; limit 1 ms", maxMeasure)), check("affine_prediction_accuracy", complete && maxPredict <= 4, fmt.Sprintf("validation max %.3f ms; limit 4 ms; fit belongs to harness", maxPredict)))
	return r
}

func Summarize(r *Report) {
	failed, blocked := false, false
	for _, run := range r.Runs {
		if run.Configuration == r.PrimaryConfiguration {
			for _, c := range run.Checks {
				failed = failed || c.Status == "fail"
				blocked = blocked || c.Status == "blocked"
			}
		}
	}
	for _, run := range r.Timing {
		for _, c := range run.Checks {
			failed = failed || c.Status == "fail"
			blocked = blocked || c.Status == "blocked"
		}
	}
	for _, c := range r.Checks {
		failed = failed || c.Status == "fail"
		blocked = blocked || c.Status == "blocked"
	}
	r.RegressionStatus = "pass"
	if failed {
		r.RegressionStatus = "fail"
	}
	r.ReadinessStatus = "pass"
	if blocked {
		r.ReadinessStatus = "blocked"
	}
	if failed {
		r.ReadinessStatus = "fail"
	}
	r.Status = "completed"
}

func Execute(opts Options) (Report, error) {
	r := Report{Version: 1, Status: "running", RegressionStatus: "incomplete", ReadinessStatus: "incomplete", Profile: opts.Profile, PrimaryConfiguration: PrimaryMode, StartedUTC: time.Now().UTC().Format(time.RFC3339), GoVersion: runtime.Version(), Platform: runtime.GOOS + "/" + runtime.GOARCH, Build: map[string]string{}, Seeds: []int64{22026, 83021, 49157}, Inputs: []FileIdentity{}, Runs: []Run{}, Timing: []TimingRun{}, Checks: []Check{}, Limitations: []string{
		"Seeds vary acoustic noise within one procedural signal family; this is not diverse real-world validation.",
		"Full-range discovery sees the entire synthetic clip. Labels are scoring-only; no claim of held-out acoustic certification.",
		"Timing tests exercise local measurement and a harness affine fit, not production drift fitting, correction or export.",
		"Allocated bytes are total Go allocations, not peak process memory. Runtime is observational and machine-dependent.",
		"Comparison configurations do not affect the nominated primary regression gate; all failures remain visible.",
	}}
	if opts.Profile != "quick" && opts.Profile != "full" {
		return r, fmt.Errorf("profile must be quick or full")
	}
	var real RealManifest
	var err error
	if opts.RealManifest != "" {
		real, err = LoadReal(opts.RealManifest)
		if err != nil {
			return r, err
		}
	}
	if err := os.Mkdir(opts.Output, 0700); err != nil {
		return r, fmt.Errorf("output directory must not exist: %w", err)
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			r.Build[s.Key] = s.Value
		}
	}
	start := time.Now()
	progress := opts.Progress
	if progress == nil {
		progress = io.Discard
	}
	seen := map[string]bool{}
	identify := func(path string) error {
		absolute, e := filepath.Abs(path)
		if e != nil {
			return e
		}
		if seen[absolute] {
			return nil
		}
		id, e := Identify(path)
		if e != nil {
			return e
		}
		r.Inputs = append(r.Inputs, id)
		seen[absolute] = true
		return nil
	}
	checkpoint := func() error { r.ElapsedSeconds = time.Since(start).Seconds(); return writeReport(opts.Output, r) }
	for _, seed := range r.Seeds {
		dataset := fmt.Sprintf("synthetic-%d", seed)
		dir := filepath.Join(opts.Output, dataset)
		cases, e := syntheticbench.GenerateSeed(dir, seed)
		if e != nil {
			return r, e
		}
		cases, e = syntheticbench.ExtendAcceptance(dir, seed, cases)
		if e != nil {
			return r, e
		}
		truth, e := json.MarshalIndent(cases, "", "  ")
		if e != nil {
			return r, e
		}
		if e = os.WriteFile(filepath.Join(dir, "ground-truth.json"), append(truth, '\n'), 0600); e != nil {
			return r, e
		}
		if e = identify(filepath.Join(dir, "ground-truth.json")); e != nil {
			return r, e
		}
		for _, s := range cases {
			p := Pair{Name: s.Name, Reference: filepath.Join(dir, s.Reference), Query: filepath.Join(dir, s.Query), Label: "matched", ToleranceMS: OffsetToleranceMS, ValidOffsetsNS: s.ValidOffsetsNS}
			if s.ExpectedOffsetNS == nil {
				p.Label = "unrelated"
			} else if len(p.ValidOffsetsNS) == 0 {
				p.ValidOffsetsNS = []int64{*s.ExpectedOffsetNS}
			}
			if len(p.ValidOffsetsNS) > 1 {
				p.Label = "ambiguous"
			}
			for _, path := range []string{p.Reference, p.Query} {
				if e = identify(path); e != nil {
					return r, e
				}
			}
			fmt.Fprintf(progress, "BENCH %s / %s\n", dataset, p.Name)
			rows := evaluatePair(dataset, p, s.Name == "silence-negative")
			if s.Name == "drift-250ppm" {
				// A single offset is evaluated against BOTH ends of the known
				// time map, rather than falsely testing only its intercept.
				for i := range rows {
					maxErr := math.Inf(1)
					if len(rows[i].Peaks) > 0 {
						v := float64(rows[i].Peaks[0].OffsetNS)
						maxErr = math.Max(math.Abs(v-4375000000), math.Abs(v-(4375000000+22*250000))) / 1e6
					}
					rows[i].Checks = append(rows[i].Checks, check("constant_offset_full_interval", maxErr <= OffsetToleranceMS, fmt.Sprintf("endpoints max %.3f ms; limit 4 ms", maxErr)))
				}
			}
			r.Runs = append(r.Runs, rows...)
			if e = checkpoint(); e != nil {
				return r, e
			}
		}
	}
	seconds := 120
	if opts.Profile == "full" {
		seconds = 7200
	}
	fmt.Fprintf(progress, "BENCH generating %d-second timing fixtures (bounded buffers)\n", seconds)
	timingDir := filepath.Join(opts.Output, "timing")
	fixtures, err := syntheticbench.GenerateTiming(timingDir, seconds)
	if err != nil {
		return r, err
	}
	for _, fixture := range fixtures {
		for _, file := range []string{fixture.Reference, fixture.Query} {
			if err = identify(filepath.Join(timingDir, file)); err != nil {
				return r, err
			}
		}
		fmt.Fprintf(progress, "BENCH local timing / %s\n", fixture.Name)
		r.Timing = append(r.Timing, runTiming(timingDir, fixture))
		if err = checkpoint(); err != nil {
			return r, err
		}
	}
	reviewed := 0
	if opts.RealManifest != "" {
		if err = identify(opts.RealManifest); err != nil {
			return r, err
		}
	}
	for _, p := range real.Pairs {
		for _, file := range []string{p.Reference, p.Query} {
			if err = identify(file); err != nil {
				return r, err
			}
		}
		fmt.Fprintf(progress, "BENCH real / %s (%s)\n", p.Name, p.Label)
		r.Runs = append(r.Runs, evaluatePair("real", p, false)...)
		if p.Label != "unreviewed" {
			reviewed++
		}
		if err = checkpoint(); err != nil {
			return r, err
		}
	}
	r.Checks = append(r.Checks,
		Check{"automatic_match_reject_policy", "blocked", "No calibrated engine decision policy; false-accept/false-reject rates cannot be computed from candidate scores"},
		Check{"end_to_end_drift_correction", "blocked", "No production drift correction tested; local measurement success alone is insufficient"},
		Check{"global_timeline_and_export", "blocked", "This suite does not certify a multi-recording graph or Premiere round trip"},
		Check{"real_world_validation", "blocked", fmt.Sprintf("%d independently labelled pairs; a reviewed, diverse real corpus and negative controls are still required", reviewed)})
	if opts.Profile == "quick" {
		r.Checks = append(r.Checks, Check{"multi_hour_timing", "blocked", "Quick profile uses 120 seconds; run full for two-hour local timing"})
	}
	Summarize(&r)
	sort.Slice(r.Inputs, func(i, j int) bool { return r.Inputs[i].Path < r.Inputs[j].Path })
	if err = checkpoint(); err != nil {
		return r, err
	}
	return r, nil
}
