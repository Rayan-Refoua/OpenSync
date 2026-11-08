package acousticverify

import (
	"fmt"
	"math"

	"opensync/internal/audio"
	"opensync/internal/dsp"
)

// SpectralEvidence measures agreement at an already estimated offset.
// It does not search for a new offset or certify acoustic identity.
type SpectralEvidence struct {
	BandCorrelation [3]float64 `json:"band_correlation"`
	FramesCompared  int        `json:"frames_compared"`
	Method          string     `json:"method"`
}

func pearson(a, b []float64) (float64, bool) {
	if len(a) != len(b) || len(a) < 3 {
		return 0, false
	}
	var sa, sb, aa, bb, ab float64
	for i := range a {
		sa += a[i]
		sb += b[i]
		aa += a[i] * a[i]
		bb += b[i] * b[i]
		ab += a[i] * b[i]
	}
	n := float64(len(a))
	va := aa - sa*sa/n
	vb := bb - sb*sb/n
	if va <= 1e-18 || vb <= 1e-18 {
		return 0, false
	}
	c := (ab - sa*sb/n) / math.Sqrt(va*vb)
	return math.Max(-1, math.Min(1, c)), true
}

// Corroborate compares spectral-flux patterns in the same two
// time-aligned acoustic regions used by a waveform measurement.
//
// Each extraction starts at the measured local sample position.
// Spectral evidence is complementary, not statistically independent
// of the waveform evidence extracted from the same acoustic events.
func Corroborate(
	ref, query *audio.Reader,
	m Measurement,
) (SpectralEvidence, error) {
	if ref == nil || query == nil ||
		ref.SampleRate() != dsp.SampleRate ||
		query.SampleRate() != dsp.SampleRate ||
		m.QueryStartNS < 0 || m.ReferenceNS < 0 ||
		m.WindowNS <= 0 || m.WindowNS > 10_000_000_000 {
		return SpectralEvidence{}, ErrProbe
	}

	qstart := samples(m.QueryStartNS)
	rstart := samples(m.ReferenceNS)
	count := samples(m.WindowNS)
	if count < dsp.WindowSize+dsp.HopSize ||
		qstart > query.Frames() ||
		rstart > ref.Frames() ||
		count > query.Frames()-qstart ||
		count > ref.Frames()-rstart {
		return SpectralEvidence{}, ErrProbe
	}

	qf, err := dsp.Extract(query, qstart, count)
	if err != nil {
		return SpectralEvidence{}, err
	}
	rf, err := dsp.Extract(ref, rstart, count)
	if err != nil {
		return SpectralEvidence{}, err
	}
	if len(qf.Frames) != len(rf.Frames) {
		return SpectralEvidence{}, fmt.Errorf("unequal feature frame counts")
	}

	result := SpectralEvidence{
		FramesCompared: len(qf.Frames) - 1,
		Method:         "three_band_log_spectral_flux_pearson_v1",
	}

	for band := 0; band < 3; band++ {
		qa := make([]float64, 0, result.FramesCompared)
		ra := make([]float64, 0, result.FramesCompared)

		// Skip the first frame: its flux has no preceding spectrum.
		for i := 1; i < len(qf.Frames); i++ {
			qa = append(qa, qf.Frames[i].Flux[band])
			ra = append(ra, rf.Frames[i].Flux[band])
		}
		correlation, ok := pearson(qa, ra)
		if !ok {
			return SpectralEvidence{}, fmt.Errorf(
				"%w: insufficient spectral variation in band %d",
				ErrProbe, band,
			)
		}
		result.BandCorrelation[band] = correlation
	}
	return result, nil
}
