package autosync

import (
	"math"
	"sort"
)

type shortPeak struct{ offset, score float64 }
type ShortWindowEvidence struct {
	Measurement      *Measurement `json:"measurement"`
	ReferenceSeconds float64      `json:"reference_seconds_searched"`
	Competitor       float64      `json:"strongest_distant_competitor"`
	PeakWidth        float64      `json:"near_peak_width_seconds"`
}

// Short recordings cannot supply twelve informative, non-overlapping regions.
// This fallback reserves two longer regions with a 40 ms guard, fits ONLY a
// constant offset on the first, and tests it on the second. Each region must
// independently beat every distant position in the reference, not just a few
// sampled negative controls. Discovery remains acoustics-only and unseeded.
func (a *NativeAnalyzer) shortPartitionProfile(ref, query int) (*NativeProfile, error) {
	d, rd := a.records[query].Duration, a.records[ref].Duration
	if d < 2 || d > 12 || rd < d+2 || rd > 3600 {
		return nil, nil
	}
	result := &NativeProfile{Prior: NativePrior{Method: "short_partition_global_waveform"}, VerificationMode: "two_guarded_regions_full_reference_competition", Status: "insufficient_short_partition_evidence", Overlap: d - .1}
	block := (d - .1) / 2
	for j := 0; j < 2; j++ {
		window, err := a.shortGlobalWindow(ref, query, .05+float64(j)*block+.02, block-.04)
		if err != nil {
			return nil, err
		}
		if window == nil {
			return result, nil
		}
		result.ShortWindows = append(result.ShortWindows, window)
	}
	first, second := result.ShortWindows[0], result.ShortWindows[1]
	for _, w := range result.ShortWindows {
		m := w.Measurement
		if !m.Supported || w.PeakWidth > .025 || roundFrame(m.Offset*16000) < 0 || roundFrame((m.Offset+d)*16000) > roundFrame(rd*16000) {
			return result, nil
		}
		m.Offset = math.Max(0, math.Min(rd-d, m.Offset))
		m.Precise = true
	}
	fit, audit := first.Measurement, second.Measurement
	model := &NativeRelationModel{Kind: "constant", Offset: fit.Offset}
	checked := judgeNative(model, []*Measurement{audit}, .025, 1)
	if !checked.Passes {
		result.Status = "inconsistent_short_partition_offsets"
		return result, nil
	}
	result.Prior.Offset = fit.Offset
	result.Training = []*Measurement{fit}
	result.Audit = []*Measurement{audit}
	result.Model = model
	result.AuditResult = &checked
	result.Confidence = math.Min(fit.Wave-fit.Control, audit.Wave-audit.Control)
	result.Range = []float64{fit.QueryTime - fit.Window/2, audit.QueryTime + audit.Window/2}
	result.Supported = 2
	result.Limit = .025
	result.Status = "validated_relation"
	return result, nil
}

