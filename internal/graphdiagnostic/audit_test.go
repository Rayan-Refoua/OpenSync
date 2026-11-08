package graphdiagnostic

import (
	"math"
	"testing"

	"opensync/internal/projectstate"
)

func newProject(names ...string) *projectstate.Project {
	p := projectstate.New()
	for _, id := range names {
		if err := p.AddRecording(projectstate.Recording{ID: id, SourcePath: id + ".wav", DurationNS: 9000000000}); err != nil {
			panic(err)
		}
	}
	return p
}
func add(t *testing.T, p *projectstate.Project, id, a, b string, offset int64) {
	t.Helper()
	if err := p.AddHypothesis(projectstate.Hypothesis{ID: id, Reference: a, Query: b, OffsetNS: offset, Status: projectstate.Discovered}); err != nil {
		t.Fatal(err)
	}
}
func find(report Report, id string) EdgeAudit {
	for _, v := range report.Edges {
		if v.HypothesisID == id {
			return v
		}
	}
	panic("missing edge " + id)
}
func TestTriangleCycleResidualAndDirection(t *testing.T) {
	p := newProject("a", "b", "c")
	add(t, p, "ab", "a", "b", 1000000000)
	add(t, p, "bc", "b", "c", 2000000000)
	add(t, p, "ac", "a", "c", 3005000000)
	report, err := Audit(p)
	if err != nil {
		t.Fatal(err)
	}
	if report.AcousticCertification != "not_assessed" || len(report.ConnectedComponents) != 1 {
		t.Fatalf("overclaim: %+v", report)
	}
	ac := find(report, "ac")
	if ac.Status != "alternative_paths_diagnostic_only" || len(ac.AlternativePaths) != 1 {
		t.Fatalf("no redundant path: %+v", ac)
	}
	path := ac.AlternativePaths[0]
	if path.PredictedOffsetNS != 3000000000 || path.ResidualNS != 5000000 {
		t.Fatalf("unexpected residual: %+v", path)
	}
	bc := find(report, "bc")
	if len(bc.AlternativePaths) != 1 || bc.AlternativePaths[0].ResidualNS != -5000000 {
		t.Fatalf("wrong reversal: %+v", bc)
	}
}
func TestDisconnectedAndParallelAreNotCertification(t *testing.T) {
	p := newProject("a", "b", "c", "d")
	add(t, p, "ab1", "a", "b", 100)
	add(t, p, "ab2", "a", "b", 101)
	report, err := Audit(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.ConnectedComponents) != 3 {
		t.Fatalf("components: %+v", report.ConnectedComponents)
	}
	for _, id := range []string{"ab1", "ab2"} {
		e := find(report, id)
		if len(e.AlternativePaths) > 0 || e.Status != "no_independent_alternative_path" {
			t.Fatalf("parallel candidate treated as separate graph path: %+v", e)
		}
	}
}
func TestMultipleAlternativesAndRejectedExcluded(t *testing.T) {
	p := newProject("a", "b", "c", "d")
	add(t, p, "ab", "a", "b", 2)
	add(t, p, "bc", "b", "c", 3)
	add(t, p, "ad", "a", "d", 4)
	add(t, p, "dc", "d", "c", 1)
	add(t, p, "ac", "a", "c", 5)
	if err := p.AddHypothesis(projectstate.Hypothesis{ID: "rejected", Reference: "b", Query: "d", OffsetNS: 100, Status: projectstate.Rejected}); err != nil {
		t.Fatal(err)
	}
	report, err := Audit(p)
	if err != nil {
		t.Fatal(err)
	}
	if report.CandidateCount != 5 {
		t.Fatal("rejected hypothesis joined graph")
	}
	ac := find(report, "ac")
	if len(ac.AlternativePaths) != 2 || ac.AlternativePaths[0].ResidualNS != 0 || ac.AlternativePaths[1].ResidualNS != 0 {
		t.Fatalf("missing redundant routes: %+v", ac)
	}
	for _, path := range ac.AlternativePaths {
		for _, id := range path.HypothesisIDs {
			if id == "rejected" {
				t.Fatal("rejected used as path")
			}
		}
	}
}
func TestOffsetNegationOverflowRejected(t *testing.T) {
	p := newProject("a", "b")
	add(t, p, "ab", "a", "b", math.MinInt64)
	if _, err := Audit(p); err == nil {
		t.Fatal("unsafe overflow accepted")
	}
}
func TestDeterministicAudit(t *testing.T) {
	p := newProject("z", "b", "a", "c")
	add(t, p, "za", "z", "a", 900)
	add(t, p, "ab", "a", "b", 800)
	add(t, p, "bc", "b", "c", 700)
	add(t, p, "zc", "z", "c", 2400)
	a, err := Audit(p)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Audit(p)
	if err != nil {
		t.Fatal(err)
	}
	for i := range a.Edges {
		if a.Edges[i].HypothesisID != b.Edges[i].HypothesisID || len(a.Edges[i].AlternativePaths) != len(b.Edges[i].AlternativePaths) {
			t.Fatal("nondeterministic graph diagnostics")
		}
	}
}
