package autosync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
)

type CandidateEvidence struct {
	NativeCandidate
	Status       string         `json:"status"`
	Slope        float64        `json:"slope,omitempty"`
	Residual     float64        `json:"residual_seconds,omitempty"`
	Confidence   float64        `json:"confidence,omitempty"`
	Observations []*Measurement `json:"observations"`
	Fit          []*Measurement `json:"fit_observations,omitempty"`
	Span         float64        `json:"observed_span_seconds,omitempty"`
	Knots        []TimePoint    `json:"timing_knots,omitempty"`
}
type HoldoutReport struct {
	Reference     int            `json:"reference"`
	Query         int            `json:"query"`
	Status        string         `json:"status"`
	Observations  []*Measurement `json:"observations"`
	Passed        int            `json:"passed_windows"`
	Contradictory int            `json:"contradictory_windows"`
	Coverage      float64        `json:"supported_span_fraction"`
	Overlap       float64        `json:"overlap_seconds"`
	Maximum       *float64       `json:"max_supported_residual_seconds"`
	Excluded      bool           `json:"fit_windows_excluded"`
	Independent   bool           `json:"independent_of_discovery"`
}
type PairEvidence struct {
	Reference  int                  `json:"reference"`
	Query      int                  `json:"query"`
	Candidates []*CandidateEvidence `json:"candidates"`
	Selected   *CandidateEvidence   `json:"selected"`
	Ambiguous  bool                 `json:"ambiguous"`
	Digest     string               `json:"input_digest"`
	Provenance string               `json:"provenance"`
	Validation *HoldoutReport       `json:"validation,omitempty"`
	Profile    *NativeProfile       `json:"multilevel_profile,omitempty"`
}

