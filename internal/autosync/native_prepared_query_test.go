package autosync

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestPreparedQueryMatchesOriginalComparisons(t *testing.T) {
	root := t.TempDir()
	reference := fixtureNoise(90, 431)
	query := append([]int16(nil), reference[20*16000:60*16000]...)
	for i := range query {
		query[i] = -query[i]
	}
	rp, qp := filepath.Join(root, "ref.wav"), filepath.Join(root, "query.wav")
	if err := writeFixtureWAV(rp, reference); err != nil {
		t.Fatal(err)
	}
	if err := writeFixtureWAV(qp, query); err != nil {
		t.Fatal(err)
	}
	a := NewNativeAnalyzer([]MediaRecord{{Cache: rp, Duration: 90}, {Cache: qp, Duration: 40}})
	defer a.Close()
	for _, start := range []float64{0, .123, 20.5, 37.95} {
		p, err := a.prepareQuery(1, start, 2)
		if err != nil {
			t.Fatal(err)
		}
		for _, offset := range []float64{20, -15, 15, 31, -start + .04, 88 - start - .05, 20} {
			want, err := a.waveform(0, 1, start, offset, 2, .35)
			if err != nil {
				t.Fatal(err)
			}
			got, err := a.waveformPrepared(0, p, offset, .35)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(want, got) {
				t.Fatalf("waveform changed at %.3f offset %.3f", start, offset)
			}
			wf, err := a.fluxScore(0, 1, start, offset, 2)
			if err != nil {
				t.Fatal(err)
			}
			gf, err := a.fluxPrepared(0, p, offset)
			if err != nil || wf != gf {
				t.Fatal("spectral comparison changed", err)
			}
			wp, err := a.phaseCheck(0, 1, start, offset, 2, .35)
			if err != nil {
				t.Fatal(err)
			}
			gp, err := a.phasePrepared(0, p, offset, .35)
			if err != nil || !reflect.DeepEqual(wp, gp) {
				t.Fatal("phase comparison changed", err)
			}
		}
		shifts := []float64{-31, -15, 15, 31}
		wc, ws, err := a.controls(0, 1, start, 20, 2, .35, shifts)
		if err != nil {
			t.Fatal(err)
		}
		gc, gs, err := a.controlsPrepared(0, p, 20, .35, shifts)
		if err != nil || wc != gc || ws != gs {
			t.Fatal("wrong-offset controls changed", err)
		}
	}
}
