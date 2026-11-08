// Package syntheticbench builds deterministic, source-referenced acoustic
// recordings with known offsets for black-box research matcher experiments.
// Synthetic ground truth is NOT a substitute for real-world validation.
package syntheticbench

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"

	"opensync/internal/audio"
	"opensync/internal/researchmatch"
)

const rate = 16000
const masterSeconds = 48
const refStart = 4.0
const queryStart = 8.375
const thirdStart = 12.125
const refSeconds = 26
const querySeconds = 22
const thirdSeconds = 22

type Scenario struct {
	Name             string `json:"name"`
	Reference        string `json:"reference"`
	Query            string `json:"query"`
	ExpectedOffsetNS *int64 `json:"expected_offset_ns"`
	// Several valid alignments may exist when acoustic content repeats.
	ValidOffsetsNS []int64 `json:"valid_offsets_ns,omitempty"`
	Purpose        string  `json:"purpose"`
}
type MatchRun struct {
	Scenario         string               `json:"scenario"`
	HopMS            int                  `json:"hop_ms"`
	IntegrationMS    int                  `json:"integration_ms"`
	Configuration    string               `json:"configuration"`
	Status           string               `json:"status"`
	Rank             int                  `json:"true_offset_rank"`
	NearestErrorMS   float64              `json:"nearest_error_ms,omitempty"`
	TopOffsetSeconds float64              `json:"top_offset_seconds,omitempty"`
	TopScore         float64              `json:"top_score,omitempty"`
	TrueOffsetScore  float64              `json:"true_offset_score,omitempty"`
	Error            string               `json:"error,omitempty"`
	Peaks            []researchmatch.Peak `json:"peaks"`
}
type Report struct {
	Status                   string     `json:"status"`
	SampleRate               int        `json:"sample_rate"`
	Seed                     int64      `json:"seed"`
	Cases                    []Scenario `json:"cases"`
	Runs                     []MatchRun `json:"runs"`
	FalsePositiveCalibration string     `json:"false_positive_calibration"`
	Certification            string     `json:"certification"`
}

func synthMaster() []float64 {
	rng := rand.New(rand.NewSource(22026))
	count := masterSeconds * rate
	out := make([]float64, count)
	// Irregular transient envelopes; no repeated periodic event template.
	var x, lp float64
	for i := range out {
		t := float64(i) / rate
		x = 0.955*x + 0.045*rng.NormFloat64()
		lp = 0.994*lp + 0.006*rng.NormFloat64()
		event := 0.10
		for j := 0; j < 40; j++ {
			center := 0.85 + float64(j)*1.13 + 0.20*math.Sin(float64(j)*3.17)
			distance := math.Abs(t - center)
			if distance < 0.22 {
				event += 1.15 * (1 - distance/0.22)
			}
		}
		// Slowly changing source and broad-band content.
		out[i] = 0.35 * event * (0.62*x + 0.18*lp +
			0.18*math.Sin(2*math.Pi*(315*t+1.4*t*t)) +
			0.10*math.Sin(2*math.Pi*(1270*t+0.7*t*t)))
	}
	return out
}
func slice(master []float64, start float64, duration int, driftPPM float64) []float64 {
	samples := duration * rate
	out := make([]float64, samples)
	for i := range out {
		pos := start*rate + float64(i)*(1+driftPPM/1e6)
		j := int(pos)
		if j < 0 || j+1 >= len(master) {
			continue
		}
		frac := pos - float64(j)
		out[i] = master[j]*(1-frac) + master[j+1]*frac
	}
	return out
}
func transform(input []float64, kind string, seed int64) []int16 {
	rng := rand.New(rand.NewSource(seed))
	output := make([]int16, len(input))
	var slow, fast float64
	for i, x := range input {
		slow += 0.025 * (x - slow)
		fast += 0.2 * (x - fast)
		switch kind {
		case "mic":
			x = 0.45*(x-slow) + 0.35*fast + 0.006*rng.NormFloat64()
		case "gate":
			if math.Abs(fast) < 0.018 {
				x *= 0.025
			}
			x = 0.70*math.Tanh(2.4*x) + 0.01*rng.NormFloat64()
		case "reference":
			x = 0.8*x + 0.001*rng.NormFloat64()
		case "unrelated":
			x = 0.12 * rng.NormFloat64()
		default:
			x = 0.8 * x
		}
		x = math.Max(-0.99, math.Min(0.99, x))
		output[i] = int16(math.Round(x * 32767))
	}
	return output
}
func writeWAV(path string, samples []int16) error {
	if int64(len(samples)) > (int64(^uint32(0))-36)/2 {
		return fmt.Errorf("WAV too large")
	}
	wav := make([]byte, 44+2*len(samples))
	copy(wav[:4], "RIFF")
	binary.LittleEndian.PutUint32(wav[4:], uint32(len(wav)-8))
	copy(wav[8:12], "WAVE")
	copy(wav[12:16], "fmt ")
	binary.LittleEndian.PutUint32(wav[16:], 16)
	binary.LittleEndian.PutUint16(wav[20:], 1)
	binary.LittleEndian.PutUint16(wav[22:], 1)
	binary.LittleEndian.PutUint32(wav[24:], rate)
	binary.LittleEndian.PutUint32(wav[28:], rate*2)
	binary.LittleEndian.PutUint16(wav[32:], 2)
	binary.LittleEndian.PutUint16(wav[34:], 16)
	copy(wav[36:40], "data")
	binary.LittleEndian.PutUint32(wav[40:], uint32(len(samples)*2))
	for i, x := range samples {
		binary.LittleEndian.PutUint16(wav[44+2*i:], uint16(x))
	}
	return os.WriteFile(path, wav, 0600)
}
func targetOffset(a, b float64) *int64 {
	x := int64(math.Round((b - a) * 1e9))
	return &x
}

