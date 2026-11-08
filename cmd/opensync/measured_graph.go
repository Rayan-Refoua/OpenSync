package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"sort"

	"opensync/internal/measuredgraph"
	"opensync/internal/projectstate"
)

type MeasuredGraphManifest struct {
	Version int `json:"version"`
	Reports []struct {
		Training string `json:"training"`
		Holdout  string `json:"holdout"`
	} `json:"reports"`
}

func readJSONStrict(path string, dest any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	var rest any
	if err := dec.Decode(&rest); err != io.EOF {
		return fmt.Errorf("%s: trailing JSON", path)
	}
	return nil
}
func candidateFromReports(training TrainingReport, hold HoldoutReport, source string) (measuredgraph.Candidate, error) {
	if training.HypothesisID != hold.HypothesisID || training.QualityMode != hold.Mode ||
		hold.Status != "HELDOUT_DIAGNOSTIC_NOT_GRAPH_CERTIFIED" ||
		hold.GraphCertification != "not_checked" {
		return measuredgraph.Candidate{}, fmt.Errorf("inconsistent report provenance %q", source)
	}
	if hold.Model != "constant" && hold.Model != "affine" {
		return measuredgraph.Candidate{}, fmt.Errorf("unexpected timing model")
	}
	if hold.HoldoutsTested != len(hold.Windows) || hold.ValidHoldouts != len(hold.Windows) ||
		hold.HoldoutsRequested == 0 || hold.ValidHoldouts < 2 ||
		hold.HoldoutsWithoutControls > 0 || hold.HoldoutFailures > 0 ||
		hold.HoldoutsAvailable < hold.HoldoutsRequested {
		return measuredgraph.Candidate{}, fmt.Errorf("insufficient verified holdout measurement coverage for %q", source)
	}
	if training.Schedule != "preallocated_disjoint_v1" ||
		hold.ValidationAllocation != "preallocated_disjoint_v1" ||
		len(training.ReservedHoldoutQuerySeconds) != len(hold.Windows) {
		return measuredgraph.Candidate{}, fmt.Errorf("unproven independent reservation for %q", source)
	}
	if hold.EffectiveWindowNS <= 0 {
		return measuredgraph.Candidate{}, fmt.Errorf("invalid acoustic window")
	}
	events := make([]measuredgraph.Event, 0, len(hold.Windows))
	window := hold.EffectiveWindowNS
	for i, w := range hold.Windows {
		if w.Error != "" || w.ValidControls <= 0 || w.ValidControls < w.ControlWins {
			return measuredgraph.Candidate{}, fmt.Errorf("holdout measurement/control missing for %q", source)
		}
		t := int64(math.Round(training.ReservedHoldoutQuerySeconds[i] * 1e9))
		if math.Abs(float64(t-w.QueryStartNS)) > 1e6 {
			return measuredgraph.Candidate{}, fmt.Errorf("holdout reservation position mismatch")
		}
		if w.MeasuredOffsetNS == 0 && w.PredictedOffsetNS != 0 {
			return measuredgraph.Candidate{}, fmt.Errorf("missing measured offset")
		}
		for _, attempt := range training.Probes {
			a := int64(math.Round(attempt.QuerySeconds * 1e9))
			if w.QueryStartNS < a+window+500_000_000 && a < w.QueryStartNS+window+500_000_000 {
				return measuredgraph.Candidate{}, fmt.Errorf("heldout event overlaps attempted training")
			}
		}
		ev := measuredgraph.Event{QueryStartNS: w.QueryStartNS, MeasuredOffsetNS: w.MeasuredOffsetNS,
			WindowNS: window, ValidControls: w.ValidControls, ControlWins: w.ControlWins}
		if w.Spectral != nil {
			ev.BandCorrelation = append([]float64(nil), w.Spectral.BandCorrelation[:]...)
		}
		events = append(events, ev)
	}
	sort.Slice(events, func(i, j int) bool { return events[i].QueryStartNS < events[j].QueryStartNS })
	return measuredgraph.Candidate{HypothesisID: training.HypothesisID,
		Reference: training.Reference, Query: training.Query,
		Model: hold.Model, SlopeNSPerSecond: hold.ModelSlopeNSPerSecond,
		Events: events, SourceReport: source}, nil
}
func measuredGraphCommand(projectPath, manifestPath, output string) error {
	if projectPath == output || manifestPath == output {
		return fmt.Errorf("output would overwrite input")
	}
	project, err := projectstate.Load(projectPath)
	if err != nil {
		return err
	}
	var manifest MeasuredGraphManifest
	if err := readJSONStrict(manifestPath, &manifest); err != nil {
		return err
	}
	if manifest.Version != 1 || len(manifest.Reports) == 0 {
		return fmt.Errorf("invalid measured-graph manifest")
	}
	candidates := make([]measuredgraph.Candidate, 0, len(manifest.Reports))
	for _, entry := range manifest.Reports {
		if entry.Training == "" || entry.Holdout == "" {
			return fmt.Errorf("missing report file path")
		}
		var train TrainingReport
		if data, err := os.ReadFile(entry.Training); err != nil {
			return err
		} else if err := json.Unmarshal(data, &train); err != nil {
			return fmt.Errorf("training report %q: %w", entry.Training, err)
		}
		var hold HoldoutReport
		if err := readJSONStrict(entry.Holdout, &hold); err != nil {
			return err
		}
		c, err := candidateFromReports(train, hold, entry.Holdout)
		if err != nil {
			return err
		}
		candidates = append(candidates, c)
	}
	report, err := measuredgraph.Analyze(project, candidates)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := exclusiveCreatePreview(output, data); err != nil {
		return err
	}
	paths := 0
	for _, edge := range report.MeasuredEdges {
		paths += len(edge.Paths)
	}
	fmt.Printf("STATUS: %s\nCANDIDATES: %d\nCONSTANT_EDGES: %d\nDEFERRED_NONCONSTANT: %d\nMEASURED_TWO_HOP_PATHS: %d\nCERTIFICATION: %s\nEXPORT_AUTHORIZATION: %s\nREPORT: %s\n",
		report.Status, report.CandidateCount, report.ConstantCandidates, report.DeferredNonConstant, paths,
		report.AcousticCertification, report.ExportAuthorization, output)
	return nil
}
