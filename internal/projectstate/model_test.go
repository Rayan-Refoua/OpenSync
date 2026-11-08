package projectstate

import "testing"

func fixture(t *testing.T) *Project {
	t.Helper()
	p := New()

	for _, id := range []string{"A", "B"} {
		if err := p.AddRecording(Recording{
			ID: id, SourcePath: id + ".wav",
			DurationNS: 120_000_000_000,
		}); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func TestCompetingHypothesesPreserved(t *testing.T) {
	p := fixture(t)

	for _, h := range []Hypothesis{
		{
			ID: "h1", Reference: "A", Query: "B",
			OffsetNS: 12_000_000_000, Status: Discovered,
		},
		{
			ID: "h2", Reference: "A", Query: "B",
			OffsetNS: 45_000_000_000, Status: Rejected,
		},
	} {
		if err := p.AddHypothesis(h); err != nil {
			t.Fatal(err)
		}
	}

	if len(p.Hypotheses) != 2 {
		t.Fatal("competing hypotheses were lost")
	}

	err := p.AddObservation(Observation{
		ID: "o1", HypothesisID: "h1",
		EventID: "event-1", Feature: "landmark",
		ReferenceNS: 15_000_000_000,
		QueryNS:     3_000_000_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Observations) != 1 {
		t.Fatal("observation not retained")
	}
}

func TestInvalidReferencesRejected(t *testing.T) {
	p := fixture(t)

	if err := p.AddHypothesis(Hypothesis{
		ID: "bad", Reference: "A", Query: "missing",
		Status: Discovered,
	}); err == nil {
		t.Fatal("unknown recording accepted")
	}

	if err := p.AddObservation(Observation{
		ID: "o1", HypothesisID: "missing",
		EventID: "event", Feature: "onset",
	}); err == nil {
		t.Fatal("orphan observation accepted")
	}
}

func TestDuplicateEvidenceRejected(t *testing.T) {
	p := fixture(t)
	r := Recording{
		ID: "A", SourcePath: "different.wav",
		DurationNS: 10_000_000_000,
	}
	if err := p.AddRecording(r); err == nil {
		t.Fatal("duplicate recording overwritten")
	}
}
