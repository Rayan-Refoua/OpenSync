package main

import (
	"opensync/internal/projectstate"
	"testing"
)

func TestSequencePlanDoesNotInventEdges(t *testing.T) {
	p := projectstate.New()
	for _, r := range []projectstate.Recording{
		{ID: "a", SourcePath: "/cam/C0593.MP4", DurationNS: 2_000_000_000},
		{ID: "b", SourcePath: "/cam/C0594.MP4", DurationNS: 2_000_000_000},
		{ID: "c", SourcePath: "/cam/C0595.MP4", DurationNS: 2_000_000_000},
	} {
		if err := p.AddRecording(r); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.AddHypothesis(projectstate.Hypothesis{ID: "ab", Reference: "a", Query: "b", Status: projectstate.Discovered}); err != nil {
		t.Fatal(err)
	}
	got, err := sequencePlanProject(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.NeighborPairs != 2 || got.WithDiscovery != 1 || got.MissingDiscovery != 1 {
		t.Fatalf("incorrect coverage: %+v", got)
	}
	if got.Pairs[0].Status != "discovery_available_requires_acoustic_validation" ||
		got.Pairs[1].Status != "discovery_not_found" ||
		got.Pairs[0].Continuity != "unverified" {
		t.Fatalf("wrong classification: %+v", got.Pairs)
	}
}
