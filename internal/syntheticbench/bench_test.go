package syntheticbench

import (
	"os"
	"path/filepath"
	"testing"

	"opensync/internal/audio"
)

func TestSyntheticGroundTruthAndValidWAV(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "generated")
	cases, err := Generate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 10 {
		t.Fatalf("expected ten scenarios, got %d", len(cases))
	}
	expected := int64(4375000000)
	if cases[0].ExpectedOffsetNS == nil || *cases[0].ExpectedOffsetNS != expected {
		t.Fatalf("wrong known offset: %+v", cases[0])
	}
	if cases[4].ExpectedOffsetNS != nil {
		t.Fatal("unrelated negative control declared matching")
	}
	if cases[5].ExpectedOffsetNS == nil || cases[6].ExpectedOffsetNS == nil ||
		*cases[5].ExpectedOffsetNS-*cases[6].ExpectedOffsetNS != expected {
		t.Fatal("triangle ground truth does not close")
	}
	for _, name := range []string{"reference.wav", "query-clean.wav", "query-mic.wav", "query-gated.wav", "query-drift.wav", "query-unrelated.wav", "third.wav", "repeat-reference.wav", "repeat-query.wav", "query-dropout.wav", "short-reference.wav", "short-query.wav"} {
		r, err := audio.Open(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("invalid WAV %s: %v", name, err)
		}
		if r.SampleRate() != rate || r.Frames() < 16*rate {
			t.Fatalf("bad source %s", name)
		}
		r.Close()
	}
	if len(cases[7].ValidOffsetsNS)!=2 || cases[7].ValidOffsetsNS[0]!=0 || cases[7].ValidOffsetsNS[1]!=24_000_000_000 { t.Fatal("repeated-source ambiguity lost") }
	if cases[9].ExpectedOffsetNS==nil || *cases[9].ExpectedOffsetNS!=30_000_000_000 { t.Fatal("short-overlap ground truth lost") }
	if _, err := Generate(dir); err == nil {
		t.Fatal("rewrote existing dataset")
	}
	if _, err := os.Stat(filepath.Join(dir, "reference.wav")); err != nil {
		t.Fatal(err)
	}
}

func TestSyntheticMatcherEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("integration benchmark with full PCM fixtures")
	}
	dir := filepath.Join(t.TempDir(), "generated")
	cases, err := Generate(dir)
	if err != nil {
		t.Fatal(err)
	}
	report, err := Evaluate(dir, []Scenario{cases[0], cases[4], cases[7]})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Runs) != 9 || report.Certification != "not_assessed" {
		t.Fatalf("bad benchmark result %+v", report)
	}
	for _, run := range report.Runs {
		if run.Scenario == "clean" {
			if run.Rank < 1 || run.NearestErrorMS > float64(run.HopMS)*1.5 {
				t.Errorf("%dms synthetic clean positive control missed: rank=%d error_ms=%.3f top=%.3fs", run.HopMS, run.Rank, run.NearestErrorMS, run.TopOffsetSeconds)
			}
		}
		if run.IntegrationMS != run.HopMS && (run.HopMS!=2 || run.IntegrationMS!=8) { t.Fatalf("invalid research configuration labeling: %+v",run) }
		if run.Scenario == "unrelated-negative" && run.Status != "negative_control_unlabeled_peaks" {
			t.Errorf("negative case falsely labeled as verified: %s", run.Status)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "results.json")); err != nil {
		t.Fatal(err)
	}
}
