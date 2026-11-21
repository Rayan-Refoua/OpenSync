package autosync

import (
	"math"
	"sort"
)

// leastSquares uses column-pivoted Householder QR. Normal equations would
// square the conditioning of mixed seconds / clock-rate observations.
func leastSquaresColumns(a [][]float64, b, work []float64) ([]float64, bool) {
	m, n := len(b), len(a)
	if m < n {
		return nil, false
	}
	perm := make([]int, n)
	for i := range perm {
		perm[i] = i
	}
	for k := 0; k < n; k++ {
		best, norm := k, 0.
		for j := k; j < n; j++ {
			v := 0.
			for i := k; i < m; i++ {
				if x := a[j][i]; x != 0 {
					v = math.Hypot(v, x)
				}
			}
			if v > norm {
				norm = v
				best = j
			}
		}
		if norm < 1e-12 {
			return nil, false
		}
		if best != k {
			a[k], a[best] = a[best], a[k]
			perm[k], perm[best] = perm[best], perm[k]
		}
		alpha := -math.Copysign(norm, a[k][k])
		v := work[:m-k]
		copy(v, a[k][k:])
		v[0] -= alpha
		vn := 0.
		for _, x := range v {
			vn = math.Hypot(vn, x)
		}
		for i := range v {
			v[i] /= vn
		}
		for j := k; j < n; j++ {
			col := a[j][k:]
			dot := 0.
			for i, x := range v {
				dot += x * col[i]
			}
			for i, x := range v {
				col[i] -= 2 * x * dot
			}
		}
		dot := 0.
		for i, x := range v {
			dot += x * b[k+i]
		}
		for i, x := range v {
			b[k+i] -= 2 * x * dot
		}
	}
	z := make([]float64, n)
	for i := n - 1; i >= 0; i-- {
		v := b[i]
		for j := i + 1; j < n; j++ {
			v -= a[j][i] * z[j]
		}
		z[i] = v / a[i][i]
	}
	result := make([]float64, n)
	for i, p := range perm {
		result[p] = z[i]
	}
	return result, true
}

type affineTransform struct{ start, rate float64 }

