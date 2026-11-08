package main

import (
	"encoding/json"
	"fmt"
	"os"

	"opensync/internal/premierexml"
	"opensync/internal/projectstate"
)

// This command accepts explicit externally reviewed placements only.
// It never turns a diagnostic probe into independently certified evidence.
func exportBaselineFallback(baselinePath, projectPath, requestPath, outputXML, outputManifest string) error {
	if outputXML == outputManifest {
		return fmt.Errorf("output paths must be different")
	}
	for _, input := range []string{baselinePath, projectPath, requestPath} {
		if input == outputXML || input == outputManifest {
			return fmt.Errorf("output would overwrite input %q", input)
		}
	}
	source, err := os.ReadFile(baselinePath)
	if err != nil {
		return err
	}
	p, err := projectstate.Load(projectPath)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(requestPath)
	if err != nil {
		return err
	}
	var request premierexml.FallbackRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		return err
	}
	result, manifest, err := premierexml.ExportBaselineFallback(source, p, request)
	if err != nil {
		return err
	}
	provenance, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	provenance = append(provenance, '\n')
	if err := exclusiveCreatePreview(outputXML, result); err != nil {
		return err
	}
	if err := exclusiveCreatePreview(outputManifest, provenance); err != nil {
		_ = os.Remove(outputXML)
		return err
	}
	fmt.Println("STATUS:", manifest.Status)
	fmt.Println("TOTAL_RECORDINGS:", manifest.Recordings)
	fmt.Println("PLURALEYES_INHERITED:", manifest.InheritedPlacements)
	fmt.Println("EXTERNALLY_REVIEWED:", manifest.ExternallyApprovedPlacements)
	fmt.Println("LOST_BASELINE_OVERLAPS:", len(manifest.Coverage.LostOverlapPairs))
	fmt.Println("ACOUSTIC_CERTIFICATION:", manifest.AcousticCertification)
	fmt.Println("XML:", outputXML)
	fmt.Println("MANIFEST:", outputManifest)
	return nil
}
