package syncgraph

import "testing"

func TestCandidateCannotCreateTimelineEdge(t *testing.T) {
	g := New()
	e := Edge{
		Reference: "cameraA",
		Query:     "cameraB",
		Status:    Discovered,
	}

	if err := g.Admit(e); err == nil {
		t.Fatal("discovered candidate entered timeline")
	}
	if g.Count() != 0 {
		t.Fatal("timeline was modified")
	}
}

func TestVerifiedEdgeRequiresCertification(t *testing.T) {
	g := New()
	e := Edge{
		Reference: "cameraA",
		Query:     "cameraB",
		Status:    Verified,
		Evidence: Evidence{
			AnchorCount:     6,
			SpanSeconds:     220,
			HoldoutCount:    6,
			FeatureFamilies: 3,
		},
	}
	if err := g.Admit(e); err == nil {
		t.Fatal("uncertified timing entered timeline")
	}

	e.Evidence.TimingCertified = true
	e.Evidence.ConflictFree = true

	if err := g.Admit(e); err != nil {
		t.Fatal(err)
	}
	if g.Count() != 1 {
		t.Fatal("expected exactly one accepted edge")
	}
}
