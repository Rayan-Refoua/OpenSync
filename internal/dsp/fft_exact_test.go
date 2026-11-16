package dsp

import (
	"math"
	"math/cmplx"
	"math/rand"
	"testing"
)

func uncachedFFT(data []complex128) error {
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

func TestFFTCachedTwiddleParity(t *testing.T) {
	random := rand.New(rand.NewSource(22))
	for _, n := range []int{1, 64, 1024, 16384, 65536} {
		got := make([]complex128, n)
		for i := range got {
			got[i] = complex(random.NormFloat64(), random.NormFloat64())
		}
		want := append([]complex128(nil), got...)
		if err := uncachedFFT(want); err != nil {
			t.Fatal(err)
		}
		if err := FFT(got); err != nil {
			t.Fatal(err)
		}
		for i, x := range got {
			if x != want[i] {
				t.Fatalf("n=%d bin=%d: %v != %v", n, i, x, want[i])
			}
		}
	}
}

func BenchmarkFingerprintFFT(b *testing.B) {
	for _, variant := range []struct {
		name      string
		transform func([]complex128) error
	}{{"original", uncachedFFT}, {"cached", FFT}} {
		b.Run(variant.name, func(b *testing.B) {
			samples := make([]complex128, 16384)
			input := make([]complex128, len(samples))
			for i := range input {
				input[i] = complex(float64(i%19)-9, 0)
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				copy(samples, input)
				if err := variant.transform(samples); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
