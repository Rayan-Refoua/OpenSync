package acousticverify

import (
	"math"
	"math/rand"
	"testing"
)

func TestBestLagFindsShift(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	ref := make([]float64, 500)
	for i := range ref {
		ref[i] = rng.NormFloat64()
	}
	query := append([]float64(nil), ref[137:337]...)

	lag, corr, err := bestLag(query, ref)
	if err != nil {
		t.Fatal(err)
	}
	if lag != 137 || math.Abs(corr-1) > 1e-9 {
		t.Fatalf("lag=%d correlation=%g", lag, corr)
	}
}

func TestBestLagAllowsPolarityInversion(t *testing.T) {
	rng := rand.New(rand.NewSource(51))
	ref := make([]float64, 400)
	for i := range ref {
		ref[i] = rng.NormFloat64()
	}
	query := make([]float64, 100)
	for i := range query {
		query[i] = -ref[91+i]
	}
	lag, corr, err := bestLag(query, ref)
	if err != nil {
		t.Fatal(err)
	}
	if lag != 91 || corr > -0.999 {
		t.Fatalf("lag=%d correlation=%g", lag, corr)
	}
}

func TestBestLagRejectsSilence(t *testing.T) {
	if _, _, err := bestLag(
		make([]float64, 100),
		make([]float64, 200),
	); err == nil {
		t.Fatal("silence accepted as measurable evidence")
	}
}
