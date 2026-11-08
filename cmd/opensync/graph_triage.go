package main

import (
	"encoding/json"
	"fmt"

	"opensync/internal/graphdiagnostic"
	"opensync/internal/projectstate"
)

func graphTriageCommand(projectPath, output string) error {
	if projectPath == output {
		return fmt.Errorf("output path must differ from project")
	}
	p, err := projectstate.Load(projectPath)
	if err != nil {
		return err
	}
	report, err := graphdiagnostic.Triage(p)
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
	fmt.Printf("STATUS: %s\nRECORDINGS: %d\nCANDIDATES: %d\nUNIQUE_PAIRS: %d\nREPRESENTATIVES: %d\nALTERNATIVES_RETAINED: %d\nTWO_HOP_PATHS: %d\nPAIRS_WITHOUT_PATH: %d\nCOMPONENTS: %d\nCERTIFICATION: %s\nREPORT: %s\n",
		report.Status, report.Recordings, report.InputCandidates, report.PairGroups, report.Representatives,
		report.AlternativeHypothesesRetained, report.TwoHopPathCount, report.PairsWithoutPath,
		len(report.ConnectedComponents), report.Certification, output)
	return nil
}
