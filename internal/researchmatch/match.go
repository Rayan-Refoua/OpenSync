// Package researchmatch performs full-range, high-density acoustic correlation.
// It is an experimental discovery engine and cannot certify synchronization.
package researchmatch

import (
	"fmt"
	"math"
	"math/cmplx"
	"sort"

	"opensync/internal/audio"
	"opensync/internal/dsp"
)

const SampleRate = 16000
const MaxFeatureFrames = 1200000 // ~40 minutes at a 2 ms hop, fail explicitly
type Config struct {
	HopSamples        int     `json:"hop_samples"`
	MinOverlapSeconds float64 `json:"min_overlap_seconds"`
	Peaks             int     `json:"peaks"`
	SeparationSeconds float64 `json:"separation_seconds"`
}

func Dream() Config {
	return Config{HopSamples: 32, MinOverlapSeconds: 8, Peaks: 12, SeparationSeconds: 1}
}

type Peak struct {
	OffsetNS        int64      `json:"offset_ns"`
	OffsetSeconds   float64    `json:"offset_seconds"`
	Score           float64    `json:"score"`
	BandCorrelation [3]float64 `json:"band_correlation"`
	OverlapSeconds  float64    `json:"overlap_seconds"`
}
type Result struct {
	Status           string  `json:"status"`
	Method           string  `json:"method"`
	HopMS            float64 `json:"hop_ms"`
	ReferenceSeconds float64 `json:"reference_seconds"`
	QuerySeconds     float64 `json:"query_seconds"`
	LagsExamined     int     `json:"lags_examined"`
	Peaks            []Peak  `json:"peaks"`
	Certification    string  `json:"certification"`
}

func (c Config) validate() error {
	if c.HopSamples != 32 && c.HopSamples != 128 {
		return fmt.Errorf("supported research hop sizes: 32 (2 ms), 128 (8 ms)")
	}
	if c.MinOverlapSeconds < 2 || c.MinOverlapSeconds > 300 || c.Peaks < 1 || c.Peaks > 30 || c.SeparationSeconds <= 0 || c.SeparationSeconds > 30 {
		return fmt.Errorf("invalid research configuration")
	}
	return nil
}

// Features uses log band energy derivatives, with continuous one-pole
// crossovers. This measures transient envelopes across different mic EQ
// and gains and is deliberately different from the legacy hash detector.
func Extract(r *audio.Reader, c Config) ([3][]float64, error) {
	var result [3][]float64
	if err := c.validate(); err != nil {
		return result, err
	}
	if r == nil || r.SampleRate() != SampleRate {
		return result, fmt.Errorf("expected 16 kHz PCM cache")
	}
	frames := r.Frames() / int64(c.HopSamples)
	if frames > MaxFeatureFrames {
		return result, fmt.Errorf("research input exceeds %d feature frames; use larger hop or segmented experiment", MaxFeatureFrames)
	}
	if frames < 2 {
		return result, fmt.Errorf("audio too short")
	}
	for b := 0; b < 3; b++ {
		result[b] = make([]float64, 0, frames)
	}
	const chunk = 65536
	alpha1 := 1 - math.Exp(-2*math.Pi*300/SampleRate)
	alpha2 := 1 - math.Exp(-2*math.Pi*1800/SampleRate)
	var low, wide float64
	var sums [3]float64
	var previous [3]float64
	var used int
	for pos := int64(0); pos < r.Frames(); {
		count := int64(chunk)
		if count > r.Frames()-pos {
			count = r.Frames() - pos
		}
		pcm, err := r.ReadFrames(pos, count)
		if err != nil {
			return result, err
		}
		for _, s := range pcm {
			x := float64(s) / 32768
			low += alpha1 * (x - low)
			wide += alpha2 * (x - wide)
			parts := [3]float64{low, wide - low, x - wide}
			for b, v := range parts {
				sums[b] += v * v
			}
			used++
			if used == c.HopSamples {
				for b := 0; b < 3; b++ {
					value := math.Log1p(600 * math.Sqrt(sums[b]/float64(used)))
					// Log energy derivative removes stationary mic gain and silence bias.
					result[b] = append(result[b], value-previous[b])
					previous[b] = value
					sums[b] = 0
				}
				used = 0
			}
		}
		pos += count
	}
	return result, nil
}
func nextPow2(n int) (int, error) {
	if n <= 0 || n > 1<<24 {
		return 0, fmt.Errorf("research FFT length too large")
	}
	p := 1
	for p < n {
		p *= 2
	}
	return p, nil
}

