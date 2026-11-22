package autosync

import (
	"path/filepath"
	"testing"
)

func TestManualMatchRevisions(t *testing.T) {
	_, records := solutionFixture(t)
	for i := range records {
		records[i].Audio = map[string]any{"start_time": "0"}
	}
	edges := []*PairEvidence{testRelation(0, 1, 12, 1), testRelation(0, 2, 24, 1)}
	layout, graph, err := nativeSolve(records, edges)
	if err != nil {
		t.Fatal(err)
	}
	graph["unresolved_timing_hypotheses"] = []map[string]any{{
		"reference": 0, "query": 3,
		"hypotheses": []map[string]any{
			{"prior": map[string]any{"offset": 4.5}, "status": "corroboration_required"},
			{"prior": map[string]any{"offset": 9}, "status": "event_supported_timing_unresolved"},
		},
	}}
	base := filepath.Join(t.TempDir(), "OpenSync.opensync")
	if err = saveNativeProject(base, "manual review", records, edges, layout, graph); err != nil {
		t.Fatal(err)
	}
	load := func(path string) *NativeProject {
		t.Helper()
		p, err := LoadNativeProject(path)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	revise := func(path, kind, candidate string) string {
		t.Helper()
		next, err := CreateSolution(path, ReviewChange{Kind: kind, Candidate: candidate})
		if err != nil {
			t.Fatal(err)
		}
		return next
	}
	candidates := ManualCandidates(load(base).Data)
	if len(candidates) != 2 {
		t.Fatalf("got %d competing candidates", len(candidates))
	}
	rejected := revise(base, "reject_candidate", candidates[0].ID)
	if _, err := CreateSolution(rejected, ReviewChange{Kind: "accept_candidate", Candidate: candidates[0].ID}); err == nil {
		t.Fatal("rejected candidate can be accepted without undo")
	}
	accepted := revise(rejected, "accept_candidate", candidates[1].ID)
	for _, path := range []string{accepted, revise(accepted, "rebuild", "")} {
		p := load(path)
		m, anchor := solutionModel(t, p, 3), solutionModel(t, p, 0)
		if m.Status != "manually_accepted" || !m.Resolved || m.Offset != anchor.Offset+9 || m.TimingBasis != "manual_audio_review" {
			t.Fatalf("unexpected manual placement: %+v", m)
		}
	}
	if solutionModel(t, load(base), 3).Resolved {
		t.Fatal("original project placement was changed")
	}
}
