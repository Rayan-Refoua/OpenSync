package main

import (
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"

	"opensync/internal/premierexml"
	"opensync/internal/projectstate"
)

// exportMeasured creates a diagnostic Premiere timeline from a selected
// hypothesis and an explicitly supplied measured offset.
//
// The offset must use the hypothesis convention:
// start(Query) - start(Reference) = measuredOffsetNS.
//
// It does not certify evidence or alter the project or original media.
func exportMeasured(
	templatePath, projectPath, hypothesisID,
	measuredOffsetText, outputPath string,
) error {
	project, err := projectstate.Load(projectPath)
	if err != nil {
		return fmt.Errorf("load project: %w", err)
	}

	h, exists := project.Hypotheses[hypothesisID]
	if !exists {
		return fmt.Errorf("unknown hypothesis %q", hypothesisID)
	}
	if h.Status == projectstate.Rejected {
		return fmt.Errorf("rejected hypothesis %q", hypothesisID)
	}

	measuredOffsetNS, err := strconv.ParseInt(measuredOffsetText, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid measured offset: %w", err)
	}

	recordingName := func(id string) string {
		source := strings.ReplaceAll(
			project.Recordings[id].SourcePath, "\\", "/",
		)
		return path.Base(source)
	}

	referenceName := recordingName(h.Reference)
	queryName := recordingName(h.Query)

	template, err := os.ReadFile(templatePath)
	if err != nil {
		return fmt.Errorf("read XML template: %w", err)
	}

	plan, err := premierexml.PairPlan(
		template, referenceName, queryName, measuredOffsetNS,
	)
	if err != nil {
		return fmt.Errorf("create placement plan: %w", err)
	}

	// Validate and generate before opening the output file.
	result, err := premierexml.Patch(template, plan)
	if err != nil {
		return fmt.Errorf("generate XML: %w", err)
	}

	// Exclusive output: no replacement of existing files.
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

	fmt.Println("STATUS: DIAGNOSTIC_EXPORT_NOT_CERTIFIED")
	fmt.Println("REFERENCE:", referenceName)
	fmt.Println("QUERY:", queryName)
	fmt.Println("MEASURED_OFFSET_NS:", measuredOffsetNS)
	fmt.Println("PLACEMENTS:", len(plan.Placements))
	fmt.Println("NEW_START_FRAME:", plan.Placements[0].NewStart)
	fmt.Println("OUTPUT:", outputPath)
	return nil
}
