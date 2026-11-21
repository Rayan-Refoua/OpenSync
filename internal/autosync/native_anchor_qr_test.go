package autosync

import (
	"math"
	"math/rand"
	"testing"
)

func originalLeastSquares(rows [][]float64, targets []float64) ([]float64, bool) {
	if len(rows) == 0 || len(rows) != len(targets) {
		return nil, false
	}
	m, n := len(rows), len(rows[0])
	if m < n {
		return nil, false
	}
	a := make([][]float64, m)
	for i := range a {
		a[i] = append([]float64(nil), rows[i]...)
	}
	b := append([]float64(nil), targets...)
	perm := make([]int, n)
	for i := range perm {
		perm[i] = i
	}
	for k := 0; k < n; k++ {
		best, norm := k, 0.
		for j := k; j < n; j++ {
			v := 0.
			for i := k; i < m; i++ {
				v = math.Hypot(v, a[i][j])
			}
			if v > norm {
				norm = v
				best = j
			}
		}
		if norm < 1e-12 {
			return nil, false
		}
		if best != k {
			for i := range a {
				a[i][k], a[i][best] = a[i][best], a[i][k]
			}
			perm[k], perm[best] = perm[best], perm[k]
		}
		alpha := -math.Copysign(norm, a[k][k])
		v := make([]float64, m-k)
		for i := k; i < m; i++ {
			v[i-k] = a[i][k]
		}
		v[0] -= alpha
		vn := 0.
		for _, x := range v {
			vn = math.Hypot(vn, x)
		}
		for i := range v {
			v[i] /= vn
		}
		for j := k; j < n; j++ {
			dot := 0.
			for i, x := range v {
				dot += x * a[k+i][j]
			}
			for i, x := range v {
				a[k+i][j] -= 2 * x * dot
			}
		}
		dot := 0.
		for i, x := range v {
			dot += x * b[k+i]
		}
		for i, x := range v {
			b[k+i] -= 2 * x * dot
		}
	}
	z := make([]float64, n)
	for i := n - 1; i >= 0; i-- {
		v := b[i]
		for j := i + 1; j < n; j++ {
			v -= a[i][j] * z[j]
		}
		z[i] = v / a[i][i]
	}
	result := make([]float64, n)
	for i, p := range perm {
		result[p] = z[i]
	}
	return result, true
}

func TestColumnMajorHouseholderParity(t *testing.T) {
	random := rand.New(rand.NewSource(17))
	for _, n := range []int{1, 4, 19, 50} {
		m := 2*n + 3
		rows := make([][]float64, m)
		rhs := make([]float64, m)
		cols := make([][]float64, n)
		for j := range cols {
			cols[j] = make([]float64, m)
		}
		for i := range rows {
			rows[i] = make([]float64, n)
			rhs[i] = random.NormFloat64()
			for j := range cols {
				if random.Intn(4) == 0 || i == j {
					rows[i][j] = random.NormFloat64() * float64(1+j*j)
				}
				cols[j][i] = rows[i][j]
			}
		}
		want, ok := originalLeastSquares(rows, rhs)
		got, gotOK := leastSquaresColumns(cols, append([]float64(nil), rhs...), make([]float64, m))
		if !ok || !gotOK {
			t.Fatalf("QR failed for %d columns", n)
		}
		for i := range got {
			if math.Float64bits(got[i]) != math.Float64bits(want[i]) {
				t.Fatalf("column %d: got %.17g want %.17g", i, got[i], want[i])
			}
		}
	}
}
