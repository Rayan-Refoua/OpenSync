package autosync

import (
	"math"
	"sort"

	"opensync/internal/researchmatch"
)

type occupiedSearch struct {
	coverage map[int][2]float64
	blocked  map[int]bool
	anchors  map[int]NativeLayout
	groups   map[string][][2]float64
}

// Use explicitly reviewed streams and independently validated graph edges.
func buildOccupiedSearch(records []MediaRecord, layout []NativeLayout, edges []*PairEvidence, decisions []map[string]any, options NativeSyncOptions) occupiedSearch {
	search := occupiedSearch{}
	if options.Mode != "chronological" || options.Analysis.DisableSearchShortcuts || options.Analysis.Verification.custom() {
		return search
	}
	search.anchors = map[int]NativeLayout{}
	search.coverage = map[int][2]float64{}
	search.blocked = map[int]bool{}
	for _, decision := range decisions {
		if decision["status"] == "unresolved_competing_events" {
			search.blocked[int(number(decision["recording"]))] = true
		}
	}
	search.groups = map[string][][2]float64{}
	for _, m := range layout {
		if m.Component == 1 && m.ComponentSize > 1 && m.Status == "acoustically_supported_not_certified" && len(m.Knots) >= 2 && m.Error <= .05 {
			search.anchors[m.Recording] = m
		}
	}
	witnesses := map[int]map[string]bool{}
	for _, edge := range edges {
		profile := edge.Profile
		if edge.Ambiguous || edge.Selected == nil || profile == nil || profile.Status != "validated_relation" || profile.AuditResult == nil ||
			!profile.AuditResult.Passes || profile.AuditResult.Ambiguous != 0 || profile.AuditResult.Supported < 2 || profile.Limit <= 0 || profile.Limit > .05 || len(profile.Range) != 2 || profile.Range[0] >= profile.Range[1] {
			continue
		}
		for _, pair := range [][2]int{{edge.Reference, edge.Query}, {edge.Query, edge.Reference}} {
			clip, other := pair[0], pair[1]
			if _, ok := search.anchors[clip]; !ok {
				continue
			}
			if _, ok := search.anchors[other]; !ok {
				continue
			}
			key, _ := SourceTrackGroup(records[clip])
			otherKey, _ := SourceTrackGroup(records[other])
			if key == otherKey {
				continue
			}
			if witnesses[clip] == nil {
				witnesses[clip] = map[string]bool{}
			}
			witnesses[clip][otherKey] = true
			lo, hi := profile.Range[0], profile.Range[1]
			if clip == edge.Reference {
				points := relationPoints(edge, records)
				lo, hi = MapTime(points, lo), MapTime(points, hi)
			}
			if previous, ok := search.coverage[clip]; ok {
				lo, hi = max(lo, previous[0]), min(hi, previous[1])
			}
			search.coverage[clip] = [2]float64{lo, hi}
		}
	}
	for i, m := range search.anchors {
		if len(witnesses[i]) < 2 || search.blocked[i] {
			delete(search.anchors, i)
			delete(search.coverage, i)
			continue
		}
		r := records[i]
		assignment := r.SourceAssignment
		if assignment == nil || !assignment.Exclusive {
			continue
		}
		_, _, basis := detectSourceTrack(r)
		if basis == "transcoded" {
			continue
		}
		span := search.coverage[i]
		start := MapTime(m.Knots, max(span[0], 0, -sourceOrigin(r))) + 2
		end := MapTime(m.Knots, min(span[1], r.Duration)) - 2
		if end > start {
			search.groups[assignment.Group] = append(search.groups[assignment.Group], [2]float64{start, end})
		}
	}
	return search
}

func (s occupiedSearch) excluded(records []MediaRecord, reference, query int) []researchmatch.OffsetSpan {
	if len(s.groups) == 0 {
		return nil
	}
	q := records[query]
	if q.SourceAssignment == nil || !q.SourceAssignment.Exclusive || s.blocked[query] {
		return nil
	}
	m, anchored := s.anchors[reference]
	if !anchored {
		return nil
	}
	_, _, basis := detectSourceTrack(q)
	if basis == "transcoded" {
		return nil
	}

	span := s.coverage[reference]
	if span[0] >= span[1] {
		return nil
	}
	refStart := MapTime(m.Knots, max(span[0], 0, m.Knots[0][0]))
	refEnd := MapTime(m.Knots, min(span[1], records[reference].Duration, m.Knots[len(m.Knots)-1][0]))
	intervals := s.groups[q.SourceAssignment.Group]
	if len(intervals) == 0 {
		return nil
	}
	inverse := InverseMapping(m.Knots)
	padding := math.Abs(sourceOrigin(q))
	var spans []researchmatch.OffsetSpan
	for _, interval := range intervals {
		// Avoid extrapolating a reference clock past its observed media.
		lo := max(interval[0]+padding, refStart)
		hi := min(interval[1]-padding, refEnd)
		if lo >= hi {
			continue
		}
		first := int(math.Ceil(MapTime(inverse, lo) * 100))
		end := int(math.Floor(MapTime(inverse, hi) * 100))
		if first < end {
			spans = append(spans, researchmatch.OffsetSpan{First: first, End: end})
		}
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].First < spans[j].First })
	merged := spans[:0]
	for _, span := range spans {
		if n := len(merged); n > 0 && span.First <= merged[n-1].End {
			merged[n-1].End = max(merged[n-1].End, span.End)
		} else {
			merged = append(merged, span)
		}
	}
	return merged
}

func (s occupiedSearch) cacheKey(records []MediaRecord) any {
	if len(s.groups) == 0 {
		return nil
	}
	// Canonicalize the intervals before hashing the report identity.
	for _, intervals := range s.groups {
		sort.Slice(intervals, func(i, j int) bool {
			if intervals[i][0] != intervals[j][0] {
				return intervals[i][0] < intervals[j][0]
			}
			return intervals[i][1] < intervals[j][1]
		})
	}
	assignments := make([]*SourceAssignment, len(records))
	for i, r := range records {
		assignments[i] = r.SourceAssignment
	}
	return []any{s.groups, s.coverage, s.blocked, assignments}
}