func nativeAnchorLayout(records []MediaRecord, edges []*PairEvidence) []NativeLayout {
	active := make([]bool, len(edges))
	for i := range active {
		active[i] = true
	}
	var transforms []affineTransform
	var groups [][]int
	roots := make([]int, len(records))
	for i := range roots {
		roots[i] = i
	}
	sort.Slice(roots, func(i, j int) bool {
		a, b := records[roots[i]], records[roots[j]]
		if a.Duration != b.Duration {
			return a.Duration > b.Duration
		}
		return a.ID < b.ID
	})
	for {
		parents := make([]int, len(records))
		for i := range parents {
			parents[i] = i
		}
		find := func(n int) int {
			for parents[n] != n {
				parents[n] = parents[parents[n]]
				n = parents[n]
			}
			return n
		}
		var indices []int
		for k, on := range active {
			if on {
				indices = append(indices, k)
			}
		}
		sort.SliceStable(indices, func(i, j int) bool {
			a, b := edges[indices[i]], edges[indices[j]]
			if a.Selected.Confidence != b.Selected.Confidence {
				return a.Selected.Confidence > b.Selected.Confidence
			}
			if a.Reference != b.Reference {
				return a.Reference < b.Reference
			}
			return a.Query < b.Query
		})
		adjacency := make([][]graphArc, len(records))
		selected := map[int]bool{}
		for _, k := range indices {
			e := edges[k]
			a, b := find(e.Reference), find(e.Query)
			if a != b {
				parents[b] = a
				selected[k] = true
				adjacency[e.Reference] = append(adjacency[e.Reference], graphArc{e.Query, k})
				adjacency[e.Query] = append(adjacency[e.Query], graphArc{e.Reference, k})
			}
		}
		transforms = make([]affineTransform, len(records))
		groups = nil
		seen := make([]bool, len(records))
		for _, root := range roots {
			if seen[root] {
				continue
			}
			seen[root] = true
			transforms[root] = affineTransform{0, 1}
			stack := []int{root}
			group := []int{}
			for len(stack) > 0 {
				node := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				group = append(group, node)
				s := transforms[node]
				for _, arc := range adjacency[node] {
					other := arc.node
					if seen[other] {
						continue
					}
					seen[other] = true
					e := edges[arc.edge]
					a, b := e.Selected.Offset, 1+e.Selected.Slope
					if node == e.Reference {
						transforms[other] = affineTransform{s.start + s.rate*a, s.rate * b}
					} else {
						transforms[other] = affineTransform{s.start - s.rate*a/b, s.rate / b}
					}
					stack = append(stack, other)
				}
			}
			sort.Ints(group)
			groups = append(groups, group)
		}
		residualOf := func(e *PairEvidence, obs []*Measurement) float64 {
			r, q := transforms[e.Reference], transforms[e.Query]
			v := 0.
			for _, o := range obs {
				v = math.Max(v, math.Abs(r.start+r.rate*(o.QueryTime+o.Offset)-q.start-q.rate*o.QueryTime))
			}
			return v
		}
		bad, worst := -1, 0.
		for k, e := range edges {
			if active[k] && !selected[k] {
				v := residualOf(e, e.Selected.Fit)
				if v > .04 && (v > worst || (v == worst && k > bad)) {
					bad = k
					worst = v
				}
			}
		}
		if bad >= 0 {
			e := edges[bad]
			active[weakestEdge(append(graphPath(adjacency, e.Reference, e.Query), bad), edges)] = false
			continue
		}
		for _, g := range groups {
			root := g[0]
			member := map[int]bool{}
			for _, i := range g {
				member[i] = true
				if records[i].Duration > records[root].Duration || (records[i].Duration == records[root].Duration && records[i].ID > records[root].ID) {
					root = i
				}
			}
			moments := map[int][]float64{}
			var component []*PairEvidence
			for k, e := range edges {
				if active[k] && member[e.Reference] && member[e.Query] {
					component = append(component, e)
					for _, o := range e.Selected.Fit {
						moments[e.Query] = append(moments[e.Query], o.QueryTime)
						moments[e.Reference] = append(moments[e.Reference], o.QueryTime+o.Offset)
					}
				}
			}
			type variable struct {
				node  int
				drift bool
			}
			var vars []variable
			index := map[variable]int{}
			for _, node := range g {
				if node == root {
					continue
				}
				v := variable{node, false}
				index[v] = len(vars)
				vars = append(vars, v)
				ts := moments[node]
				lo, hi := math.Inf(1), math.Inf(-1)
				for _, x := range ts {
					lo = math.Min(lo, x)
					hi = math.Max(hi, x)
				}
				if len(ts) >= 3 && hi-lo >= 30 {
					v = variable{node, true}
					index[v] = len(vars)
					vars = append(vars, v)
				}
			}
			if len(vars) == 0 {
				continue
			}
			rows := [][]float64{}
			targets := []float64{}
			for _, e := range component {
				for _, o := range e.Selected.Fit {
					tq, tr := o.QueryTime, o.QueryTime+o.Offset
					row := make([]float64, len(vars))
					for _, term := range []struct {
						v variable
						x float64
					}{{variable{e.Query, false}, 1}, {variable{e.Reference, false}, -1}, {variable{e.Query, true}, tq}, {variable{e.Reference, true}, -tr}} {
						if j, ok := index[term.v]; ok {
							row[j] += term.x
						}
					}
					rows = append(rows, row)
					targets = append(targets, tr-tq)
				}
			}
			if len(rows) == 0 {
				continue
			}
			x := make([]float64, len(vars))
			origin := transforms[root]
			for j, v := range vars {
				if v.drift {
					x[j] = transforms[v.node].rate/origin.rate - 1
				} else {
					x[j] = (transforms[v.node].start - origin.start) / origin.rate
				}
			}
			dot := func(row []float64) float64 {
				v := 0.
				for j, c := range row {
					v += c * x[j]
				}
				return v
			}
			driftColumns := []int{}
			for j, v := range vars {
				if v.drift {
					driftColumns = append(driftColumns, j)
				}
			}
			m := len(rows) + len(driftColumns)
			columnStorage := make([]float64, m*len(vars))
			aa := make([][]float64, len(vars))
			bb, work := make([]float64, m), make([]float64, m)
			valid := true
			for iteration := 0; iteration < 8; iteration++ {
				// Restore canonical column order after the preceding QR pivoting.
				for j := range aa {
					aa[j] = columnStorage[j*m : (j+1)*m]
				}
				for _, col := range aa {
					clear(col[len(rows):])
				}
				clear(bb[len(rows):])
				for i, row := range rows {
					weight := math.Sqrt(1 / math.Max(1, math.Abs(dot(row)-targets[i])/.015))
					for j, v := range row {
						aa[j][i] = v * weight
					}
					bb[i] = targets[i] * weight
				}
				for i, j := range driftColumns {
					aa[j][len(rows)+i] = 5
				}
				var ok bool
				x, ok = leastSquaresColumns(aa, bb, work)
				if !ok {
					valid = false
					break
				}
			}
			if !valid {
				continue
			}
			maximum := 0.
			for i, row := range rows {
				maximum = math.Max(maximum, math.Abs(dot(row)-targets[i]))
			}
			for j, v := range vars {
				if v.drift && math.Abs(x[j]) > .002 {
					valid = false
				}
			}
			if valid && maximum <= .04 {
				transforms[root] = affineTransform{0, 1}
				for _, node := range g {
					if node == root {
						continue
					}
					rate := 1.
					if j, ok := index[variable{node, true}]; ok {
						rate += x[j]
					}
					transforms[node] = affineTransform{x[index[variable{node, false}]], rate}
				}
			}
		}
		bad, worst = -1, 0.
		nodeError := make([]float64, len(records))
		for k, e := range edges {
			if !active[k] {
				continue
			}
			v := residualOf(e, edgeObservations(e))
			nodeError[e.Reference] = math.Max(nodeError[e.Reference], v)
			nodeError[e.Query] = math.Max(nodeError[e.Query], v)
			if (v > .04 || math.Abs(transforms[e.Reference].rate-1) > .002 || math.Abs(transforms[e.Query].rate-1) > .002) && (bad < 0 || v > worst || (v == worst && k > bad)) {
				bad = k
				worst = v
			}
		}
		if bad >= 0 {
			active[bad] = false
			continue
		}
		orderGroups(groups, records)
		result := []NativeLayout{}
		for ci, g := range groups {
			lo := math.Inf(1)
			for _, node := range g {
				lo = math.Min(lo, transforms[node].start)
			}
			for _, node := range g {
				error := math.Max(.001, nodeError[node])
				for k, e := range edges {
					if active[k] && (e.Reference == node || e.Query == node) {
						error = math.Max(error, e.Selected.Residual)
					}
				}
				status := "unresolved_no_supported_relation"
				if len(g) > 1 {
					status = "unresolved_disconnected_group"
					if ci == 0 {
						status = "acoustically_supported_not_certified"
					}
				}
				tr := transforms[node]
				result = append(result, NativeLayout{Recording: node, Component: ci + 1, ComponentSize: len(g), Start: tr.start - lo, Rate: tr.rate, Error: error, Status: status})
			}
		}
		return result
	}
}
