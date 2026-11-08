package measuredgraph

import (
	"math"
	"testing"

	"opensync/internal/projectstate"
)

func fixture(t *testing.T) *projectstate.Project {
	t.Helper()
	p := projectstate.New()
	for _, id := range []string{"A", "B", "C", "D"} {
		if err := p.AddRecording(projectstate.Recording{ID: id, SourcePath: id + ".wav", DurationNS: 300000000000}); err != nil {
			t.Fatal(err)
		}
	}
	for _, h := range []projectstate.Hypothesis{
		{ID: "ab", Reference: "A", Query: "B", OffsetNS: 128000000, Status: projectstate.Discovered},
		{ID: "bc", Reference: "B", Query: "C", OffsetNS: 128000000, Status: projectstate.Discovered},
		{ID: "ac", Reference: "A", Query: "C", OffsetNS: 256000000, Status: projectstate.Discovered},
		{ID: "ab_alt", Reference: "A", Query: "B", OffsetNS: 256000000, Status: projectstate.Discovered},
	} {
		if err := p.AddHypothesis(h); err != nil {
			t.Fatal(err)
		}
	}
	return p
}
func measured(id, a, b string, offset int64) Candidate {
	return Candidate{HypothesisID: id, Reference: a, Query: b, Model: "constant", SourceReport: id + ".json",
		Events: []Event{
			{QueryStartNS: 1e9, WindowNS: 2e9, MeasuredOffsetNS: offset, ValidControls: 2, BandCorrelation: []float64{0.4, 0.2, 0.1}},
			{QueryStartNS: 5e9, WindowNS: 2e9, MeasuredOffsetNS: offset + 500000, ValidControls: 2, BandCorrelation: []float64{0.1, -0.1, 0.2}},
			{QueryStartNS: 9e9, WindowNS: 2e9, MeasuredOffsetNS: offset - 500000, ValidControls: 2, BandCorrelation: []float64{0.2, 0.3, 0.4}},
		}}
}
func TestMeasuredCyclesUsePreciseOffsetsNotDiscoveryGrid(t *testing.T) {
	p := fixture(t)
	result, err := Analyze(p, []Candidate{
		measured("ab", "A", "B", 52711000000),
		measured("bc", "B", "C", 12075000000),
		measured("ac", "A", "C", 64788000000),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.AcousticCertification != "not_assessed" || result.ExportAuthorization != "denied" {
		t.Fatal("false certification")
	}
	if result.ConstantCandidates != 3 {
		t.Fatal("lost precise edges")
	}
	for _, row := range result.MeasuredEdges {
		if row.Edge.OffsetNS%128000000 == 0 {
			t.Fatalf("quantized measurement incorrectly: %+v", row.Edge)
		}
		if row.Edge.IndependentEventCount != 3 || row.Edge.SpectralEventCount != 3 {
			t.Fatalf("spectral bands counted as independent events: %+v", row.Edge)
		}
		if row.Edge.HypothesisID == "ac" {
			if len(row.Paths) != 1 {
				t.Fatalf("measured path absent: %+v", row)
			}
			if row.Paths[0].ResidualNS != 2000000 {
				t.Fatalf("wrong precise cycle residual: %+v", row.Paths[0])
			}
			if row.Paths[0].Comparison != "outside_observed_spread_not_certified" {
				t.Fatalf("unexpected empirical residual assessment %+v", row.Paths[0])
			}
		}
	}
}
func TestCompetingHypothesesRetainedButNotCountedAsExtraRoute(t *testing.T) {
	p := fixture(t)
	result, err := Analyze(p, []Candidate{
		measured("ab", "A", "B", 1000000000), measured("ab_alt", "A", "B", 2000000000),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.MeasuredEdges) != 2 {
		t.Fatal("alternative evidence discarded")
	}
	for _, e := range result.MeasuredEdges {
		if len(e.Paths) != 0 || e.Status != "no_measured_alternative_path" {
			t.Fatalf("parallel candidate became corroborating path: %+v", e)
		}
	}
}
func TestAffineDeferredAndIsolatedNodesUncertified(t *testing.T) {
	p := fixture(t)
	a := measured("ab", "A", "B", 1000000000)
	a.Model = "affine"
	a.SlopeNSPerSecond = -1350000
	r, err := Analyze(p, []Candidate{a})
	if err != nil {
		t.Fatal(err)
	}
	if r.DeferredNonConstant != 1 || r.ConstantCandidates != 0 || len(r.MeasuredEdges) != 0 {
		t.Fatalf("affine flattened into constant timing edge: %+v", r)
	}
}
func TestRejectInvalidAndOverlappingEvents(t *testing.T) {
	p := fixture(t)
	e := measured("ab", "A", "B", 1e9)
	e.Events[1].QueryStartNS = 2e9
	if _, err := Analyze(p, []Candidate{e}); err == nil {
		t.Fatal("overlapping windows accepted")
	}
	e = measured("ab", "A", "B", 1e9)
	e.Events[0].BandCorrelation = []float64{0.2, 0.2, 0.2, 0.2}
	if _, err := Analyze(p, []Candidate{e}); err == nil {
		t.Fatal("four bands accepted as event evidence")
	}
	e = measured("ab", "A", "B", 1e9)
	e.Query = "C"
	if _, err := Analyze(p, []Candidate{e}); err == nil {
		t.Fatal("identity mismatch accepted")
	}
	e = measured("ab", "A", "B", 1e9)
	e.Events[1].MeasuredOffsetNS = math.MaxInt64
	if _, err := Analyze(p, []Candidate{e}); err == nil {
		t.Fatal("out-of-recording measured edge accepted")
	}
}
