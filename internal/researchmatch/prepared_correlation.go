package researchmatch

import (
	"math/cmplx"
	"opensync/internal/dsp"
)

// CorrelationQuery belongs to one measurement. Its query transform can be
// reused for several reference positions. It is not shared between workers or
// persisted. One padded query transform and its reference scratch buffer are
// retained. Returned dot products have independent storage.
type CorrelationQuery struct {
	query    []float64
	spectrum []complex128
	scratch  []complex128
}

func PrepareCorrelationQuery(query []float64) *CorrelationQuery {
	return &CorrelationQuery{query: query}
}
func (q *CorrelationQuery) Dots(reference []float64) ([]float64, error) {
	size, err := nextPow2(len(reference) + len(q.query) - 1)
	if err != nil {
		return nil, err
	}
	if len(q.spectrum) != size {
		q.spectrum = make([]complex128, size)
		for i := range q.query {
			q.spectrum[i] = complex(q.query[len(q.query)-1-i], 0)
		}
		if err = dsp.FFT(q.spectrum); err != nil {
			q.spectrum = nil
			return nil, err
		}
	}
	if len(q.scratch) != size {
		q.scratch = make([]complex128, size)
	}
	a := q.scratch
	for i, x := range reference {
		a[i] = complex(x, 0)
	}
	clear(a[len(reference):])
	if err = dsp.FFT(a); err != nil {
		return nil, err
	}
	for i := range a {
		a[i] *= q.spectrum[i]
		a[i] = cmplx.Conj(a[i])
	}
	if err = dsp.FFT(a); err != nil {
		return nil, err
	}
	out := make([]float64, len(reference)+len(q.query)-1)
	for i := range out {
		out[i] = real(cmplx.Conj(a[i])) / float64(size)
	}
	return out, nil
}
