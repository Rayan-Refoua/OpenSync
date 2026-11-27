package autosync

import (
	"math"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestAdaptiveTimingAgainstDenseProfile(t *testing.T) {
	dir := t.TempDir()
	refPCM := fixtureNoise(400, 431)
	ref := filepath.Join(dir, "reference.wav")
	if err := writeFixtureWAV(ref, refPCM); err != nil {
		t.Fatal(err)
	}
	queryPCM := append([]int16(nil), refPCM[20*16000:380*16000]...)
	for _, name := range []string{"continuous", "dropout", "sample discontinuity"} {
		t.Run(name, func(t *testing.T) {
			copy(queryPCM, refPCM[20*16000:380*16000])
			switch name {
			case "dropout":
				clear(queryPCM[:6*16000])
			case "sample discontinuity":
				copy(queryPCM[:6*16000], refPCM[20*16000+320:26*16000+320])
			}
			query := filepath.Join(dir, name+".wav")
			if err := writeFixtureWAV(query, queryPCM); err != nil {
				t.Fatal(err)
			}
			records := []MediaRecord{{Cache: ref, Duration: 400}, {Cache: query, Duration: 360}}
			prior := NativePrior{Offset: 20, Strength: .8}
			profile := func(adaptive bool) (*NativeProfile, int64, time.Duration) {
				a := NewNativeAnalyzer(records)
				defer a.Close()
				start := time.Now()
				p, err := a.profileCandidateWithPolicy(0, 1, prior, adaptive)
				if err != nil {
					t.Fatal(err)
				}
				return p, a.profileWindows[0].Load() + a.profileWindows[1].Load() + a.profileWindows[2].Load(), time.Since(start)
			}
			dense, denseCount, denseTime := profile(false)
			adaptive, adaptiveCount, adaptiveTime := profile(true)
			t.Logf("dense: %d windows, %v; adaptive: %d windows, %v (%s)", denseCount, denseTime, adaptiveCount, adaptiveTime, adaptive.TimingSchedule)
			if name != "continuous" {
				if adaptive.TimingSchedule != "adaptive_escalated_v2" || adaptiveCount != denseCount {
					t.Fatal("dropout must complete the original schedule without duplicate measurements")
				}
				adaptive.TimingSchedule = ""
				if !reflect.DeepEqual(dense, adaptive) {
					t.Fatal("escalation changed dense evidence")
				}
				return
			}
			if adaptive.TimingSchedule != "adaptive_sparse_v2" || adaptiveCount != 25 || denseCount < 5*adaptiveCount {
				t.Fatal("continuous overlap did not use the bounded sparse schedule")
			}
			if dense.Status != "validated_relation" || adaptive.Status != dense.Status ||
				dense.Model == nil || adaptive.Model == nil ||
				dense.Model.Kind != adaptive.Model.Kind || dense.Model.Offset != adaptive.Model.Offset || math.Abs(adaptive.Model.Offset-20) > 1e-6 {
				t.Fatalf("unexpected model difference: dense %+v, adaptive %+v", dense.Model, adaptive.Model)
			}
		})
	}
}
