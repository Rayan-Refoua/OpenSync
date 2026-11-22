package autosync

import (
	"context"
	"fmt"
	"math"
	"opensync/internal/audio"
	"opensync/internal/dsp"
	"opensync/internal/researchmatch"
	"sort"
	"sync"
	"sync/atomic"
)

type Measurement struct {
	QueryTime             float64        `json:"query_time"`
	Offset                float64        `json:"offset"`
	Wave                  float64        `json:"wave_correlation"`
	Window                float64        `json:"window"`
	Competing             float64        `json:"competing_local_correlation"`
	Distinct              bool           `json:"local_peak_distinct"`
	Flux                  float64        `json:"flux_correlation"`
	Controls              int            `json:"controls"`
	Control               float64        `json:"strongest_control"`
	Supported             bool           `json:"supported"`
	Precise               bool           `json:"precise"`
	Stable                bool           `json:"multiscale_consistent"`
	Central               *float64       `json:"central_offset"`
	Radius                float64        `json:"search_radius"`
	Phase                 *PhaseEvidence `json:"phase"`
	PhaseAgrees           bool           `json:"phase_agrees"`
	Boundary              bool           `json:"at_search_boundary"`
	Passes                bool           `json:"passes,omitempty"`
	AcousticallySupported bool           `json:"acoustically_supported,omitempty"`
	Expected              float64        `json:"expected_offset,omitempty"`
	Residual              float64        `json:"residual_seconds,omitempty"`
}
type PhaseEvidence struct {
	Offset     float64 `json:"offset"`
	Prominence float64 `json:"prominence"`
	Contrast   float64 `json:"contrast"`
	Distinct   bool    `json:"distinct"`
}
type NativeAnalyzer struct {
	ctx          context.Context
	records      []MediaRecord
	readers      []*audio.Reader
	io           chan struct{}
	lock         sync.Mutex
	computeOnce  sync.Once
	compute      *nativeComputePool
	computeStats atomic.Pointer[nativeComputePool]
}

func NewNativeAnalyzer(records []MediaRecord) *NativeAnalyzer {
	return &NativeAnalyzer{ctx: context.Background(), records: records, readers: make([]*audio.Reader, len(records)), io: make(chan struct{}, 8)}
}
func (a *NativeAnalyzer) Close() {
	if a.compute != nil {
		a.compute.close()
	}
	for _, r := range a.readers {
		if r != nil {
			r.Close()
		}
	}
}
func pcmWindowFrames(start, length float64, total int64) (first, count int64) {
	first, count = roundFrame(start*16000), roundFrame(length*16000)
	// A window ending at the recording boundary can round up both its start
	// and length even though its combined endpoint is within the PCM grid.
	// Preserve the existing length for interior windows.
	if first >= 0 && count > 0 && first+count == total+1 &&
		(start+length)*16000 <= float64(total)+0.500001 {
		count--
	}
	return first, count
}

