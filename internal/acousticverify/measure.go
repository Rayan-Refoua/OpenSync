package acousticverify

import (
	"errors"
	"math"

	"opensync/internal/audio"
)

const (
	sampleRate = 16000
	decimation = 8
)

// Probe requests a bounded measurement. OffsetNS follows the
// project convention: reference_local = query_local + offset.
type Probe struct {
	QueryStartNS int64
	OffsetNS     int64
	WindowNS     int64
	RadiusNS     int64
}

// Measurement is acoustic evidence, NOT certification.
// Correlation alone must never admit a synchronization edge.
type Measurement struct {
	QueryStartNS   int64   `json:"query_start_ns"`
	ReferenceNS    int64   `json:"reference_ns"`
	MeasuredNS     int64   `json:"measured_offset_ns"`
	Correlation    float64 `json:"correlation"`
	SearchRadiusNS int64   `json:"search_radius_ns"`
	WindowNS       int64   `json:"window_ns"`
	Method         string  `json:"method"`
}

var ErrProbe = errors.New("invalid or out-of-bounds acoustic probe")

func samples(ns int64) int64 {
	return int64(math.Round(float64(ns) * sampleRate / 1e9))
}

func downsample(pcm []int16) []float64 {
	n := len(pcm) / decimation
	out := make([]float64, n)
	for i := range out {
		var sum float64
		for j := 0; j < decimation; j++ {
			sum += float64(pcm[i*decimation+j])
		}
		out[i] = sum / decimation
	}
	// First differences suppress stationary low-frequency components.
	for i := len(out) - 1; i > 0; i-- {
		out[i] -= out[i-1]
	}
	if len(out) > 0 {
		out[0] = 0
	}
	return out
}

// bestLag searches all complete query placements inside reference.
// It maximizes absolute Pearson correlation, allowing polarity flips.
func bestLag(query, reference []float64) (int, float64, error) {
	n := len(query)
	if n < 2 || len(reference) < n {
		return 0, 0, ErrProbe
	}

	var qsum, qsq float64
	for _, v := range query {
		qsum += v
		qsq += v * v
	}
	qvar := qsq - qsum*qsum/float64(n)
	if qvar <= 1e-12 {
		return 0, 0, ErrProbe
	}

	best := -1.0
	bestLag := 0
	bestCorr := 0.0

	for lag := 0; lag <= len(reference)-n; lag++ {
		var rsum, rsq, dot float64
		for i, v := range query {
			r := reference[lag+i]
			rsum += r
			rsq += r * r
			dot += v * r
		}
		rvar := rsq - rsum*rsum/float64(n)
		if rvar <= 1e-12 {
			continue
		}
		corr := (dot - qsum*rsum/float64(n)) /
			math.Sqrt(qvar*rvar)
		if math.Abs(corr) > best {
			best = math.Abs(corr)
			bestLag = lag
			bestCorr = corr
		}
	}
	if best < 0 {
		return 0, 0, ErrProbe
	}
	return bestLag, bestCorr, nil
}

// Measure reads only the requested windows from two PCM caches.
// It does not modify media, project state, or the graph.
func Measure(ref, query *audio.Reader, p Probe) (Measurement, error) {
	if ref == nil || query == nil ||
		ref.SampleRate() != sampleRate ||
		query.SampleRate() != sampleRate ||
		p.QueryStartNS < 0 || p.WindowNS <= 0 ||
		p.RadiusNS < 0 || p.RadiusNS > 1e9 ||
		p.WindowNS > 10e9 {
		return Measurement{}, ErrProbe
	}

	qstart := samples(p.QueryStartNS)
	window := samples(p.WindowNS)
	radius := samples(p.RadiusNS)
	expected := samples(p.OffsetNS) + qstart

	// Use a decimation-aligned search range.
	start := expected - radius
	start -= ((start % decimation) + decimation) % decimation

	qcount := window / decimation * decimation
	if qcount < 2*decimation {
		return Measurement{}, ErrProbe
	}
	rcount := qcount + 2*(radius/decimation+2)*decimation

	if qstart < 0 || start < 0 ||
		qstart+qcount > query.Frames() ||
		start+rcount > ref.Frames() {
		return Measurement{}, ErrProbe
	}

	qpcm, err := query.ReadFrames(qstart, qcount)
	if err != nil {
		return Measurement{}, err
	}
	rpcm, err := ref.ReadFrames(start, rcount)
	if err != nil {
		return Measurement{}, err
	}

	lag, corr, err := bestLag(downsample(qpcm), downsample(rpcm))
	if err != nil {
		return Measurement{}, err
	}

	referenceSample := start + int64(lag*decimation)
	// A valid search result must lie within the requested radius.
	if referenceSample < expected-radius-decimation ||
		referenceSample > expected+radius+decimation {
		return Measurement{}, ErrProbe
	}

	return Measurement{
		QueryStartNS:   p.QueryStartNS,
		ReferenceNS:    referenceSample * 1e9 / sampleRate,
		MeasuredNS:     (referenceSample - qstart) * 1e9 / sampleRate,
		Correlation:    corr,
		SearchRadiusNS: p.RadiusNS,
		WindowNS:       p.WindowNS,
		Method:         "pcm16_mean8_first_difference_pearson_v1",
	}, nil
}
