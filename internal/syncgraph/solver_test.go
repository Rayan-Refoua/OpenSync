package syncgraph

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"testing"
)

func ns(seconds float64) int64 { return int64(math.Round(seconds * 1e9)) }
func near(t *testing.T, actual, expected, tolerance float64) {
	t.Helper()
	if math.Abs(actual-expected) > tolerance {
		t.Fatalf("got %.12f, want %.12f (tol %.12f)", actual, expected, tolerance)
	}
}
func fixture() ([]string, []Constraint) {
	return []string{"F", "A", "D", "C", "B", "E", "ISOLATED"}, []Constraint{
		{"A", "B", ns(12.003), 1},
		{"B", "C", ns(17.998), 1},
		{"A", "C", ns(30.007), 1},
		{"C", "D", ns(15.006), 1},
		{"B", "D", ns(33.001), 1},
		{"E", "F", ns(4), 1},
	}
}
func TestWeightedComponentsAndCandidates(t *testing.T) {
	nodes, edges := fixture()
	s, err := SolveOffsets(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Components) != 3 {
		t.Fatalf("expected 3 components, got %d", len(s.Components))
	}
	main := s.Components[s.NodeComponent["A"]]
	if !reflect.DeepEqual(main.Nodes, []string{"A", "B", "C", "D"}) {
		t.Fatal(main.Nodes)
	}
	if main.CycleRank != 2 || main.EdgeCount != 5 {
		t.Fatalf("bad cycle count: %+v", main)
	}
	near(t, main.StartsSeconds["B"], 12.005625, 1e-8)
	near(t, main.StartsSeconds["C"], 30.004375, 1e-8)
	near(t, main.StartsSeconds["D"], 45.0085, 1e-8)
	near(t, main.MaxAbsResidualSeconds, 0.002625, 1e-8)
	if s.Components[s.NodeComponent["ISOLATED"]].EdgeCount != 0 {
		t.Fatal("isolate connected")
	}
	good, yes, err := s.Compare(Constraint{Reference: "A", Query: "D", OffsetNS: ns(45.010)})
	if err != nil || !yes {
		t.Fatalf("good: %v %v", yes, err)
	}
	near(t, good, -0.0015, 1e-8)
	bad, yes, err := s.Compare(Constraint{Reference: "A", Query: "D", OffsetNS: ns(6726.956)})
	if err != nil || !yes {
		t.Fatalf("bad: %v %v", yes, err)
	}
	if math.Abs(bad) < 6000 {
		t.Fatalf("false offset not detected: %f", bad)
	}
	_, yes, err = s.Compare(Constraint{Reference: "D", Query: "E", OffsetNS: ns(120)})
	if err != nil || yes {
		t.Fatalf("bridge unexpectedly independently comparable: %v %v", yes, err)
	}
}
func TestFaultInjectionExposesLargeResidual(t *testing.T) {
	nodes, edges := fixture()
	edges = append(edges, Constraint{"A", "D", ns(6726.956), 1})
	s, err := SolveOffsets(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}
	if s.Components[s.NodeComponent["A"]].MaxAbsResidualSeconds < 1000 {
		t.Fatal("incorrect cycle was concealed")
	}
}
func TestWeightedFit(t *testing.T) {
	s, err := SolveOffsets([]string{"A", "B"}, []Constraint{
		{"A", "B", ns(10), 9},
		{"A", "B", ns(12), 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	near(t, s.Components[0].StartsSeconds["B"], 10.2, 1e-9)
	if s.Components[0].CycleRank != 1 {
		t.Fatal("parallel edges must form a cycle")
	}
}
func TestNegativeOffsetSign(t *testing.T) {
	s, err := SolveOffsets([]string{"A", "B"}, []Constraint{{"A", "B", ns(-40), 1}})
	if err != nil {
		t.Fatal(err)
	}
	near(t, s.Components[0].StartsSeconds["B"], -40, 1e-9)
}
func TestOrderingIsStable(t *testing.T) {
	nodes, edges := fixture()
	first, err := SolveOffsets(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}
	for i, j := 0, len(edges)-1; i < j; i, j = i+1, j-1 {
		edges[i], edges[j] = edges[j], edges[i]
	}
	second, err := SolveOffsets(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("input ordering changed output\n%+v\n%+v", first, second)
	}
}
func TestBadInputFailClosed(t *testing.T) {
	cases := []struct {
		nodes []string
		edges []Constraint
	}{
		{[]string{"A", "A"}, nil},
		{[]string{""}, nil},
		{[]string{"A"}, []Constraint{{"A", "B", 1, 1}}},
		{[]string{"A", "B"}, []Constraint{{"A", "A", 1, 1}}},
		{[]string{"A", "B"}, []Constraint{{"A", "B", 1, 0}}},
		{[]string{"A", "B"}, []Constraint{{"A", "B", 1, math.NaN()}}},
		{[]string{"A", "B"}, []Constraint{{"A", "B", 1, math.Inf(1)}}},
	}
	for i, c := range cases {
		_, err := SolveOffsets(c.nodes, c.edges)
		if err == nil {
			t.Fatalf("case %d accepted invalid input", i)
		}
	}
	_, err := SolveOffsets([]string{"A", "B"}, []Constraint{{"A", "B", 1, 0}})
	if !errors.Is(err, ErrInvalidConstraint) {
		t.Fatalf("expected wrapped ErrInvalidConstraint, got %v", err)
	}
}
func TestSolveAdmittedGraphDoesNotChangeAdmission(t *testing.T) {
	g := New()
	if err := g.Admit(Edge{Reference: "A", Query: "B", OffsetNS: ns(4), Status: Discovered}); err == nil {
		t.Fatal("unverified edge admitted")
	}
	verified := Edge{Reference: "A", Query: "B", OffsetNS: ns(4), Status: Verified,
		Evidence: Evidence{AnchorCount: 3, SpanSeconds: 30, HoldoutCount: 2, FeatureFamilies: 2, TimingCertified: true, ConflictFree: true}}
	if err := g.Admit(verified); err != nil {
		t.Fatal(err)
	}
	s, err := g.Solve([]string{"A", "B", "C"})
	if err != nil {
		t.Fatal(err)
	}
	if g.Count() != 1 || len(s.Components) != 2 {
		t.Fatal("solver mutated the accepted graph or lost isolated node")
	}
	near(t, s.Components[s.NodeComponent["A"]].StartsSeconds["B"], 4, 1e-9)
}
func TestLongChainUsesSparseSolver(t *testing.T) {
	const count = 1500
	nodes := make([]string, count)
	for i := range nodes {
		nodes[i] = fmt.Sprintf("%04d", i)
	}
	edges := make([]Constraint, 0, count-1)
	for i := 0; i < count-1; i++ {
		edges = append(edges, Constraint{nodes[i], nodes[i+1], ns(3.25), 1})
	}
	s, err := SolveOffsets(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Components) != 1 || s.Components[0].CycleRank != 0 {
		t.Fatalf("bad chain: %+v", s.Components[0])
	}
	near(t, s.Components[0].StartsSeconds[nodes[count-1]], 3.25*(count-1), 1e-6)
}
func TestEmptyGraph(t *testing.T) {
	s, err := SolveOffsets(nil, nil)
	if err != nil || len(s.Components) != 0 {
		t.Fatalf("empty graph: %+v %v", s, err)
	}
}

func TestAdmittedDriftIsNotSilentlyIgnored(t *testing.T) {
	g := New()
	e := Edge{Reference: "A", Query: "B", OffsetNS: ns(4), DriftPPM: 100, Status: Verified,
		Evidence: Evidence{AnchorCount: 3, SpanSeconds: 30, HoldoutCount: 2, FeatureFamilies: 2, TimingCertified: true, ConflictFree: true}}
	if err := g.Admit(e); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Solve([]string{"A", "B"}); err == nil {
		t.Fatal("drift was ignored")
	}
}
