package main

import (
	"encoding/json"
	"fmt"
	"os"

	"opensync/internal/premierexml"
)

func exportPremiere(sourcePath, planPath, outputPath string) error {
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		return fmt.Errorf("read template: %w", err)
	}

	planData, err := os.ReadFile(planPath)
	if err != nil {
		return fmt.Errorf("read plan: %w", err)
	}

	var plan premierexml.Plan
	if err := json.Unmarshal(planData, &plan); err != nil {
		return fmt.Errorf("parse plan: %w", err)
	}

	result, err := premierexml.Patch(source, plan)
	if err != nil {
		return fmt.Errorf("patch Premiere XML: %w", err)
	}

	f, err := os.OpenFile(
		outputPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600,
	)
	if err != nil {
		return err
	}

	if _, err := f.Write(result); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	fmt.Printf("EXPORT: %s\n", outputPath)
	fmt.Printf("PLACEMENTS: %d\n", len(plan.Placements))
	fmt.Printf("BYTES: %d\n", len(result))
	fmt.Println("STATUS: DIAGNOSTIC_EXPORT_NOT_CERTIFIED")
	return nil
}
