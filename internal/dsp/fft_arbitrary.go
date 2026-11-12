package dsp

import (
	"math"
	"math/cmplx"
	"sync"
)

// NextFastLen matches the smooth complex-transform lengths used by the
// reference verifier. Padding length affects PHAT weighting, not just speed.
func NextFastLen(n int) int {
	for {
		m := n
		for _, p := range []int{2, 3, 5, 7, 11} {
			for m%p == 0 && m > 1 {
				m /= p
			}
		}
		if m == 1 {
			return n
		}
		n++
	}
}

type chirpPlan struct {
	n, size       int
	chirp, kernel []complex128
}

var chirpCache = struct {
	sync.Mutex
	plans map[int]*chirpPlan
	bytes int
}{plans: map[int]*chirpPlan{}}

func newChirp(n int) *chirpPlan {
	p := &chirpPlan{n: n, size: 1}
	for p.size < 2*n-1 {
		p.size *= 2
	}
	p.chirp = make([]complex128, n)
	p.kernel = make([]complex128, p.size)
	for i := 0; i < n; i++ {
		angle := math.Pi * float64((int64(i)*int64(i))%int64(2*n)) / float64(n)
		p.chirp[i] = cmplx.Rect(1, -angle)
		v := cmplx.Conj(p.chirp[i])
		p.kernel[i] = v
		if i > 0 {
			p.kernel[p.size-i] = v
		}
	}
	FFT(p.kernel)
	return p
}
func FFTArbitrary(data []complex128) error {
	n := len(data)
	if n < 1 {
		return ErrFFTSize
	}
	if n&(n-1) == 0 {
		return FFT(data)
	}
	chirpCache.Lock()
	p := chirpCache.plans[n]
	chirpCache.Unlock()
	if p == nil {
		p = newChirp(n)
		bytes := 16 * (len(p.chirp) + len(p.kernel))
		chirpCache.Lock()
		if chirpCache.bytes+bytes <= 64<<20 {
			if prior := chirpCache.plans[n]; prior != nil {
				p = prior
			} else {
				chirpCache.plans[n] = p
				chirpCache.bytes += bytes
			}
		}
		chirpCache.Unlock()
	}
	work := make([]complex128, p.size)
	for i, v := range data {
		work[i] = v * p.chirp[i]
	}
	FFT(work)
	for i := range work {
		work[i] = cmplx.Conj(work[i] * p.kernel[i])
	}
	FFT(work)
	for i := range data {
		data[i] = cmplx.Conj(work[i]) / complex(float64(p.size), 0) * p.chirp[i]
	}
	return nil
}
