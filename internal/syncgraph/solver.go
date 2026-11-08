package syncgraph

import (
	"errors"
	"fmt"
	"math"
	"sort"
)

// Constraint is a measured temporal relationship supplied by an external
// verification stage. It does NOT assert that the acoustic match is correct.
// Convention: start(Query) - start(Reference) == OffsetNS.
// Weight is a positive relative inverse-variance weight; zero is invalid.
// Independent event evidence must not be counted repeatedly as independent
// constraints merely because it produced multiple pairwise comparisons.
type Constraint struct {
	Reference string  `json:"reference"`
	Query     string  `json:"query"`
	OffsetNS  int64   `json:"offset_ns"`
	Weight    float64 `json:"weight"`
}

// ConstraintResidual reports the fitted relative offset minus the observation.
type ConstraintResidual struct {
	Reference              string  `json:"reference"`
	Query                  string  `json:"query"`
	ObservedOffsetNS       int64   `json:"observed_offset_ns"`
	PredictedOffsetSeconds float64 `json:"predicted_offset_seconds"`
	ResidualSeconds        float64 `json:"residual_seconds"`
}

// Component's earliest lexicographic node is an arbitrary zero-time anchor.
// Its StartsSeconds values are relative, not absolute timeline placements.
type Component struct {
	Nodes                 []string             `json:"nodes"`
	StartsSeconds         map[string]float64   `json:"starts_seconds"`
	EdgeCount             int                  `json:"edge_count"`
	CycleRank             int                  `json:"cycle_rank"`
	MaxAbsResidualSeconds float64              `json:"max_abs_residual_seconds"`
	RMSResidualSeconds    float64              `json:"rms_residual_seconds"`
	Residuals             []ConstraintResidual `json:"residuals"`
}

type Solution struct {
	Components    []Component    `json:"components"`
	NodeComponent map[string]int `json:"node_component"`
}

var ErrInvalidConstraint = errors.New("invalid temporal constraint")

type solverEdge struct {
	a, b   int
	offset float64
	weight float64
	input  Constraint
}

