// Package measuredgraph compares precise, event-sourced acoustic measurements.
// Nothing in this package can certify synchronization or authorize an export.
package measuredgraph

import (
	"fmt"
	"math"
	"sort"

	"opensync/internal/projectstate"
)

type Event struct {
	QueryStartNS     int64     `json:"query_start_ns"`
	MeasuredOffsetNS int64     `json:"measured_offset_ns"`
	WindowNS         int64     `json:"window_ns"`
	BandCorrelation  []float64 `json:"band_correlation,omitempty"`
	ValidControls    int       `json:"valid_controls"`
	ControlWins      int       `json:"control_wins"`
}
type Candidate struct {
	HypothesisID     string  `json:"hypothesis_id"`
	Reference        string  `json:"reference"`
	Query            string  `json:"query"`
	Model            string  `json:"model"`
	SlopeNSPerSecond float64 `json:"slope_ns_per_second"`
	Events           []Event `json:"events"`
	SourceReport     string  `json:"source_report"`
}
type Edge struct {
	HypothesisID          string `json:"hypothesis_id"`
	Reference             string `json:"reference"`
	Query                 string `json:"query"`
	OffsetNS              int64  `json:"offset_ns"`
	MaxDeviationNS        int64  `json:"max_deviation_ns"`
	IndependentEventCount int    `json:"independent_event_count"`
	SpectralEventCount    int    `json:"spectral_event_count"`
	ControlWins           int    `json:"control_wins"`
	SourceReport          string `json:"source_report"`
	Status                string `json:"status"`
}
type Path struct {
	Via                 string `json:"via"`
	FirstEdgeID         string `json:"first_edge_id"`
	SecondEdgeID        string `json:"second_edge_id"`
	OffsetNS            int64  `json:"offset_ns"`
	ResidualNS          int64  `json:"residual_ns"`
	EmpiricalEnvelopeNS int64  `json:"empirical_envelope_ns"`
	Comparison          string `json:"comparison"`
}
type EdgeResult struct {
	Edge   Edge   `json:"edge"`
	Paths  []Path `json:"paths"`
	Status string `json:"status"`
}
type Report struct {
	Status                string       `json:"status"`
	CandidateCount        int          `json:"candidate_count"`
	ConstantCandidates    int          `json:"constant_candidates"`
	DeferredNonConstant   int          `json:"deferred_nonconstant"`
	DeferredCandidates    []Candidate  `json:"deferred_candidates"`
	MeasuredEdges         []EdgeResult `json:"measured_edges"`
	AcousticCertification string       `json:"acoustic_certification"`
	ExportAuthorization   string       `json:"export_authorization"`
}
type pair struct{ a, b string }

