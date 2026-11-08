package main

import (
	"fmt"
	"path/filepath"

	"opensync/internal/syntheticbench"
)

func researchSyntheticCommand(outputDir string) error {
	cases, err := syntheticbench.Generate(outputDir)
	if err != nil {
		return err
	}
	report, err := syntheticbench.Evaluate(outputDir, cases)
	if err != nil {
		return err
	}
	fmt.Printf("STATUS: %s\nSCENARIOS: %d\nRUNS: %d\nCERTIFICATION: %s\nREPORT: %s\n", report.Status, len(report.Cases), len(report.Runs), report.Certification, filepath.Join(outputDir, "results.json"))
	for _, run := range report.Runs {
		fmt.Printf("%-19s HOP=%dms %-42s RANK=%d ERROR_MS=%.3f TOP=%.4f OFFSET=%.3fs\n", run.Scenario, run.HopMS, run.Status, run.Rank, run.NearestErrorMS, run.TopScore, run.TopOffsetSeconds)
	}
	return nil
}
