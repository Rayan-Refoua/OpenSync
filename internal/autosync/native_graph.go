package autosync

import (
	"container/heap"
	"fmt"
	"math"
	"sort"
)

type NativeLayout struct {
	Recording     int         `json:"recording"`
	Component     int         `json:"component"`
	ComponentSize int         `json:"component_size"`
	Start         float64     `json:"start_seconds"`
	Rate          float64     `json:"rate"`
	Knots         []TimePoint `json:"timing_knots,omitempty"`
	Error         float64     `json:"fit_residual_seconds"`
	Status        string      `json:"status"`
}
type graphArc struct{ node, edge int }
type graphVisit struct {
	error              float64
	node, parent, edge int
}
type graphQueue []graphVisit

func (q graphQueue) Len() int { return len(q) }
func (q graphQueue) Less(i, j int) bool {
	a, b := q[i], q[j]
	if a.error != b.error {
		return a.error < b.error
	}
	if a.node != b.node {
		return a.node < b.node
	}
	if a.parent != b.parent {
		return a.parent < b.parent
	}
	return a.edge < b.edge
}
func (q graphQueue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }
func (q *graphQueue) Push(v any)   { *q = append(*q, v.(graphVisit)) }
func (q *graphQueue) Pop() any     { a := *q; v := a[len(a)-1]; *q = a[:len(a)-1]; return v }
func edgeObservations(e *PairEvidence) []*Measurement {
	out := append([]*Measurement(nil), e.Selected.Fit...)
	if e.Validation != nil {
		for _, o := range e.Validation.Observations {
			if o.Passes {
				out = append(out, o)
			}
		}
	}
	return out
}
func relationPoints(e *PairEvidence, records []MediaRecord) []TimePoint {
	m := e.Selected
	if len(m.Knots) > 0 {
		return m.Knots
	}
	d := records[e.Query].Duration
	return []TimePoint{{0, m.Offset}, {d, m.Offset + (1+m.Slope)*d}}
}
func rejectExcerptBridges(records []MediaRecord, edges []*PairEvidence) ([]*PairEvidence, []map[string]any) {
	parent := make([]int, len(records))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(n int) int {
		for parent[n] != n {
			parent[n] = parent[parent[n]]
			n = parent[n]
		}
		return n
	}
	strong := []*PairEvidence{}
	type weakEdge struct {
		e    *PairEvidence
		span float64
	}
	var weak []weakEdge
	for _, e := range edges {
		lo, hi := math.Inf(1), math.Inf(-1)
		for _, o := range edgeObservations(e) {
			lo = math.Min(lo, o.QueryTime-o.Window/2)
			hi = math.Max(hi, o.QueryTime+o.Window/2)
		}
		span := hi - lo
		shorter := math.Min(records[e.Reference].Duration, records[e.Query].Duration)
		if shorter >= 120 && span < 30 && span < .1*shorter {
			weak = append(weak, weakEdge{e, span})
		} else {
			strong = append(strong, e)
			parent[find(e.Query)] = find(e.Reference)
		}
	}
	rejected := []map[string]any{}
	for _, w := range weak {
		e := w.e
		if find(e.Reference) == find(e.Query) {
			strong = append(strong, e)
		} else {
			rejected = append(rejected, map[string]any{"reference": e.Reference, "query": e.Query, "status": "unresolved_short_excerpt_bridge", "supported_span_seconds": w.span})
		}
	}
	return strong, rejected
}
func edgeFromProfile(pair NativeProfilePair, p *NativeProfile, records []MediaRecord) *PairEvidence {
	m, d := p.Model, records[pair.Query].Duration
	var points []TimePoint
	if m.Kind == "piecewise_linear" {
		points = append(points, TimePoint{0, m.Knots[0][1]})
		for _, k := range m.Knots {
			points = append(points, TimePoint{k[0], k[0] + k[1]})
		}
		points = append(points, TimePoint{d, d + m.Knots[len(m.Knots)-1][1]})
	} else {
		points = []TimePoint{{0, m.Offset}, {d, m.Offset + (1+m.Slope)*d}}
	}
	train, checks := []*Measurement{}, []*Measurement{}
	for _, o := range p.Training {
		if o.Precise {
			train = append(train, o)
		}
	}
	for _, role := range [][]*Measurement{p.Selection, p.Audit} {
		for _, o := range role {
			if o.Supported && (o.Precise || math.Abs(MapTime(points, o.QueryTime)-o.QueryTime-o.Offset) <= p.Limit) {
				copy := *o
				copy.Passes = true
				checks = append(checks, &copy)
			}
		}
	}
	residual := 0.
	for _, o := range append(append([]*Measurement(nil), train...), checks...) {
		residual = math.Max(residual, math.Abs(MapTime(points, o.QueryTime)-o.QueryTime-o.Offset))
	}
	for _, w := range p.ShortWindows {
		residual = math.Max(residual, w.PeakWidth)
	}
	selected := &CandidateEvidence{NativeCandidate: NativeCandidate{Offset: points[0][1]}, Slope: (points[len(points)-1][1]-points[0][1])/d - 1, Knots: points, Confidence: p.Confidence * math.Sqrt(float64(len(train))), Residual: residual, Fit: train, Span: p.Range[1] - p.Range[0]}
	e := &PairEvidence{Reference: pair.Reference, Query: pair.Query, Selected: selected, Validation: &HoldoutReport{Observations: checks}, Profile: p}
	if p.VerificationMode != "" {
		e.Provenance = "two guarded acoustic regions; full-reference competing offsets; constant timing only; not independent physical-capture certification"
		e.Validation.Reference = pair.Reference
		e.Validation.Query = pair.Query
		e.Validation.Status = "short_partition_acoustically_supported"
		e.Validation.Passed = len(checks)
		e.Validation.Excluded = true
		e.Validation.Independent = false
		e.Validation.Overlap = p.Overlap
	}
	return e
}
func graphPath(adjacency [][]graphArc, start, target int) []int {
	type visit struct {
		node int
		path []int
	}
	queue := []visit{{start, nil}}
	seen := map[int]bool{start: true}
	for len(queue) > 0 {
		v := queue[0]
		queue = queue[1:]
		if v.node == target {
			return v.path
		}
		for _, a := range adjacency[v.node] {
			if !seen[a.node] {
				seen[a.node] = true
				p := append(append([]int(nil), v.path...), a.edge)
				queue = append(queue, visit{a.node, p})
			}
		}
	}
	return nil
}
func weakestEdge(indices []int, edges []*PairEvidence) int {
	k := indices[0]
	for _, j := range indices[1:] {
		if edges[j].Selected.Confidence < edges[k].Selected.Confidence || (edges[j].Selected.Confidence == edges[k].Selected.Confidence && j > k) {
			k = j
		}
	}
	return k
}
func orderGroups(groups [][]int, records []MediaRecord) {
	total := func(g []int) float64 {
		s := 0.
		for _, i := range g {
			s += records[i].Duration
		}
		return s
	}
	sort.SliceStable(groups, func(i, j int) bool {
		a, b := groups[i], groups[j]
		if len(a) != len(b) {
			return len(a) > len(b)
		}
		sa, sb := total(a), total(b)
		if sa != sb {
			return sa > sb
		}
		return a[0] < b[0]
	})
}
func nativeSolve(records []MediaRecord, edges []*PairEvidence) ([]NativeLayout, map[string]any, error) {
	active := make([]bool, len(edges))
	for i, e := range edges {
		if e.Reference < 0 || e.Reference >= len(records) || e.Query < 0 || e.Query >= len(records) || e.Reference == e.Query || e.Selected == nil || len(edgeObservations(e)) == 0 {
			return nil, nil, fmt.Errorf("invalid temporal edge %d", i)
		}
		if err := ValidateMapping(relationPoints(e, records)); err != nil {
			return nil, nil, err
		}
		active[i] = true
	}
	rootsOrder := make([]int, len(records))
	for i := range rootsOrder {
		rootsOrder[i] = i
	}
	sort.Slice(rootsOrder, func(i, j int) bool {
		a, b := records[rootsOrder[i]], records[rootsOrder[j]]
		if (a.Video != nil) != (b.Video != nil) {
			return a.Video != nil
		}
		if a.Duration != b.Duration {
			return a.Duration > b.Duration
		}
		return a.ID < b.ID
	})
	rejected := []map[string]any{}
	for {
		available, adjacency := make([][]graphArc, len(records)), make([][]graphArc, len(records))
		for k, e := range edges {
			if active[k] {
				available[e.Reference] = append(available[e.Reference], graphArc{e.Query, k})
				available[e.Query] = append(available[e.Query], graphArc{e.Reference, k})
			}
		}
		maps := make([][]TimePoint, len(records))
		errors := make([]float64, len(records))
		var groups [][]int
		var roots []int
		for _, root := range rootsOrder {
			if maps[root] != nil {
				continue
			}
			q := &graphQueue{{0, root, -1, -1}}
			heap.Init(q)
			settled := map[int]bool{}
			group := []int{}
			for q.Len() > 0 {
				v := heap.Pop(q).(graphVisit)
				if settled[v.node] {
					continue
				}
				settled[v.node] = true
				group = append(group, v.node)
				errors[v.node] = v.error
				if v.parent < 0 {
					maps[v.node] = []TimePoint{{0, 0}, {records[v.node].Duration, records[v.node].Duration}}
				} else {
					relation := relationPoints(edges[v.edge], records)
					if v.parent == edges[v.edge].Query {
						relation = InverseMapping(relation)
					}
					maps[v.node] = ComposeMapping(maps[v.parent], relation, records[v.node].Duration)
					adjacency[v.parent] = append(adjacency[v.parent], graphArc{v.node, v.edge})
					adjacency[v.node] = append(adjacency[v.node], graphArc{v.parent, v.edge})
				}
				for _, a := range available[v.node] {
					if !settled[a.node] {
						heap.Push(q, graphVisit{v.error + math.Max(.001, edges[a.edge].Selected.Residual), a.node, v.node, a.edge})
					}
				}
			}
			sort.Ints(group)
			groups = append(groups, group)
			roots = append(roots, root)
		}
		checks := []map[string]any{}
		bad := -1
		worst := 0.
		maximum := 0.
		for k, e := range edges {
			if !active[k] {
				continue
			}
			residual := 0.
			obs := edgeObservations(e)
			for _, o := range obs {
				residual = math.Max(residual, math.Abs(MapTime(maps[e.Reference], o.QueryTime+o.Offset)-MapTime(maps[e.Query], o.QueryTime)))
			}
			checks = append(checks, map[string]any{"edge": k, "maximum_residual_seconds": residual, "observations": len(obs)})
			maximum = math.Max(maximum, residual)
			if residual > .05 && (residual > worst || (residual == worst && k > bad)) {
				bad = k
				worst = residual
			}
		}
		if bad >= 0 {
			e := edges[bad]
			path := append(graphPath(adjacency, e.Reference, e.Query), bad)
			weak := weakestEdge(path, edges)
			active[weak] = false
			rejected = append(rejected, map[string]any{"edge": weak, "reason": "inconsistent_timing_path", "maximum_residual_seconds": worst})
			continue
		}
		orderGroups(groups, records)
		layout := []NativeLayout{}
		for ci, g := range groups {
			origin := math.Inf(1)
			for _, i := range g {
				origin = math.Min(origin, MapTime(maps[i], 0))
			}
			for _, i := range g {
				points := append([]TimePoint(nil), maps[i]...)
				for j := range points {
					points[j][1] -= origin
				}
				rate := (points[len(points)-1][1] - points[0][1]) / records[i].Duration
				status := "unresolved_no_supported_relation"
				if len(g) > 1 {
					status = "unresolved_disconnected_group"
					if ci == 0 {
						status = "acoustically_supported_not_certified"
					}
				}
				layout = append(layout, NativeLayout{i, ci + 1, len(g), points[0][1], rate, points, math.Max(.001, errors[i]), status})
			}
		}
		selected := []int{}
		for k, on := range active {
			if on {
				selected = append(selected, k)
			}
		}
		return layout, map[string]any{"components": groups, "active_edges": selected, "rejected_edges": rejected, "reference_recordings": roots, "global_evidence_residuals": checks, "maximum_global_residual_seconds": maximum, "independent_certification": "not_performed", "clock_policy": "prefer_video_reference", "timing_model": "monotonic_piecewise"}, nil
	}
}
