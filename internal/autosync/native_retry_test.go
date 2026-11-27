package autosync

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestTargetedRetryRequiresReviewBeforePlacement(t *testing.T) {
	root := t.TempDir()
	audio := fixtureNoise(65, 3921)
	var records []MediaRecord
	for i, span := range [][2]int{{0, 65}, {6, 26}, {32, 52}} {
		id := fmt.Sprint(i)
		path := filepath.Join(root, "clip-"+id+".wav")
		if err := writeFixtureWAV(path, audio[span[0]*16000:span[1]*16000]); err != nil {
			t.Fatal(err)
		}
		st, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, MediaRecord{ID: id, Name: filepath.Base(path), Source: path,
			Duration: float64(span[1] - span[0]), Audio: map[string]any{"sample_rate": "16000"},
			SourceBytes: st.Size(), SourceMtime: st.ModTime().UnixNano()})
	}
	edges := []*PairEvidence{testRelation(0, 1, 6, 1)}
	layout, graph, err := nativeSolve(records, edges)
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(root, "OpenSync.opensync")
	if err := saveNativeProject(base, "retry fixture", records, edges, layout, graph); err != nil {
		t.Fatal(err)
	}
	ref := 0
	revision, count, err := RetryRecordings(context.Background(), base, RetryOptions{Clips: []int{2}, Reference: &ref})
	if err != nil || count == 0 {
		t.Fatalf("retry found %d candidates, %v", count, err)
	}
	original, err := LoadNativeProject(base)
	if err != nil {
		t.Fatal(err)
	}
	review, err := LoadNativeProject(revision)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original.Data.Models, review.Data.Models) {
		t.Fatal("retry changed timing before review")
	}
	verifiedPath, applied, err := VerifyReviewWaveforms(context.Background(), revision)
	if err != nil || applied != 1 {
		t.Fatalf("waveform review applied %d, error %v", applied, err)
	}
	verified, err := LoadNativeProject(verifiedPath)
	if err != nil {
		t.Fatal(err)
	}
	if model := solutionModel(t, verified, 2); !model.Resolved || model.TimingBasis != "waveform_verified_review" || math.Abs(model.Offset-32) > .01 {
		t.Fatalf("unexpected waveform placement: %+v", model)
	}
	rebuilt, err := CreateSolution(verifiedPath, ReviewChange{Kind: "rebuild"})
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := LoadNativeProject(rebuilt)
	if err != nil {
		t.Fatal(err)
	}
	if model := solutionModel(t, reloaded, 2); model.TimingBasis != "waveform_verified_review" {
		t.Fatalf("waveform verification was lost on rebuild: %+v", model)
	}
	for _, c := range ManualCandidates(review.Data) {
		if c.Reference != ref || c.Query != 2 || math.Abs(c.Shift-32) > .01 {
			continue
		}
		accepted, err := CreateSolution(revision, ReviewChange{Kind: "accept_candidate", Candidate: c.ID})
		if err != nil {
			t.Fatal(err)
		}
		placed, err := LoadNativeProject(accepted)
		if err != nil {
			t.Fatal(err)
		}
		if model := solutionModel(t, placed, 2); !model.Resolved || math.Abs(model.Offset-32) > .01 {
			t.Fatalf("unexpected accepted model: %+v", model)
		}
		return
	}
	t.Fatal("expected proposed match at 32 seconds")
}
