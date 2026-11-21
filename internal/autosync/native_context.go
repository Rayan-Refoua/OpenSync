package autosync

import (
	"math"
	"sort"
	"strings"

	"opensync/internal/researchmatch"
)

func nativeComparisonPlacements(records []MediaRecord, layout []NativeLayout, profiles []NativeProfilePair, discovery *NativeDistributedReport) []map[string]any {
	models := map[int]NativeLayout{}
	for _, m := range layout {
		models[m.Recording] = m
	}
	searches := map[[2]int]NativeDistributedPair{}
	if discovery != nil {
		for _, p := range discovery.Pairs {
			searches[[2]int{p.Reference, p.Query}] = p
		}
	}
	choices := map[int][]map[string]any{}
	for _, pair := range profiles {
		r, q := pair.Reference, pair.Query
		ref, query := models[r], models[q]
		if !strings.HasPrefix(query.Status, "unresolved") || strings.HasPrefix(ref.Status, "unresolved") {
			continue
		}
		peaks := searches[[2]int{r, q}].Peaks
		if len(peaks) == 0 {
			continue
		}
		top := peaks[0]
		if top.BackgroundZ < 5 || (len(peaks) > 1 && top.Score < 1.15*peaks[1].Score) {
			continue
		}
		for _, h := range pair.Profiles {
			if math.Abs(h.Prior.Offset-top.Offset) > .1 {
				continue
			}
			usable := func(o *Measurement) bool {
				return !o.Boundary && o.Wave >= .1 && o.Wave >= 1.5*o.Control && o.Wave-o.Control >= .035
			}
			ts := []float64{}
			for _, o := range h.Training {
				if usable(o) {
					ts = append(ts, o.Offset)
				}
			}
			if len(ts) < 3 {
				continue
			}
			offset := median(ts)
			if math.Abs(offset-top.Offset) > .12 {
				continue
			}
			matched := []*Measurement{}
			counts := map[string]int{}
			failed, excluded := false, 0
			for i, role := range [][]*Measurement{h.Training, h.Selection, h.Audit} {
				name := []string{"training", "selection", "audit"}[i]
				good, inside := 0, 0
				for _, o := range role {
					if usable(o) {
						good++
						if math.Abs(o.Offset-offset) <= .12 {
							inside++
							matched = append(matched, o)
						}
					}
					if o.Precise && math.Abs(o.Offset-offset) > .12 {
						failed = true
					}
				}
				required := 2
				if i == 0 {
					required = 3
				}
				if inside < required || inside < int(math.Ceil(.6*float64(good))) {
					failed = true
				}
				counts[name] = inside
				excluded += len(role) - inside
			}
			if failed {
				continue
			}
			lo, hi, residual := math.Inf(1), math.Inf(-1), 0.
			for _, o := range matched {
				lo = math.Min(lo, o.QueryTime-o.Window/2)
				hi = math.Max(hi, o.QueryTime+o.Window/2)
				residual = math.Max(residual, math.Abs(o.Offset-offset))
			}
			duration := records[q].Duration
			if hi-lo < .6*duration {
				continue
			}
			mid := duration / 2
			start := MapTime(ref.Knots, offset+mid) - mid
			variation := 0.
			for _, t := range []float64{0, mid, duration} {
				variation = math.Max(variation, math.Abs(MapTime(ref.Knots, offset+t)-(start+t)))
			}
			uncertainty := residual + ref.Error + variation + .01
			if uncertainty > .14 {
				continue
			}
			choices[q] = append(choices[q], map[string]any{"recording": q, "reference": r, "start_seconds": start, "rate": 1., "timing_knots": []TimePoint{{0, start}, {duration, start + duration}}, "fit_residual_seconds": uncertainty, "status": "provisional_acoustic_review_required", "matched_windows": counts, "measured_window_residual_seconds": residual, "proposed_pair_offset_seconds": offset, "discovery_offset_seconds": top.Offset, "excluded_waveform_windows": excluded, "physical_capture_alignment": "not_independently_established"})
		}
	}
	// Multiple distinctive spectral searches can suggest a useful comparison
	// placement even when waveform audits do not support a timing model.
	// Keep it separate from the solved graph and require explicit export opt-in.
	type spectralChoice struct {
		reference    int
		start, score float64
	}
	spectral := map[int][]spectralChoice{}
	if discovery != nil {
		for _, pair := range discovery.Pairs {
			r, q := models[pair.Reference], models[pair.Query]
			if len(choices[pair.Query]) > 0 || !strings.HasPrefix(q.Status, "unresolved") ||
				strings.HasPrefix(r.Status, "unresolved") || strings.HasPrefix(r.Status, "provisional") || r.ComponentSize < 2 || len(r.Knots) < 2 ||
				len(pair.Peaks) < 2 || records[pair.Query].Duration > 60 {
				continue
			}
			top := pair.Peaks[0]
			if top.BackgroundZ < 8 || top.Score < .14 || top.Score < 1.4*pair.Peaks[1].Score ||
				top.Available < 3 || top.Offset < 0 || top.Offset+records[pair.Query].Duration > records[pair.Reference].Duration {
				continue
			}
			spectral[pair.Query] = append(spectral[pair.Query], spectralChoice{pair.Reference, MapTime(r.Knots, top.Offset), top.Score})
		}
	}
	for q, options := range spectral {
		if len(options) < 2 {
			continue
		}
		lo, hi := options[0].start, options[0].start
		best := options[0]
		refs := map[string]bool{}
		for _, option := range options {
			lo, hi = math.Min(lo, option.start), math.Max(hi, option.start)
			if option.score > best.score {
				best = option
			}
			identity := records[option.reference].CacheSHA
			if identity == "" {
				identity = records[option.reference].ID
			}
			refs[identity] = true
		}
		if hi-lo > .05 || len(refs) < 2 {
			continue
		}
		choices[q] = append(choices[q], map[string]any{
			"recording": q, "reference": best.reference, "start_seconds": best.start, "rate": 1.,
			"status":                    "provisional_spectral_consensus_review_required",
			"timing_basis":              "independent_reference_spectral_consensus",
			"reference_recording_count": len(refs), "reference_start_spread_seconds": hi - lo,
			"physical_capture_alignment": "not_independently_established",
		})
	}
	// Large rooms can preserve changes in spectral energy even when the
	// microphone waveforms differ. Retain these hypotheses for review with
	// their reference's component, including components with no global anchor.
	onsets := map[int][]map[string]any{}
	if discovery != nil {
		for _, pair := range discovery.Pairs {
			r, q := models[pair.Reference], models[pair.Query]
			if len(choices[pair.Query]) != 0 || q.Status != "unresolved_no_supported_relation" || len(r.Knots) < 2 {
				continue
			}
			peak, ok := researchmatch.DistinctOnsetPeak(pair.OnsetPeaks)
			duration := records[pair.Query].Duration
			if !ok || peak.Offset < 0 || peak.Offset+duration > records[pair.Reference].Duration {
				continue
			}
			start := MapTime(r.Knots, peak.Offset+duration/2) - duration/2
			if math.Max(math.Abs(MapTime(r.Knots, peak.Offset)-start), math.Abs(MapTime(r.Knots, peak.Offset+duration)-(start+duration))) > .03 {
				continue
			}
			basis := "onset_consensus"
			if strings.HasPrefix(r.Status, "unresolved") {
				basis = "local_onset_consensus"
			}
			onsets[pair.Query] = append(onsets[pair.Query], map[string]any{
				"recording": pair.Query, "reference": pair.Reference, "component": r.Component,
				"start_seconds": start, "rate": 1., "status": "provisional_onset_review_required",
				"timing_basis": basis, "proposed_pair_offset_seconds": peak.Offset,
				"onset_section_scores": peak.Sections, "onset_score": peak.Score,
				"physical_capture_alignment": "not_independently_established",
			})
		}
	}
	for q, options := range onsets {
		if len(options) != 1 {
			continue
		}
		option := options[0]
		ref := int(number(option["reference"]))
		if len(choices[ref]) == 0 && len(onsets[ref]) == 0 {
			choices[q] = options
		}
	}
	ids := []int{}
	for q := range choices {
		ids = append(ids, q)
	}
	sort.Ints(ids)
	result := []map[string]any{}
	for _, q := range ids {
		options := choices[q]
		lo, hi := math.Inf(1), math.Inf(-1)
		for _, o := range options {
			lo = math.Min(lo, number(o["start_seconds"]))
			hi = math.Max(hi, number(o["start_seconds"]))
		}
		if hi-lo > .15 {
			continue
		}
		sort.SliceStable(options, func(i, j int) bool {
			a, b := number(options[i]["fit_residual_seconds"]), number(options[j]["fit_residual_seconds"])
			if a != b {
				return a < b
			}
			return number(options[i]["reference"]) < number(options[j]["reference"])
		})
		result = append(result, options[0])
	}
	return result
}
func nativeDelayProfile(p *NativeProfile) map[string]any {
	observations := []map[string]any{}
	byTime := map[float64]float64{}
	precise := 0
	lo, hi := math.Inf(1), math.Inf(-1)
	for i, role := range [][]*Measurement{p.Training, p.Selection, p.Audit} {
		for _, o := range role {
			if o.Supported {
				observations = append(observations, map[string]any{"query_time": o.QueryTime, "arrival_offset": o.Offset, "precise": o.Precise, "role": []string{"training", "selection", "audit"}[i]})
				lo = math.Min(lo, o.Offset)
				hi = math.Max(hi, o.Offset)
				if o.Precise {
					precise++
					byTime[o.QueryTime] = o.Offset
				}
			}
		}
	}
	sort.SliceStable(observations, func(i, j int) bool {
		a, b := number(observations[i]["query_time"]), number(observations[j]["query_time"])
		if a != b {
			return a < b
		}
		return observations[i]["role"].(string) < observations[j]["role"].(string)
	})
	result := map[string]any{"observations": observations, "precise_observations": precise, "interpretation": "insufficient_precise_timing", "physical_cause": "not_identifiable", "permits_automatic_placement": false, "permits_video_retiming": false}
	if len(observations) > 0 {
		result["observed_offset_range_seconds"] = []float64{lo, hi}
	}
	if len(byTime) < 4 {
		return result
	}
	times := []float64{}
	for t := range byTime {
		times = append(times, t)
	}
	sort.Float64s(times)
	span := times[len(times)-1] - times[0]
	if span < 2 {
		return result
	}
	var slopes, offsets []float64
	lo, hi = math.Inf(1), math.Inf(-1)
	for i, t := range times {
		lo = math.Min(lo, byTime[t])
		hi = math.Max(hi, byTime[t])
		for _, u := range times[i+1:] {
			slopes = append(slopes, (byTime[u]-byTime[t])/(u-t))
		}
	}
	slope := median(slopes)
	for _, t := range times {
		offsets = append(offsets, byTime[t]-slope*t)
	}
	intercept := median(offsets)
	error := 0.
	for _, t := range times {
		error = math.Max(error, math.Abs(byTime[t]-intercept-slope*t))
	}
	result["observed_span_seconds"] = span
	result["precise_offset_range_seconds"] = []float64{lo, hi}
	result["apparent_rate_ppm"] = slope * 1e6
	result["linear_residual_seconds"] = error
	switch {
	case hi-lo <= .025:
		result["interpretation"] = "stable_acoustic_offset"
	case error <= .025:
		result["interpretation"] = "smooth_arrival_trend_clock_or_motion"
	default:
		result["interpretation"] = "changing_paths_or_conflicting_measurements"
	}
	return result
}
func nativeRelationshipMap(records []MediaRecord, profiles []NativeProfilePair, graph map[string]any) map[string]any {
	relations := []map[string]any{}
	for _, pair := range profiles {
		hypotheses := []map[string]any{}
		for _, h := range pair.Profiles {
			status := "candidate_same_event_not_confirmed"
			if h.Status == "validated_relation" {
				status = "timing_supported"
			} else if h.Status == "corroboration_required" || h.Status == "event_supported_timing_unresolved" {
				status = "event_supported_timing_unresolved"
			}
			hypotheses = append(hypotheses, map[string]any{"candidate_offset": h.Prior.Offset, "verification_status": h.Status, "relationship_status": status, "delay_profile": nativeDelayProfile(h)})
		}
		relations = append(relations, map[string]any{"reference": pair.Reference, "query": pair.Query, "hypotheses": hypotheses})
	}
	nodes := []map[string]any{}
	for i, r := range records {
		kind := "audio_recording"
		if r.Video != nil {
			kind = "camera_recording"
		}
		nodes = append(nodes, map[string]any{"recording": i, "id": r.ID, "name": r.Name, "kind": kind})
	}
	return map[string]any{"version": "acoustic-relationships-v1", "coordinate_system": "relative_acoustic_arrival_seconds", "physical_camera_positions": "not_established", "camera_identity": "recordings_not_assumed_to_be_distinct_devices", "placement_authority": "validated_timing_graph_only", "nodes": nodes, "relations": relations, "graph_conflicts": graph["rejected_edges"]}
}
