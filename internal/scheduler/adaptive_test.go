package scheduler

import (
	"reflect"
	"testing"

	"opensync/internal/projectstate"
)

func TestReplanDefersFailureAndRetainsAlternatives(t *testing.T) {
	p := demoProject(t)
	first, err := Replan(p, Feedback{Version: 1})
	if err != nil {
		t.Fatal(err)
	}
	if first.Next == nil || first.SeedRecordingID != "master" {
		t.Fatalf("missing first action: %+v", first)
	}
	revised, err := Replan(p, Feedback{Version: 1, Attempts: []Attempt{{
		HypothesisID: first.Next.HypothesisID, Outcome: "probe_failed", ReportPath: "local/report.json",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if revised.Next == nil || revised.Next.HypothesisID == first.Next.HypothesisID {
		t.Fatalf("failed probe repeatedly selected: %+v", revised)
	}
	if len(revised.Deferred) != 1 || revised.Deferred[0].ReportPath != "local/report.json" {
		t.Fatalf("failure provenance was dropped: %+v", revised)
	}
	if revised.Pending != 3 {
		t.Fatalf("other hypotheses prematurely discarded: %+v", revised)
	}
}

func TestReplanSwitchesAnchorWhenExhausted(t *testing.T) {
	p := demoProject(t)
	feedback := Feedback{Version: 1, Attempts: []Attempt{
		{HypothesisID: "m1", Outcome: "measured_inconclusive"},
		{HypothesisID: "m2", Outcome: "probe_failed"},
		{HypothesisID: "repeat", Outcome: "measured_inconclusive"},
	}}
	next, err := Replan(p, feedback)
	if err != nil {
		t.Fatal(err)
	}
	if next.Next == nil || next.Next.HypothesisID != "alternate" {
		t.Fatalf("expected another anchor to uncover take1/take2: %+v", next)
	}
	feedback.Attempts = append(feedback.Attempts, Attempt{
		HypothesisID: "alternate", Outcome: "probe_failed",
	})
	done, err := Replan(p, feedback)
	if err != nil {
		t.Fatal(err)
	}
	if done.Next != nil || done.Status != "NO_UNTRIED_DISCOVERY_HYPOTHESES" {
		t.Fatalf("unexpected unexplored connection: %+v", done)
	}
	if len(done.Deferred) != 4 {
		t.Fatal("inconclusive results were dropped")
	}
}

func TestReplanUsesOnlyExplicitVerifiedProjectEdges(t *testing.T) {
	p := demoProject(t)
	h := p.Hypotheses["m1"]
	h.Status = projectstate.Verified
	p.Hypotheses["m1"] = h
	next, err := Replan(p, Feedback{Version: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(next.VerifiedRecordings, []string{"master", "take1"}) {
		t.Fatalf("discovery was treated as verified: %+v", next.VerifiedRecordings)
	}
	if next.Next == nil {
		t.Fatal("lost remaining hypotheses")
	}
	// A diagnostic report is never a way to confer Verified status.
	if _, err := Replan(p, Feedback{Version: 1, Attempts: []Attempt{{
		HypothesisID: "m2", Outcome: "verified",
	}}}); err == nil {
		t.Fatal("feedback independently certified a discovered hypothesis")
	}
}

func TestReplanFailsClosed(t *testing.T) {
	p := demoProject(t)
	for _, f := range []Feedback{
		{Version: 0},
		{Version: 2},
		{Version: 1, Attempts: []Attempt{{HypothesisID: "unknown", Outcome: "probe_failed"}}},
		{Version: 1, Attempts: []Attempt{{HypothesisID: "rejected", Outcome: "probe_failed"}}},
		{Version: 1, Attempts: []Attempt{{HypothesisID: "m1", Outcome: "nonsense"}}},
		{Version: 1, Attempts: []Attempt{
			{HypothesisID: "m1", Outcome: "probe_failed"},
			{HypothesisID: "m1", Outcome: "probe_failed"},
		}},
	} {
		if _, err := Replan(p, f); err == nil {
			t.Fatalf("invalid feedback accepted: %+v", f)
		}
	}
}
