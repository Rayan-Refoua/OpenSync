package dsp

import (
	"math"
	"math/cmplx"
	"testing"
)

func TestArbitraryFFTAgainstDirectTransform(t *testing.T) {
	for _, n := range []int{1, 2, 3, 7, 11, 16, 25, 49, 77, 121, 143, 512} {
		input := make([]complex128, n)
		for i := range input {
			input[i] = complex(math.Sin(float64(i)*.731), math.Cos(float64(i)*.21))
		}
		got := append([]complex128(nil), input...)
		if err := FFTArbitrary(got); err != nil {
			t.Fatal(err)
		}
		for k := range got {
			want := complex(0, 0)
			for j, x := range input {
				want += x * cmplx.Rect(1, -2*math.Pi*float64(j*k)/float64(n))
			}
			if cmplx.Abs(got[k]-want) > 1e-9 {
				t.Fatalf("n=%d k=%d difference %g", n, k, cmplx.Abs(got[k]-want))
			}
		}
	}
}
func TestNextFastLen(t *testing.T) {
	for n, want := range map[int]int{1: 1, 13: 14, 17: 18, 1001: 1008, 12345: 12348, 32769: 32805} {
		if got := NextFastLen(n); got != want {
			t.Errorf("%d: got %d want %d", n, got, want)
		}
	}
}
