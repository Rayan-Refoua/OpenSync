package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"opensync/internal/syncgraph"
)

// solveDiagnostic exposes the core solver through the real OpenSync CLI.
// It only reports fit diagnostics; no source media, graph admission state,
// or timeline is written. Input constraints must come from an external
// verification stage; this command does not authenticate their evidence.
func solveDiagnostic(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var request struct {
		Nodes       []string               `json:"nodes"`
		Constraints []syncgraph.Constraint `json:"constraints"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return fmt.Errorf("invalid solver input: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("solver input contains trailing data or is invalid: %v", err)
	}
	if request.Nodes == nil {
		return fmt.Errorf("solver input missing nodes")
	}
	result, err := syncgraph.SolveOffsets(request.Nodes, request.Constraints)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(struct {
		Status           string             `json:"status"`
		TimelineModified bool               `json:"timeline_modified"`
		Solution         syncgraph.Solution `json:"solution"`
	}{Status: "DIAGNOSTIC_ONLY_NOT_ACOUSTICALLY_CERTIFIED", TimelineModified: false, Solution: result})
}
