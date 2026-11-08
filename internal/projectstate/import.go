package projectstate

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

// Discovery evidence is retained for investigation. Votes and anchors
// are not independent acoustic verification observations.
type DiscoveryAnchor struct {
	QueryStartSeconds float64 `json:"query_start"`
	OffsetBucket      int64   `json:"offset_bucket"`
	OffsetSeconds     float64 `json:"offset_seconds"`
	Votes             int     `json:"votes"`
}

type DiscoveryEvidence struct {
	Source         string            `json:"source"`
	Votes          int               `json:"votes"`
	SupportWindows int               `json:"support_windows"`
	Anchors        []DiscoveryAnchor `json:"anchors"`
}

type ImportSummary struct {
	Recordings   int `json:"recordings"`
	Hypotheses   int `json:"hypotheses"`
	Anchors      int `json:"anchors"`
	Observations int `json:"observations"`
}

type inventoryEntry struct {
	Source          string  `json:"source"`
	DurationSeconds float64 `json:"duration_seconds"`
}

type discoveryReport struct {
	Experiment string `json:"experiment"`
	Status     string `json:"status"`
	Results    []struct {
		QueryID    string `json:"query_id"`
		QueryName  string `json:"query_name"`
		Candidates []struct {
			ReferenceID    string            `json:"reference_id"`
			ReferenceName  string            `json:"reference_name"`
			OffsetSeconds  float64           `json:"offset_seconds"`
			Votes          int               `json:"votes"`
			SupportWindows int               `json:"support_windows"`
			Anchors        []DiscoveryAnchor `json:"anchors"`
			Status         string            `json:"status"`
		} `json:"candidates"`
	} `json:"results"`
}

func finite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

func secondsNS(v float64) (int64, error) {
	n := math.Round(v * 1e9)
	if !finite(n) || n < -math.MaxInt64 || n >= 0x1p63 {
		return 0, fmt.Errorf("%w: invalid seconds value", ErrInvalid)
	}
	return int64(n), nil
}

func mediaName(source string) string {
	source = strings.ReplaceAll(source, "\\", "/")
	i := strings.LastIndexByte(source, '/')
	return source[i+1:]
}

func validateDiscovery(e *DiscoveryEvidence) error {
	if e == nil {
		return nil
	}
	if e.Source == "" || e.Votes <= 0 ||
		e.SupportWindows <= 0 || len(e.Anchors) == 0 {
		return fmt.Errorf("%w: discovery summary", ErrInvalid)
	}
	for _, a := range e.Anchors {
		if a.Votes <= 0 || !finite(a.QueryStartSeconds) ||
			a.QueryStartSeconds < 0 || !finite(a.OffsetSeconds) {
			return fmt.Errorf("%w: discovery anchor", ErrInvalid)
		}
	}
	return nil
}

// ImportDiscovery constructs a new project from the existing research
// inventory and fingerprint report. It performs no media I/O and grants
// no acoustic certification.
func ImportDiscovery(inventoryJSON, reportJSON []byte) (*Project, ImportSummary, error) {
	var summary ImportSummary
	var inventory map[string]inventoryEntry
	if err := json.Unmarshal(inventoryJSON, &inventory); err != nil {
		return nil, summary, err
	}
	if len(inventory) == 0 {
		return nil, summary, fmt.Errorf("%w: empty inventory", ErrInvalid)
	}

	var report discoveryReport
	if err := json.Unmarshal(reportJSON, &report); err != nil {
		return nil, summary, err
	}
	if report.Experiment != "M4-005" || report.Status != "discovery_only" {
		return nil, summary, fmt.Errorf("%w: unsupported discovery report", ErrInvalid)
	}

	p := New()
	ids := make([]string, 0, len(inventory))
	for id := range inventory {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		entry := inventory[id]
		ns, err := secondsNS(entry.DurationSeconds)
		if err != nil || ns <= 0 {
			return nil, summary, fmt.Errorf("%w: recording duration %q", ErrInvalid, id)
		}
		if err := p.AddRecording(Recording{
			ID: id, SourcePath: entry.Source, DurationNS: ns,
		}); err != nil {
			return nil, summary, err
		}
	}

	seenQueries := make(map[string]bool)
	for _, result := range report.Results {
		if seenQueries[result.QueryID] {
			return nil, summary, fmt.Errorf("%w: duplicate query", ErrInvalid)
		}
		seenQueries[result.QueryID] = true

		query, found := p.Recordings[result.QueryID]
		if !found || mediaName(query.SourcePath) != result.QueryName {
			return nil, summary, fmt.Errorf("%w: query identity mismatch", ErrInvalid)
		}

		for i, candidate := range result.Candidates {
			ref, found := p.Recordings[candidate.ReferenceID]
			if !found || mediaName(ref.SourcePath) != candidate.ReferenceName {
				return nil, summary, fmt.Errorf("%w: reference identity mismatch", ErrInvalid)
			}
			if candidate.Status != "discovered_unverified" {
				return nil, summary, fmt.Errorf("%w: unexpected candidate status", ErrInvalid)
			}

			ns, err := secondsNS(candidate.OffsetSeconds)
			if err != nil {
				return nil, summary, err
			}
			evidence := &DiscoveryEvidence{
				Source:         report.Experiment,
				Votes:          candidate.Votes,
				SupportWindows: candidate.SupportWindows,
				Anchors:        append([]DiscoveryAnchor(nil), candidate.Anchors...),
			}
			if err := validateDiscovery(evidence); err != nil {
				return nil, summary, err
			}
			for _, anchor := range evidence.Anchors {
				if anchor.QueryStartSeconds*1e9 > float64(query.DurationNS)+1 {
					return nil, summary, fmt.Errorf("%w: anchor outside query", ErrInvalid)
				}
			}

			// Discovery offset: reference_local = query_local + offset.
			// Therefore start(query) - start(reference) = offset.
			h := Hypothesis{
				ID:        fmt.Sprintf("%s/%s/%d", result.QueryID, candidate.ReferenceID, i),
				Reference: candidate.ReferenceID,
				Query:     result.QueryID,
				OffsetNS:  ns,
				Status:    Discovered,
				Discovery: evidence,
			}
			if err := p.AddHypothesis(h); err != nil {
				return nil, summary, err
			}
			summary.Anchors += len(candidate.Anchors)
		}
	}

	if err := p.Validate(); err != nil {
		return nil, summary, err
	}
	summary.Recordings = len(p.Recordings)
	summary.Hypotheses = len(p.Hypotheses)
	summary.Observations = len(p.Observations)
	return p, summary, nil
}