// Generate writes new self-contained 16 kHz PCM fixtures and ground truth.
// It refuses to reuse a directory; no user audio is accessed or overwritten.
func Generate(dir string) ([]Scenario, error) {
	if err := os.Mkdir(dir, 0700); err != nil {
		return nil, fmt.Errorf("benchmark directory must not exist: %w", err)
	}
	master := synthMaster()
	files := []struct {
		name    string
		start   float64
		seconds int
		drift   float64
		style   string
		seed    int64
	}{
		{"reference.wav", refStart, refSeconds, 0, "reference", 10},
		{"query-clean.wav", queryStart, querySeconds, 0, "clean", 11},
		{"query-mic.wav", queryStart, querySeconds, 0, "mic", 12},
		{"query-gated.wav", queryStart, querySeconds, 0, "gate", 13},
		{"query-drift.wav", queryStart, querySeconds, 250, "mic", 14},
		{"query-unrelated.wav", queryStart, querySeconds, 0, "unrelated", 15},
		{"third.wav", thirdStart, thirdSeconds, 0, "mic", 16},
	}
	for _, entry := range files {
		wave := transform(slice(master, entry.start, entry.seconds, entry.drift), entry.style, entry.seed)
		if err := writeWAV(filepath.Join(dir, entry.name), wave); err != nil {
			return nil, err
		}
	}
	// M24: controlled, competing true correspondences, missing events,
	// and the minimum genuine overlap at a source boundary.
	repeatSource := slice(master, 1, 24, 0)
	repeatReference := append(append([]float64(nil), repeatSource...), repeatSource[:12*rate]...)
	if err := writeWAV(filepath.Join(dir, "repeat-reference.wav"), transform(repeatReference, "clean", 40)); err != nil {
		return nil, err
	}
	if err := writeWAV(filepath.Join(dir, "repeat-query.wav"), transform(repeatSource[:12*rate], "clean", 41)); err != nil {
		return nil, err
	}

	missing := transform(slice(master, queryStart, querySeconds, 0), "mic", 42)
	noise := rand.New(rand.NewSource(43))
	for i := range missing {
		// A deterministic 70% interval dropout, not merely a gain reduction.
		phase := (float64(i) / rate)
		phase -= 3 * math.Floor(phase/3)
		if phase >= 0.9 {
			missing[i] = int16(math.Round(450 * noise.NormFloat64()))
		}
	}
	if err := writeWAV(filepath.Join(dir, "query-dropout.wav"), missing); err != nil {
		return nil, err
	}

	if err := writeWAV(filepath.Join(dir, "short-reference.wav"), transform(slice(master, 0, 38, 0), "reference", 44)); err != nil {
		return nil, err
	}
	if err := writeWAV(filepath.Join(dir, "short-query.wav"), transform(slice(master, 30, 18, 0), "mic", 45)); err != nil {
		return nil, err
	}

	truth := targetOffset(refStart, queryStart)
	aThird := targetOffset(refStart, thirdStart)
	bThird := targetOffset(queryStart, thirdStart)
	cases := []Scenario{
		{"clean", "reference.wav", "query-clean.wav", nil, truth, "positive baseline shared source"},
		{"microphone", "reference.wav", "query-mic.wav", nil, truth, "gain, filtering and independent mic noise"},
		{"gate-compression", "reference.wav", "query-gated.wav", nil, truth, "gate, saturation and microphone noise"},
		{"drift-250ppm", "reference.wav", "query-drift.wav", nil, truth, "increasing nonlinear-in-position mismatch; single offset not sufficient"},
		{"unrelated-negative", "reference.wav", "query-unrelated.wav", nil, nil, "independent noise must not be automatically certified"},
		{"triangle-A-C", "reference.wav", "third.wav", nil, aThird, "known time offset on third camera"},
		{"triangle-B-C", "query-clean.wav", "third.wav", nil, bThird, "known independent triangle leg"},
		{Name: "repeated-content", Reference: "repeat-reference.wav", Query: "repeat-query.wav", ExpectedOffsetNS: targetOffset(0, 0), ValidOffsetsNS: []int64{0, 24_000_000_000}, Purpose: "two equally genuine placements at offsets 0s and +24s"},
		{Name: "missing-70pct", Reference: "reference.wav", Query: "query-dropout.wav", ExpectedOffsetNS: truth, Purpose: "long missing-gated intervals replaced by independent noise"},
		{Name: "short-overlap-8s", Reference: "short-reference.wav", Query: "short-query.wav", ExpectedOffsetNS: targetOffset(0, 30), Purpose: "eight-second shared segment at end of 38-second reference"},
	}
	return cases, nil
}
func Evaluate(dir string, cases []Scenario) (Report, error) {
	report := Report{Status: "SYNTHETIC_DISCOVERY_DIAGNOSTIC_NOT_CERTIFIED",
		SampleRate: rate, Seed: 22026, Cases: cases, Runs: []MatchRun{},
		FalsePositiveCalibration: "not_calibrated",
		Certification:            "not_assessed",
	}
	for _, scenario := range cases {
		ref, err := audio.Open(filepath.Join(dir, scenario.Reference))
		if err != nil {
			return report, err
		}
		query, err := audio.Open(filepath.Join(dir, scenario.Query))
		if err != nil {
			ref.Close()
			return report, err
		}
		for _, mode := range []struct {
			name           string
			hop, integrate int
		}{{"8ms", 8, 1}, {"2ms", 2, 1}, {"2ms-integrated-8ms", 2, 4}} {
			cfg := researchmatch.Dream()
			cfg.HopSamples = mode.hop * rate / 1000
			cfg.IntegrationFrames = mode.integrate
			matched, err := researchmatch.Match(ref, query, cfg)
			row := MatchRun{Scenario: scenario.Name, HopMS: mode.hop, IntegrationMS: mode.hop * mode.integrate, Configuration: mode.name, Status: "diagnostic", Peaks: []researchmatch.Peak{}, Rank: -1}
			if err != nil {
				row.Status = "measurement_failed"
				row.Error = err.Error()
			} else {
				row.Peaks = matched.Peaks
				if len(matched.Peaks) > 0 {
					row.TopScore = matched.Peaks[0].Score
					row.TopOffsetSeconds = matched.Peaks[0].OffsetSeconds
				}
				if scenario.ExpectedOffsetNS != nil {
					best := math.Inf(1)
					truths := scenario.ValidOffsetsNS
					if len(truths) == 0 {
						truths = []int64{*scenario.ExpectedOffsetNS}
					}
					for i, peak := range matched.Peaks {
						for _, expected := range truths {
							deviation := math.Abs(float64(peak.OffsetNS-expected)) / 1e6
							if deviation < best {
								best = deviation
								row.Rank = i + 1
							}
						}
					}
					if math.IsInf(best, 1) {
						row.Rank = -1
					} else {
						row.NearestErrorMS = best
					}
					if row.Rank < 0 || row.NearestErrorMS > float64(mode.hop)*1.5 {
						row.Status = "ground_truth_not_in_top_peaks"
					} else {
						row.Status = "ground_truth_recovered_not_certified"
					}
				} else {
					row.Status = "negative_control_unlabeled_peaks"
				}
			}
			report.Runs = append(report.Runs, row)
		}
		ref.Close()
		query.Close()
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return report, err
	}
	if err := os.WriteFile(filepath.Join(dir, "results.json"), append(data, '\n'), 0600); err != nil {
		return report, err
	}
	return report, nil
}
