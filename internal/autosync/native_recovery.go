package autosync

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"opensync/internal/researchmatch"
	"path/filepath"
	"slices"
	"sort"
	"sync"
)

type envelopeCache struct {
	Digest   string       `json:"digest"`
	Version  string       `json:"version"`
	Features [3][]float32 `json:"features"`
}

// Only the explicitly enabled disk cache retains pair checkpoints. Empty
// candidates mean this exact full-range search finished without a proposal.
func (a *NativeAnalyzer) cachedEnvelopePair(ctx context.Context, out string, pair NativePair, features [][3][]float32) ([]NativeCandidate, error) {
	search := func() ([]NativeCandidate, error) {
		return envelopeCandidatesScheduled(ctx, features[pair.Reference], features[pair.Query], a.computeTasks)
	}
	if memoryWorkspace(a.ctx) != nil {
		return search()
	}
	ref, query := a.records[pair.Reference], a.records[pair.Query]
	if ref.CacheSHA == "" || query.CacheSHA == "" {
		return search()
	}
	input, _ := json.Marshal([]any{"envelope-pair-v1", ref.ID, ref.CacheSHA, ref.Duration, query.ID, query.CacheSHA, query.Duration})
	path := filepath.Join(out, "recovery-pairs-v1", shaBytes(input)+".json")
	var saved struct {
		Complete   bool              `json:"complete"`
		Candidates []NativeCandidate `json:"candidates"`
	}
	if data, err := workspaceRead(a.ctx, path); err == nil && json.Unmarshal(data, &saved) == nil && saved.Complete {
		return saved.Candidates, nil
	}
	candidates, err := search()
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	saved.Complete, saved.Candidates = true, candidates
	return candidates, workspaceJSON(a.ctx, path, saved)
}

