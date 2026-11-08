package scheduler

import (
	"fmt"
	"math"
	"sort"

	"opensync/internal/projectstate"
)

// Attempt is the external investigation log, not an acoustic certification.
// Inconclusive and failed probes are preserved, not rejected hypotheses.
// A completed attempt can be revisited with a different method or window.
type Attempt struct {
	HypothesisID string `json:"hypothesis_id"`
	Outcome      string `json:"outcome"` // measured_inconclusive | probe_failed
	ReportPath   string `json:"report_path,omitempty"`
}

type Feedback struct {
	Version  int       `json:"version"`
	Attempts []Attempt `json:"attempts"`
}

type NextStep struct {
	HypothesisID     string  `json:"hypothesis_id"`
	Reference        string  `json:"reference"`
	Query            string  `json:"query"`
	OffsetNS         int64   `json:"discovery_offset_ns"`
	Reason           string  `json:"reason"`
	Phase            string  `json:"phase"`
	SupportWindows   int     `json:"discovery_windows"`
	Score            float64 `json:"investigation_priority"`
	CompetingOptions int     `json:"competing_options"`
}

type AdaptivePlan struct {
	Version            int       `json:"version"`
	Status             string     `json:"status"`
	SeedRecordingID    string     `json:"seed_recording_id"`
	VerifiedRecordings []string   `json:"verified_recordings"`
	Next               *NextStep  `json:"next,omitempty"`
	Pending            int       `json:"pending"`
	Deferred           []Attempt  `json:"deferred"`
	RejectedExcluded   int       `json:"rejected_excluded"`
}

