package autosync

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

type ManualCandidate struct {
	ID        string  `json:"id"`
	Reference int     `json:"reference"`
	Query     int     `json:"query"`
	Shift     float64 `json:"shift"` // Reference media time minus query media time.
	Status    string  `json:"status"`
	CanAccept bool    `json:"canAccept"`
}

// Reapply choices against the new solution so an accepted clip follows its
// reference after a rebuild. A rejected or no-longer-placed reference prevents
// reapplication and the old choice becomes available for review again.
func reapplyManualCandidates(records []MediaRecord, models []map[string]any, graph map[string]any) ([]map[string]any, map[string]any, error) {
	decisions, _ := graph["manual_candidate_decisions"].(map[string]any)
	var accepted []string
	for id, decision := range decisions {
		if decision == "accepted" {
			accepted = append(accepted, id)
		}
	}
	sort.Strings(accepted)
	for _, id := range accepted {
		delete(decisions, id)
	}
	for len(accepted) > 0 {
		var pending []string
		for _, id := range accepted {
			payload, err := json.Marshal(map[string]any{"recordings": records, "timing_models": models, "graph": graph})
			if err != nil {
				return nil, nil, err
			}
			var data NativePayload
			if err = json.Unmarshal(payload, &data); err != nil {
				return nil, nil, err
			}
			next, updated, _, err := reviewCandidate(&NativeProject{Payload: payload, Data: data}, id, true)
			if err != nil {
				pending = append(pending, id)
			} else {
				models, graph = next, updated
			}
		}
		if len(pending) == len(accepted) {
			break
		}
		accepted = pending
	}
	return models, graph, nil
}

func ManualCandidates(p NativePayload) []ManualCandidate {
	decisions, _ := p.Graph["manual_candidate_decisions"].(map[string]any)
	models := map[int]TimeModel{}
	for _, m := range p.Models {
		models[m.Recording] = m
	}
	var pairs []struct {
		Reference, Query int
		Hypotheses       []struct {
			Prior struct {
				Offset *float64
			}
			Status string
		}
	}
	raw, _ := json.Marshal(p.Graph["unresolved_timing_hypotheses"])
	if json.Unmarshal(raw, &pairs) != nil {
		return nil
	}
	var result []ManualCandidate
	seen := map[string]bool{}
	for _, pair := range pairs {
		r, q := pair.Reference, pair.Query
		reference, refOK := models[r]
		query, queryOK := models[q]
		if !refOK || !queryOK || r == q || r < 0 || q < 0 || r >= len(p.Records) || q >= len(p.Records) ||
			p.Records[r].Audio == nil || p.Records[q].Audio == nil {
			continue
		}
		for _, h := range pair.Hypotheses {
			r, q := pair.Reference, pair.Query
			if h.Prior.Offset == nil || strings.HasPrefix(h.Status, "insufficient_") || h.Status == "validated_relation" {
				continue
			}
			shift := *h.Prior.Offset + reference.AudioOrigin - query.AudioOrigin
			preferredReverse := query.Resolved && !reference.Resolved
			for _, reversed := range []bool{false, true} {
				r, q, offset := r, q, shift
				if reversed {
					r, q, offset = q, r, -offset
				}
				id := fmt.Sprintf("%d:%d:%.9f", r, q, offset)
				pending := reversed == preferredReverse && !(reference.Resolved && query.Resolved)
				if seen[id] || (!pending && decisions[id] == nil) ||
					math.Min(p.Records[q].Duration, p.Records[r].Duration-offset)-math.Max(0, -offset) < .25 {
					continue
				}
				seen[id] = true
				canAccept := !models[q].Resolved && !strings.HasPrefix(models[r].Status, "provisional_") &&
					models[q].ComponentSize == 1 && (models[r].Resolved || models[q].TimingBasis != "manual_local_audio_review")
				result = append(result, ManualCandidate{id, r, q, offset, h.Status, canAccept})
			}
		}
	}
	return result
}