func (a *NativeAnalyzer) envelopeFeatures(record int, out string) ([3][]float32, error) {
	var empty [3][]float32
	r := a.records[record]
	path := filepath.Join(out, "features-native", r.ID+"-envelope-v1.json")
	if data, err := workspaceRead(a.ctx, path); err == nil {
		var c envelopeCache
		if json.Unmarshal(data, &c) == nil && c.Digest == r.CacheSHA && c.Version == "envelope-native-v1" {
			return c.Features, nil
		}
	}
	old := filepath.Join(out, "features", r.ID+"-envelope-v1.npz")
	if workspaceExists(a.ctx, old) {
		return legacyEnvelope(old, r.CacheSHA)
	}
	var bands [3][]float32
	ranges := [][2]int{{7, 29}, {29, 77}, {77, 193}}
	total := roundFrame(r.Duration * 16000)
	chunkCount := int((total + 256*3750 - 1) / (256 * 3750))
	chunks := make([][3][]float32, chunkCount)
	errors := make([]error, chunkCount)
	a.computeTasks(chunkCount, func(index int) {
		first := int64(index) * 256 * 3750
		count := min(int64(256*3750+256), total-first)
		if count < 512 {
			return
		}
		samples, err := a.read(record, float64(first)/16000, float64(count)/16000)
		if err != nil {
			errors[index] = err
			return
		}
		buffer := make([]complex128, 512)
		frames := 1 + (len(samples)-512)/256
		for t := 0; t < frames; t++ {
			for i := range buffer {
				buffer[i] = complex(float64(samples[t*256+i]*fluxWindow[i]), 0)
			}
			fluxPlan.Transform(buffer)
			for b, band := range ranges {
				sum := float32(0)
				for f := band[0]; f < band[1]; f++ {
					re, im := float32(real(buffer[f])/256), float32(imag(buffer[f])/256)
					mag := float32(math.Hypot(float64(re), float64(im)))
					sum += mag * mag
				}
				mean := sum / float32(band[1]-band[0])
				chunks[index][b] = append(chunks[index][b], float32(math.Log(float64(float32(1e-10)+mean))))
			}
		}
	})
	for i, c := range chunks {
		if errors[i] != nil {
			return empty, errors[i]
		}
		for b := range bands {
			bands[b] = append(bands[b], c[b]...)
		}
	}
	var features [3][]float32
	weights := make([]float64, 9)
	sum := 0.
	for i := range weights {
		x := float64(i - 4)
		weights[i] = math.Exp(-.5 * x * x)
		sum += weights[i]
	}
	for i := range weights {
		weights[i] /= sum
	}
	for b, band := range bands {
		if len(band) < 2 {
			features[b] = []float32{}
			continue
		}
		smoothed := make([]float32, len(band))
		for t := range band {
			v := 0.
			for k, w := range weights {
				i := t + k - 4
				for i < 0 || i >= len(band) {
					if i < 0 {
						i = -i - 1
					}
					if i >= len(band) {
						i = 2*len(band) - i - 1
					}
				}
				v += float64(band[i]) * w
			}
			smoothed[t] = float32(v)
		}
		features[b] = make([]float32, len(band)-1)
		for i := range features[b] {
			features[b][i] = smoothed[i+1] - smoothed[i]
		}
	}
	if err := workspaceMkdir(a.ctx, filepath.Dir(path), 0755); err != nil {
		return empty, err
	}
	if err := workspaceJSON(a.ctx, path, envelopeCache{r.CacheSHA, "envelope-native-v1", features}); err != nil {
		return empty, err
	}
	return features, nil
}
func envelopeCandidates(ctx context.Context, reference, query [3][]float32) ([]NativeCandidate, error) {
	return envelopeCandidatesScheduled(ctx, reference, query, func(n int, work func(int)) {
		for i := 0; i < n; i++ {
			work(i)
		}
	})
}
func envelopeCandidatesScheduled(ctx context.Context, reference, query [3][]float32, schedule func(int, func(int))) ([]NativeCandidate, error) {
	width := min(len(query[0]), 1250)
	if width < 50 || len(reference[0]) < width {
		return nil, nil
	}
	starts := []int{}
	for _, v := range linspace(0, float64(len(query[0])-width), 3) {
		i := int(v)
		if len(starts) == 0 || starts[len(starts)-1] != i {
			starts = append(starts, i)
		}
	}
	var candidates []NativeCandidate
	for _, first := range starts {
		var q [3][]float64
		var energy [3]float64
		informative := 0
		for b := range q {
			q[b] = make([]float64, width)
			mean := 0.
			for i := range q[b] {
				q[b][i] = float64(query[b][first+i])
				mean += q[b][i]
			}
			mean /= float64(width)
			for i := range q[b] {
				q[b][i] -= mean
				energy[b] += q[b][i] * q[b][i]
			}
			if energy[b] > 1e-8 {
				informative++
			}
		}
		if informative < 2 {
			continue
		}
		positions := len(reference[0]) - width + 1
		score := make([]float64, positions)
		// Bounded reference tiles avoid per-worker transforms proportional to a
		// multi-hour recording. Every valid lag is still searched exactly once.
		tiles := (positions + 32767) / 32768
		curves := make([][3][]float64, tiles)
		errors := make([]error, tiles*3)
		schedule(tiles*3, func(job int) {
			if err := ctx.Err(); err != nil {
				errors[job] = err
				return
			}
			tile, b := job/3, job%3
			begin := tile * 32768
			end := min(positions, begin+32768)
			r := make([]float64, end-begin+width-1)
			for i := range r {
				r[i] = float64(reference[b][begin+i])
			}
			dots, err := researchmatch.CorrelationDots(r, q[b])
			if err != nil {
				errors[job] = err
				return
			}
			curves[tile][b] = make([]float64, end-begin)
			sum, squares := 0., 0.
			for _, x := range r[:width] {
				sum += x
				squares += x * x
			}
			for i := range curves[tile][b] {
				if i > 0 {
					old, new := r[i-1], r[i+width-1]
					sum += new - old
					squares += new*new - old*old
				}
				variance := math.Max(0, squares-sum*sum/float64(width))
				curves[tile][b][i] = dots[i+width-1] / math.Sqrt(math.Max(variance*energy[b], 1e-20))
			}
		})
		for _, err := range errors {
			if err != nil {
				return nil, err
			}
		}
		for tile, bands := range curves {
			begin := tile * 32768
			end := min(positions, begin+32768)
			for i := begin; i < end; i++ {
				a, b, c := bands[0][i-begin], bands[1][i-begin], bands[2][i-begin]
				score[i] = a + b + c - math.Min(a, math.Min(b, c)) - math.Max(a, math.Max(b, c))
			}
			curves[tile] = [3][]float64{}
		}
		for peak := 0; peak < 4; peak++ {
			best := 0
			for i, v := range score {
				if v > score[best] {
					best = i
				}
			}
			strength := score[best]
			if strength < .12 {
				break
			}
			candidates = append(candidates, NativeCandidate{Offset: float64(best-first) * .016, EnvelopeScore: strength, AnchorTime: (float64(first) + float64(width)/2) * .016, Method: "multiband_envelope_full_range"})
			for i := max(0, best-47); i < min(len(score), best+48); i++ {
				score[i] = -1
			}
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].EnvelopeScore != candidates[j].EnvelopeScore {
			return candidates[i].EnvelopeScore > candidates[j].EnvelopeScore
		}
		return candidates[i].Offset < candidates[j].Offset
	})
	picked := []NativeCandidate{}
	for _, c := range candidates {
		distinct := true
		for _, p := range picked {
			if math.Abs(c.Offset-p.Offset) < .5 {
				distinct = false
				break
			}
		}
		if distinct {
			picked = append(picked, c)
		}
		if len(picked) == 8 {
			break
		}
	}
	return picked, nil
}
func (a *NativeAnalyzer) recover(ctx context.Context, edges []*PairEvidence, out string) ([]NativePair, error) {
	path := filepath.Join(out, "recovery-discovery.json")
	settings := analysisSettings(a.ctx).Verification

	adjacency := make([][]int, len(a.records))
	for _, e := range edges {
		adjacency[e.Reference] = append(adjacency[e.Reference], e.Query)
		adjacency[e.Query] = append(adjacency[e.Query], e.Reference)
	}
	seen := make([]bool, len(a.records))
	var groups [][]int
	for i := range a.records {
		if seen[i] {
			continue
		}
		seen[i] = true
		g := []int{i}
		for k := 0; k < len(g); k++ {
			for _, other := range adjacency[g[k]] {
				if !seen[other] {
					seen[other] = true
					g = append(g, other)
				}
			}
		}
		sort.Ints(g)
		groups = append(groups, g)
	}
	orderGroups(groups, a.records)
	targets := map[int]bool{}
	for _, g := range groups[1:] {
		for _, i := range g {
			targets[i] = true
		}
	}
	targetIDs := []int{}
	for i := range targets {
		targetIDs = append(targetIDs, i)
	}
	sort.Ints(targetIDs)
	if data, err := workspaceRead(a.ctx, path); err == nil {
		var saved struct {
			Pairs        []NativePair
			Targets      []int `json:"target_recordings"`
			Verification VerificationSettings
		}
		if err = json.Unmarshal(data, &saved); err != nil {
			return nil, err
		}
		_ = saved.Verification.normalize()
		if saved.Verification == settings && slices.Equal(saved.Targets, targetIDs) {
			return saved.Pairs, nil
		}
	}
	features := make([][3][]float32, len(a.records))
	errs := make([]error, len(a.records))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < nativeWorkers(len(a.records)); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if len(targets) > 0 && ctx.Err() == nil {
					features[i], errs[i] = a.envelopeFeatures(i, out)
				}
			}
		}()
	}
	for i := range a.records {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	pairs := []NativePair{}
	for q, query := range a.records {
		for r, ref := range a.records {
			if (!targets[q] && !targets[r]) || ref.Duration < query.Duration || (ref.Duration == query.Duration && r <= q) {
				continue
			}
			pairs = append(pairs, NativePair{Reference: r, Query: q})
		}
	}
	if len(pairs) > 0 && memoryWorkspace(a.ctx) == nil {
		if err := workspaceMkdir(a.ctx, filepath.Join(out, "recovery-pairs-v1"), 0755); err != nil {
			return nil, err
		}
	}
	errs = make([]error, len(pairs))
	jobs = make(chan int)
	for w := 0; w < min(8, nativeWorkers(len(pairs))); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				p := &pairs[i]
				p.Candidates, errs[i] = a.cachedEnvelopePair(ctx, out, *p, features)
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
	selected := []NativePair{}
	for i, p := range pairs {
		if errs[i] != nil {
			return nil, errs[i]
		}
		if len(p.Candidates) > 0 {
			selected = append(selected, p)
		}
	}
	if err := workspaceJSON(a.ctx, path, map[string]any{"version": "envelope-recovery-v1", "verification": settings, "target_recordings": targetIDs, "pairs": selected, "searched_pairs": len(pairs), "searched_without_candidates": len(pairs) - len(selected), "baseline_used": false}); err != nil {
		return nil, err
	}
	fmt.Printf("RECOVERY: %d full-range pairs\n", len(selected))
	return selected, nil
}
