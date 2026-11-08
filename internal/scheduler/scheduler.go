// Package scheduler prioritizes cheap acoustic investigations.
// It does not measure audio, admit graph edges, or infer recording chronology.
package scheduler

import (
	"fmt"
	"math"
	"sort"

	"opensync/internal/projectstate"
)

type Anchor struct {
	RecordingID      string  `json:"recording_id"`
	SourcePath       string  `json:"source_path"`
	DurationSeconds  float64 `json:"duration_seconds"`
	UniqueNeighbors  int     `json:"unique_neighbors"`
	HypothesisCount  int     `json:"hypothesis_count"`
	DiscoveryWindows int     `json:"discovery_windows"`
	Priority         float64 `json:"priority"`
}

type Investigation struct {
	HypothesisID     string `json:"hypothesis_id"`
	Reference        string `json:"reference"`
	Query            string `json:"query"`
	OffsetNS         int64  `json:"discovery_offset_ns"`
	SupportWindows   int    `json:"discovery_windows"`
	Votes            int    `json:"discovery_votes"`
	Phase            string `json:"phase"`
	AnchorID         string `json:"anchor_id,omitempty"`
	NewRecordingID   string `json:"new_recording_id,omitempty"`
	CompetingOptions int    `json:"competing_options"`
	Reason           string `json:"reason"`
}

// Plan is a deterministic investigation order. "Covered" means only that a
// recording has been reached by a proposed investigation, never synchronized.
// An offline discovery graph does not establish valid acoustic connections.
type Plan struct {
	Version          int             `json:"version"`
	Status           string          `json:"status"`
	SeedRecordingID  string          `json:"seed_recording_id"`
	Anchors          []Anchor        `json:"anchors"`
	Investigations   []Investigation `json:"investigations"`
	RejectedExcluded int             `json:"rejected_excluded"`
}

