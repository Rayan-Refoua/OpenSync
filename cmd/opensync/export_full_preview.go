package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"opensync/internal/premierexml"
	"opensync/internal/projectstate"
)

// exportFullPreview builds a full original-media Premiere sequence from the
// existing template and one validated diagnostic measurement report.
// Every other placement remains inherited and explicitly unverified.
func exportFullPreview(templatePath, projectPath, reportPath, outputXML, outputManifest string) error {
	if outputXML == outputManifest || outputXML == templatePath ||
		outputManifest == templatePath || outputXML == projectPath ||
		outputManifest == projectPath || outputXML == reportPath ||
		outputManifest == reportPath {
		return fmt.Errorf("input/output paths must be distinct")
	}
	for _, filename := range []string{outputXML, outputManifest} {
		if _, err := os.Stat(filename); err == nil {
			return fmt.Errorf("refusing to overwrite %s", filename)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	source, err := os.ReadFile(templatePath)
	if err != nil {
		return err
	}
	p, err := projectstate.Load(projectPath)
	if err != nil {
		return err
	}
	reportData, err := os.ReadFile(reportPath)
	if err != nil {
		return err
	}
	var report measuredReport
	if err := json.Unmarshal(reportData, &report); err != nil {
		return err
	}
	h, ok := p.Hypotheses[report.HypothesisID]
	if !ok || h.Query != report.Query {
		return fmt.Errorf("unknown or inconsistent diagnostic report")
	}

	// Existing exportReport validates the individual acoustic measurements
	// and produces a collision-checked XML without changing the template.
	// Use a private temporary path, not a pre-existing output file.
	temp, err := os.CreateTemp(filepath.Dir(outputXML), ".opensync-preview-*.xml")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	if err := os.Remove(tempPath); err != nil {
		return err
	}
	defer os.Remove(tempPath)
	if err := exportReport(templatePath, projectPath, reportPath, tempPath); err != nil {
		return err
	}
	preview, err := os.ReadFile(tempPath)
	if err != nil {
		return err
	}
	manifest, err := premierexml.BuildFullPreviewManifest(source, preview, p, report.Query)
	if err != nil {
		return err
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	manifestBytes = append(manifestBytes, '\n')
	if err := exclusiveCreatePreview(outputXML, preview); err != nil {
		return err
	}
	if err := exclusiveCreatePreview(outputManifest, manifestBytes); err != nil {
		_ = os.Remove(outputXML)
		return err
	}
	fmt.Println("FULL_PREVIEW: COMPLETE")
	fmt.Println("RECORDINGS:", manifest.TotalRecordings)
	fmt.Println("MEASURED_DIAGNOSTIC:", manifest.DiagnosticPlacements)
	fmt.Println("INHERITED_UNVERIFIED:", manifest.InheritedPlacements)
	fmt.Println("PREMIERE_XML:", outputXML)
	fmt.Println("PROVENANCE:", outputManifest)
	return nil
}

func exclusiveCreatePreview(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}
