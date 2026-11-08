package projectstate

import (
	"fmt"
	"sort"

	"opensync/internal/syncgraph"
)

// SolveHypotheses constructs a diagnostic timing solution from explicitly
// selected project hypotheses. It does not verify acoustic evidence,
// admit graph edges, mutate the project, or publish a timeline.
//
// All selected hypotheses receive equal weight. This is intentional:
// raw fingerprint votes are not calibrated inverse variances.
// Rejected hypotheses cannot participate in a selected solution.
//
// Offset convention:
// start(Query) - start(Reference) == OffsetNS.
func SolveHypotheses(p *Project, hypothesisIDs []string) (syncgraph.Solution, error) {
	if err := p.Validate(); err != nil {
		return syncgraph.Solution{}, err
	}

	nodes := make([]string, 0, len(p.Recordings))
	for id := range p.Recordings {
		nodes = append(nodes, id)
	}
	sort.Strings(nodes)

	seen := make(map[string]bool, len(hypothesisIDs))
	constraints := make([]syncgraph.Constraint, 0, len(hypothesisIDs))

	for _, id := range hypothesisIDs {
		if seen[id] {
			return syncgraph.Solution{}, fmt.Errorf(
				"%w: duplicate selected hypothesis %q", ErrInvalid, id)
		}
		seen[id] = true

		h, exists := p.Hypotheses[id]
		if !exists {
			return syncgraph.Solution{}, fmt.Errorf(
				"%w: unknown hypothesis %q", ErrInvalid, id)
		}
		if h.Status == Rejected {
			return syncgraph.Solution{}, fmt.Errorf(
				"%w: rejected hypothesis %q", ErrInvalid, id)
		}

		constraints = append(constraints, syncgraph.Constraint{
			Reference: h.Reference,
			Query:     h.Query,
			OffsetNS:  h.OffsetNS,
			Weight:    1,
		})
	}

	return syncgraph.SolveOffsets(nodes, constraints)
}
