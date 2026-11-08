package main

import (
	"encoding/json"
	"fmt"
	"os"

	"opensync/internal/graphdiagnostic"
	"opensync/internal/projectstate"
)

func graphAuditCommand(projectPath, output string) error {
	if projectPath == output {
		return fmt.Errorf("output path must differ from project")
	}
	p, err := projectstate.Load(projectPath)
	if err != nil {
		return err
	}
	report, err := graphdiagnostic.Audit(p)
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
	redundant, truncated := 0, 0
	for _, edge := range report.Edges {
		if len(edge.AlternativePaths) > 0 {
			redundant++
		}
		if edge.PathsTruncated {
			truncated++
		}
	}
	fmt.Printf("STATUS: %s\nRECORDINGS: %d\nCANDIDATES: %d\nCOMPONENTS: %d\nEDGES_WITH_ALTERNATE_PATHS: %d\nTRUNCATED_PATH_SETS: %d\nCERTIFICATION: %s\nREPORT: %s\n",
		report.Status, report.RecordingCount, report.CandidateCount, len(report.ConnectedComponents), redundant, truncated, report.AcousticCertification, output)
	return nil
}
