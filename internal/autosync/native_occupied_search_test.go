package autosync

import "testing"

func TestOccupancyRequiresReviewedStreamAndIndependentTiming(t *testing.T) {
	assignment := &SourceAssignment{Group: "reviewed", Exclusive: true}
	records := []MediaRecord{
		{Name: "occupied.mov", Source: "a/occupied.mov", Duration: 60, SourceAssignment: assignment},
		{Name: "reference.mov", Source: "b/reference.mov", Duration: 60},
		{Name: "witness1.mov", Source: "c/witness1.mov", Duration: 60},
		{Name: "witness2.mov", Source: "d/witness2.mov", Duration: 60},
		{Name: "unplaced.mov", Source: "a/unplaced.mov", Duration: 5, SourceAssignment: assignment},
	}
	var layout []NativeLayout
	for i := range records[:4] {
		layout = append(layout, NativeLayout{Recording: i, Component: 1, ComponentSize: 4, Status: "acoustically_supported_not_certified", Knots: []TimePoint{{0, 0}, {60, 60}}})
	}
	var edges []*PairEvidence
	for _, pair := range [][2]int{{0, 2}, {0, 3}, {1, 2}, {1, 3}} {
		edges = append(edges, &PairEvidence{Reference: pair[0], Query: pair[1], Selected: &CandidateEvidence{}, Profile: &NativeProfile{
			Status: "validated_relation", Limit: .02, Range: []float64{10, 50},
			AuditResult: &ModelJudgment{Passes: true, Supported: 2},
		}})
	}
	options := NativeSyncOptions{Mode: "chronological"}
	got := buildOccupiedSearch(records, layout, edges, nil, options).excluded(records, 1, 4)
	if len(got) != 1 || got[0].First != 1200 || got[0].End != 4800 {
		t.Fatalf("expected only the interior of the validated interval, got %v", got)
	}
	options.Analysis.Verification.MinWave = .05
	if got := buildOccupiedSearch(records, layout, edges, nil, options).excluded(records, 1, 4); len(got) != 0 {
		t.Fatalf("lower verification threshold still excluded offsets: %v", got)
	}
	options.Analysis.Verification = VerificationSettings{}
	records[4].SourceAssignment = nil
	if got := buildOccupiedSearch(records, layout, edges, nil, options).excluded(records, 1, 4); len(got) != 0 {
		t.Fatalf("unreviewed clip excluded offsets: %v", got)
	}
}