func pairOf(a, b string) pair {
	if a > b {
		return pair{b, a}
	}
	return pair{a, b}
}
func safeAdd(a, b int64) (int64, bool) {
	if b > 0 && a > math.MaxInt64-b || b < 0 && a < math.MinInt64-b {
		return 0, false
	}
	return a + b, true
}
func oriented(e Edge, from, to string) (int64, bool) {
	if e.Reference == from && e.Query == to {
		return e.OffsetNS, true
	}
	if e.Query == from && e.Reference == to && e.OffsetNS != math.MinInt64 {
		return -e.OffsetNS, true
	}
	return 0, false
}
func absDiff(a, b int64) (int64, bool) {
	if b == math.MinInt64 {
		return 0, false
	}
	d, ok := safeAdd(a, -b)
	if !ok || d == math.MinInt64 {
		return 0, false
	}
	if d < 0 {
		return -d, true
	}
	return d, true
}
func validateCandidate(p *projectstate.Project, c Candidate) error {
	h, ok := p.Hypotheses[c.HypothesisID]
	if !ok || h.Status == projectstate.Rejected || h.Reference != c.Reference || h.Query != c.Query {
		return fmt.Errorf("hypothesis identity mismatch for %q", c.HypothesisID)
	}
	if len(c.Events) == 0 {
		return fmt.Errorf("no acoustic events for %q", c.HypothesisID)
	}
	if c.Model != "constant" && c.Model != "affine" {
		return fmt.Errorf("unsupported measured timing model %q", c.Model)
	}
	if math.IsNaN(c.SlopeNSPerSecond) || math.IsInf(c.SlopeNSPerSecond, 0) {
		return fmt.Errorf("invalid slope")
	}
	previousEnd := int64(-1)
	for _, e := range c.Events {
		if e.QueryStartNS < 0 || e.WindowNS <= 0 || e.QueryStartNS > math.MaxInt64-e.WindowNS {
			return fmt.Errorf("invalid event interval")
		}
		if e.QueryStartNS < previousEnd {
			return fmt.Errorf("overlapping or unordered acoustic events")
		}
		previousEnd = e.QueryStartNS + e.WindowNS
		if e.ValidControls < 0 || e.ControlWins < 0 || e.ControlWins > e.ValidControls {
			return fmt.Errorf("invalid control count")
		}
		if len(e.BandCorrelation) != 0 && len(e.BandCorrelation) != 3 {
			return fmt.Errorf("invalid spectral band count")
		}
		for _, b := range e.BandCorrelation {
			if math.IsNaN(b) || math.IsInf(b, 0) || b < -1 || b > 1 {
				return fmt.Errorf("invalid band value")
			}
		}
		// Source-relative local times must remain within query and reference.
		q := p.Recordings[c.Query]
		r := p.Recordings[c.Reference]
		if previousEnd > q.DurationNS || e.MeasuredOffsetNS > math.MaxInt64-e.QueryStartNS {
			return fmt.Errorf("event outside query")
		}
		refStart := e.QueryStartNS + e.MeasuredOffsetNS
		if refStart < 0 || refStart > r.DurationNS || e.WindowNS > r.DurationNS-refStart {
			return fmt.Errorf("measured event outside reference")
		}
	}
	return nil
}
func asEdge(c Candidate) (Edge, error) {
	offsets := make([]int64, 0, len(c.Events))
	spectral, wins := 0, 0
	for _, e := range c.Events {
		offsets = append(offsets, e.MeasuredOffsetNS)
		if len(e.BandCorrelation) == 3 {
			spectral++
		}
		wins += e.ControlWins
	}
	sort.Slice(offsets, func(i, j int) bool { return offsets[i] < offsets[j] })
	mid := offsets[len(offsets)/2]
	if len(offsets)%2 == 0 {
		// Overflow-safe integer midpoint.
		left := offsets[len(offsets)/2-1]
		mid = left/2 + mid/2 + (left%2+mid%2)/2
	}
	var deviation int64
	for _, v := range offsets {
		d, ok := absDiff(v, mid)
		if !ok {
			return Edge{}, fmt.Errorf("offset spread overflow")
		}
		if d > deviation {
			deviation = d
		}
	}
	return Edge{HypothesisID: c.HypothesisID, Reference: c.Reference, Query: c.Query,
		OffsetNS: mid, MaxDeviationNS: deviation, IndependentEventCount: len(offsets),
		SpectralEventCount: spectral, ControlWins: wins, SourceReport: c.SourceReport,
		Status: "measured_not_certified"}, nil
}

