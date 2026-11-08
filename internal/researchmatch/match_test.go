package researchmatch

import (
	"math"
	"math/rand"
	"testing"
)

func TestCrossDotsFullRange(t *testing.T) {
	ref := []float64{0, 2, 3, 5}
	query := []float64{2, 3}
	got, err := crossDots(ref, query)
	if err != nil {
		t.Fatal(err)
	}
	expected := []float64{0, 6, 13, 21, 10}
	if len(got) != len(expected) {
		t.Fatalf("dot length %d", len(got))
	}
	for i, v := range expected {
		if math.Abs(got[i]-v) > 1e-8 {
			t.Fatalf("dot %d got %.9f want %.9f", i, got[i], v)
		}
	}
}
func TestFullLagSubGridOffsetAndNegativeOffset(t *testing.T) {
	c := Dream()
	const total = 12000
	const queryLength = 5600
	rng := rand.New(rand.NewSource(34))
	var ref [3][]float64
	for band := 0; band < 3; band++ {
		ref[band] = make([]float64, total)
		for i := range ref[band] {
			ref[band][i] = rng.NormFloat64()
		}
	}
	for _, offset := range []int{187, -187} {
		var query [3][]float64
		for band := 0; band < 3; band++ {
			query[band] = make([]float64, queryLength)
			for i := range query[band] {
				r := i + offset
				if r >= 0 && r < total {
					query[band][i] = ref[band][r]
				}
			}
		}
		result, err := MatchFeatures(ref, query, c)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Peaks) == 0 {
			t.Fatal("no peaks")
		}
		want := float64(offset*c.HopSamples) / SampleRate
		if math.Abs(result.Peaks[0].OffsetSeconds-want) > 1e-9 {
			t.Fatalf("offset=%d peak %+v want %.6f", offset, result.Peaks[0], want)
		}
		if result.Peaks[0].Score < 0.98 {
			t.Fatalf("strong match not recovered: %+v", result.Peaks[0])
		}
		if result.Certification != "not_assessed" {
			t.Fatal("research claimed certification")
		}
	}
}
func TestNoFalseCertificationOrInvalidConfig(t *testing.T) {
	c := Dream()
	c.HopSamples = 16
	if _, err := MatchFeatures([3][]float64{}, [3][]float64{}, c); err == nil {
		t.Fatal("invalid hop accepted")
	}
	c = Dream()
	c.IntegrationFrames = 4
	c.HopSamples = 128
	if _, err := MatchFeatures([3][]float64{}, [3][]float64{}, c); err == nil {
		t.Fatal("unsupported integrated 8ms hop accepted")
	}
	c = Dream()
	c.MinOverlapSeconds = 8
	var x [3][]float64
	for b := range x {
		x[b] = make([]float64, 100)
	}
	if _, err := MatchFeatures(x, x, c); err == nil {
		t.Fatal("short overlap accepted")
	}
}
func TestFFTEnvelopeConstantSignalsRejected(t *testing.T) {
	c := Dream()
	ref, query := [3][]float64{}, [3][]float64{}
	for b := 0; b < 3; b++ {
		ref[b] = make([]float64, 6000)
		query[b] = make([]float64, 6000)
	}
	result, err := MatchFeatures(ref, query, c)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Peaks) != 0 {
		t.Fatalf("silence matched itself %+v", result.Peaks)
	}
}
