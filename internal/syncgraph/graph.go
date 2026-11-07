package syncgraph

import (
	"errors"
	"fmt"
)

type Status string

const (
	Discovered   Status = "discovered"
	Corroborated Status = "corroborated"
	Verified     Status = "verified"
)

type Evidence struct {
	AnchorCount     int
	SpanSeconds     float64
	HoldoutCount    int
	FeatureFamilies int

	// Certification is provided by the verification engine,
	// not by fingerprint discovery or a correlation score.
	TimingCertified bool
	ConflictFree    bool
}

type Edge struct {
	Reference string
	Query     string
	Status    Status
	OffsetNS  int64
	DriftPPM  float64
	Evidence  Evidence
}

type Graph struct {
	accepted map[string]Edge
}

func New() *Graph {
	return &Graph{accepted: make(map[string]Edge)}
}

var ErrNotVerified = errors.New("timeline admission denied: edge not verified")

func key(a, b string) string {
	return fmt.Sprintf("%d:%s|%d:%s", len(a), a, len(b), b)
}

// Admit is intentionally fail-closed. No discovered candidate
// can create a synchronized timeline relationship.
func (g *Graph) Admit(edge Edge) error {
	if edge.Reference == "" || edge.Query == "" ||
		edge.Reference == edge.Query ||
		edge.Status != Verified ||
		!edge.Evidence.TimingCertified ||
		!edge.Evidence.ConflictFree ||
		edge.Evidence.AnchorCount < 2 ||
		edge.Evidence.HoldoutCount < 1 ||
		edge.Evidence.FeatureFamilies < 2 ||
		edge.Evidence.SpanSeconds <= 0 {
		return ErrNotVerified
	}

	k := key(edge.Reference, edge.Query)
	if _, exists := g.accepted[k]; exists {
		return fmt.Errorf("edge already admitted: %s", k)
	}
	g.accepted[k] = edge
	return nil
}

func (g *Graph) Count() int {
	return len(g.accepted)
}
