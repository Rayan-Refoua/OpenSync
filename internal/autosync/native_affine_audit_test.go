package autosync

import "testing"

func TestGlobalAffineAuditUsesIndependentWindows(t *testing.T) {
	records := []MediaRecord{{ID: "a", Duration: 240}, {ID: "b", Duration: 210}, {ID: "c", Duration: 180}}
	start := []float64{0, 10, 20}
	rate := []float64{1, 1.0001, .99985}
	var layout []NativeLayout
	for i, r := range records {
		bias := 0.
		if i == 2 {
			bias = .012
		}
		layout = append(layout, NativeLayout{
			Recording: i, Component: 1, ComponentSize: 3, Rate: rate[i], Start: start[i] + bias,
			Knots: []TimePoint{{0, start[i] + bias}, {r.Duration, start[i] + bias + r.Duration*rate[i]}},
		})
	}
	var edges []*PairEvidence
	for _, pair := range [][2]int{{0, 1}, {0, 2}, {1, 2}} {
		r, q := pair[0], pair[1]
		offset, slope := (start[q]-start[r])/rate[r], rate[q]/rate[r]-1
		e := &PairEvidence{Reference: r, Query: q, Selected: &CandidateEvidence{NativeCandidate: NativeCandidate{Offset: offset}, Slope: slope, Confidence: 1},
			Validation: &HoldoutReport{Status: "pass_disjoint_windows", Excluded: true, Passed: 3}}
		for _, x := range []float64{10, 70, 130} {
			e.Selected.Fit = append(e.Selected.Fit, &Measurement{QueryTime: x, Offset: offset + slope*x})
		}
		for _, x := range []float64{35, 100, 150} {
			e.Validation.Observations = append(e.Validation.Observations, &Measurement{QueryTime: x, Offset: offset + slope*x, Passes: true})
		}
		edges = append(edges, e)
	}
	run := func(want string) {
		t.Helper()
		out := auditGlobalAffine(records, layout, edges, []int{0, 1, 2})
		if len(out) != 1 || out[0].Status != want {
			t.Fatalf("audit %+v, want %s", out, want)
		}
	}
	run("affine_improved_holdout_median")
	report := auditGlobalAffine(records, layout, edges, []int{0, 1, 2})
	graph := map[string]any{"active_edges": []int{0, 1, 2}, "reference_recordings": []int{0}}
	promoted := append([]NativeLayout(nil), layout...)
	applyGlobalAffine(promoted, edges, graph, report)
	if report[0].Status != "affine_applied" || promoted[0].Start != layout[0].Start ||
		promoted[0].Rate != layout[0].Rate || promoted[2].Start == layout[2].Start {
		t.Fatalf("guarded promotion failed: %+v", report[0])
	}
	graph["rejected_edges"] = []map[string]any{{"edge": 2}}
	applyGlobalAffine(layout, edges, graph, report)
	if report[0].Status != "affine_conflicting_graph" || layout[2].Start != start[2]+.012 {
		t.Fatal("a conflicting graph was changed")
	}
	edges[2].Validation.Observations[1].Offset += .080
	run("affine_holdout_disagreement")
	edges[2].Selected.Knots = []TimePoint{{0, 10}, {90, 100.1}, {180, 190}}
	run("piecewise_timing_required")
}
