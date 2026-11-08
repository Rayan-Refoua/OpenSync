package graphdiagnostic

import (
	"encoding/json"
	"math"
	"testing"

	"opensync/internal/projectstate"
)

func TestTriageEnumeratesAllTwoHopPathsWithoutCap(t *testing.T) {
	p := projectstate.New()
	addRecording := func(id string) {
		t.Helper()
		if err := p.AddRecording(projectstate.Recording{ID: id, SourcePath: id + ".wav", DurationNS: 10e9}); err != nil {
			t.Fatal(err)
		}
	}
	addHyp := func(id, a, b string, offset int64) {
		t.Helper()
		if err := p.AddHypothesis(projectstate.Hypothesis{ID: id, Reference: a, Query: b, OffsetNS: offset, Status: projectstate.Discovered}); err != nil {
			t.Fatal(err)
		}
	}
	addRecording("A")
	addRecording("B")
	addHyp("direct", "A", "B", 100)
	for i := 0; i < 40; i++ {
		id := string(rune('a'+i/26)) + string(rune('a'+i%26))
		addRecording("M" + id)
		addHyp("a"+id, "A", "M"+id, 40)
		addHyp("b"+id, "M"+id, "B", 60)
	}
	report, err := Triage(p)
	if err != nil {
		t.Fatal(err)
	}
	var edge *TriagePair
	for i := range report.Pairs {
		if report.Pairs[i].First == "A" && report.Pairs[i].Second == "B" {
			edge = &report.Pairs[i]
			break
		}
	}
	if edge == nil || len(edge.Paths) != 40 {
		t.Fatalf("two-hop paths truncated: %+v", edge)
	}
	if report.Certification != "not_assessed" {
		t.Fatal("false graph certification")
	}
	for _, path := range edge.Paths {
		if path.ResidualNS != 0 {
			t.Fatalf("unexpected residual %+v", path)
		}
	}
}

func TestTriageParallelCandidatesNotIndependentPaths(t *testing.T) {
	p := projectstate.New()
	for _, id := range []string{"A", "B", "C"} {
		if err := p.AddRecording(projectstate.Recording{ID: id, SourcePath: id + ".wav", DurationNS: 10e9}); err != nil {
			t.Fatal(err)
		}
	}
	for _, h := range []projectstate.Hypothesis{
		{ID: "ab1", Reference: "A", Query: "B", OffsetNS: 100, Status: projectstate.Discovered},
		{ID: "ab2", Reference: "A", Query: "B", OffsetNS: 200, Status: projectstate.Discovered},
		{ID: "bc", Reference: "B", Query: "C", OffsetNS: 300, Status: projectstate.Discovered},
		{ID: "ac", Reference: "A", Query: "C", OffsetNS: 400, Status: projectstate.Discovered},
	} {
		if err := p.AddHypothesis(h); err != nil {
			t.Fatal(err)
		}
	}
	report, err := Triage(p)
	if err != nil {
		t.Fatal(err)
	}
	if report.InputCandidates != 4 || report.PairGroups != 3 || report.AlternativeHypothesesRetained != 1 {
		t.Fatalf("bad candidate accounting: %+v", report)
	}
	for _, pair := range report.Pairs {
		if pair.First == "A" && pair.Second == "B" {
			if len(pair.AlternateHypothesisIDs) != 1 || pair.AlternateHypothesisIDs[0] != "ab2" {
				t.Fatalf("alternative lost: %+v", pair)
			}
			if len(pair.Paths) != 1 || pair.Paths[0].Via != "C" {
				t.Fatalf("parallel counted as path: %+v", pair)
			}
		}
	}
	a, _ := json.Marshal(report)
	report2, _ := Triage(p)
	b, _ := json.Marshal(report2)
	if string(a) != string(b) {
		t.Fatal("nondeterministic output")
	}
}
func TestTriageDisconnectedAndOverflow(t *testing.T) {
	p := projectstate.New()
	for _, id := range []string{"A", "B", "C"} {
		if err := p.AddRecording(projectstate.Recording{ID: id, SourcePath: id + ".wav", DurationNS: 10e9}); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.AddHypothesis(projectstate.Hypothesis{ID: "ab", Reference: "A", Query: "B", OffsetNS: 1, Status: projectstate.Discovered}); err != nil {
		t.Fatal(err)
	}
	r, err := Triage(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.ConnectedComponents) != 2 || r.PairsWithoutPath != 1 {
		t.Fatalf("isolated nodes lost: %+v", r)
	}
	p.Hypotheses["ab"] = projectstate.Hypothesis{ID: "ab", Reference: "A", Query: "B", OffsetNS: math.MinInt64, Status: projectstate.Discovered}
	if _, err := Triage(p); err == nil {
		t.Fatal("unnegatable offset accepted")
	}
}
