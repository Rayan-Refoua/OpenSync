package sourcetracks

import (
	"opensync/internal/projectstate"
	"testing"
)

func TestChronologicalPairsAndMissingCounter(t *testing.T) {
	p := projectstate.New()
	records := []projectstate.Recording{
		{ID: "a", SourcePath: `D:\CamA\C0593.MP4`, DurationNS: 1_000_000_000},
		{ID: "b", SourcePath: `D:\CamA\C0594.MP4`, DurationNS: 1_000_000_000},
		{ID: "c", SourcePath: `D:\CamA\C0595.MP4`, DurationNS: 1_000_000_000},
		{ID: "gap", SourcePath: `D:\CamA\C0598.MP4`, DurationNS: 1_000_000_000},
		{ID: "cameraB", SourcePath: `D:\CamB\C0596.MP4`, DurationNS: 1_000_000_000},
		{ID: "otherSeries", SourcePath: `D:\CamA\M0596.MP4`, DurationNS: 1_000_000_000},
	}
	for _, r := range records {
		if err := p.AddRecording(r); err != nil {
			t.Fatal(err)
		}
	}
	pairs, err := SequentialNeighbors(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 2 || pairs[0].Before != "a" || pairs[0].After != "b" || pairs[1].Before != "b" || pairs[1].After != "c" {
		t.Fatalf("unexpected pairs: %+v", pairs)
	}
	if SequencePriority(p, "b", "a") != 0 || SequencePriority(p, "a", "c") != 1 || SequencePriority(p, "b", "cameraB") != 2 {
		t.Fatal("wrong candidate priority")
	}
	for _, pair := range pairs {
		if pair.Continuity != "unverified" {
			t.Fatalf("invented continuity: %+v", pair)
		}
	}
}

func TestAmbiguousDuplicateNumbersExcluded(t *testing.T) {
	p := projectstate.New()
	for _, r := range []projectstate.Recording{
		{ID: "a", SourcePath: "/cam/C001.MP4", DurationNS: 1_000_000_000},
		{ID: "b", SourcePath: "/cam/C01.MP4", DurationNS: 1_000_000_000},
		{ID: "c", SourcePath: "/cam/C002.MP4", DurationNS: 1_000_000_000},
	} {
		if err := p.AddRecording(r); err != nil {
			t.Fatal(err)
		}
	}
	pairs, err := SequentialNeighbors(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 0 {
		t.Fatalf("ambiguous numbering must not create edges: %+v", pairs)
	}
}