// Dot products of every possible overlap, FFT(ref) * FFT(reverse(query)).
func crossDots(ref, query []float64) ([]float64, error) {
	size, err := nextPow2(len(ref) + len(query) - 1)
	if err != nil {
		return nil, err
	}
	a := make([]complex128, size)
	b := make([]complex128, size)
	for i, x := range ref {
		a[i] = complex(x, 0)
	}
	for i, x := range query {
		b[i] = complex(query[len(query)-1-i], 0)
		_ = x
	}
	if err := dsp.FFT(a); err != nil {
		return nil, err
	}
	if err := dsp.FFT(b); err != nil {
		return nil, err
	}
	for i := range a {
		a[i] *= b[i]
		a[i] = cmplx.Conj(a[i])
	}
	if err := dsp.FFT(a); err != nil {
		return nil, err
	}
	out := make([]float64, len(ref)+len(query)-1)
	for i := range out {
		out[i] = real(cmplx.Conj(a[i])) / float64(size)
	}
	return out, nil
}
func prefix(a []float64) ([]float64, []float64) {
	sums := make([]float64, len(a)+1)
	squares := make([]float64, len(a)+1)
	for i, x := range a {
		sums[i+1] = sums[i] + x
		squares[i+1] = squares[i] + x*x
	}
	return sums, squares
}
func bandScores(reference, query []float64, minFrames int) ([]float64, error) {
	dots, err := crossDots(reference, query)
	if err != nil {
		return nil, err
	}
	qr, qq := prefix(query)
	rr, rq := prefix(reference)
	// Lag k: ref[j+k] corresponds to query[j].
	for idx := range dots {
		k := idx - (len(query) - 1)
		q0 := 0
		if k < 0 {
			q0 = -k
		}
		q1 := len(query)
		if candidate := len(reference) - k; candidate < q1 {
			q1 = candidate
		}
		n := q1 - q0
		if n < minFrames {
			dots[idx] = math.NaN()
			continue
		}
		r0 := q0 + k
		r1 := q1 + k
		nn := float64(n)
		sumQ := qr[q1] - qr[q0]
		sumR := rr[r1] - rr[r0]
		vQ := qq[q1] - qq[q0] - sumQ*sumQ/nn
		vR := rq[r1] - rq[r0] - sumR*sumR/nn
		if vQ <= 1e-9 || vR <= 1e-9 {
			dots[idx] = math.NaN()
			continue
		}
		corr := (dots[idx] - sumQ*sumR/nn) / math.Sqrt(vQ*vR)
		dots[idx] = math.Max(-1, math.Min(1, corr))
	}
	return dots, nil
}
func MatchFeatures(ref, query [3][]float64, c Config) (Result, error) {
	if err := c.validate(); err != nil {
		return Result{}, err
	}
	if len(ref[0]) == 0 || len(query[0]) == 0 {
		return Result{}, fmt.Errorf("empty features")
	}
	for b := 1; b < 3; b++ {
		if len(ref[b]) != len(ref[0]) || len(query[b]) != len(query[0]) {
			return Result{}, fmt.Errorf("band lengths differ")
		}
	}
	minFrames := int(math.Ceil(c.MinOverlapSeconds * SampleRate / float64(c.HopSamples)))
	if minFrames > len(ref[0]) || minFrames > len(query[0]) {
		return Result{}, fmt.Errorf("insufficient recording overlap")
	}
	var bands [3][]float64
	for b := 0; b < 3; b++ {
		scores, err := bandScores(ref[b], query[b], minFrames)
		if err != nil {
			return Result{}, err
		}
		bands[b] = scores
	}
	result := Result{Status: "EXHAUSTIVE_DISCOVERY_DIAGNOSTIC_NOT_CERTIFIED",
		Method:           "three_band_log_energy_derivative_fft_pearson_v1",
		HopMS:            float64(c.HopSamples) * 1000 / SampleRate,
		ReferenceSeconds: float64(len(ref[0])*c.HopSamples) / SampleRate,
		QuerySeconds:     float64(len(query[0])*c.HopSamples) / SampleRate,
		LagsExamined:     len(bands[0]), Peaks: []Peak{}, Certification: "not_assessed"}
	candidates := make([]Peak, 0, len(bands[0])/8)
	for i := range bands[0] {
		var score float64
		var correlations [3]float64
		valid := true
		for b := 0; b < 3; b++ {
			v := bands[b][i]
			if math.IsNaN(v) {
				valid = false
				break
			}
			correlations[b] = v
			// Only positive transient agreement is useful for ranking.
			if v > 0 {
				score += v
			}
		}
		if !valid {
			continue
		}
		score /= 3
		if score <= 0 {
			continue
		}
		lag := i - (len(query[0]) - 1)
		q0 := 0
		if lag < 0 {
			q0 = -lag
		}
		q1 := len(query[0])
		if n := len(ref[0]) - lag; n < q1 {
			q1 = n
		}
		seconds := float64(lag*c.HopSamples) / SampleRate
		candidates = append(candidates, Peak{OffsetNS: int64(lag*c.HopSamples) * 1e9 / SampleRate,
			OffsetSeconds: seconds, Score: score, BandCorrelation: correlations,
			OverlapSeconds: float64((q1-q0)*c.HopSamples) / SampleRate})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		return candidates[i].OffsetNS < candidates[j].OffsetNS
	})
	separation := int64(math.Ceil(c.SeparationSeconds * 1e9))
	for _, candidate := range candidates {
		distinct := true
		for _, selected := range result.Peaks {
			if math.Abs(float64(candidate.OffsetNS-selected.OffsetNS)) < float64(separation) {
				distinct = false
				break
			}
		}
		if distinct {
			result.Peaks = append(result.Peaks, candidate)
		}
		if len(result.Peaks) >= c.Peaks {
			break
		}
	}
	return result, nil
}
func Match(ref, query *audio.Reader, c Config) (Result, error) {
	r, err := Extract(ref, c)
	if err != nil {
		return Result{}, err
	}
	q, err := Extract(query, c)
	if err != nil {
		return Result{}, err
	}
	return MatchFeatures(r, q, c)
}