func (a *NativeAnalyzer) read(record int, start, length float64) ([]float32, error) {
	if err := a.ctx.Err(); err != nil {
		return nil, err
	}
	if w := memoryWorkspace(a.ctx); w != nil {
		r := a.records[record]
		first, count := pcmWindowFrames(start, length, roundFrame(r.Duration*16000))
		samples, err := w.audio.read(a.ctx, r, first, count)
		if err != nil {
			return nil, fmt.Errorf("%s: PCM window %.9fs + %.9fs: %w", r.Name, start, length, err)
		}
		values := make([]float32, len(samples))
		for i, v := range samples {
			values[i] = float32(v) / 32768
		}
		return values, nil
	}
	a.lock.Lock()
	r := a.readers[record]
	var err error
	if r == nil {
		r, err = audio.Open(a.records[record].Cache)
		if err == nil {
			a.readers[record] = r
		}
	}
	a.lock.Unlock()
	if err != nil {
		return nil, err
	}
	first, count := pcmWindowFrames(start, length, r.Frames())
	if first < 0 || count < 0 || first+count > r.Frames() {
		return nil, fmt.Errorf("%s: PCM interval outside recording (start %.9fs, length %.9fs, available %d samples)", a.records[record].Name, start, length, r.Frames())
	}
	a.io <- struct{}{}
	samples, err := r.ReadFrames(first, count)
	<-a.io
	if err != nil {
		return nil, err
	}
	values := make([]float32, len(samples))
	for i, v := range samples {
		values[i] = float32(v) / 32768
	}
	return values, nil
}
func prepareWave(x []float32) []float64 {
	n := len(x) / 8
	if n < 2 {
		return nil
	}
	out := make([]float64, n-1)
	previous := float32(0)
	for i := 0; i < n; i++ {
		sum := float32(0)
		for _, v := range x[i*8 : i*8+8] {
			sum += v
		}
		mean := sum / 8
		if i > 0 {
			out[i-1] = float64(mean - previous)
		}
		previous = mean
	}
	return out
}
func median(x []float64) float64 {
	if len(x) == 0 {
		return 0
	}
	v := append([]float64(nil), x...)
	sort.Float64s(v)
	return (v[(len(v)-1)/2] + v[len(v)/2]) / 2
}
func linspace(a, b float64, n int) []float64 {
	if n <= 0 {
		return nil
	}
	r := make([]float64, n)
	for i := range r {
		r[i] = a
		if n > 1 {
			r[i] = a + (b-a)*float64(i)/float64(n-1)
		}
	}
	if n > 1 {
		r[n-1] = b
	}
	return r
}
func pearson(a, b []float64) float64 {
	if len(a) != len(b) || len(a) < 2 {
		return 0
	}
	sa, sb := 0., 0.
	for i := range a {
		sa += a[i]
		sb += b[i]
	}
	sa /= float64(len(a))
	sb /= float64(len(a))
	ab, aa, bb := 0., 0., 0.
	for i := range a {
		x, y := a[i]-sa, b[i]-sb
		ab += x * y
		aa += x * x
		bb += y * y
	}
	if aa/float64(len(a)) < 1e-16 || bb/float64(len(b)) < 1e-16 {
		return 0
	}
	return math.Max(-1, math.Min(1, ab/math.Sqrt(aa*bb)))
}
func normCorrelation(r, q []float64) (int, float64, float64, error) {
	if len(q) < 8 || len(r) < len(q) {
		return 0, 0, 0, nil
	}
	mean := 0.
	for _, v := range q {
		mean += v
	}
	mean /= float64(len(q))
	center := make([]float64, len(q))
	energy := 0.
	for i, v := range q {
		center[i] = v - mean
		energy += center[i] * center[i]
	}
	if energy < 1e-10 {
		return 0, 0, 0, nil
	}
	dots, err := researchmatch.CorrelationDots(r, center)
	if err != nil {
		return 0, 0, 0, err
	}
	sums, squares := make([]float64, len(r)+1), make([]float64, len(r)+1)
	for i, v := range r {
		sums[i+1] = sums[i] + v
		squares[i+1] = squares[i] + v*v
	}
	values := make([]float64, len(r)-len(q)+1)
	best := 0
	for i := range values {
		sum := sums[i+len(q)] - sums[i]
		variance := squares[i+len(q)] - squares[i] - sum*sum/float64(len(q))
		if variance >= 1e-10 {
			values[i] = dots[i+len(q)-1] / math.Sqrt(math.Max(variance, 1e-20)*energy)
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
func (a *NativeAnalyzer) waveform(ref, query int, qtime, offset, length, radius float64) (*Measurement, error) {
	start := math.Max(0, qtime+offset-radius)
	stop := math.Min(a.records[ref].Duration, qtime+offset+length+radius)
	if stop-start < length || qtime < 0 || qtime+length > a.records[query].Duration {
		return nil, nil
	}
	q, err := a.read(query, qtime, length)
	if err != nil {
		return nil, err
	}
	r, err := a.read(ref, start, stop-start)
	if err != nil {
		return nil, err
	}
	lag, corr, competing, err := normCorrelation(prepareWave(r), prepareWave(q))
	if err != nil {
		return nil, err
	}
	if corr == 0 {
		return nil, nil
	}
	wave := math.Abs(corr)
	return &Measurement{QueryTime: qtime + length/2, Offset: start - qtime + float64(lag)*8/16000, Wave: wave, Window: length, Competing: competing, Distinct: wave-competing >= .025 && wave >= 1.1*competing}, nil
}

var fluxPlan, _ = dsp.NewFFTPlan(512)
var fluxWindow = func() []float32 {
	w := make([]float32, 512)
	for i := range w {
		w[i] = float32(.5 - .5*math.Cos(2*math.Pi*float64(i)/512))
	}
	return w
}()

func fluxFeatures(x []float32) [3][]float64 {
	var out [3][]float64
	if len(x) < 512 {
		return out
	}
	frames := 1 + (len(x)-512)/128
	for b := range out {
		out[b] = make([]float64, max(0, frames-1))
	}
	previous := make([]float32, 257)
	buffer := make([]complex128, 512)
	bands := [][2]int{{7, 29}, {29, 77}, {77, 193}}
	for t := 0; t < frames; t++ {
		for i := range buffer {
			buffer[i] = complex(float64(x[t*128+i]*fluxWindow[i]), 0)
		}
		fluxPlan.Transform(buffer)
		sums := [3]float32{}
		for f := 7; f < 193; f++ {
			re, im := float32(real(buffer[f])/256), float32(imag(buffer[f])/256)
			mag := float32(math.Hypot(float64(re), float64(im)))
			log := float32(math.Log1p(float64(float32(100 * mag))))
			diff := max(float32(0), log-previous[f])
			previous[f] = log
			for b, band := range bands {
				if f >= band[0] && f < band[1] {
					sums[b] += diff
					break
				}
			}
		}
		if t > 0 {
			for b, band := range bands {
				out[b][t-1] = float64(sums[b] / float32(band[1]-band[0]))
			}
		}
	}
	return out
}
func (a *NativeAnalyzer) fluxScore(ref, query int, qtime, offset, length float64) (float64, error) {
	if qtime+offset < 0 || qtime+offset+length > a.records[ref].Duration {
		return 0, nil
	}
	r, err := a.read(ref, qtime+offset, length)
	if err != nil {
		return 0, err
	}
	q, err := a.read(query, qtime, length)
	if err != nil {
		return 0, err
	}
	rf, qf := fluxFeatures(r), fluxFeatures(q)
	values := make([]float64, 3)
	for b := range values {
		values[b] = pearson(rf[b], qf[b])
	}
	return median(values), nil
}
func phasePeak(r, q []float64) (lag int, prominence, contrast float64, ok bool) {
	if len(q) < 64 || len(r) < len(q) {
		return
	}
	mr, mq := 0., 0.
	for _, v := range r {
		mr += v
	}
	for _, v := range q {
		mq += v
	}
	mr /= float64(len(r))
	mq /= float64(len(q))
	size := dsp.NextFastLen(len(r) + len(q) - 1)
	a, b := make([]complex128, size), make([]complex128, size)
	vr, vq := 0., 0.
	for i, v := range r {
		x := v - mr
		a[i] = complex(x, 0)
		vr += x * x
	}
	for i, v := range q {
		x := v - mq
		b[i] = complex(x, 0)
		vq += x * x
	}
	if vr/float64(len(r)) < 1e-14 || vq/float64(len(q)) < 1e-14 {
		return
	}
	var workspace dsp.ArbitraryFFTWorkspace
	workspace.Transform(a)
	workspace.Transform(b)
	magnitudes := make([]float64, size)
	maxMag := 0.
	for i := range a {
		a[i] *= complex(real(b[i]), -imag(b[i]))
		magnitudes[i] = math.Hypot(real(a[i]), imag(a[i]))
		maxMag = math.Max(maxMag, magnitudes[i])
	}
	for i := range a {
		den := math.Pow(math.Max(magnitudes[i], maxMag*1e-6), .8)
		a[i] /= complex(den, 0)
		a[i] = complex(real(a[i]), -imag(a[i]))
	}
	workspace.Transform(a)
	values := make([]float64, len(r)-len(q)+1)
	peak, meanSquare := 0., 0.
	for i := range values {
		v := math.Abs(real(a[i]) / float64(size))
		values[i] = v
		meanSquare += v * v
		if v > peak {
			peak = v
			lag = i
		}
	}
	competitor := 0.
	for i, v := range values {
		if i < lag-8 || i > lag+8 {
			competitor = math.Max(competitor, v)
		}
	}
	prominence = peak / math.Max(competitor, 1e-20)
	contrast = peak / math.Max(math.Sqrt(meanSquare/float64(len(values))), 1e-20)
	ok = true
	return
}
func (a *NativeAnalyzer) phaseCheck(ref, query int, start, offset, length, radius float64) (*PhaseEvidence, error) {
	left := math.Max(0, start+offset-radius)
	right := math.Min(a.records[ref].Duration, start+offset+length+radius)
	if right-left < length {
		return nil, nil
	}
	r, err := a.read(ref, left, right-left)
	if err != nil {
		return nil, err
	}
	q, err := a.read(query, start, length)
	if err != nil {
		return nil, err
	}
	lag, p, c, ok := phasePeak(prepareWave(r), prepareWave(q))
	if !ok {
		return nil, nil
	}
	return &PhaseEvidence{left - start + float64(lag)*.0005, p, c, p >= 1.2 && c >= 5}, nil
}
