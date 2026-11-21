package autosync

import (
	"math"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// A timestamped recorder can sample the same playback reference repeatedly.
// Estimate its relative clock from a majority of distinct direct matches.
type recorderPrediction struct {
	ref     int
	offset  float64
	basis   string
	anchors []int
}

func timestampRecovery(a *NativeAnalyzer, records []MediaRecord, edges []*PairEvidence) (map[int]recorderPrediction, error) {
	out := map[int]recorderPrediction{}
	ambiguous := map[int]bool{}
	families := map[string][]int{}
	times := map[int]float64{}
	for i, r := range records {
		stem := strings.TrimSuffix(r.Name, filepath.Ext(r.Name))
		t, err := time.Parse("20060102_150405", stem)
		if err != nil {
			continue
		}
		times[i] = float64(t.Unix())
		key := filepath.Dir(r.Source) + "\x00" + r.SourceGroup
		families[key] = append(families[key], i)
	}
	for _, ids := range families {
		byRef := map[int]map[int]float64{}
		for _, q := range ids {
			for _, e := range edges {
				if e.Query != q || e.Selected == nil {
					continue
				}
				if _, same := times[e.Reference]; same {
					continue
				}
				if byRef[e.Reference] == nil {
					byRef[e.Reference] = map[int]float64{}
				}
				byRef[e.Reference][q] = e.Selected.Offset - times[q]
			}
		}
		for ref, votes := range byRef {
			if len(votes) < 3 {
				continue
			}
			var sorted []float64
			for _, v := range votes {
				sorted = append(sorted, v)
			}
			sort.Float64s(sorted)
			center := median(sorted)
			var anchors []int
			var values []float64
			for _, q := range ids {
				if v, ok := votes[q]; ok && math.Abs(v-center) <= 1.5 {
					anchors = append(anchors, q)
					values = append(values, v)
				}
			}
			if len(anchors) < 3 || 2*len(anchors) <= len(votes) {
				continue
			}
			center = median(values)
			for _, q := range ids {
				expected := times[q] + center
				d := records[q].Duration
				if expected < 0 || expected+d > records[ref].Duration || d < 2 || records[q].Audio == nil {
					continue
				}
				window := math.Min(2, (d-.3)/3)
				var observations []float64
				for _, t := range []float64{.05, (d - window) / 2, d - window - .05} {
					m, err := a.waveform(ref, q, t, expected, window, 2)
					if err != nil {
						return nil, err
					}
					if m != nil && m.Distinct && m.Wave >= .10 {
						observations = append(observations, m.Offset)
					}
				}
				predicted := expected
				basis := "filename_clock_consensus"
				for i, x := range observations {
					for _, y := range observations[i+1:] {
						if math.Abs(x-y) <= .025 {
							predicted = (x + y) / 2
							basis = "filename_clock_and_waveform"
						}
					}
				}
				if basis == "filename_clock_consensus" {
					w := math.Min(2, (d-.3)/5)
					var offsets []float64
					for _, t := range linspace(.05, d-w-.05, 5) {
						offset, ok, err := spectralClockWindow(a, ref, q, t, expected, w)
						if err != nil {
							return nil, err
						}
						if ok {
							offsets = append(offsets, offset)
						}
					}
					for i, x := range offsets {
						for _, y := range offsets[i+1:] {
							if math.Abs(x-y) <= .03 {
								predicted = (x + y) / 2
								basis = "filename_clock_and_spectral_agreement"
							}
						}
					}
				}
				if _, exists := out[q]; exists {
					ambiguous[q] = true
				}
				out[q] = recorderPrediction{ref, predicted, basis, anchors}
			}
		}
	}
	for q := range ambiguous {
		delete(out, q)
	}
	return out, nil
}

// Match two disjoint spectrogram excerpts near the calibrated recorder clock.
// Per-bin centering suppresses stable microphone coloration and room noise.
func spectralClockWindow(a *NativeAnalyzer, ref, q int, t, expected, length float64) (float64, bool, error) {
	low := math.Max(0, expected+t-2)
	x, err := a.read(q, t, length)
	if err != nil {
		return 0, false, err
	}
	y, err := a.read(ref, low, math.Min(a.records[ref].Duration-low, length+4))
	if err != nil {
		return 0, false, err
	}
	features := func(samples []float32) [][48]float64 {
		var frames [][48]float64
		var bins [512]complex128
		for start := 0; start+512 <= len(samples); start += 160 {
			for i, w := range fluxWindow {
				bins[i] = complex(float64(samples[start+i]*w), 0)
			}
			fluxPlan.Transform(bins[:])
			var frame [48]float64
			for j := range frame {
				k := 3 + j*2
				frame[j] = math.Log(1e-7 + math.Hypot(real(bins[k]), imag(bins[k])) + math.Hypot(real(bins[k+1]), imag(bins[k+1])))
			}
			frames = append(frames, frame)
		}
		return frames
	}
	u, v := features(x), features(y)
	if len(u) < 50 || len(v) < len(u) {
		return 0, false, nil
	}
	var avg, energy [48]float64
	for _, row := range u {
		for j, x := range row {
			avg[j] += x / float64(len(u))
		}
	}
	for i := range u {
		for j := range avg {
			u[i][j] -= avg[j]
			energy[j] += u[i][j] * u[i][j]
		}
	}
	scores := make([]float64, len(v)-len(u)+1)
	best := 0
	for k := range scores {
		var sum, sq, dot [48]float64
		for i, row := range u {
			for j, x := range row {
				y := v[k+i][j]
				sum[j] += y
				sq[j] += y * y
				dot[j] += x * y
			}
		}
		for j := range sum {
			varY := sq[j] - sum[j]*sum[j]/float64(len(u))
			if varY > 0 && energy[j] > 0 {
				scores[k] += dot[j] / math.Sqrt(varY*energy[j]) / 48
			}
		}
		if scores[k] > scores[best] {
			best = k
		}
	}
	competitor := 0.
	for k, s := range scores {
		if math.Abs(float64(k-best)) > 10 {
			competitor = math.Max(competitor, s)
		}
	}
	return low - t + float64(best)*.01, scores[best] > .12 && scores[best]-competitor > .03, nil
}
