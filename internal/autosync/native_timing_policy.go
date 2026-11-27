package autosync

import "math"

// Automatic mode is conservative about both the potential savings and the
// evidence it replaces. The workload floor counts only windows eligible for
// omission, so a modest project or many short clips stay on the dense path.
const automaticTimingMinSavedWindows = 1024

func adaptiveTimingPairs(policy AnalysisSettings, records []MediaRecord, pairs []NativePriorPair, edges []*PairEvidence) []bool {
	if policy.TimingProfiles == "dense" || policy.Verification.custom() {
		return nil
	}
	holdouts := make(map[[2]int]*HoldoutReport, len(edges))
	for _, edge := range edges {
		h := edge.Validation
		if h != nil && h.Status == "pass_disjoint_windows" && h.Excluded && h.Contradictory == 0 {
			holdouts[[2]int{edge.Reference, edge.Query}] = h
		}
	}
	eligible := make([]bool, len(pairs))
	sparseWindows := sparseTimingQuotas[0] + sparseTimingQuotas[1] + sparseTimingQuotas[2]
	saved := 0
	for i, pair := range pairs {
		if len(pair.Candidates) != 1 {
			continue
		}
		prior := pair.Candidates[0]
		begin, end := timingBounds(records[pair.Reference].Duration, records[pair.Query].Duration, prior)
		overlap := end - begin
		if !sparseTimingEligible(prior, overlap) {
			continue
		}
		if policy.TimingProfiles == "auto" && !holdoutSupportsSparseTiming(holdouts[[2]int{pair.Reference, pair.Query}], prior, begin, end) {
			continue
		}
		eligible[i] = true
		saved += 4*min(129, int(overlap/10)+1) - 3 - sparseWindows
	}
	if policy.TimingProfiles == "auto" && saved < automaticTimingMinSavedWindows {
		return nil
	}
	return eligible
}

func holdoutSupportsSparseTiming(h *HoldoutReport, prior NativePrior, begin, end float64) bool {
	if h == nil {
		return false
	}
	count := 0
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, m := range h.Observations {
		if !m.Passes || m.QueryTime < begin || m.QueryTime > end {
			continue
		}
		if math.Abs(m.Offset-prior.Offset) > .008 {
			return false
		}
		count++
		lo = math.Min(lo, m.QueryTime)
		hi = math.Max(hi, m.QueryTime)
	}
	return count >= 3 && hi-lo >= .65*(end-begin)
}
