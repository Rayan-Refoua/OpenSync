package dsp

import (
	"errors"
	"math"
	"math/cmplx"

	"opensync/internal/audio"
)

const (
	WindowSize = 512
	HopSize    = 128
	SampleRate = 16000
)

var ErrFeatureRange = errors.New("invalid feature extraction range")

// Frame carries the absolute sample position of the FFT window center.
// Energy is mean-square normalized PCM amplitude.
// Flux contains positive log-spectral changes in three frequency bands.
type Frame struct {
	CenterSample int64
	Energy       float64
	Flux         [3]float64
}

type Features struct {
	Frames []Frame
}

var bands = [3][2]float64{
	{200, 900},
	{900, 2400},
	{2400, 6000},
}

// Extract operates on a bounded PCM interval. It deliberately does not
// decide whether recordings match or create any timeline relationships.
// Frame-to-frame flux is local to this extraction interval.
func Extract(r *audio.Reader, start, count int64) (Features, error) {
	if start < 0 || count < WindowSize || count > 10*SampleRate {
		return Features{}, ErrFeatureRange
	}
	if r.SampleRate() != SampleRate {
		return Features{}, errors.New("unexpected sample rate")
	}

	pcm, err := r.ReadFrames(start, count)
	if err != nil {
		return Features{}, err
	}

	number := 1 + (len(pcm)-WindowSize)/HopSize
	result := Features{Frames: make([]Frame, 0, number)}

	var window [WindowSize]float64
	for i := range window {
		window[i] = 0.5 - 0.5*math.Cos(
			2*math.Pi*float64(i)/float64(WindowSize-1))
	}

	// Reuse FFT and spectral buffers for all frames in this block.
	spectrum := make([]complex128, WindowSize)
	previous := make([]float64, WindowSize/2+1)
	current := make([]float64, WindowSize/2+1)

	for frame := 0; frame < number; frame++ {
		base := frame * HopSize
		var energy float64

		for i := 0; i < WindowSize; i++ {
			v := float64(pcm[base+i]) / 32768
			energy += v * v
			spectrum[i] = complex(v*window[i], 0)
		}

		if err := FFT(spectrum); err != nil {
			return Features{}, err
		}

		out := Frame{
			CenterSample: start + int64(base+WindowSize/2),
			Energy:       energy / WindowSize,
		}

		for bin := range current {
			current[bin] = math.Log1p(100 * cmplx.Abs(spectrum[bin]))
		}

		if frame > 0 {
			var counts [3]int
			for bin := 1; bin < len(current); bin++ {
				hz := float64(bin) * SampleRate / WindowSize
				change := math.Max(0, current[bin]-previous[bin])

				for band, limits := range bands {
					if hz >= limits[0] && hz < limits[1] {
						out.Flux[band] += change
						counts[band]++
					}
				}
			}
			for band, n := range counts {
				if n != 0 {
					out.Flux[band] /= float64(n)
				}
			}
		}

		copy(previous, current)
		result.Frames = append(result.Frames, out)
	}

	return result, nil
}
