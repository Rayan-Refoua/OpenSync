package autosync

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"testing"
	"time"
)

func TestChronologyFirstChecksAudioBeforeSkippingRecovery(t *testing.T) {
	root := t.TempDir()
	audio := fixtureNoise(70, 49171)
	records := []MediaRecord{{ID: "ref", Name: "reference.wav", Source: filepath.Join(root, "ref", "reference.wav"), Duration: 70, Audio: map[string]any{}}}
	records[0].Cache = filepath.Join(root, "reference.wav")
	if err := writeFixtureWAV(records[0].Cache, audio); err != nil {
		t.Fatal(err)
	}
	for i, start := range []int{0, 24, 48} {
		name := fmt.Sprintf("C%04d.mov", i+1)
		stamp := time.Date(2026, 11, 27, 8, 0, start, 0, time.UTC)
		r := MediaRecord{ID: name, Name: name, Source: filepath.Join(root, "card", name),
			Cache: filepath.Join(root, name+".wav"), Duration: 20,
			Audio: map[string]any{}, Video: map[string]any{"width": 1920, "height": 1080},
			FormatTags:  map[string]any{"creation_time": stamp.Format(time.RFC3339)},
			SourceMtime: stamp.Add(20 * time.Second).UnixNano()}
		if err := writeFixtureWAV(r.Cache, audio[start*16000:(start+20)*16000]); err != nil {
			t.Fatal(err)
		}
		records = append(records, r)
	}
	ctx := context.Background()
	analyzer := NewNativeAnalyzer(records)
	defer analyzer.Close()
	progress := startNativeProgress(ctx)
	defer progress.close()
	edges := []*PairEvidence{testRelation(0, 1, 0, .9), testRelation(0, 3, 48, .9)}
	early, err := analyzer.chronologyFirst(ctx, edges, nil, root, progress)
	if err != nil {
		t.Fatal(err)
	}
	if len(early) != 1 || early[0].Query != 2 || math.Abs(early[0].Selected.Offset-24) > .01 {
		t.Fatalf("known bracketed recording did not pass the early check: %v", early)
	}
	result, err := analyzer.recover(ctx, append(edges, early...), root)
	if err != nil || len(result) != 0 {
		t.Fatalf("broad recovery for a connected job: %d pairs, %v", len(result), err)
	}

	broad, err := analyzer.recover(ctx, edges, root)
	if err != nil || len(broad) == 0 {
		t.Fatalf("ordinary recovery did not search the unconnected clip: %d pairs, %v", len(broad), err)
	}

	records[2].SourceMtime += int64(time.Hour)
	early, err = analyzer.chronologyFirst(ctx, edges, nil, root, progress)
	if err != nil || len(early) != 0 {
		t.Fatalf("incorrect modification time allowed early exit: %v", err)
	}
	guidedLayout, _, err := nativeSolve(records, []*PairEvidence{testRelation(0, 1, 0, .9), testRelation(0, 3, 40.25, .9)})
	if err != nil {
		t.Fatal(err)
	}
	estimates := earlyChronologyEstimates(records, guidedLayout, true)
	if len(estimates) != 1 || estimates[0].Basis != "tight_recorder_order_acoustic_search_only" || math.Abs(estimates[0].Start-20.125) > 1e-6 {
		t.Fatalf("tight recorder bracket did not seed the acoustic search: %+v", estimates)
	}
}
