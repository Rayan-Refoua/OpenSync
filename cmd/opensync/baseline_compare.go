package main

import (
	"encoding/json"
	"fmt"
	"os"

	"opensync/internal/premierexml"
	"opensync/internal/projectstate"
)

func baselineCompareCommand(baselinePath, candidatePath, projectPath, outputPath string) error {
	for _, input := range []string{baselinePath, candidatePath, projectPath} {
		if input == outputPath {
			return fmt.Errorf("output cannot overwrite input")
		}
	}
	baseline, err := os.ReadFile(baselinePath)
	if err != nil {
		return err
	}
	candidate, err := os.ReadFile(candidatePath)
	if err != nil {
		return err
	}
	p, err := projectstate.Load(projectPath)
	if err != nil {
		return err
	}
	result, err := premierexml.CompareBaselineCoverage(baseline, candidate, p)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := exclusiveCreatePreview(outputPath, data); err != nil {
		return err
	}
	fmt.Println("STATUS:", result.Status)
	fmt.Println("BASELINE_RECORDINGS:", result.BaselineRecordings)
	fmt.Println("CANDIDATE_RECORDINGS:", result.CandidateRecordings)
	fmt.Println("BASELINE_OVERLAP_PAIRS:", result.BaselineOverlapPairs)
	fmt.Println("RETAINED_OVERLAP_PAIRS:", result.RetainedOverlapPairs)
	fmt.Println("NEW_OVERLAP_PAIRS:", len(result.NewOverlapPairs))
	fmt.Println("LOST_OVERLAP_PAIRS:", len(result.LostOverlapPairs))
	fmt.Println("CHANGED_VIDEO_PLACEMENTS:", result.ChangedVideoPlacements)
	fmt.Println("ACOUSTIC_CERTIFICATION:", result.AcousticCertification)
	fmt.Println("REPORT:", outputPath)
	return nil
}
