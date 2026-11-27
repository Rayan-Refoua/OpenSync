package autosync

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
)

type waveformReviewEvidence struct {
	Shift      float64        `json:"shift"`
	Validation *HoldoutReport `json:"validation"`
}

// VerifyReviewWaveforms proposes a single revision for pending candidates
// whose placement is corroborated by the waveform and withheld windows.
func VerifyReviewWaveforms(ctx context.Context, path string) (string, int, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", 0, err
	}
	project, err := LoadNativeProject(path)
	if err != nil {
		return "", 0, err
	}
	records := project.Data.Records
	ledger, err := projectLedger(project)
	if err != nil {
		return "", 0, err
	}
	blocked, rejected, err := reviewBlocked(records, ledger)
	if err != nil {
		return "", 0, err
	}

	models := map[int]TimeModel{}
	for _, m := range project.Data.Models {
		models[m.Recording] = m
	}
	decisions, _ := project.Data.Graph["manual_candidate_decisions"].(map[string]any)
	var candidates []ManualCandidate
	unavailable := map[int]bool{}
	actionable := map[int]bool{}
	all := ManualCandidates(project.Data)
	for _, c := range all {
		if !c.AuditOnly && decisions[c.ID] == nil && c.CanAccept && models[c.Reference].Resolved && !blocked[c.Query] {
			actionable[c.Query] = true
		}
	}
	for _, c := range all {
		if c.AuditOnly || decisions[c.ID] != nil || !actionable[c.Query] || blocked[c.Reference] ||
			rejected[orderedReviewPair(records[c.Reference].ID, records[c.Query].ID)] {
			continue
		}
		if checkSource(records[c.Query]) != nil || checkSource(records[c.Reference]) != nil {
			unavailable[c.Query] = true
			continue
		}
		candidates = append(candidates, c)
	}
	if len(candidates) == 0 {
		return "", 0, nil
	}

	workspace := &nativeWorkspace{data: map[string][]byte{}, limit: 512 << 20, audio: newDirectAudio()}
	defer workspace.audio.Close()
	ctx = context.WithValue(ctx, workspaceKey{}, workspace)
	analyzer := NewNativeAnalyzer(records)
	defer analyzer.Close()
	analyzer.ctx = ctx
	var checkedEdges []*PairEvidence
	byPair := map[*CandidateEvidence]ManualCandidate{}
	for i, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return "", 0, err
		}
		if unavailable[candidate.Query] {
			continue
		}
		observe(ctx, ProgressEvent{Kind: "pair", Stage: "Waveform review", Completed: i, Total: len(candidates), Reference: candidate.Reference, Query: candidate.Query, Status: "Verifying"})
		ref, query := models[candidate.Reference], models[candidate.Query]
		offset := candidate.Shift - ref.AudioOrigin + query.AudioOrigin
		prior := NativeCandidate{Offset: offset, Method: "review_waveform"}
		if records[candidate.Query].Duration < 30 {
			// Leave room between fit windows for independent validation on short clips.
			prior.VerificationWindow, prior.VerificationMaxWindows = 2, 5
		}
		fit, err := analyzer.assessCandidate(candidate.Reference, candidate.Query, prior)
		if err != nil {
			return "", 0, err
		}
		if fit.Status != "acoustically_supported_not_certified" {
			prior.VerificationWindow, prior.VerificationMaxWindows = 1.25, 7
			fit, err = analyzer.assessCandidate(candidate.Reference, candidate.Query, prior)
			if err != nil {
				return "", 0, err
			}
		}
		if fit.Status != "acoustically_supported_not_certified" {
			continue
		}
		// The review timing model can apply a constant shift. Leave a
		// material relative drift or a different event to manual review.
		if math.Abs(fit.Offset-offset) > .25 || math.Abs(fit.Slope)*records[candidate.Query].Duration > .025 {
			unavailable[candidate.Query] = true
			continue
		}
		byPair[fit] = candidate
		checkedEdges = append(checkedEdges, &PairEvidence{Reference: candidate.Reference, Query: candidate.Query, Selected: fit})
	}
	verified, err := analyzer.validateEdges(checkedEdges, filepath.Dir(path), "waveform-review-")
	if err != nil {
		return "", 0, err
	}

	type result struct {
		candidate ManualCandidate
		evidence  waveformReviewEvidence
		start     float64
	}
	var order []int
	choices := map[int]result{}
	for _, edge := range verified {
		c := byPair[edge.Selected]
		v := edge.Validation
		if !v.Excluded || v.Passed < 2 || v.Contradictory != 0 || v.Coverage < .6 || v.Maximum == nil || *v.Maximum > .04 {
			continue
		}
		if !c.CanAccept || !models[c.Reference].Resolved {
			unavailable[c.Query] = true
			continue
		}
		ref, query := models[c.Reference], models[c.Query]
		shift := edge.Selected.Offset + ref.AudioOrigin - query.AudioOrigin
		start := ref.Offset + ref.Rate*shift
		if len(ref.Knots) >= 2 {
			start = MapTime(ref.Knots, shift)
		}
		if previous, ok := choices[c.Query]; ok {
			if math.Abs(previous.start-start) > .05 {
				unavailable[c.Query] = true
			}
			continue
		}
		choices[c.Query] = result{c, waveformReviewEvidence{Shift: shift, Validation: v}, start}
		order = append(order, c.Query)
	}

	var content struct {
		Models []map[string]any `json:"timing_models"`
		Graph  map[string]any   `json:"graph"`
	}
	if err := json.Unmarshal(project.Payload, &content); err != nil {
		return "", 0, err
	}
	count := 0
	for _, q := range order {
		if unavailable[q] {
			continue
		}
		choice := choices[q]
		if overlapsExclusiveSource(records, models, q, choice.start) {
			continue
		}
		proofs, _ := content.Graph["automatic_waveform_matches"].(map[string]any)
		if proofs == nil {
			proofs = map[string]any{}
		}
		proofs[choice.candidate.ID] = choice.evidence
		content.Graph["automatic_waveform_matches"] = proofs
		payload, err := json.Marshal(map[string]any{"recordings": records, "timing_models": content.Models, "graph": content.Graph})
		if err != nil {
			return "", 0, err
		}
		var data NativePayload
		if err := json.Unmarshal(payload, &data); err != nil {
			return "", 0, err
		}
		content.Models, content.Graph, _, err = reviewCandidate(&NativeProject{Payload: payload, Data: data}, choice.candidate.ID, true)
		if err != nil {
			return "", 0, err
		}
		models[q] = TimeModel{Recording: q, Resolved: true, Offset: choice.start, Rate: models[choice.candidate.Reference].Rate}
		count++
	}
	if count == 0 {
		return "", 0, nil
	}
	if err := ctx.Err(); err != nil {
		return "", 0, err
	}
	return saveMatchRevision(path, project, fmt.Sprintf("Waveform review · %d placements", count), content.Models, content.Graph, count)
}

func overlapsExclusiveSource(records []MediaRecord, models map[int]TimeModel, q int, start float64) bool {
	assignment := records[q].SourceAssignment
	if assignment == nil || !assignment.Exclusive {
		return false
	}
	for i, m := range models {
		other := records[i].SourceAssignment
		if i == q || !m.Resolved || other == nil || other.Group != assignment.Group {
			continue
		}
		duration := records[i].Duration * m.Rate
		if math.Min(start+records[q].Duration, m.Offset+duration)-math.Max(start, m.Offset) > .05 {
			return true
		}
	}
	return false
}
