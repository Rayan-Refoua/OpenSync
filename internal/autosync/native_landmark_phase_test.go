package autosync

import (
	"context"
	"math"
	"math/rand"
	"os"
	"testing"
	"time"
)

// Research-only phase sweep. Each phase is a competing discovery view of the
// SAME audio, never an additional independent verification observation.
func TestLandmarkPhaseResearch(t *testing.T) {
	output := os.Getenv("OPENSYNC_PHASE_REPORT")
	if output == "" {
		t.Skip("explicit phase research report path required")
	}
	generate := func(low, high float64, seed int64) []int16 {
		rng := rand.New(rand.NewSource(seed))
		data := make([]float64, 40*16000)
		for event := 0; event < 240; event++ {
			start := rng.Intn(len(data) - 4000)
			length := 200 + rng.Intn(3800)
			f := low + rng.Float64()*(high-low)
			amplitude := 600 + 1200*rng.Float64()
			phase := rng.Float64() * 2 * math.Pi
			for i := 0; i < length; i++ {
				x := float64(i) / float64(length)
				data[start+i] += amplitude * math.Sin(math.Pi*x) * math.Sin(2*math.Pi*f*float64(i)/16000+phase)
			}
		}
		pcm := make([]int16, len(data))
		for i, x := range data {
			pcm[i] = int16(math.Max(-32767, math.Min(32767, x)))
		}
		return pcm
	}
	var reports []map[string]any
	for _, band := range []struct {
		name   string
		lo, hi float64
	}{{"low", 200, 1000}, {"high", 2500, 5500}, {"broad", 200, 5500}} {
		ref := generate(band.lo, band.hi, 71)
		unrelated := generate(band.lo, band.hi, 915)
		rh, _ := FingerprintSamples(ref, 0)
		for _, duration := range []float64{1.2, 2.4, 4.8} {
			repeated := append([]int16(nil), ref...)
			copy(repeated[25*16000:], ref[9*16000:14*16000])
			repeatHashes, _ := FingerprintSamples(repeated, 0)
			query := append([]int16(nil), ref[9*16000:9*16000+int(duration*16000)]...)
			var competition []map[string]any
			for _, shift := range []int{0, 32, 64, 96} {
				qh, _ := FingerprintSamples(query[shift:], float64(shift)/16000)
				pairs, err := NativeDiscover(context.Background(), []MediaRecord{{Duration: 40}, {Duration: duration}}, [][]Fingerprint{repeatHashes, qh})
				if err != nil {
					t.Fatal(err)
				}
				matched := 0
				if len(pairs) > 0 {
					for _, offset := range []float64{9, 25} {
						for _, c := range pairs[0].Candidates {
							if math.Abs(c.Offset-offset) < .2 {
								matched++
								break
							}
						}
					}
				}
				competition = append(competition, map[string]any{"shift_samples": shift, "repeated_regions_retained": matched})
			}
			reports = append(reports, map[string]any{"band": band.name, "duration": duration, "control": "repeated_passage", "phases": competition})
			for _, negative := range []bool{false, true} {
				for _, phase := range []int{0, 37, 79, 113} {
					first := 9*16000 + phase
					n := int(duration * 16000)
					source := ref
					if negative {
						source = unrelated
					}
					query := append([]int16(nil), source[first:first+n]...)
					// Mild deterministic coloration and interference.
					for i := len(query) - 1; i > 0; i-- {
						query[i] = int16(.7*float64(query[i]) + .15*float64(query[i-1]) + .08*float64(unrelated[i]))
					}
					start := time.Now()
					var phases []map[string]any
					for _, shift := range []int{0, 32, 64, 96} {
						qh, _ := FingerprintSamples(query[shift:], float64(shift)/16000)
						pairs, err := NativeDiscover(context.Background(), []MediaRecord{{Duration: 40}, {Duration: duration}}, [][]Fingerprint{rh, qh})
						if err != nil {
							t.Fatal(err)
						}
						hit, top, votes, alternatives := false, false, 0, 0
						if len(pairs) > 0 {
							alternatives = len(pairs[0].Candidates)
							for i, c := range pairs[0].Candidates {
								if math.Abs(c.Offset-float64(first)/16000) < .2 {
									hit = true
									if i == 0 {
										top = true
									}
								}
								if i == 0 {
									votes = c.Votes
								}
							}
						}
						phases = append(phases, map[string]any{"shift_samples": shift, "true_region_candidate": hit && !negative, "true_region_rank_one": top && !negative, "top_votes": votes, "candidate_count": alternatives})
					}
					reports = append(reports, map[string]any{"band": band.name, "duration": duration, "negative_control": negative, "source_phase_samples": phase, "phases": phases, "four_phase_milliseconds": float64(time.Since(start).Microseconds()) / 1000})
				}
			}
		}
	}
	if err := writeJSON(output, reports); err != nil {
		t.Fatal(err)
	}
}