func (a *NativeAnalyzer) signalSupported(m *Measurement, controls int, strongest, flux float64) bool {
	v := analysisSettings(a.ctx).Verification
	return controls >= 2 && m.Wave >= v.MinWave && m.Wave >= strongest*v.ControlRatio && m.Wave-strongest >= v.ControlMargin && (flux >= v.MinFlux || m.Wave >= .65)
}
func (a *NativeAnalyzer) controls(ref, query int, start, offset, length, radius float64, shifts []float64) (int, float64, error) {
	count, strongest := 0, 0.
	for _, shift := range shifts {
		m, err := a.waveform(ref, query, start, offset+shift, length, radius)
		if err != nil {
			return 0, 0, err
		}
		if m != nil {
			count++
			strongest = math.Max(strongest, m.Wave)
		}
	}
	if count == 0 {
		strongest = 1
	}
	return count, strongest, nil
}
func (a *NativeAnalyzer) assessCandidate(ref, query int, c NativeCandidate) (*CandidateEvidence, error) {
	result := &CandidateEvidence{NativeCandidate: c, Status: "weak_or_inconsistent", Observations: []*Measurement{}}
	begin := math.Max(0, -c.Offset) + .6
	end := math.Min(a.records[query].Duration, a.records[ref].Duration-c.Offset) - .6
	overlap := end - begin
	if overlap < .8 {
		result.Status = "insufficient_overlap"
		return result, nil
	}
	window, maxWindows, divisor := 4., 7, 3.
	if c.VerificationWindow > 0 {
		window = c.VerificationWindow
		divisor = 5
	}
	if c.VerificationMaxWindows > 0 {
		maxWindows = c.VerificationMaxWindows
	}
	length := math.Min(window, overlap/divisor)
	count := min(maxWindows, int(overlap/(length+.5)))
	if count < 2 {
		result.Status = "insufficient_distinct_regions"
		return result, nil
	}
	times := linspace(begin, end-length, count)
	observations := make([]*Measurement, len(times))
	errors := make([]error, len(times))
	a.computeTasks(len(times), func(i int) {
		if err := a.ctx.Err(); err != nil {
			errors[i] = err
			return
		}
		t := times[i]
		radius := .35
		if c.Method == "calibrated_chronology_audio_search" {
			radius = 2
		}
		if c.AnchorTime != 0 {
			radius += math.Abs(t+length/2-c.AnchorTime) * .001
		}
		prepared, err := a.prepareQuery(query, t, length)
		if err != nil {
			errors[i] = err
			return
		}
		m, err := a.waveformPrepared(ref, prepared, c.Offset, radius)
		if err != nil {
			errors[i] = err
			return
		}
		if m == nil {
			return
		}
		m.Flux, err = a.fluxPrepared(ref, prepared, m.Offset)
		if err != nil {
			errors[i] = err
			return
		}
		m.Controls, m.Control, err = a.controlsPrepared(ref, prepared, c.Offset, radius, []float64{-31, -15, 15, 31})
		if err != nil {
			errors[i] = err
			return
		}
		m.Supported = a.signalSupported(m, m.Controls, m.Control, m.Flux)
		observations[i] = m
	})
	for i, m := range observations {
		if errors[i] != nil {
			return nil, errors[i]
		}
		if m != nil {
			result.Observations = append(result.Observations, m)
		}
	}
	var good []*Measurement
	for _, m := range result.Observations {
		if m.Supported {
			good = append(good, m)
		}
	}
	required := 2
	if overlap >= 12 {
		required = 3
	}
	if len(good) < required {
		return result, nil
	}
	tmin, tmax := good[0].QueryTime, good[0].QueryTime
	var slopes []float64
	for i, m := range good {
		tmin = math.Min(tmin, m.QueryTime)
		tmax = math.Max(tmax, m.QueryTime)
		for _, n := range good[i+1:] {
			if n.QueryTime-m.QueryTime > 1 {
				slopes = append(slopes, (n.Offset-m.Offset)/(n.QueryTime-m.QueryTime))
			}
		}
	}
	slope := 0.
	if len(slopes) > 0 && tmax-tmin >= 30 {
		slope = median(slopes)
	}
	offsets := make([]float64, len(good))
	for i, m := range good {
		offsets[i] = m.Offset - slope*m.QueryTime
	}
	intercept := median(offsets)
	var keep []*Measurement
	maxResidual := 0.
	for _, m := range good {
		err := math.Abs(m.Offset - intercept - slope*m.QueryTime)
		if err <= .025 {
			keep = append(keep, m)
			maxResidual = math.Max(maxResidual, err)
		}
	}
	if len(keep) < required || math.Abs(slope) > .001 {
		return result, nil
	}
	strength := make([]float64, len(keep))
	tmin, tmax = keep[0].QueryTime, keep[0].QueryTime
	for i, m := range keep {
		strength[i] = m.Wave - m.Control
		tmin = math.Min(tmin, m.QueryTime)
		tmax = math.Max(tmax, m.QueryTime)
	}
	result.Status = "acoustically_supported_not_certified"
	result.Offset = intercept
	result.Slope = slope
	result.Residual = maxResidual
	result.Confidence = median(strength) * math.Sqrt(float64(len(keep)))
	result.Fit = keep
	result.Span = tmax - tmin
	return result, nil
}
func verificationDigest(pair NativePair, refSHA, querySHA string, settings VerificationSettings) string {
	input := map[string]any{"pair": pair, "reference": refSHA, "query": querySHA, "verifier": "native-v1"}
	if settings.custom() {
		input["verification"] = settings
	}
	encoded, _ := json.Marshal(input)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func (a *NativeAnalyzer) verify(ctx context.Context, pairs []NativePair, out, namespace string, initial ...map[[2]int]NativePair) ([]*PairEvidence, error) {
	settings := analysisSettings(a.ctx).Verification
	folder := filepath.Join(out, namespace)
	if err := workspaceMkdir(a.ctx, folder, 0755); err != nil {
		return nil, err
	}
	results := make([]*PairEvidence, len(pairs))
	errs := make([]error, len(pairs))
	jobs := make(chan int)
	var wg sync.WaitGroup
	var progressMu sync.Mutex
	completed := 0
	var reused atomic.Int64
	reportProgress := func(i int) {
		progressMu.Lock()
		defer progressMu.Unlock()
		completed++
		status := "no supported candidate"
		if results[i] != nil && results[i].Selected != nil {
			status = "pair supported; final validation pending"
		}
		observe(ctx, ProgressEvent{Kind: "pair", Stage: namespace, Completed: completed, Total: len(pairs), Reference: pairs[i].Reference, Query: pairs[i].Query, Status: status})
	}
	for w := 0; w < nativeWorkers(len(pairs)); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if ctx.Err() != nil {
					continue
				}
				pair := pairs[i]
				ref, query := a.records[pair.Reference], a.records[pair.Query]
				digest := verificationDigest(pair, ref.CacheSHA, query.CacheSHA, settings)
				path := filepath.Join(folder, ref.ID+"_"+query.ID+".json")
				if data, err := workspaceRead(a.ctx, path); err == nil {
					var cached PairEvidence
					if json.Unmarshal(data, &cached) == nil && cached.Reference == pair.Reference && cached.Query == pair.Query && (cached.Digest == digest || !settings.custom() && cached.Digest == legacyPairDigest(pair, ref.CacheSHA, query.CacheSHA)) {
						a.reusedVerificationCandidates.Add(int64(len(cached.Candidates)))
						results[i] = &cached
						reportProgress(i)
						continue
					}
				}
				result := &PairEvidence{Reference: pair.Reference, Query: pair.Query, Digest: digest, Provenance: "discovery_source_reused; no independent certification", Candidates: []*CandidateEvidence{}}
				prior := map[NativeCandidate]*CandidateEvidence{}
				if namespace == "evidence-recovery-v2" && len(initial) > 0 {
					if original, ok := initial[0][[2]int{pair.Reference, pair.Query}]; ok {
						path := filepath.Join(out, "evidence-v2", ref.ID+"_"+query.ID+".json")
						if data, err := workspaceRead(a.ctx, path); err == nil {
							var saved PairEvidence
							if json.Unmarshal(data, &saved) == nil && saved.Reference == pair.Reference &&
								saved.Query == pair.Query && len(saved.Candidates) == len(original.Candidates) &&
								saved.Digest == verificationDigest(original, ref.CacheSHA, query.CacheSHA, settings) {
								for j, candidate := range original.Candidates {
									prior[candidate] = saved.Candidates[j]
								}
							}
						}
					}
				}
				for _, c := range pair.Candidates {
					if previous := prior[c]; previous != nil {
						result.Candidates = append(result.Candidates, previous)
						reused.Add(1)
						a.reusedVerificationCandidates.Add(1)
						continue
					}
					a.verificationCandidates.Add(1)
					e, err := a.assessCandidate(pair.Reference, pair.Query, c)
					if err != nil {
						errs[i] = err
						break
					}
					result.Candidates = append(result.Candidates, e)
				}
				if errs[i] != nil {
					continue
				}
				var supported []*CandidateEvidence
				for _, c := range result.Candidates {
					if c.Status == "acoustically_supported_not_certified" {
						supported = append(supported, c)
					}
				}
				sort.SliceStable(supported, func(i, j int) bool { return supported[i].Confidence > supported[j].Confidence })
				if len(supported) > 0 {
					result.Ambiguous = len(supported) > 1 && supported[1].Confidence >= settings.AmbiguityRatio*supported[0].Confidence
					if !result.Ambiguous {
						result.Selected = supported[0]
					}
				}
				errs[i] = workspaceJSON(a.ctx, path, result)
				results[i] = result
				reportProgress(i)
			}
		}()
	}
	for i := range pairs {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	var edges []*PairEvidence
	for i, r := range results {
		if errs[i] != nil {
			return nil, errs[i]
		}
		if r.Selected != nil {
			edges = append(edges, r)
		}
	}
	if err := workspaceJSON(a.ctx, filepath.Join(out, namespace+"-edges.json"), map[string]any{"version": "opensync-autonomous-v1", "edges": edges}); err != nil {
		return nil, err
	}
	fmt.Printf("VERIFY %s: %d/%d supported pairs\n", namespace, len(edges), len(pairs))
	if count := reused.Load(); count != 0 {
		observe(ctx, ProgressEvent{Kind: "evidence", Stage: namespace, Status: fmt.Sprintf("Reused %d identical candidate checks", count)})
	}
	return edges, nil
}
func (a *NativeAnalyzer) validateEdges(edges []*PairEvidence, out, namespace string) ([]*PairEvidence, error) {
	var accepted []*PairEvidence
	reports := make([]*HoldoutReport, len(edges))
	work := func(edge *PairEvidence) (*HoldoutReport, error) {
		// Final validation receives the unchanged, already accepted edges plus
		// newly refined edges. Only the latter require fresh holdout windows.
		if (namespace == "" || namespace == "initial-") && edge.Validation != nil && edge.Validation.Status == "pass_disjoint_windows" {
			return edge.Validation, nil
		}
		ref, query := edge.Reference, edge.Query
		model := edge.Selected
		offset, rate := model.Offset, 1+model.Slope
		begin := math.Max(0, -offset/rate) + .1
		end := math.Min(a.records[query].Duration, (a.records[ref].Duration-offset)/rate) - .1
		var occupied [][2]float64
		for _, m := range model.Observations {
			occupied = append(occupied, [2]float64{math.Max(begin, m.QueryTime-m.Window/2-.1), math.Min(end, m.QueryTime+m.Window/2+.1)})
		}
		sort.Slice(occupied, func(i, j int) bool {
			if occupied[i][0] != occupied[j][0] {
				return occupied[i][0] < occupied[j][0]
			}
			return occupied[i][1] < occupied[j][1]
		})
		var gaps, regions [][2]float64
		cursor := begin
		for _, r := range occupied {
			if r[0]-cursor >= .55 {
				gaps = append(gaps, [2]float64{cursor, r[0]})
			}
			cursor = math.Max(cursor, r[1])
		}
		if end-cursor >= .55 {
			gaps = append(gaps, [2]float64{cursor, end})
		}
		for _, r := range gaps {
			if r[1]-r[0] >= 1.3 {
				mid := (r[0] + r[1]) / 2
				regions = append(regions, [2]float64{r[0], mid - .05}, [2]float64{mid + .05, r[1]})
			} else {
				regions = append(regions, r)
			}
		}
		report := &HoldoutReport{Reference: ref, Query: query, Status: "insufficient_or_contradictory_holdout", Overlap: end - begin, Excluded: true, Observations: []*Measurement{}}
		used := map[int]bool{}
		selected := []int{}
		for _, index := range linspace(0, float64(len(regions)-1), min(9, len(regions))) {
			i := int(index)
			if !used[i] {
				used[i] = true
				selected = append(selected, i)
			}
		}
		observations := make([]*Measurement, len(selected))
		errors := make([]error, len(selected))
		a.computeTasks(len(selected), func(j int) {
			if err := a.ctx.Err(); err != nil {
				errors[j] = err
				return
			}
			region := regions[selected[j]]
			length := math.Min(2, region[1]-region[0]-.1)
			start := (region[0] + region[1] - length) / 2
			expected := offset + model.Slope*(start+length/2)
			prepared, err := a.prepareQuery(query, start, length)
			if err != nil {
				errors[j] = err
				return
			}
			m, err := a.waveformPrepared(ref, prepared, expected, .08)
			if err != nil {
				errors[j] = err
				return
			}
			if m == nil {
				return
			}
			m.Controls, m.Control, err = a.controlsPrepared(ref, prepared, expected, .08, []float64{-31, -15, 15, 31})
			if err != nil {
				errors[j] = err
				return
			}
			m.Flux, err = a.fluxPrepared(ref, prepared, m.Offset)
			if err != nil {
				errors[j] = err
				return
			}
			m.AcousticallySupported = m.Distinct && a.signalSupported(m, m.Controls, m.Control, m.Flux)
			m.Expected = expected
			m.Residual = m.Offset - expected
			m.Passes = m.AcousticallySupported && math.Abs(m.Residual) <= .025
			observations[j] = m
		})
		allGood := append([]*Measurement(nil), model.Fit...)
		for i, m := range observations {
			if errors[i] != nil {
				return nil, errors[i]
			}
			if m == nil {
				continue
			}
			if m.Passes {
				report.Passed++
				allGood = append(allGood, m)
			} else if m.AcousticallySupported {
				report.Contradictory++
			}
			if m.AcousticallySupported {
				v := math.Abs(m.Residual)
				if report.Maximum == nil || v > *report.Maximum {
					report.Maximum = &v
				}
			}
			report.Observations = append(report.Observations, m)
		}
		span := 0.
		if len(allGood) > 0 {
			lo, hi := math.Inf(1), math.Inf(-1)
			for _, m := range allGood {
				lo = math.Min(lo, m.QueryTime-m.Window/2)
				hi = math.Max(hi, m.QueryTime+m.Window/2)
			}
			span = hi - lo
		}
		report.Coverage = span / math.Max(end-begin, 1e-9)
		if report.Passed >= 2 && report.Contradictory == 0 && (end-begin < 30 || report.Coverage >= .8) {
			report.Status = "pass_disjoint_windows"
		}
		return report, nil
	}
	errs := make([]error, len(edges))
	scheduleNativeHoldouts(len(a.records), nativeWorkers(len(edges)), edges, namespace == "" || namespace == "initial-", func(i int) bool {
		reports[i], errs[i] = work(edges[i])
		return errs[i] == nil && reports[i].Status == "pass_disjoint_windows"
	})
	for i, report := range reports {
		if errs[i] != nil {
			return nil, errs[i]
		}
		if report.Status == "pass_disjoint_windows" {
			copy := *edges[i]
			copy.Validation = report
			accepted = append(accepted, &copy)
		}
	}
	if err := workspaceJSON(a.ctx, filepath.Join(out, namespace+"holdout-validation.json"), map[string]any{"version": "disjoint-windows-v2-local-ambiguity", "pairs": reports, "input_edges": len(edges), "accepted_edges": len(accepted)}); err != nil {
		return nil, err
	}
	if err := workspaceJSON(a.ctx, filepath.Join(out, namespace+"validated-edges.json"), map[string]any{"version": "opensync-autonomous-v1", "edges": accepted}); err != nil {
		return nil, err
	}
	return accepted, nil
}
