package acousticverify_test

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"opensync/internal/acousticverify"
	"opensync/internal/audio"
	"opensync/internal/syncgraph"
)

const syntheticRate = 16000

// writeSyntheticWAV generates an ordinary PCM16 mono fixture in a temporary
// directory. Nothing from the user's media archive is opened or modified.
func writeSyntheticWAV(t *testing.T, name string, samples []int16) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	dataBytes := uint32(len(samples) * 2)
	for _, part := range []any{
		[4]byte{'R', 'I', 'F', 'F'}, uint32(36) + dataBytes,
		[4]byte{'W', 'A', 'V', 'E'}, [4]byte{'f', 'm', 't', ' '},
		uint32(16), uint16(1), uint16(1), uint32(syntheticRate),
		uint32(syntheticRate * 2), uint16(2), uint16(16),
		[4]byte{'d', 'a', 't', 'a'}, dataBytes,
	} {
		if err := binary.Write(f, binary.LittleEndian, part); err != nil {
			t.Fatal(err)
		}
	}
	if err := binary.Write(f, binary.LittleEndian, samples); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func nextNoise(seed *uint32) float64 {
	*seed = *seed*1664525 + 1013904223
	return float64((*seed>>8)&0xffff)/32767.5 - 1
}

// A, B, and C are three sensors of the same synthetic sound field.
// B has inverted polarity and a different gain; all tracks have independent
// noise. True starts are A=0s, B=1.2s, C=2.4s.
func syntheticRecordings(t *testing.T) map[string]*audio.Reader {
	t.Helper()
	const total = 12 * syntheticRate
	const length = 8 * syntheticRate
	sound := make([]float64, total)
	seed := uint32(0x5eeda123)
	for i := range sound {
		// Multiple amplitude envelopes, tones, and deterministic broadband
		// transients keep windows identifiable across the three recordings.
		phase := float64(i) / syntheticRate
		envelope := 0.35 + 0.55*math.Pow(math.Sin(phase*1.37), 2)
		sound[i] = envelope * (7200*nextNoise(&seed) +
			2800*math.Sin(2*math.Pi*117*phase) +
			1900*math.Sin(2*math.Pi*317*phase))
	}
	type camera struct {
		name  string
		start int
		gain  float64
		noise uint32
	}
	cameras := []camera{
		{"A", 0, 1, 17},
		{"B", 1200 * syntheticRate / 1000, -0.8, 29},
		{"C", 2400 * syntheticRate / 1000, 0.65, 43},
	}
	readers := make(map[string]*audio.Reader, len(cameras))
	for _, cam := range cameras {
		pcm := make([]int16, length)
		noise := cam.noise
		for i := range pcm {
			v := sound[cam.start+i]*cam.gain + 380*nextNoise(&noise)
			pcm[i] = int16(math.Round(v))
		}
		r, err := audio.Open(writeSyntheticWAV(t, cam.name+".wav", pcm))
		if err != nil {
			t.Fatal(err)
		}
		readers[cam.name] = r
	}
	t.Cleanup(func() {
		for _, r := range readers {
			_ = r.Close()
		}
	})
	return readers
}

func TestSyntheticAcousticTriangleEndToEnd(t *testing.T) {
	readers := syntheticRecordings(t)
	cases := []struct {
		ref, query string
		truthNS    int64
	}{
		{"A", "B", 1_200_000_000},
		{"B", "C", 1_200_000_000},
		{"A", "C", 2_400_000_000},
	}
	const toleranceNS = int64(1_000_000) // 1ms; synthetic truth is exact.
	const windowNS = int64(1_200_000_000)
	const radiusNS = int64(25_000_000)
	trainingTimes := []int64{600_000_000, 1_900_000_000}
	constraints := make([]syncgraph.Constraint, 0, len(cases))
	for _, pair := range cases {
		var offsets []int64
		for _, qtime := range trainingTimes {
			m, err := acousticverify.Measure(readers[pair.ref], readers[pair.query],
				acousticverify.Probe{
					QueryStartNS: qtime,
					OffsetNS:     pair.truthNS + 5_000_000, // deliberately biased seed
					WindowNS:     windowNS, RadiusNS: radiusNS,
				})
			if err != nil {
				t.Fatalf("%s-%s at %d: %v", pair.ref, pair.query, qtime, err)
			}
			if math.Abs(float64(m.MeasuredNS-pair.truthNS)) > float64(toleranceNS) {
				t.Fatalf("%s-%s acoustic error: %dns", pair.ref, pair.query, m.MeasuredNS-pair.truthNS)
			}
			if math.Abs(m.Correlation) < 0.5 {
				t.Fatalf("%s-%s weak synthetic match %.3f", pair.ref, pair.query, m.Correlation)
			}
			offsets = append(offsets, m.MeasuredNS)
		}
		sort.Slice(offsets, func(i, j int) bool { return offsets[i] < offsets[j] })
		// The third region is never used for the fit: held-out acoustic
		// evidence must agree with the first two measured regions.
		holdout, err := acousticverify.Measure(readers[pair.ref], readers[pair.query],
			acousticverify.Probe{
				QueryStartNS: 3_400_000_000,
				OffsetNS:     offsets[0], WindowNS: windowNS, RadiusNS: radiusNS,
			})
		if err != nil || math.Abs(float64(holdout.MeasuredNS-pair.truthNS)) > float64(toleranceNS) {
			t.Fatalf("%s-%s holdout mismatch: %+v %v", pair.ref, pair.query, holdout, err)
		}
		constraints = append(constraints, syncgraph.Constraint{
			Reference: pair.ref, Query: pair.query, OffsetNS: offsets[0], Weight: 1,
		})
	}

	solved, err := syncgraph.SolveOffsets([]string{"A", "B", "C", "ISOLATED"}, constraints)
	if err != nil {
		t.Fatal(err)
	}
	if len(solved.Components) != 2 {
		t.Fatalf("expected triangle plus isolated component: %+v", solved)
	}
	component := solved.Components[solved.NodeComponent["A"]]
	if component.CycleRank != 1 || component.EdgeCount != 3 {
		t.Fatalf("expected one graph cycle: %+v", component)
	}
	for name, want := range map[string]float64{"A": 0, "B": 1.2, "C": 2.4} {
		if math.Abs(component.StartsSeconds[name]-want) > 0.001 {
			t.Fatalf("%s solved at %.9fs; ground truth %.9fs", name, component.StartsSeconds[name], want)
		}
	}
	if solved.Components[solved.NodeComponent["ISOLATED"]].EdgeCount != 0 {
		t.Fatal("isolate was spuriously connected")
	}

	// Fault injection: a large competing offset must leave an observable
	// inconsistency. Least squares is diagnostic, not an edge-admission gate.
	bad := append([]syncgraph.Constraint(nil), constraints...)
	bad = append(bad, syncgraph.Constraint{
		Reference: "A", Query: "C", OffsetNS: 3_400_000_000, Weight: 1,
	})
	conflicted, err := syncgraph.SolveOffsets([]string{"A", "B", "C"}, bad)
	if err != nil {
		t.Fatal(err)
	}
	if conflicted.Components[0].MaxAbsResidualSeconds < 0.2 {
		t.Fatalf("grossly incorrect edge was concealed: %+v", conflicted.Components[0])
	}
}