// Build retains all non-rejected candidate hypotheses. It prefers long,
// well-connected candidate anchors, then schedules graph expansion before
// alternative and cycle checks. These are computational priorities, not
// evidence-strength, probability, or certification scores.
func Build(p *projectstate.Project) (Plan, error) {
	if err := p.Validate(); err != nil {
		return Plan{}, err
	}
	if len(p.Recordings) == 0 {
		return Plan{}, fmt.Errorf("empty project")
	}
	neighbors := make(map[string]map[string]bool, len(p.Recordings))
	byNode := make(map[string][]projectstate.Hypothesis, len(p.Recordings))
	windows := make(map[string]int, len(p.Recordings))
	anchors := make([]Anchor, 0, len(p.Recordings))
	result := Plan{Version: 1, Status: "DISCOVERY_ONLY_NOT_CERTIFIED",
		Anchors: anchors, Investigations: []Investigation{}}
	for id := range p.Recordings {
		neighbors[id] = map[string]bool{}
	}
	ids := make([]string, 0, len(p.Hypotheses))
	for id := range p.Hypotheses {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	pairOptions := map[string]int{}
	pairKey := func(a, b string) string {
		if a > b {
			a, b = b, a
		}
		return a + "\x00" + b
	}
	for _, id := range ids {
		h := p.Hypotheses[id]
		if h.Status == projectstate.Rejected {
			result.RejectedExcluded++
			continue
		}
		neighbors[h.Reference][h.Query] = true
		neighbors[h.Query][h.Reference] = true
		byNode[h.Reference] = append(byNode[h.Reference], h)
		byNode[h.Query] = append(byNode[h.Query], h)
		pairOptions[pairKey(h.Reference, h.Query)]++
		if h.Discovery != nil {
			windows[h.Reference] += h.Discovery.SupportWindows
			windows[h.Query] += h.Discovery.SupportWindows
		}
	}
	for id, r := range p.Recordings {
		// Log scaling prevents extremely long clips or dense repeated
		// hypotheses from dominating every scheduling decision.
		duration := float64(r.DurationNS) / 1e9
		degree := len(neighbors[id])
		priority := math.Log1p(duration)*math.Log1p(float64(degree)+1)
		result.Anchors = append(result.Anchors, Anchor{
			RecordingID: id, SourcePath: r.SourcePath,
			DurationSeconds: duration, UniqueNeighbors: degree,
			HypothesisCount: len(byNode[id]),
			DiscoveryWindows: windows[id], Priority: priority,
		})
	}
	sort.Slice(result.Anchors, func(i, j int) bool {
		a, b := result.Anchors[i], result.Anchors[j]
		if a.Priority != b.Priority {
			return a.Priority > b.Priority
		}
		if a.UniqueNeighbors != b.UniqueNeighbors {
			return a.UniqueNeighbors > b.UniqueNeighbors
		}
		return a.RecordingID < b.RecordingID
	})
	seed := result.Anchors[0].RecordingID
	result.SeedRecordingID = seed

	// Seeded frontier expansion is only a proposed investigation path.
	// No discovered edge is silently promoted into a solved component.
	covered := map[string]bool{seed: true}
	scheduled := map[string]bool{}
	for {
		var best *Investigation
		var bestNewScore float64
		for _, id := range ids {
			h := p.Hypotheses[id]
			if h.Status == projectstate.Rejected || scheduled[id] {
				continue
			}
			a, b := covered[h.Reference], covered[h.Query]
			if a == b {
				continue
			}
			anchor, next := h.Reference, h.Query
			if !a {
				anchor, next = h.Query, h.Reference
			}
			// Distinct windows influence investigation scheduling only.
			// A high vote count is deliberately NOT a confidence score.
			support := 0
			if h.Discovery != nil {
				support = h.Discovery.SupportWindows
			}
			score := math.Log1p(float64(support)) +
				0.2*math.Log1p(float64(len(neighbors[next]))) +
				0.1*math.Log1p(float64(p.Recordings[next].DurationNS)/1e9)
			candidate := Investigation{
				HypothesisID: id, Reference: h.Reference, Query: h.Query,
				OffsetNS: h.OffsetNS, SupportWindows: support,
				Phase: "frontier", AnchorID: anchor, NewRecordingID: next,
				CompetingOptions: pairOptions[pairKey(h.Reference, h.Query)],
				Reason: "investigate link to unvisited recording; alignment unverified",
			}
			if h.Discovery != nil {
				candidate.Votes = h.Discovery.Votes
			}
			if best == nil || score > bestNewScore ||
				(score == bestNewScore && id < best.HypothesisID) {
				copy := candidate
				best = &copy
				bestNewScore = score
			}
		}
		if best == nil {
			break
		}
		result.Investigations = append(result.Investigations, *best)
		covered[best.NewRecordingID] = true
		scheduled[best.HypothesisID] = true
	}

	// Every retained alternative survives the scheduling pass. These
	// include ambiguous offsets, parallel matches and potential cycles.
	for _, id := range ids {
		h := p.Hypotheses[id]
		if h.Status == projectstate.Rejected || scheduled[id] {
			continue
		}
		item := Investigation{
			HypothesisID: id, Reference: h.Reference, Query: h.Query,
			OffsetNS: h.OffsetNS, Phase: "alternatives",
			CompetingOptions: pairOptions[pairKey(h.Reference, h.Query)],
			Reason: "retain competing hypothesis or possible cycle for independent verification",
		}
		if h.Discovery != nil {
			item.SupportWindows = h.Discovery.SupportWindows
			item.Votes = h.Discovery.Votes
		}
		result.Investigations = append(result.Investigations, item)
	}
	sort.SliceStable(result.Investigations, func(i, j int) bool {
		a, b := result.Investigations[i], result.Investigations[j]
		if a.Phase != b.Phase {
			return a.Phase == "frontier"
		}
		if a.Phase == "frontier" {
			return false // preserve greedy expansion order
		}
		if a.SupportWindows != b.SupportWindows {
			return a.SupportWindows > b.SupportWindows
		}
		return a.HypothesisID < b.HypothesisID
	})
	return result, nil
}