// Replan chooses exactly one next diagnostic action after incorporating
// previously attempted hypotheses. Only hypotheses marked Verified by
// projectstate can establish the verified graph backbone. A diagnostic
// measurement report NEVER changes this status on its own.
//
// Discovery candidates are not connected components of the measured graph.
// Failed/inconclusive attempts are deferred, rather than erased from the
// hypothesis pool. A later recovery stage may re-open them with a new method.
func Replan(p *projectstate.Project, feedback Feedback) (AdaptivePlan, error) {
	baseline, err := Build(p)
	if err != nil {
		return AdaptivePlan{}, err
	}
	if feedback.Version != 1 {
		return AdaptivePlan{}, fmt.Errorf("unsupported feedback version %d", feedback.Version)
	}
	attempted := map[string]bool{}
	for _, a := range feedback.Attempts {
		h, exists := p.Hypotheses[a.HypothesisID]
		if !exists || h.Status == projectstate.Rejected ||
			(a.Outcome != "measured_inconclusive" && a.Outcome != "probe_failed") {
			return AdaptivePlan{}, fmt.Errorf("invalid investigation feedback for %q", a.HypothesisID)
		}
		if attempted[a.HypothesisID] {
			return AdaptivePlan{}, fmt.Errorf("duplicate investigation attempt %q", a.HypothesisID)
		}
		attempted[a.HypothesisID] = true
	}
	out := AdaptivePlan{
		Version: 1, Status: "DIAGNOSTIC_SCHEDULER_NOT_CERTIFICATION",
		SeedRecordingID: baseline.SeedRecordingID,
		VerifiedRecordings: []string{}, Deferred: append([]Attempt{}, feedback.Attempts...),
		RejectedExcluded: baseline.RejectedExcluded,
	}
	sort.Slice(out.Deferred, func(i, j int) bool {
		return out.Deferred[i].HypothesisID < out.Deferred[j].HypothesisID
	})

	// Explicitly verified edges identify usable connected components.
	// Use union-find only for *selection of work*, not timing reconstruction.
	parent := map[string]string{}
	for id := range p.Recordings {
		parent[id] = id
	}
	var root func(string) string
	root = func(x string) string {
		if parent[x] != x {
			parent[x] = root(parent[x])
		}
		return parent[x]
	}
	union := func(a, b string) {
		x, y := root(a), root(b)
		if x != y {
			if x > y {
				x, y = y, x
			}
			parent[y] = x
		}
	}
	for _, h := range p.Hypotheses {
		if h.Status == projectstate.Verified {
			union(h.Reference, h.Query)
		}
	}
	verified := map[string]bool{}
	for _, h := range p.Hypotheses {
		if h.Status == projectstate.Verified {
			verified[h.Reference], verified[h.Query] = true, true
		}
	}
	for id := range verified {
		out.VerifiedRecordings = append(out.VerifiedRecordings, id)
	}
	sort.Strings(out.VerifiedRecordings)

	priority := map[string]float64{}
	for _, a := range baseline.Anchors {
		priority[a.RecordingID] = a.Priority
	}
	// Anchor order incorporates original (unmodified) discovery connectivity;
	// no candidate counts as an accepted graph edge.
	seed := baseline.SeedRecordingID
	if len(verified) != 0 {
		// Prefer the largest verified component and its most useful reference.
		sizes := map[string]int{}
		for id := range verified {
			sizes[root(id)]++
		}
		bestSize := -1
		bestPriority := -1.0
		for _, a := range baseline.Anchors {
			if !verified[a.RecordingID] {
				continue
			}
			sz := sizes[root(a.RecordingID)]
			if sz > bestSize || (sz == bestSize && a.Priority > bestPriority) {
				seed, bestSize, bestPriority = a.RecordingID, sz, a.Priority
			}
		}
	}
	out.SeedRecordingID = seed
	connected := map[string]bool{seed: true}
	if verified[seed] {
		for id := range verified {
			if root(id) == root(seed) {
				connected[id] = true
			}
		}
	}

	// Re-evaluation is essential: if the current anchor has exhausted
	// its untried hypotheses, switch to another promising anchor.
	seeds := []string{seed}
	for _, a := range baseline.Anchors {
		if a.RecordingID != seed {
			seeds = append(seeds, a.RecordingID)
		}
	}

	pairOptions := map[string]int{}
	key := func(a, b string) string {
		if a > b {
			a, b = b, a
		}
		return a + "\x00" + b
	}
	for _, h := range p.Hypotheses {
		if h.Status != projectstate.Rejected {
			pairOptions[key(h.Reference, h.Query)]++
		}
	}
	ids := make([]string, 0, len(p.Hypotheses))
	for id := range p.Hypotheses {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, h := range p.Hypotheses {
		if h.Status != projectstate.Rejected && h.Status != projectstate.Verified &&
			!attempted[h.ID] {
			out.Pending++
		}
	}
	for _, candidateSeed := range seeds {
		frontier := connected
		if candidateSeed != seed {
			frontier = map[string]bool{candidateSeed: true}
		}
		var best *NextStep
		for _, id := range ids {
			h := p.Hypotheses[id]
			if attempted[id] || h.Status == projectstate.Rejected ||
				h.Status == projectstate.Verified {
				continue
			}
			a, b := frontier[h.Reference], frontier[h.Query]
			if !a && !b {
				continue
			}
			windows := 0
			if h.Discovery != nil {
				windows = h.Discovery.SupportWindows
			}
			score := math.Log1p(float64(windows)) +
				0.15 * (priority[h.Reference]+priority[h.Query])
			phase := "expand"
			if a && b {
				phase = "cycle_or_alternative"
				score *= 0.95
			}
			item := NextStep{
				HypothesisID: h.ID, Reference: h.Reference, Query: h.Query,
				OffsetNS: h.OffsetNS, SupportWindows: windows,
				Score: score, Phase: phase,
				CompetingOptions: pairOptions[key(h.Reference, h.Query)],
				Reason: "investigate acoustic correspondence; no certification inferred",
			}
			if best == nil || item.Score > best.Score ||
				(item.Score == best.Score && item.HypothesisID < best.HypothesisID) {
				copy := item
				best = &copy
			}
		}
		if best != nil {
			out.Next = best
			out.SeedRecordingID = candidateSeed
			return out, nil
		}
	}
	out.Status = "NO_UNTRIED_DISCOVERY_HYPOTHESES"
	return out, nil
}
