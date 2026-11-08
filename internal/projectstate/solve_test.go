package projectstate

import (
	"math"
	"reflect"
	"testing"
)

func solverFixture(t *testing.T) *Project {
	t.Helper()

	p := New()
	for _, id := range []string{"A", "B", "C", "D"} {
		if err := p.AddRecording(Recording{
			ID: id, SourcePath: id + ".wav",
			DurationNS: 120_000_000_000,
		}); err != nil {
			t.Fatal(err)
		}
	}

	for _, h := range []Hypothesis{
		{ID: "AB", Reference: "A", Query: "B", OffsetNS: 10_000_000_000, Status: Discovered},
		{ID: "BC", Reference: "B", Query: "C", OffsetNS: 20_000_000_000, Status: Corroborated},
		{ID: "AC", Reference: "A", Query: "C", OffsetNS: 30_000_000_000, Status: Verified},
		{ID: "AC_wrong", Reference: "A", Query: "C", OffsetNS: 50_000_000_000, Status: Discovered},
		{ID: "AC_rejected", Reference: "A", Query: "C", OffsetNS: 80_000_000_000, Status: Rejected},
	} {
		if err := p.AddHypothesis(h); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func TestProjectSolverReconstructsComponents(t *testing.T) {
	p := solverFixture(t)

	before := len(p.Hypotheses)
	s, err := SolveHypotheses(p, []string{"AB", "BC", "AC"})
	if err != nil {
		t.Fatal(err)
	}

	if len(s.Components) != 2 {
		t.Fatalf("expected 2 components, got %d", len(s.Components))
	}

	c := s.Components[s.NodeComponent["A"]]
	if c.CycleRank != 1 {
		t.Fatalf("expected redundant path, cycle rank %d", c.CycleRank)
	}
	if math.Abs(c.StartsSeconds["C"]-30) > 1e-6 {
		t.Fatalf("wrong reconstructed time: %v", c.StartsSeconds["C"])
	}
	if c.MaxAbsResidualSeconds > 1e-6 {
		t.Fatalf("unexpected residual: %v", c.MaxAbsResidualSeconds)
	}

	isolated := s.Components[s.NodeComponent["D"]]
	if !reflect.DeepEqual(isolated.Nodes, []string{"D"}) {
		t.Fatalf("isolated recording lost: %v", isolated.Nodes)
	}

	if len(p.Hypotheses) != before {
		t.Fatal("diagnostic solver modified project evidence")
	}
}

func TestProjectSolverExposesConflicts(t *testing.T) {
	p := solverFixture(t)

	s, err := SolveHypotheses(p, []string{
		"AB", "BC", "AC", "AC_wrong",
	})
	if err != nil {
		t.Fatal(err)
	}

	c := s.Components[s.NodeComponent["A"]]
	if c.CycleRank != 2 {
		t.Fatalf("expected 2 independent cycles, got %d", c.CycleRank)
	}
	if c.MaxAbsResidualSeconds < 1 {
		t.Fatalf("contradictory hypothesis was hidden: %v", c.MaxAbsResidualSeconds)
	}
}

func TestProjectSolverRejectsInvalidSelections(t *testing.T) {
	p := solverFixture(t)

	for _, ids := range [][]string{
		{"missing"},
		{"AC_rejected"},
		{"AB", "AB"},
	} {
		if _, err := SolveHypotheses(p, ids); err == nil {
			t.Fatalf("invalid selection accepted: %v", ids)
		}
	}
}

func TestProjectSolverEmptySelectionPreservesNodes(t *testing.T) {
	p := solverFixture(t)

	s, err := SolveHypotheses(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Components) != 4 {
		t.Fatalf("expected 4 isolated nodes, got %d", len(s.Components))
	}
}
