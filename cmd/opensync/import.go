package main

import (
	"encoding/json"
	"fmt"
	"os"

	"opensync/internal/projectstate"
)

// importDiscovery imports research evidence into a persistent project.
// It does not access media or admit timeline relationships.
func importDiscovery(inventoryPath, reportPath, outputPath string) error {
	if _, err := os.Stat(outputPath); err == nil {
		return fmt.Errorf("output already exists: %s", outputPath)
	} else if !os.IsNotExist(err) {
		return err
	}

	inventory, err := os.ReadFile(inventoryPath)
	if err != nil {
		return err
	}
	report, err := os.ReadFile(reportPath)
	if err != nil {
		return err
	}

	project, summary, err := projectstate.ImportDiscovery(inventory, report)
	if err != nil {
		return err
	}
	if err := projectstate.Save(outputPath, project); err != nil {
		return err
	}

	result := struct {
		Status           string                     `json:"status"`
		Project          string                     `json:"project"`
		Import           projectstate.ImportSummary `json:"import"`
		CertifiedEdges   int                        `json:"certified_edges"`
		TimelineModified bool                       `json:"timeline_modified"`
	}{
		Status:           "DISCOVERY_IMPORTED_NOT_CERTIFIED",
		Project:          outputPath,
		Import:           summary,
		CertifiedEdges:   0,
		TimelineModified: false,
	}

	return json.NewEncoder(os.Stdout).Encode(result)
}