func reviewCandidate(p *NativeProject, id string, accept bool) ([]map[string]any, map[string]any, string, error) {
	var candidate *ManualCandidate
	for _, c := range ManualCandidates(p.Data) {
		if c.ID == id {
			candidate = &c
			break
		}
	}
	if candidate == nil {
		return nil, nil, "", fmt.Errorf("candidate is no longer available")
	}
	payload, err := rawObject(p.Payload)
	if err != nil {
		return nil, nil, "", err
	}
	models := make([]map[string]any, 0, len(p.Data.Models))
	for _, entry := range payload["timing_models"].([]any) {
		models = append(models, entry.(map[string]any))
	}
	graph := payload["graph"].(map[string]any)
	decisions, _ := graph["manual_candidate_decisions"].(map[string]any)
	if decisions == nil {
		decisions = map[string]any{}
	}
	graph["manual_candidate_decisions"] = decisions
	if decisions[id] != nil {
		return nil, nil, "", fmt.Errorf("candidate was already reviewed; undo the revision to review it again")
	}
	label := "Reject candidate"
	decisions[id] = "rejected"
	if accept {
		ledger, err := projectLedger(p)
		if err != nil {
			return nil, nil, "", err
		}
		blocked, pairs, err := reviewBlocked(p.Data.Records, ledger)
		if err != nil {
			return nil, nil, "", err
		}
		r, q := candidate.Reference, candidate.Query
		if blocked[r] || blocked[q] || pairs[orderedReviewPair(p.Data.Records[r].ID, p.Data.Records[q].ID)] {
			return nil, nil, "", fmt.Errorf("restore the rejected recording or relationship before accepting this candidate")
		}
		var anchor, target TimeModel
		for _, m := range p.Data.Models {
			switch m.Recording {
			case r:
				anchor = m
			case q:
				target = m
			}
		}
		if !candidate.CanAccept {
			decisions[id] = "confirmed"
			return models, graph, "Confirm match · " + p.Data.Records[q].Name, nil
		}
		length := p.Data.Records[q].Duration
		point := func(x float64) float64 {
			if len(anchor.Knots) >= 2 {
				return MapTime(anchor.Knots, x+candidate.Shift)
			}
			return anchor.Offset + anchor.Rate*(x+candidate.Shift)
		}
		knots := []TimePoint{{0, point(0)}}
		for _, knot := range anchor.Knots {
			x := knot[0] - candidate.Shift
			if x > 0 && x < length {
				knots = append(knots, TimePoint{x, knot[1]})
			}
		}
		knots = append(knots, TimePoint{length, point(length)})
		for _, m := range models {
			if int(number(m["recording"])) != q {
				continue
			}
			analysis := append([]TimePoint(nil), knots...)
			for i := range analysis {
				analysis[i][0] -= target.AudioOrigin
			}
			m["status"], m["timing_basis"], m["globally_resolved"] = "manually_accepted", "manual_audio_review", anchor.Resolved
			if !anchor.Resolved {
				m["status"], m["timing_basis"] = "unresolved_disconnected_group", "manual_local_audio_review"
			}
			m["timing_knots"], m["analysis_timing_knots"] = knots, analysis
			m["component_offset_seconds"], m["analysis_component_offset_seconds"] = point(0), point(target.AudioOrigin)
			m["rate"], m["fit_residual_seconds"] = (point(length)-point(0))/length, 0
			delete(m, "placement_uncertainty_seconds")
		}
		if !anchor.Resolved && target.Component != anchor.Component {
			groups := graph["components"].([]any)
			old := groups[target.Component-1].([]any)
			kept := make([]any, 0, len(old))
			for _, index := range old {
				if int(number(index)) != q {
					kept = append(kept, index)
				}
			}
			groups[target.Component-1] = kept
			groups[anchor.Component-1] = append(groups[anchor.Component-1].([]any), q)
			for _, m := range models {
				component := int(number(m["component"]))
				if int(number(m["recording"])) == q {
					component = anchor.Component
					m["component"] = component
					m["time_domain"] = fmt.Sprintf("component-%d", component)
				}
				m["component_size"] = len(groups[component-1].([]any))
			}
		}
		decisions[id] = "accepted"
		label = "Accept candidate"
	}
	return models, graph, label + " · " + p.Data.Records[candidate.Query].Name, nil
}
