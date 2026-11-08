package main

import (
	"encoding/json"
	"fmt"
	"os"

	"opensync/internal/projectstate"
	"opensync/internal/scheduler"
)

func scheduleDiscovery(projectPath, outputPath string) error {
	project, err := projectstate.Load(projectPath)
	if err != nil {
		return fmt.Errorf("load project: %w", err)
	}
	plan, err := scheduler.Build(project)
	if err != nil {
		return fmt.Errorf("schedule discovery: %w", err)
	}
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	f, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	fmt.Println("STATUS:", plan.Status)
	fmt.Println("RECORDINGS:", len(plan.Anchors))
	fmt.Println("INVESTIGATIONS:", len(plan.Investigations))
	fmt.Println("SEED:", plan.SeedRecordingID)
	fmt.Println("OUTPUT:", outputPath)
	for i, a := range plan.Anchors {
		if i >= 5 {
			break
		}
		fmt.Printf("ANCHOR_%d: %s duration=%.1fs neighbors=%d\n",
			i+1, a.RecordingID, a.DurationSeconds, a.UniqueNeighbors)
	}
	for i, item := range plan.Investigations {
		if i >= 10 {
			break
		}
		fmt.Printf("NEXT_%d: %s phase=%s support_windows=%d alternatives_for_pair=%d\n",
			i+1, item.HypothesisID, item.Phase, item.SupportWindows, item.CompetingOptions)
	}
	return nil
}
