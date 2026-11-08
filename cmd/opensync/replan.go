package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"opensync/internal/projectstate"
	"opensync/internal/scheduler"
)

func replanDiscovery(projectPath, feedbackPath, outputPath string) error {
	p, err := projectstate.Load(projectPath)
	if err != nil {
		return fmt.Errorf("load project: %w", err)
	}
	data, err := os.ReadFile(feedbackPath)
	if err != nil {
		return fmt.Errorf("read feedback: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var f scheduler.Feedback
	if err := dec.Decode(&f); err != nil {
		return fmt.Errorf("parse feedback: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("trailing or malformed feedback")
	}
	plan, err := scheduler.Replan(p, f)
	if err != nil {
		return err
	}
	result, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	result = append(result, '\n')
	output, err := os.OpenFile(outputPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err := output.Write(result); err != nil {
		output.Close()
		return err
	}
	if err := output.Sync(); err != nil {
		output.Close()
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	fmt.Println("STATUS:", plan.Status)
	fmt.Println("SEED:", plan.SeedRecordingID)
	fmt.Println("VERIFIED_RECORDINGS:", len(plan.VerifiedRecordings))
	fmt.Println("PENDING:", plan.Pending)
	fmt.Println("DEFERRED:", len(plan.Deferred))
	if plan.Next != nil {
		fmt.Println("NEXT_HYPOTHESIS:", plan.Next.HypothesisID)
		fmt.Println("NEXT_REASON:", plan.Next.Reason)
	}
	fmt.Println("OUTPUT:", outputPath)
	return nil
}
