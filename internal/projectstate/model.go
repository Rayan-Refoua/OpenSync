package projectstate

import (
	"errors"
	"fmt"
)

type Status string

const (
	Discovered   Status = "discovered"
	Corroborated Status = "corroborated"
	Verified     Status = "verified"
	Rejected     Status = "rejected"
)

// Recording has a stable project-local identity.
// SourcePath is a reference; media is never modified.
type Recording struct {
	ID         string `json:"id"`
	SourcePath string `json:"source_path"`
	DurationNS int64  `json:"duration_ns"`
}

// Hypothesis represents one possible temporal relationship.
// Multiple hypotheses may exist for the same recording pair.
// OffsetNS means start(Query) - start(Reference).
type Hypothesis struct {
	ID        string             `json:"id"`
	Reference string             `json:"reference"`
	Query     string             `json:"query"`
	OffsetNS  int64              `json:"offset_ns"`
	Status    Status             `json:"status"`
	Discovery *DiscoveryEvidence `json:"discovery,omitempty"`
}

// Observation records an acoustic correspondence.
// EventID identifies evidence provenance; observations sharing
// one event must not be counted as independent verification.
type Observation struct {
	ID           string `json:"id"`
	HypothesisID string `json:"hypothesis_id"`
	EventID      string `json:"event_id"`
	Feature      string `json:"feature"`
	ReferenceNS  int64  `json:"reference_ns"`
	QueryNS      int64  `json:"query_ns"`
}

type Project struct {
	Version      int                    `json:"version"`
	Recordings   map[string]Recording   `json:"recordings"`
	Hypotheses   map[string]Hypothesis  `json:"hypotheses"`
	Observations map[string]Observation `json:"observations"`
}

var ErrInvalid = errors.New("invalid project evidence")

func New() *Project {
	return &Project{
		Version:      1,
		Recordings:   make(map[string]Recording),
		Hypotheses:   make(map[string]Hypothesis),
		Observations: make(map[string]Observation),
	}
}

func (p *Project) AddRecording(r Recording) error {
	if r.ID == "" || r.SourcePath == "" || r.DurationNS <= 0 {
		return fmt.Errorf("%w: recording", ErrInvalid)
	}
	if _, exists := p.Recordings[r.ID]; exists {
		return fmt.Errorf("%w: duplicate recording %q", ErrInvalid, r.ID)
	}
	p.Recordings[r.ID] = r
	return nil
}

func (p *Project) AddHypothesis(h Hypothesis) error {
	if h.ID == "" || h.Reference == h.Query ||
		h.Reference == "" || h.Query == "" {
		return fmt.Errorf("%w: hypothesis", ErrInvalid)
	}
	if h.Status != Discovered && h.Status != Corroborated &&
		h.Status != Verified && h.Status != Rejected {
		return fmt.Errorf("%w: hypothesis status", ErrInvalid)
	}
	if _, ok := p.Recordings[h.Reference]; !ok {
		return fmt.Errorf("%w: unknown reference", ErrInvalid)
	}
	if _, ok := p.Recordings[h.Query]; !ok {
		return fmt.Errorf("%w: unknown query", ErrInvalid)
	}
	if err := validateDiscovery(h.Discovery); err != nil {
		return err
	}
	if _, exists := p.Hypotheses[h.ID]; exists {
		return fmt.Errorf("%w: duplicate hypothesis", ErrInvalid)
	}
	p.Hypotheses[h.ID] = h
	return nil
}

func (p *Project) AddObservation(o Observation) error {
	if o.ID == "" || o.EventID == "" || o.Feature == "" ||
		o.ReferenceNS < 0 || o.QueryNS < 0 {
		return fmt.Errorf("%w: observation", ErrInvalid)
	}
	if _, ok := p.Hypotheses[o.HypothesisID]; !ok {
		return fmt.Errorf("%w: unknown hypothesis", ErrInvalid)
	}
	if _, exists := p.Observations[o.ID]; exists {
		return fmt.Errorf("%w: duplicate observation", ErrInvalid)
	}
	p.Observations[o.ID] = o
	return nil
}
