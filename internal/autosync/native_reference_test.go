package autosync

import "testing"

func TestPreferredRecorderChangesReferenceWithoutCreatingEdges(t *testing.T) {
	records := []MediaRecord{{ID: "video", Duration: 80, Video: map[string]any{}}, {ID: "master", Duration: 60}, {ID: "unrelated", Duration: 20}}
	edge := testRelation(0, 1, 5, 1)
	edge.Selected.Slope = .0002
	for _, m := range edge.Selected.Fit {
		m.Offset += m.QueryTime * edge.Selected.Slope
	}
	layout, graph, err := nativeSolve(records, []*PairEvidence{edge}, 1)
	if err != nil {
		t.Fatal(err)
	}
	roots := graph["reference_recordings"].([]int)
	if len(roots) != 2 || roots[0] != 1 || roots[1] != 2 {
		t.Fatalf("unexpected references: %v", roots)
	}
	for _, m := range layout {
		if m.Recording == 1 && m.Rate != 1 || m.Recording == 2 && m.ComponentSize != 1 {
			t.Fatalf("unexpected layout for recording %d: %+v", m.Recording, m)
		}
	}
}
