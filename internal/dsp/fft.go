package dsp

import (
	"errors"
	"math"
	"math/cmplx"
)

var ErrFFTSize = errors.New("FFT length must be a positive power of two")

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
