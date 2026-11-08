package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"opensync/internal/audio"
	"opensync/internal/projectstate"
	"opensync/internal/researchmatch"
)

type researchReport struct {
	Status          string               `json:"status"`
	Reference       string               `json:"reference"`
	Query           string               `json:"query"`
	ReferenceSource string               `json:"reference_source"`
	QuerySource     string               `json:"query_source"`
	Research        researchmatch.Result `json:"research"`
}

func researchMatchCommand(projectPath, indexPath, referenceID, queryID, hopMS, outPath string) error {
	for _, input := range []string{projectPath, indexPath} {
		if input == outPath {
			return fmt.Errorf("output would overwrite input")
		}
	}
	if referenceID == queryID {
		return fmt.Errorf("reference and query must differ")
	}
	p, err := projectstate.Load(projectPath)
	if err != nil {
		return err
	}
	refRec, ok := p.Recordings[referenceID]
	if !ok {
		return fmt.Errorf("unknown reference %q", referenceID)
	}
	queryRec, ok := p.Recordings[queryID]
	if !ok {
		return fmt.Errorf("unknown query %q", queryID)
	}
	var index map[string]probeCacheEntry
	data, err := os.ReadFile(indexPath)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &index); err != nil {
		return err
	}
	normalize := func(s string) string { return strings.ToLower(strings.ReplaceAll(s, "\\", "/")) }
	open := func(id string, source string) (*audio.Reader, error) {
		record, exists := index[id]
		if !exists || record.Cache == "" || normalize(record.Source) != normalize(source) {
			return nil, fmt.Errorf("missing cache or identity mismatch for %q", id)
		}
		return audio.Open(record.Cache)
	}
	ref, err := open(referenceID, refRec.SourcePath)
	if err != nil {
		return err
	}
	defer ref.Close()
	query, err := open(queryID, queryRec.SourcePath)
	if err != nil {
		return err
	}
	defer query.Close()
	cfg := researchmatch.Dream()
	switch hopMS {
	case "2":
		cfg.HopSamples = 32
	case "2s8":
		cfg.HopSamples = 32
		cfg.IntegrationFrames = 4
	case "8":
		cfg.HopSamples = 128
	default:
		return fmt.Errorf("supported modes: 8, 2, or 2s8 (2ms hop with 8ms integration)")
	}
	result, err := researchmatch.Match(ref, query, cfg)
	if err != nil {
		return err
	}
	report := researchReport{
		Status:    "RESEARCH_DISCOVERY_ONLY_NOT_VERIFIED",
		Reference: referenceID, Query: queryID,
		ReferenceSource: refRec.SourcePath, QuerySource: queryRec.SourcePath,
		Research: result,
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if err := exclusiveCreatePreview(outPath, encoded); err != nil {
		return err
	}
	fmt.Printf("STATUS: %s\nREFERENCE: %s\nQUERY: %s\nHOP_MS: %.1f\nINTEGRATION_MS: %.1f\nLAGS: %d\nPEAKS: %d\nCERTIFICATION: %s\nREPORT: %s\n",
		report.Status, referenceID, queryID, result.HopMS, result.IntegrationMS, result.LagsExamined, len(result.Peaks), result.Certification, outPath)
	for i, peak := range result.Peaks {
		if i >= 5 {
			break
		}
		fmt.Printf("PEAK_%d: OFFSET=%.6fs SCORE=%.5f OVERLAP=%.2fs BANDS=[%.3f,%.3f,%.3f]\n",
			i+1, peak.OffsetSeconds, peak.Score, peak.OverlapSeconds,
			peak.BandCorrelation[0], peak.BandCorrelation[1], peak.BandCorrelation[2])
	}
	return nil
}
