package autosync

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"strings"

	"opensync/internal/researchmatch"
)

type RetryOptions struct {
	Clips     []int `json:"clips"`
	Reference *int  `json:"reference"`
	Broader   bool  `json:"broader"`
}

// RetryRecordings adds proposals to a review revision. It never writes timing
// changes. The ordinary candidate acceptance path applies any later decision.
func RetryRecordings(ctx context.Context, path string, options RetryOptions) (string, int, error) {
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
	for _, model := range project.Data.Models {
		models[model.Recording] = model
	}
	var references []int
	for i, r := range records {
		if (options.Reference != nil && *options.Reference != i) || reviewPlacementOpen(models[i]) || strings.HasPrefix(models[i].Status, "provisional_") || r.Audio == nil || blocked[i] {
			continue
		}
		if err := checkSource(r); err != nil {
			if options.Reference != nil {
				return "", 0, err
			}
			continue
		}
		references = append(references, i)
	}
	selected := make(map[int]bool, len(options.Clips))
	var pairs []NativePair
	for _, q := range options.Clips {
		if q < 0 || q >= len(records) || blocked[q] || records[q].Audio == nil || !reviewPlacementOpen(models[q]) {
			return "", 0, fmt.Errorf("select unresolved recordings with audio and no recording rejection")
		}
		if selected[q] {
			continue
		}
		selected[q] = true
		if err := checkSource(records[q]); err != nil {
			return "", 0, err
		}
		for _, r := range references {
			if rejected[orderedReviewPair(records[r].ID, records[q].ID)] {
				continue
			}
			pairs = append(pairs, NativePair{Reference: r, Query: q})
		}
	}
	if len(pairs) == 0 {
		return "", 0, fmt.Errorf("no eligible placed audio references for the selected recordings")
	}
	workspace := &nativeWorkspace{data: map[string][]byte{}, limit: 512 << 20, audio: newDirectAudio()}
	defer workspace.audio.Close()
	ctx = context.WithValue(ctx, workspaceKey{}, workspace)
	analyzer := NewNativeAnalyzer(records)
	defer analyzer.Close()
	analyzer.ctx = ctx
	out := filepath.Dir(path)
	featurePaths := map[int]string{}
	for i := range pairs {
		if err := ctx.Err(); err != nil {
			return "", 0, err
		}
		pair := &pairs[i]
		r, q := pair.Reference, pair.Query
		observe(ctx, ProgressEvent{Kind: "pair", Stage: "Retry discovery", Completed: i, Total: len(pairs), Reference: r, Query: q, Status: "Searching"})
		ref, err := analyzer.envelopeFeatures(r, out)
		if err != nil {
			return "", 0, err
		}
		query, err := analyzer.envelopeFeatures(q, out)
		if err != nil {
			return "", 0, err
		}
		if records[r].Duration >= records[q].Duration {
			pair.Candidates, err = envelopeCandidatesScheduled(ctx, ref, query, analyzer.computeTasks)
		} else {
			pair.Candidates, err = envelopeCandidatesScheduled(ctx, query, ref, analyzer.computeTasks)
			for i := range pair.Candidates {
				c := &pair.Candidates[i]
				c.AnchorTime += c.Offset
				c.Offset = -c.Offset
			}
		}
		if err != nil {
			return "", 0, err
		}
		if options.Broader && records[q].Duration <= 295 {
			for _, index := range []int{r, q} {
				if featurePaths[index] != "" {
					continue
				}
				target := filepath.Join(out, fmt.Sprintf("retry-%d.features", index))
				if err := distributedFeaturesForRecord(ctx, records[index], target); err != nil {
					return "", 0, err
				}
				featurePaths[index] = target
			}
			result, err := retryDistributed(ctx, featurePaths[r], featurePaths[q], analyzer.computeTasks)
			if err != nil {
				return "", 0, err
			}
			for _, peak := range result.Peaks {
				pair.Candidates = append(pair.Candidates, NativeCandidate{Offset: peak.Offset, Method: "retry_distributed"})
			}
		}
	}

	var content struct {
		Models []map[string]any `json:"timing_models"`
		Graph  map[string]any   `json:"graph"`
	}
	if err := json.Unmarshal(project.Payload, &content); err != nil {
		return "", 0, err
	}
	hypotheses, _ := content.Graph["unresolved_timing_hypotheses"].([]any)
	existing := ManualCandidates(project.Data)
	added := 0
	for i, pair := range pairs {
		observe(ctx, ProgressEvent{Kind: "pair", Stage: "Retry verification", Completed: i, Total: len(pairs), Reference: pair.Reference, Query: pair.Query, Status: "Verifying"})
		var proposals []map[string]any
		var attempted []float64
		for _, candidate := range pair.Candidates {
			duplicate := false
			for _, offset := range attempted {
				duplicate = duplicate || math.Abs(offset-candidate.Offset) < .02
			}
			if duplicate {
				continue
			}
			attempted = append(attempted, candidate.Offset)
			c, err := analyzer.assessCandidate(pair.Reference, pair.Query, candidate)
			if err != nil {
				return "", 0, err
			}
			if c.Status != "acoustically_supported_not_certified" {
				continue
			}
			delta := c.Offset + models[pair.Reference].AudioOrigin - models[pair.Query].AudioOrigin
			for _, prior := range existing {
				if prior.Reference == pair.Reference && prior.Query == pair.Query && math.Abs(prior.Shift-delta) < .02 {
					duplicate = true
					break
				}
			}
			if duplicate {
				continue
			}
			existing = append(existing, ManualCandidate{Reference: pair.Reference, Query: pair.Query, Shift: delta})
			proposals = append(proposals, map[string]any{"prior": c.NativeCandidate, "status": "retry_acoustic_candidate", "evidence": c})
		}
		if len(proposals) > 0 {
			hypotheses = append(hypotheses, map[string]any{"reference": pair.Reference, "query": pair.Query, "hypotheses": proposals})
			added += len(proposals)
		}
	}
	if added == 0 {
		return "", 0, nil
	}
	if err := ctx.Err(); err != nil {
		return "", 0, err
	}
	content.Graph["unresolved_timing_hypotheses"] = hypotheses
	return saveMatchRevision(path, project, fmt.Sprintf("Retry recordings · %d proposals", added), content.Models, content.Graph, added)
}

func saveMatchRevision(path string, project *NativeProject, label string, models []map[string]any, graph map[string]any, count int) (string, int, error) {
	basePath := path
	if revision, ok := project.Data.Graph["solution_revision"].(map[string]any); ok {
		basePath = stringValue(revision["base_project"])
	}
	base, err := LoadNativeProject(basePath)
	if err != nil {
		return "", 0, err
	}
	revision, err := writeSolutionRevision(path, basePath, base.Hash, label, models, graph)
	return revision, count, err
}

func retryDistributed(ctx context.Context, refPath, queryPath string, schedule func(int, func(int))) (researchmatch.DistributedResult, error) {
	ref, err := openDistributedWorkspace(ctx, refPath)
	if err != nil {
		return researchmatch.DistributedResult{}, err
	}
	defer ref.Close()
	query, err := openDistributedWorkspace(ctx, queryPath)
	if err != nil {
		return researchmatch.DistributedResult{}, err
	}
	defer query.Close()
	return researchmatch.SearchDistributedScheduled(ref, query, -1, -1, schedule)
}
