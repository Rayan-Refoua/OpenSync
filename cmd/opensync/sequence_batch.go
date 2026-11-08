package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"

	"opensync/internal/projectstate"
	"opensync/internal/quality"
)

type SequentialBatch struct {
	Status           string                  `json:"status"`
	Mode             string                  `json:"mode"`
	Pairs            int                     `json:"pairs"`
	MissingDiscovery int                     `json:"missing_discovery"`
	CandidateCount   int                     `json:"candidate_count"`
	Attempted        int                     `json:"attempted"`
	Results          []SequentialBatchResult `json:"results"`
}
type SequentialBatchResult struct {
	Before       string          `json:"before"`
	After        string          `json:"after"`
	BeforeSource string          `json:"before_source"`
	AfterSource  string          `json:"after_source"`
	Status       string          `json:"status"`
	Candidates   []RecoveryEntry `json:"candidates"`
}

func sequenceCandidates(project *projectstate.Project, pair SequentialPair, mode quality.Mode) ([]projectstate.Hypothesis, error) {
	items := make([]projectstate.Hypothesis, 0, len(pair.Hypotheses))
	seen := make(map[string]bool)
	for _, id := range pair.Hypotheses {
		if seen[id] {
			return nil, fmt.Errorf("duplicate hypothesis %q", id)
		}
		seen[id] = true
		h, ok := project.Hypotheses[id]
		if !ok {
			return nil, fmt.Errorf("unknown hypothesis %q for %s/%s", id, pair.Before, pair.After)
		}
		if h.Status == projectstate.Rejected {
			return nil, fmt.Errorf("rejected hypothesis %q", id)
		}
		if !(h.Reference == pair.Before && h.Query == pair.After || h.Reference == pair.After && h.Query == pair.Before) {
			return nil, fmt.Errorf("hypothesis %q endpoints do not match pair", id)
		}
		items = append(items, h)
	}
	sort.Slice(items, func(i, j int) bool {
		a, b := items[i], items[j]
		aw, bw, av, bv := 0, 0, 0, 0
		if a.Discovery != nil {
			aw = a.Discovery.SupportWindows
			av = a.Discovery.Votes
		}
		if b.Discovery != nil {
			bw = b.Discovery.SupportWindows
			bv = b.Discovery.Votes
		}
		if aw != bw {
			return aw > bw
		}
		if av != bv {
			return av > bv
		}
		return a.ID < b.ID
	})
	limit := len(items)
	switch mode {
	case quality.Draft:
		if limit > 1 {
			limit = 1
		}
	case quality.Normal:
		if limit > 2 {
			limit = 2
		}
	}
	return items[:limit], nil
}

func sequenceBatchCommand(projectPath, indexPath, planPath, mode, outdir string) error {
	preset, err := quality.Parse(mode)
	if err != nil {
		return err
	}
	project, err := projectstate.Load(projectPath)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(planPath)
	if err != nil {
		return err
	}
	var plan SequentialReview
	if err := json.Unmarshal(raw, &plan); err != nil {
		return err
	}
	if plan.Status != "CHRONOLOGY_PROPOSED_NOT_VERIFIED" {
		return fmt.Errorf("unsupported sequence plan status %q", plan.Status)
	}
	if plan.NeighborPairs != len(plan.Pairs) {
		return fmt.Errorf("sequence plan count mismatch")
	}
	// All pair identities must agree with the immutable project snapshot.
	for _, pair := range plan.Pairs {
		before, bok := project.Recordings[pair.Before]
		after, aok := project.Recordings[pair.After]
		if !bok || !aok || before.SourcePath != pair.BeforeSource || after.SourcePath != pair.AfterSource {
			return fmt.Errorf("sequence plan source identity mismatch for %s -> %s", pair.Before, pair.After)
		}
		if _, err := sequenceCandidates(project, pair, preset.Mode); err != nil {
			return err
		}
	}
	if err := os.Mkdir(outdir, 0700); err != nil {
		return fmt.Errorf("output directory must be new: %w", err)
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	report := SequentialBatch{Status: "SEQUENTIAL_ACOUSTIC_DIAGNOSTIC_NOT_CERTIFIED", Mode: string(preset.Mode), Pairs: len(plan.Pairs), Results: make([]SequentialBatchResult, 0, len(plan.Pairs))}
	for i, pair := range plan.Pairs {
		item := SequentialBatchResult{Before: pair.Before, After: pair.After, BeforeSource: pair.BeforeSource, AfterSource: pair.AfterSource, Candidates: []RecoveryEntry{}}
		hypotheses, _ := sequenceCandidates(project, pair, preset.Mode)
		if len(hypotheses) == 0 {
			item.Status = "missing_discovery"
			report.MissingDiscovery++
		} else {
			item.Status = "measured_diagnostic"
		}
		report.CandidateCount += len(pair.Hypotheses)
		for j, h := range hypotheses {
			entry := RecoveryEntry{HypothesisID: h.ID, Reference: h.Reference, Query: h.Query, PredictedOffsetNS: h.OffsetNS, Report: fmt.Sprintf("sequence-%03d-hyp-%02d.json", i+1, j+1)}
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			command := exec.CommandContext(ctx, executable, "probe", projectPath, indexPath, h.ID, string(preset.Mode))
			var stderr bytes.Buffer
			command.Stderr = &stderr
			data, callErr := command.Output()
			cancel()
			if callErr != nil {
				entry.Outcome = "probe_failed"
				entry.Error = stderr.String()
				if entry.Error == "" {
					entry.Error = callErr.Error()
				}
				data, _ = json.Marshal(map[string]string{"error": entry.Error})
			} else if err := evidenceRecovery(data, &entry); err != nil {
				entry.Outcome = "invalid_evidence"
				entry.Error = err.Error()
			}
			if entry.Outcome == "probe_failed" || entry.Outcome == "invalid_evidence" {
				item.Status = "contains_failed_probe"
			}
			if err := os.WriteFile(filepath.Join(outdir, entry.Report), data, 0600); err != nil {
				return err
			}
			report.Attempted++
			item.Candidates = append(item.Candidates, entry)
			fmt.Fprintf(os.Stderr, "SEQUENCE %d/%d HYP %d/%d: %s\n", i+1, len(plan.Pairs), j+1, len(hypotheses), entry.Outcome)
		}
		report.Results = append(report.Results, item)
	}
	output, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	output = append(output, '\n')
	if err := os.WriteFile(filepath.Join(outdir, "sequence-batch.json"), output, 0600); err != nil {
		return err
	}
	fmt.Printf("STATUS: %s\nMODE: %s\nPAIRS: %d\nMISSING_DISCOVERY: %d\nATTEMPTED: %d\nOUTPUT: %s\n", report.Status, report.Mode, report.Pairs, report.MissingDiscovery, report.Attempted, filepath.Join(outdir, "sequence-batch.json"))
	return nil
}