// SolveOffsets performs deterministic weighted least-squares reconstruction
// in each connected component of the supplied graph. It uses a sparse graph
// Laplacian and diagonally preconditioned conjugate gradient, not a dense
// O(nodes^2) matrix. It neither admits edges nor certifies their evidence.
// Nodes must be explicitly declared; isolated nodes remain in the solution.
func SolveOffsets(nodes []string, constraints []Constraint) (Solution, error) {
	solution := Solution{Components: []Component{}, NodeComponent: map[string]int{}}
	ids := append([]string(nil), nodes...)
	sort.Strings(ids)
	index := make(map[string]int, len(ids))
	for i, id := range ids {
		if id == "" || (i > 0 && id == ids[i-1]) {
			return Solution{}, fmt.Errorf("invalid or duplicate node %q", id)
		}
		index[id] = i
	}

	edges := make([]solverEdge, 0, len(constraints))
	for k, c := range constraints {
		a, okA := index[c.Reference]
		b, okB := index[c.Query]
		if !okA || !okB || a == b || c.Reference == "" || c.Query == "" ||
			!(c.Weight > 0) || math.IsNaN(c.Weight) || math.IsInf(c.Weight, 0) {
			return Solution{}, fmt.Errorf("%w at index %d: %q -> %q", ErrInvalidConstraint, k, c.Reference, c.Query)
		}
		edges = append(edges, solverEdge{a: a, b: b, offset: float64(c.OffsetNS) / 1e9, weight: c.Weight, input: c})
	}
	// A stable ordering makes diagnostics independent of map iteration order.
	sort.Slice(edges, func(i, j int) bool {
		a, b := edges[i], edges[j]
		if a.a != b.a {
			return a.a < b.a
		}
		if a.b != b.b {
			return a.b < b.b
		}
		if a.input.OffsetNS != b.input.OffsetNS {
			return a.input.OffsetNS < b.input.OffsetNS
		}
		return a.weight < b.weight
	})

	adjacency := make([][]int, len(ids))
	for k, e := range edges {
		adjacency[e.a] = append(adjacency[e.a], k)
		adjacency[e.b] = append(adjacency[e.b], k)
	}

	seen := make([]bool, len(ids))
	for seed := range ids {
		if seen[seed] {
			continue
		}
		seen[seed] = true
		queue := []int{seed}
		members := []int{}
		for len(queue) > 0 {
			u := queue[0]
			queue = queue[1:]
			members = append(members, u)
			for _, k := range adjacency[u] {
				e := edges[k]
				v := e.a
				if v == u {
					v = e.b
				}
				if !seen[v] {
					seen[v] = true
					queue = append(queue, v)
				}
			}
		}
		sort.Ints(members)
		local := make(map[int]int, len(members))
		names := make([]string, len(members))
		for i, global := range members {
			local[global] = i
			names[i] = ids[global]
			solution.NodeComponent[ids[global]] = len(solution.Components)
		}

		localEdges := make([]solverEdge, 0)
		for _, e := range edges {
			_, okA := local[e.a]
			_, okB := local[e.b]
			if okA && okB {
				e.a = local[e.a]
				e.b = local[e.b]
				localEdges = append(localEdges, e)
			}
		}
		values, err := fitOffsets(len(members), localEdges)
		if err != nil {
			return Solution{}, fmt.Errorf("component anchored at %q: %w", names[0], err)
		}
		component := Component{
			Nodes: names, StartsSeconds: make(map[string]float64, len(names)),
			EdgeCount: len(localEdges),
			CycleRank: len(localEdges) - len(names) + 1,
			Residuals: make([]ConstraintResidual, 0, len(localEdges)),
		}
		for i, name := range names {
			component.StartsSeconds[name] = values[i]
		}
		var weightedSquares, totalWeight float64
		for _, e := range localEdges {
			predicted := values[e.b] - values[e.a]
			r := predicted - e.offset
			component.Residuals = append(component.Residuals, ConstraintResidual{
				Reference: e.input.Reference, Query: e.input.Query,
				ObservedOffsetNS:       e.input.OffsetNS,
				PredictedOffsetSeconds: predicted, ResidualSeconds: r,
			})
			if math.Abs(r) > component.MaxAbsResidualSeconds {
				component.MaxAbsResidualSeconds = math.Abs(r)
			}
			weightedSquares += e.weight * r * r
			totalWeight += e.weight
		}
		if totalWeight > 0 {
			component.RMSResidualSeconds = math.Sqrt(weightedSquares / totalWeight)
		}
		solution.Components = append(solution.Components, component)
	}
	return solution, nil
}

// Compare reports a candidate's timing discrepancy against an existing
// solved path. connected=false means no independent path is available.
// A small residual alone NEVER certifies the candidate's acoustic evidence.
func (s Solution) Compare(c Constraint) (residualSeconds float64, connected bool, err error) {
	a, okA := s.NodeComponent[c.Reference]
	b, okB := s.NodeComponent[c.Query]
	if !okA || !okB || c.Reference == c.Query || c.Reference == "" || c.Query == "" {
		return 0, false, fmt.Errorf("unknown or invalid candidate endpoints: %q -> %q", c.Reference, c.Query)
	}
	if a != b {
		return 0, false, nil
	}
	starts := s.Components[a].StartsSeconds
	return starts[c.Query] - starts[c.Reference] - float64(c.OffsetNS)/1e9, true, nil
}

// Solve exposes accepted graph edges to the offset solver without changing
// admission decisions. Caller provides all known nodes to retain isolates.
// Existing Admit() status flags come from an external verifier and must not
// be interpreted as proof generated by this mathematical solver.
func (g *Graph) Solve(nodes []string) (Solution, error) {
	if g == nil {
		return Solution{}, fmt.Errorf("nil graph")
	}
	allNodes := append([]string(nil), nodes...)
	known := make(map[string]bool, len(allNodes))
	for _, n := range allNodes {
		known[n] = true
	}
	constraints := make([]Constraint, 0, len(g.accepted))
	for _, e := range g.accepted {
		if e.DriftPPM != 0 {
			return Solution{}, fmt.Errorf("constant-offset solver cannot fit nonzero drift on %q -> %q", e.Reference, e.Query)
		}
		for _, n := range []string{e.Reference, e.Query} {
			if !known[n] {
				allNodes = append(allNodes, n)
				known[n] = true
			}
		}
		constraints = append(constraints, Constraint{
			Reference: e.Reference, Query: e.Query,
			OffsetNS: e.OffsetNS, Weight: 1,
		})
	}
	return SolveOffsets(allNodes, constraints)
}

