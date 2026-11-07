package dsp

import (
	"math"
	"math/cmplx"
	"testing"
)

func TestFFTAgainstDirectDFT(t *testing.T) {
	for _, n := range []int{1, 2, 4, 8, 16, 32} {
		signal := make([]complex128, n)
		for i := range signal {
			signal[i] = complex(
				math.Sin(float64(i)*0.7),
				math.Cos(float64(i)*0.3),
			)
		}

		got := append([]complex128(nil), signal...)
		if err := FFT(got); err != nil {
			t.Fatal(err)
		}

		for k := 0; k < n; k++ {
			var expected complex128
			for j, v := range signal {
				angle := -2 * math.Pi * float64(k*j) / float64(n)
				expected += v * cmplx.Rect(1, angle)
			}
			if cmplx.Abs(got[k]-expected) > 1e-9 {
				t.Fatalf("n=%d bin=%d got=%v expected=%v",
					n, k, got[k], expected)
			}
		}
	}
}

func TestFFTRejectsInvalidLength(t *testing.T) {
	for _, n := range []int{0, 3, 5, 7} {
		if err := FFT(make([]complex128, n)); err == nil {
			t.Fatalf("accepted FFT length %d", n)
		}
	}
}
