package dsp

import (
	"errors"
	"math"
	"math/bits"
	"math/cmplx"
	"sync"
)

var ErrFFTSize = errors.New("FFT length must be a positive power of two")

// Each stage used the same recurrence from 1 in every butterfly block.
// Cache that exact sequence (rather than recomputing angles per coefficient).
const cachedFFTStages = 18

var fftStages [cachedFFTStages]struct {
	once     sync.Once
	twiddles []complex128
}

func stageTwiddles(length int, step complex128) []complex128 {
	level := bits.TrailingZeros(uint(length)) - 1
	if level >= len(fftStages) {
		return nil
	}
	entry := &fftStages[level]
	entry.once.Do(func() {
		entry.twiddles = make([]complex128, length/2)
		w := complex(1, 0)
		for i := range entry.twiddles {
			entry.twiddles[i] = w
			w *= step
		}
	})
	return entry.twiddles
}

// FFT transforms data in place using an iterative radix-2 FFT.
// The forward transform is unnormalized.
func FFT(data []complex128) error {
	n := len(data)
	if n == 0 || n&(n-1) != 0 {
		return ErrFFTSize
	}

	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for j&bit != 0 {
			j ^= bit
			bit >>= 1
		}
		j ^= bit
		if i < j {
			data[i], data[j] = data[j], data[i]
		}
	}

	for length := 2; length <= n; length *= 2 {
		angle := -2 * math.Pi / float64(length)
		wlen := cmplx.Rect(1, angle)
		if coefficients := stageTwiddles(length, wlen); coefficients != nil {
			for start := 0; start < n; start += length {
				block := data[start : start+length]
				left, right := block[:length/2], block[length/2:]
				for j, w := range coefficients {
					a, b := left[j], right[j]*w
					left[j], right[j] = a+b, a-b
				}
			}
			continue
		}
		for start := 0; start < n; start += length {
			w := complex(1, 0)
			for j := 0; j < length/2; j++ {
				a := data[start+j]
				b := data[start+j+length/2] * w
				data[start+j] = a + b
				data[start+j+length/2] = a - b
				w *= wlen
			}
		}
	}
	return nil
}
