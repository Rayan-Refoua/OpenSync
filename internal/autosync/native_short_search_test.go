package autosync

import (
	"math"
	"path/filepath"
	"testing"
)

func shortTestAnalyzer(t *testing.T, reference, query []int16) *NativeAnalyzer {
	t.Helper()
	root := t.TempDir()
	records := []MediaRecord{}
	for i, samples := range [][]int16{reference, query} {
		name := []string{"reference.wav", "query.wav"}[i]
		path := filepath.Join(root, name)
		if err := writeFixtureWAV(path, samples); err != nil {
			t.Fatal(err)
		}
		records = append(records, MediaRecord{ID: name, Name: name, Source: path, Cache: path, Duration: float64(len(samples)) / 16000, Audio: map[string]any{"sample_rate": "16000"}})
	}
	a := NewNativeAnalyzer(records)
	t.Cleanup(a.Close)
	return a
}

func TestShortPartitionKnownOffsetsAndBoundaries(t *testing.T) {
	ref := fixtureNoise(72, 87171)
	for _, test := range []struct {
		name          string
		start, length float64
		degrade       bool
	}{
		{"origin", 0, 2.1, false}, {"fractional_sample_phase", 5.1231875, 4.2, true}, {"tile_boundary", 59.3123125, 8.1, false}, {"source_end", 67.8, 4.2, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			first, count := roundFrame(test.start*16000), roundFrame(test.length*16000)
			query := append([]int16(nil), ref[first:first+count]...)
			if test.degrade {
				noise := fixtureNoise(5, 332)
				for i := len(query) - 1; i > 0; i-- {
					query[i] = int16(-.6*float64(query[i]) - .15*float64(query[i-1]) + .05*float64(noise[i]))
				}
			}
			a := shortTestAnalyzer(t, ref, query)
			p, err := a.shortPartitionProfile(0, 1)
			if err != nil {
				t.Fatal(err)
			}
			if p == nil || p.Status != "validated_relation" {
				t.Fatalf("known match rejected: %+v", p)
			}
			if math.Abs(p.Model.Offset-test.start) > .002 {
				t.Fatalf("offset %.9f, want %.9f", p.Model.Offset, test.start)
			}
			if p.Model.Kind != "constant" || p.Model.Slope != 0 || len(p.Training) != 1 || len(p.Audit) != 1 {
				t.Fatal("short fallback invented drift or duplicate evidence")
			}
			train, audit := p.Training[0], p.Audit[0]
			if audit.QueryTime-audit.Window/2-(train.QueryTime+train.Window/2) < .039 {
				t.Fatal("holdout guard missing")
			}
		})
	}
}

func TestShortPartitionRejectsFalseAndAmbiguousMatches(t *testing.T) {
	base := fixtureNoise(30, 902)
	excerpt := append([]int16(nil), base[5*16000:9*16000]...)
	repeated := append([]int16(nil), base...)
	copy(repeated[20*16000:], excerpt)
	spliced := append([]int16(nil), base[5*16000:7*16000]...)
	spliced = append(spliced, base[20*16000:22*16000]...)
	partial := append([]int16(nil), base[5*16000:7*16000]...)
	partial = append(partial, fixtureNoise(2, 711)...)
	tone := make([]int16, len(base))
	for i := range tone {
		tone[i] = int16(10000 * math.Sin(float64(i)*2*math.Pi*330/16000))
	}
	for _, test := range []struct {
		name       string
		ref, query []int16
	}{
		{"repeated_music_passage", repeated, excerpt},
		{"inconsistent_half_offsets", base, spliced},
		{"only_one_matching_half", base, partial},
		{"stationary_tone", tone, tone[5*16000 : 9*16000]},
		{"silence", make([]int16, len(base)), make([]int16, 4*16000)},
		{"too_short", base, excerpt[:24000]},
	} {
		t.Run(test.name, func(t *testing.T) {
			a := shortTestAnalyzer(t, test.ref, test.query)
			p, err := a.shortPartitionProfile(0, 1)
			if err != nil {
				t.Fatal(err)
			}
			if p != nil && p.Status == "validated_relation" {
				t.Fatalf("false acceptance: %+v", p)
			}
		})
	}
	for seed := 1; seed <= 8; seed++ {
		a := shortTestAnalyzer(t, base, fixtureNoise(4, uint32(seed+3000)))
		p, err := a.shortPartitionProfile(0, 1)
		if err != nil {
			t.Fatal(err)
		}
		if p != nil && p.Status == "validated_relation" {
			t.Fatalf("unrelated audio accepted, seed %d", seed)
		}
	}
}