// Search every valid lag at all eight PCM phases. Two-millisecond bins retain their actual strongest
// sample lag; they bound peak bookkeeping without changing the winning sample.
// The 100 ms event neighborhood is NOT the allowed timing residual.
func (a *NativeAnalyzer) shortGlobalWindow(ref, query int, start, length float64) (*ShortWindowEvidence, error) {
	duration := a.records[ref].Duration
	start = float64(roundFrame(start*16000)) / 16000
	length = float64(roundFrame(length*16000)) / 16000
	positions := int(math.Floor((duration-length)*2000)) + 1
	if positions <= 0 {
		return nil, nil
	}
	const tile = 120000
	count := 8 * ((positions + tile - 1) / tile)
	peaks := make([][]shortPeak, count)
	errors := make([]error, count)
	a.computeTasks(count, func(job int) {
		if err := a.ctx.Err(); err != nil {
			errors[job] = err
			return
		}
		first := (job / 8) * tile
		phase := job % 8
		last := min(positions, first+tile)
		p, err := a.prepareQuery(query, start, length)
		if err != nil {
			errors[job] = err
			return
		}
		if p == nil || p.energy < 1e-10 {
			return
		}
		samples, err := a.read(ref, float64(first)/2000, float64(last-first-1)/2000+length)
		if err != nil {
			errors[job] = err
			return
		}
		r := prepareWave(samples[phase:])
		n := len(p.wave)
		if len(r) < n {
			return
		}
		// Each job owns its mutable FFT preparation.
		dots, err := p.correlation.Dots(r)
		if err != nil {
			errors[job] = err
			return
		}
		sum, squares := 0., 0.
		for _, v := range r[:n] {
			sum += v
			squares += v * v
		}
		for i := 0; i <= len(r)-n; i++ {
			if i > 0 {
				old, new := r[i-1], r[i+n-1]
				sum += new - old
				squares += new*new - old*old
			}
			variance := squares - sum*sum/float64(n)
			score := 0.
			if variance > 1e-10 {
				score = math.Min(1, math.Abs(dots[i+n-1])/math.Sqrt(variance*p.energy))
			}
			v := shortPeak{float64(first+i)/2000 + float64(phase)/16000 - start, score}
			if i%4 == 0 {
				peaks[job] = append(peaks[job], v)
			} else if v.score > peaks[job][len(peaks[job])-1].score {
				peaks[job][len(peaks[job])-1] = v
			}
		}
		sort.Slice(peaks[job], func(i, j int) bool {
			x, y := peaks[job][i], peaks[job][j]
			if x.score != y.score {
				return x.score > y.score
			}
			return x.offset < y.offset
		})
		// At most 101 two-ms bins fit in the excluded +/-100 ms neighborhood.
		// 128 therefore retains the strongest distant competitor in every job.
		peaks[job] = append([]shortPeak(nil), peaks[job][:min(128, len(peaks[job]))]...)
	})
	best := shortPeak{score: -1}
	for i, ps := range peaks {
		if errors[i] != nil {
			return nil, errors[i]
		}
		for _, p := range ps {
			if p.score > best.score {
				best = p
			}
		}
	}
	if best.score <= 0 {
		return nil, nil
	}
	competitor, lo, hi := 0., best.offset, best.offset
	for _, ps := range peaks {
		for _, p := range ps {
			if math.Abs(p.offset-best.offset) > .1 {
				competitor = math.Max(competitor, p.score)
			} else if p.score >= .9*best.score {
				lo = math.Min(lo, p.offset)
				hi = math.Max(hi, p.offset)
			}
		}
	}
	prepared, err := a.prepareQuery(query, start, length)
	if err != nil {
		return nil, err
	}
	controls := max(0, int((duration-length-.2)/length))
	// No lower local peak can pass the existing waveform gates if the global
	// maximum fails them. Avoid expensive feature checks for these negatives.
	if controls < 2 || best.score < .1 || best.score < 1.5*competitor || best.score-competitor < .035 {
		return &ShortWindowEvidence{&Measurement{QueryTime: start + length/2, Window: length, Offset: best.offset, Wave: best.score, Controls: controls, Control: competitor, Competing: competitor}, duration, competitor, hi - lo}, nil
	}
	near := []shortPeak{}
	for _, ps := range peaks {
		for _, p := range ps {
			if math.Abs(p.offset-best.offset) <= .025 {
				near = append(near, p)
			}
		}
	}
	sort.Slice(near, func(i, j int) bool {
		if near[i].score != near[j].score {
			return near[i].score > near[j].score
		}
		return near[i].offset < near[j].offset
	})
	var m *Measurement
	var tried []float64
	for _, p := range near {
		duplicate := false
		for _, offset := range tried {
			if math.Abs(p.offset-offset) < .002 {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		tried = append(tried, p.offset)
		flux, err := a.fluxPrepared(ref, prepared, p.offset)
		if err != nil {
			return nil, err
		}
		candidate := &Measurement{QueryTime: start + length/2, Window: length, Offset: p.offset, Wave: p.score, Flux: flux, Controls: controls, Control: competitor, Competing: competitor, Distinct: p.score >= 1.5*competitor && p.score-competitor >= .035}
		candidate.Supported = a.signalSupported(candidate, controls, competitor, flux)
		if m == nil || candidate.Supported {
			m = candidate
		}
		if candidate.Supported || len(tried) >= 16 {
			break
		}
	}
	return &ShortWindowEvidence{m, duration, competitor, hi - lo}, nil
}
