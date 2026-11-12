package autosync

import (
	"math"
	"opensync/internal/researchmatch"
)

// A measurement and its wrong-offset controls use identical query samples.
// Prepare them once while retaining each reference comparison and its checks.
type preparedQuery struct {
	start, length float64
	samples       []float32
	wave          []float64
	energy        float64
	correlation   *researchmatch.CorrelationQuery
	flux          [3][]float64
	fluxReady     bool
}

func (a *NativeAnalyzer) prepareQuery(query int, start, length float64) (*preparedQuery, error) {
	if start < 0 || length < 0 || start+length > a.records[query].Duration {
		return nil, nil
	}
	samples, err := a.read(query, start, length)
	if err != nil {
		return nil, err
	}
	p := &preparedQuery{start: start, length: length, samples: samples, wave: prepareWave(samples)}
	mean := 0.
	for _, v := range p.wave {
		mean += v
	}
	if len(p.wave) > 0 {
		mean /= float64(len(p.wave))
	}
	center := make([]float64, len(p.wave))
	for i, v := range p.wave {
		center[i] = v - mean
		p.energy += center[i] * center[i]
	}
	p.correlation = researchmatch.PrepareCorrelationQuery(center)
	return p, nil
}
func (p *preparedQuery) correlate(r []float64) (int, float64, float64, error) {
	n := len(p.wave)
	if n < 8 || len(r) < n || p.energy < 1e-10 {
		return 0, 0, 0, nil
	}
	dots, err := p.correlation.Dots(r)
	if err != nil {
		return 0, 0, 0, err
	}
	sums, squares := make([]float64, len(r)+1), make([]float64, len(r)+1)
	for i, v := range r {
		sums[i+1] = sums[i] + v
		squares[i+1] = squares[i] + v*v
	}
	values := make([]float64, len(r)-n+1)
	best := 0
	for i := range values {
		sum := sums[i+n] - sums[i]
		variance := squares[i+n] - squares[i] - sum*sum/float64(n)
		if variance >= 1e-10 {
			values[i] = dots[i+n-1] / math.Sqrt(math.Max(variance, 1e-20)*p.energy)
		}
		if math.Abs(values[i]) > math.Abs(values[best]) {
			best = i
		}
	}
	competing := 0.
	for i, v := range values {
		if i < best-8 || i > best+8 {
			competing = math.Max(competing, math.Abs(v))
		}
	}
	return best, math.Max(-1, math.Min(1, values[best])), math.Min(1, competing), nil
}
func (a *NativeAnalyzer) waveformPrepared(ref int, p *preparedQuery, offset, radius float64) (*Measurement, error) {
	if p == nil {
		return nil, nil
	}
	start := math.Max(0, p.start+offset-radius)
	stop := math.Min(a.records[ref].Duration, p.start+offset+p.length+radius)
	if stop-start < p.length {
		return nil, nil
	}
	r, err := a.read(ref, start, stop-start)
	if err != nil {
		return nil, err
	}
	lag, corr, competing, err := p.correlate(prepareWave(r))
	if err != nil {
		return nil, err
	}
	if corr == 0 {
		return nil, nil
	}
	wave := math.Abs(corr)
	return &Measurement{QueryTime: p.start + p.length/2, Offset: start - p.start + float64(lag)*8/16000, Wave: wave, Window: p.length, Competing: competing, Distinct: wave-competing >= .025 && wave >= 1.1*competing}, nil
}
func (a *NativeAnalyzer) controlsPrepared(ref int, p *preparedQuery, offset, radius float64, shifts []float64) (int, float64, error) {
	count, strongest := 0, 0.
	for _, shift := range shifts {
		m, err := a.waveformPrepared(ref, p, offset+shift, radius)
		if err != nil {
			return 0, 0, err
		}
		if m != nil {
			count++
			strongest = math.Max(strongest, m.Wave)
		}
	}
	if count == 0 {
		strongest = 1
	}
	return count, strongest, nil
}
func (a *NativeAnalyzer) fluxPrepared(ref int, p *preparedQuery, offset float64) (float64, error) {
	if p == nil || p.start+offset < 0 || p.start+offset+p.length > a.records[ref].Duration {
		return 0, nil
	}
	r, err := a.read(ref, p.start+offset, p.length)
	if err != nil {
		return 0, err
	}
	if !p.fluxReady {
		p.flux = fluxFeatures(p.samples)
		p.fluxReady = true
	}
	rf := fluxFeatures(r)
	values := make([]float64, 3)
	for b := range values {
		values[b] = pearson(rf[b], p.flux[b])
	}
	return median(values), nil
}
func (a *NativeAnalyzer) phasePrepared(ref int, p *preparedQuery, offset, radius float64) (*PhaseEvidence, error) {
	if p == nil {
		return nil, nil
	}
	left := math.Max(0, p.start+offset-radius)
	right := math.Min(a.records[ref].Duration, p.start+offset+p.length+radius)
	if right-left < p.length {
		return nil, nil
	}
	r, err := a.read(ref, left, right-left)
	if err != nil {
		return nil, err
	}
	lag, prominence, contrast, ok := phasePeak(prepareWave(r), p.wave)
	if !ok {
		return nil, nil
	}
	return &PhaseEvidence{left - p.start + float64(lag)*.0005, prominence, contrast, prominence >= 1.2 && contrast >= 5}, nil
}
