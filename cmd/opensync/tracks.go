package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"opensync/internal/projectstate"
	"opensync/internal/sourcetracks"
)

func proposeTracks(projectPath, outputPath string) error {
	project, err := projectstate.Load(projectPath)
	if err != nil {
		return fmt.Errorf("load project: %w", err)
	}

	proposal, err := sourcetracks.Build(project)
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(proposal, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	// Exclusive creation: never overwrite an earlier proposal.
	f, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}

	fmt.Printf(
		"TRACK_PROPOSAL: %s\nTRACKS: %d\nNEIGHBORS: %d\nSTATUS: %s\n",
		filepath.Clean(outputPath),
		len(proposal.Tracks),
		len(proposal.Neighbors),
		proposal.Status,
	)
	return nil
}
