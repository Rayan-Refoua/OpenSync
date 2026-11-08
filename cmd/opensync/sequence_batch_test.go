package main

import (
	"opensync/internal/projectstate"
	"opensync/internal/quality"
	"testing"
)

func TestSequentialBatchPresetBreadthAndValidation(t *testing.T) {
	p := projectstate.New()
	for _, r := range []projectstate.Recording{
		{ID: "a", SourcePath: "/cam/C0593.MP4", DurationNS: 1_000_000_000},
		{ID: "b", SourcePath: "/cam/C0594.MP4", DurationNS: 1_000_000_000},
		{ID: "c", SourcePath: "/cam/C0595.MP4", DurationNS: 1_000_000_000},
	} {
		if err := p.AddRecording(r); err != nil {
			t.Fatal(err)
		}
	}
	for _, h := range []projectstate.Hypothesis{
		{ID: "aa", Reference: "a", Query: "b", Status: projectstate.Discovered},
		{ID: "bb", Reference: "a", Query: "b", Status: projectstate.Discovered},
		{ID: "cc", Reference: "b", Query: "a", Status: projectstate.Discovered},
		{ID: "dd", Reference: "c", Query: "b", Status: projectstate.Discovered},
	} {
		if err := p.AddHypothesis(h); err != nil {
			t.Fatal(err)
		}
	}
	pair := SequentialPair{Before: "a", After: "b", Hypotheses: []string{"aa", "bb", "cc"}}
	modes := []struct {
		name  quality.Mode
		count int
	}{{quality.Draft, 1}, {quality.Normal, 2}, {quality.RockSolid, 3}}
	for _, m := range modes {
		got, err := sequenceCandidates(p, pair, m.name)
		if err != nil || len(got) != m.count {
			t.Fatalf("%s: got %v error %v", m.name, got, err)
		}
	}
	wrong := SequentialPair{Before: "a", After: "b", Hypotheses: []string{"dd"}}
	if _, err := sequenceCandidates(p, wrong, quality.RockSolid); err == nil {
		t.Fatal("invalid pair identity accepted")
	}
	duplicate := SequentialPair{Before: "a", After: "b", Hypotheses: []string{"aa", "aa"}}
	if _, err := sequenceCandidates(p, duplicate, quality.RockSolid); err == nil {
		t.Fatal("duplicate hypothesis accepted")
	}
}