// Analyze is deterministic and never uses the 128-ms discovery offsets as
// timing constraints. Multiple bands from one event count as ONE event.
// A constant-offset edge is provisional even when cycles agree.
// Affine relationships remain in the output as deferred: a scalar offset
// would be physically misleading for a drifting recording.
func Analyze(p *projectstate.Project, candidates []Candidate) (Report, error) {
	if err := p.Validate(); err != nil {
		return Report{}, err
	}
	sorted := append([]Candidate(nil), candidates...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].HypothesisID != sorted[j].HypothesisID {
			return sorted[i].HypothesisID < sorted[j].HypothesisID
		}
		return sorted[i].SourceReport < sorted[j].SourceReport
	})
	result := Report{
		Status:         "MEASURED_GRAPH_DIAGNOSTIC_NOT_CERTIFIED",
		CandidateCount: len(sorted), MeasuredEdges: []EdgeResult{}, DeferredCandidates: []Candidate{},
		AcousticCertification: "not_assessed", ExportAuthorization: "denied",
	}
	seen := map[string]bool{}
	for _, c := range sorted {
		if seen[c.HypothesisID] {
			return Report{}, fmt.Errorf("duplicate hypothesis report %q", c.HypothesisID)
		}
		seen[c.HypothesisID] = true
		if err := validateCandidate(p, c); err != nil {
			return Report{}, err
		}
		if c.Model != "constant" {
			result.DeferredNonConstant++
			result.DeferredCandidates = append(result.DeferredCandidates, c)
			continue
		}
		edge, err := asEdge(c)
		if err != nil {
			return Report{}, err
		}
		result.MeasuredEdges = append(result.MeasuredEdges, EdgeResult{Edge: edge, Paths: []Path{}, Status: "no_measured_alternative_path"})
		result.ConstantCandidates++
	}
	// Pick one representative per source pair ONLY for alternative-path
	// diagnostics; retain every competing measured hypothesis in the output.
	rep := map[pair]Edge{}
	for _, item := range result.MeasuredEdges {
		e := item.Edge
		k := pairOf(e.Reference, e.Query)
		prev, exists := rep[k]
		if !exists || e.IndependentEventCount > prev.IndependentEventCount ||
			e.IndependentEventCount == prev.IndependentEventCount && e.HypothesisID < prev.HypothesisID {
			rep[k] = e
		}
	}
	names := make([]string, 0, len(p.Recordings))
	for id := range p.Recordings {
		names = append(names, id)
	}
	sort.Strings(names)
	for i := range result.MeasuredEdges {
		item := &result.MeasuredEdges[i]
		e := item.Edge
		for _, via := range names {
			if via == e.Reference || via == e.Query {
				continue
			}
			first, okFirst := rep[pairOf(e.Reference, via)]
			second, okSecond := rep[pairOf(via, e.Query)]
			if !okFirst || !okSecond {
				continue
			}
			a, ok := oriented(first, e.Reference, via)
			if !ok {
				return Report{}, fmt.Errorf("invalid first path direction")
			}
			b, ok := oriented(second, via, e.Query)
			if !ok {
				return Report{}, fmt.Errorf("invalid second path direction")
			}
			predicted, ok := safeAdd(a, b)
			if !ok {
				return Report{}, fmt.Errorf("path offset overflow")
			}
			if predicted == math.MinInt64 {
				return Report{}, fmt.Errorf("path residual overflow")
			}
			residual, ok := safeAdd(e.OffsetNS, -predicted)
			if !ok {
				return Report{}, fmt.Errorf("cycle residual overflow")
			}
			envelope, ok := safeAdd(e.MaxDeviationNS, first.MaxDeviationNS)
			if !ok {
				return Report{}, fmt.Errorf("uncertainty overflow")
			}
			envelope, ok = safeAdd(envelope, second.MaxDeviationNS)
			if !ok {
				return Report{}, fmt.Errorf("uncertainty overflow")
			}
			abs, ok := absDiff(residual, 0)
			if !ok {
				return Report{}, fmt.Errorf("residual magnitude overflow")
			}
			comparison := "outside_observed_spread_not_certified"
			if abs <= envelope {
				comparison = "within_observed_spread_not_certified"
			}
			item.Paths = append(item.Paths, Path{Via: via, FirstEdgeID: first.HypothesisID, SecondEdgeID: second.HypothesisID,
				OffsetNS: predicted, ResidualNS: residual, EmpiricalEnvelopeNS: envelope, Comparison: comparison})
		}
		if len(item.Paths) > 0 {
			item.Status = "measured_paths_available_review_required"
		}
	}
	return result, nil
}