// fitOffsets minimizes sum w * (start[b]-start[a]-delta)^2,
// fixing start[0]=0 to remove the component's unconstrained origin.
func fitOffsets(n int, edges []solverEdge) ([]float64, error) {
	values := make([]float64, n)
	if n <= 1 {
		return values, nil
	}

	// A spanning tree provides a useful warm start, especially for chains.
	adj := make([][]int, n)
	for k, e := range edges {
		adj[e.a] = append(adj[e.a], k)
		adj[e.b] = append(adj[e.b], k)
	}
	visited := make([]bool, n)
	visited[0] = true
	queue := []int{0}
	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]
		for _, k := range adj[u] {
			e := edges[k]
			v, delta := e.b, e.offset
			if e.b == u {
				v, delta = e.a, -e.offset
			}
			if !visited[v] {
				visited[v] = true
				values[v] = values[u] + delta
				queue = append(queue, v)
			}
		}
	}
	for _, ok := range visited {
		if !ok {
			return nil, errors.New("internal error: component disconnected")
		}
	}

	// Node zero is pinned. Other variables use indices 1..n-1.
	diag, rhs := make([]float64, n), make([]float64, n)
	for _, e := range edges {
		if e.a != 0 {
			diag[e.a] += e.weight
			rhs[e.a] -= e.weight * e.offset
		}
		if e.b != 0 {
			diag[e.b] += e.weight
			rhs[e.b] += e.weight * e.offset
		}
	}
	apply := func(x []float64) []float64 {
		y := make([]float64, n)
		for _, e := range edges {
			d := e.weight * (x[e.b] - x[e.a])
			if e.a != 0 {
				y[e.a] -= d
			}
			if e.b != 0 {
				y[e.b] += d
			}
		}
		return y
	}
	dot := func(x, y []float64) float64 {
		var sum float64
		for i := 1; i < n; i++ {
			sum += x[i] * y[i]
		}
		return sum
	}

	r := make([]float64, n)
	Ax := apply(values)
	for i := 1; i < n; i++ {
		r[i] = rhs[i] - Ax[i]
	}
	threshold := 1e-12 * math.Max(1, math.Sqrt(dot(rhs, rhs)))
	if math.Sqrt(dot(r, r)) <= threshold {
		return values, nil
	}
	z, p := make([]float64, n), make([]float64, n)
	for i := 1; i < n; i++ {
		if !(diag[i] > 0) {
			return nil, errors.New("non-positive graph diagonal")
		}
		z[i] = r[i] / diag[i]
		p[i] = z[i]
	}
	rz := dot(r, z)
	if !(rz > 0) || math.IsInf(rz, 0) || math.IsNaN(rz) {
		return nil, errors.New("invalid graph residual")
	}
	limit := 40 * n
	if limit < 200 {
		limit = 200
	}
	for step := 0; step < limit; step++ {
		Ap := apply(p)
		denom := dot(p, Ap)
		if !(denom > 0) || math.IsInf(denom, 0) || math.IsNaN(denom) {
			return nil, errors.New("invalid graph curvature")
		}
		alpha := rz / denom
		for i := 1; i < n; i++ {
			values[i] += alpha * p[i]
			r[i] -= alpha * Ap[i]
		}
		if math.Sqrt(dot(r, r)) <= threshold {
			for _, v := range values {
				if math.IsNaN(v) || math.IsInf(v, 0) {
					return nil, errors.New("non-finite graph solution")
				}
			}
			return values, nil
		}
		for i := 1; i < n; i++ {
			z[i] = r[i] / diag[i]
		}
		next := dot(r, z)
		if !(next > 0) || math.IsNaN(next) || math.IsInf(next, 0) {
			return nil, errors.New("invalid graph search direction")
		}
		beta := next / rz
		for i := 1; i < n; i++ {
			p[i] = z[i] + beta*p[i]
		}
		rz = next
	}
	return nil, fmt.Errorf("graph solver did not converge within %d iterations", limit)
}
