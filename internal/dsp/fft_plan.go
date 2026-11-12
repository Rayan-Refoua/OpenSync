package dsp

import (
	"math"
	"math/cmplx"
)

// FFTPlan caches immutable permutations and twiddles for repeated transforms.
// A plan can be shared by workers; each worker owns its transform buffer.
type FFTPlan struct {
	reverse  []int
	twiddles [][]complex128
}

func NewFFTPlan(size int) (*FFTPlan, error) {
	if size < 1 || size&(size-1) != 0 {
		return nil, ErrFFTSize
	}
	p := &FFTPlan{reverse: make([]int, size)}
	for i, j := 1, 0; i < size; i++ {
		bit := size >> 1
		for j&bit != 0 {
			j ^= bit
			bit >>= 1
		}
		j ^= bit
		p.reverse[i] = j
	}
	for length := 2; length <= size; length *= 2 {
		row := make([]complex128, length/2)
		for j := range row {
			row[j] = cmplx.Rect(1, -2*math.Pi*float64(j)/float64(length))
		}
		p.twiddles = append(p.twiddles, row)
	}
	return p, nil
}
func (p *FFTPlan) Transform(data []complex128) error {
	if len(data) != len(p.reverse) {
		return ErrFFTSize
	}
	for i, j := range p.reverse {
		if i < j {
			data[i], data[j] = data[j], data[i]
		}
	}
	for stage, twiddles := range p.twiddles {
		length := 2 << stage
		for start := 0; start < len(data); start += length {
			for j, w := range twiddles {
				a, b := data[start+j], data[start+j+length/2]*w
				data[start+j] = a + b
				data[start+j+length/2] = a - b
			}
		}
	}
	return nil
}
