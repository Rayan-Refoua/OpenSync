package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"opensync/internal/projectstate"
	"opensync/internal/scheduler"
)

// investigateNext runs exactly one candidate. The outcome is diagnostic,
// never a certification or mutation of project evidence. Output paths must
// be new; "-": empty initial feedback.
func investigateNext(projectPath, inventoryPath, feedbackPath, reportPath, nextFeedbackPath string) error {
	if reportPath == nextFeedbackPath || reportPath == feedbackPath ||
		nextFeedbackPath == projectPath || nextFeedbackPath == inventoryPath ||
		reportPath == projectPath || reportPath == inventoryPath {
		return fmt.Errorf("input and output paths must be distinct")
	}
	p, err := projectstate.Load(projectPath)
	if err != nil {
		return err
	}
	feedback := scheduler.Feedback{Version: 1, Attempts: []scheduler.Attempt{}}
	if feedbackPath != "-" {
		data, err := os.ReadFile(feedbackPath)
		if err != nil {
			return err
		}
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&feedback); err != nil {
			return err
		}
		var extra any
		if err := dec.Decode(&extra); err != io.EOF {
			return fmt.Errorf("unexpected trailing feedback data")
		}
		if nextFeedbackPath == feedbackPath {
			return fmt.Errorf("feedback cannot be overwritten")
		}
	}

	plan, err := scheduler.Replan(p, feedback)
	if err != nil {
		return err
	}
	if plan.Next == nil {
		return fmt.Errorf("no untried acoustic discovery hypotheses")
	}
	for _, filename := range []string{reportPath, nextFeedbackPath} {
		if _, err := os.Stat(filename); err == nil {
			return fmt.Errorf("refusing to overwrite %s", filename)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}

	var report bytes.Buffer
	outcome := "measured_inconclusive"
	probeError := probeHypothesisTo(projectPath, inventoryPath, plan.Next.HypothesisID, &report)
	if probeError != nil {
		outcome = "probe_failed"
		report.Reset()
		// No large raw source content or credentials are included in errors.
		if err := json.NewEncoder(&report).Encode(map[string]string{
			"status": "PROBE_FAILED", "hypothesis_id": plan.Next.HypothesisID,
			"error": probeError.Error(),
		}); err != nil {
			return err
		}
	}
	feedback.Attempts = append(feedback.Attempts, scheduler.Attempt{
		HypothesisID: plan.Next.HypothesisID,
		Outcome: outcome, ReportPath: reportPath,
	})
	if _, err := scheduler.Replan(p, feedback); err != nil {
		return fmt.Errorf("invalid post-probe feedback: %w", err)
	}
	nextData, err := json.MarshalIndent(feedback, "", "  ")
	if err != nil {
		return err
	}
	nextData = append(nextData, '\n')

	// Exclusive output creation and rollback of the *new* report if the
	// feedback file cannot be committed. No existing file is overwritten.
	if err := exclusiveWrite(reportPath, report.Bytes()); err != nil {
		return err
	}
	if err := exclusiveWrite(nextFeedbackPath, nextData); err != nil {
		_ = os.Remove(reportPath)
		return err
	}
	fmt.Println("INVESTIGATED:", plan.Next.HypothesisID)
	fmt.Println("OUTCOME:", outcome)
	fmt.Println("REPORT:", reportPath)
	fmt.Println("NEXT_FEEDBACK:", nextFeedbackPath)
	if probeError != nil {
		fmt.Println("PROBE_ERROR:", probeError)
	}
	fmt.Println("CERTIFICATION: UNCHANGED")
	return nil
}

func exclusiveWrite(path string, content []byte) (err error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	success := false
	defer func() {
		_ = f.Close()
		if !success {
			_ = os.Remove(path)
		}
	}()
	if _, err := f.Write(content); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	success = true
	return nil
}
