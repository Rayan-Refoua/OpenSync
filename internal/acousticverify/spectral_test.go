package acousticverify

import (
	"math"
	"testing"
)

func TestPearsonSameAndInverse(t *testing.T) {
	a := []float64{1, 4, 2, 7, 3, 9}
	b := []float64{1, 4, 2, 7, 3, 9}
	c := []float64{-1, -4, -2, -7, -3, -9}

	same, ok := pearson(a, b)
	if !ok || math.Abs(same-1) > 1e-12 {
		t.Fatalf("identical correlation: %v %v", same, ok)
	}
	inverse, ok := pearson(a, c)
	if !ok || math.Abs(inverse+1) > 1e-12 {
		t.Fatalf("inverse correlation: %v %v", inverse, ok)
	}
}

func TestPearsonRejectsConstantSignal(t *testing.T) {
	_, ok := pearson(
		[]float64{0, 0, 0, 0},
		[]float64{1, 2, 3, 4},
	)
	if ok {
		t.Fatal("constant signal accepted")
	}
}
