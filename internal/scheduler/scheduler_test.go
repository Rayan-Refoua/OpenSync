package scheduler

import (
	"encoding/json"
	"reflect"
	"testing"

	"opensync/internal/projectstate"
)

func demoProject(t *testing.T) *projectstate.Project {
	t.Helper()
	p := projectstate.New()
	for _, rec := range []projectstate.Recording{
		{ID: "master", SourcePath: "/music/master.wav", DurationNS: 90_000_000_000},
		{ID: "take1", SourcePath: "/music/take1.wav", DurationNS: 30_000_000_000},
		{ID: "take2", SourcePath: "/music/take2.wav", DurationNS: 40_000_000_000},
		{ID: "outtake", SourcePath: "/music/outtake.wav", DurationNS: 120_000_000_000},
	} {
		if err := p.AddRecording(rec); err != nil {
			t.Fatal(err)
		}
	}
	for _, h := range []projectstate.Hypothesis{
		{ID: "m1", Reference: "master", Query: "take1", OffsetNS: 10_000_000_000, Status: projectstate.Discovered,
			Discovery: &projectstate.DiscoveryEvidence{Source: "synthetic", Votes: 10, SupportWindows: 3,
				Anchors: []projectstate.DiscoveryAnchor{{QueryStartSeconds: 1, Votes: 10}}}},
		{ID: "m2", Reference: "master", Query: "take2", OffsetNS: 20_000_000_000, Status: projectstate.Discovered,
			Discovery: &projectstate.DiscoveryEvidence{Source: "synthetic", Votes: 8, SupportWindows: 2,
				Anchors: []projectstate.DiscoveryAnchor{{QueryStartSeconds: 2, Votes: 8}}}},
		{ID: "repeat", Reference: "master", Query: "take1", OffsetNS: 35_000_000_000, Status: projectstate.Discovered,
			Discovery: &projectstate.DiscoveryEvidence{Source: "synthetic", Votes: 10, SupportWindows: 3,
				Anchors: []projectstate.DiscoveryAnchor{{QueryStartSeconds: 1, Votes: 10}}}},
		{ID: "alternate", Reference: "take1", Query: "take2", OffsetNS: 10_000_000_000, Status: projectstate.Discovered},
		{ID: "rejected", Reference: "master", Query: "outtake", OffsetNS: 1, Status: projectstate.Rejected},
	} {
		if err := p.AddHypothesis(h); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func TestAutomaticAnchorAndAlternatives(t *testing.T) {
	p := demoProject(t)
	plan, err := Build(p)
	if err != nil {
		t.Fatal(err)
	}
	if plan.SeedRecordingID != "master" {
		t.Fatalf("expected connected master anchor, got %s", plan.SeedRecordingID)
	}
	if len(plan.Anchors) != 4 || len(plan.Investigations) != 4 || plan.RejectedExcluded != 1 {
		t.Fatalf("unexpected inventory: %+v", plan)
	}
	if plan.Investigations[0].Phase != "frontier" || plan.Investigations[1].Phase != "frontier" {
		t.Fatalf("expected initial expansion: %+v", plan.Investigations)
	}
	if plan.Investigations[2].Phase != "alternatives" || plan.Investigations[3].Phase != "alternatives" {
		t.Fatalf("repeated chorus or cycle hypothesis was discarded: %+v", plan.Investigations)
	}
	if plan.Investigations[0].CompetingOptions != 2 {
		t.Fatalf("competing master/take1 offsets not reported: %+v", plan.Investigations[0])
	}
	if plan.Status != "DISCOVERY_ONLY_NOT_CERTIFIED" {
		t.Fatalf("unexpected certification: %s", plan.Status)
	}
}

func TestDeterministicAcrossSerialization(t *testing.T) {
	p := demoProject(t)
	first, err := Build(p)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var copy projectstate.Project
	if err := json.Unmarshal(b, &copy); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		another, err := Build(&copy)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(first, another) {
			t.Fatal("scheduler changed with input map order")
		}
	}
}

func TestEmptyAndInvalid(t *testing.T) {
	if _, err := Build(nil); err == nil {
		t.Fatal("nil project accepted")
	}
	if _, err := Build(projectstate.New()); err == nil {
		t.Fatal("empty project accepted")
	}
}
