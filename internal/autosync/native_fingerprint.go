package autosync

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"opensync/internal/audio"
	"opensync/internal/dsp"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sort"
	"sync"
	"time"
)

type Fingerprint [2]int64
type FingerprintMetrics struct {
	STFT, MaxFilter, Percentile, Peaks, Pairing float64
	Chunks                                      int
	TiedPeaks                                   int
}
type fingerprintCache struct {
	Version     string             `json:"version"`
	Digest      string             `json:"digest"`
	Hashes      []Fingerprint      `json:"hashes"`
	Metrics     FingerprintMetrics `json:"metrics"`
	Alternative []Fingerprint      `json:"alternative,omitempty"`
}

const fingerprintVersion = "native-fingerprint-v2"

var fpPlan, _ = dsp.NewFFTPlan(1024)
var fpWindow = func() []float32 {
	w := make([]float32, 1024)
	for i := range w {
		w[i] = float32(.5 - .5*math.Cos(2*math.Pi*float64(i)/1024))
	}
	return w
}()

func selectFloat(a []float32, k int) float32 {
	lo, hi := 0, len(a)-1
	for lo < hi {
		pivot := a[(lo+hi)/2]
		i, j := lo, hi
		for i <= j {
			for a[i] < pivot {
				i++
			}
			for a[j] > pivot {
				j--
			}
			if i <= j {
				a[i], a[j] = a[j], a[i]
				i++
				j--
			}
		}
		if k <= j {
			hi = j
		} else if k >= i {
			lo = i
		} else {
			break
		}
	}
	return a[k]
}
func maxLine(src, dst []float32, stride, count, radius int, queue []int) {
	head, tail, next := 0, 0, 0
	for i := 0; i < count; i++ {
		right := min(count-1, i+radius)
		for next <= right {
			v := src[next*stride]
			for tail > head && src[queue[tail-1]*stride] <= v {
				tail--
			}
			queue[tail] = next
			tail++
			next++
		}
		for tail > head && queue[head] < i-radius {
			head++
		}
		dst[i*stride] = src[queue[head]*stride]
	}
}
func fingerprintSpectrum(samples []int16) []float32 {
	frames := 1 + (len(samples)-1024)/128
	mag := make([]float32, 513*frames)
	buffer := make([]complex128, 1024)
	for t := 0; t < frames; t++ {
		for k := range buffer {
			buffer[k] = complex(float64(float32(samples[t*128+k])/32768*fpWindow[k]), 0)
		}
		fpPlan.Transform(buffer)
		for f := 8; f < 385; f++ {
			re := float32(real(buffer[f]) / 512)
			im := float32(imag(buffer[f]) / 512)
			absolute := float32(math.Hypot(float64(re), float64(im)))
			mag[f*frames+t] = float32(math.Log1p(float64(float32(200 * absolute))))
		}
	}
	return mag
}

// A float32 plateau can be a rounding artefact rather than two acoustic
// peaks. Resolve those rare ties from the original PCM at higher precision;
// otherwise a duplicate can displace a different peak from the quota.
func fingerprintPreciseMagnitude(samples []int16, frame, frequency int) float64 {
	var re, im float64
	for i := 0; i < 1024; i++ {
		v := float64(samples[frame*128+i]) / 32768 * (.5 - .5*math.Cos(2*math.Pi*float64(i)/1024))
		angle := -2 * math.Pi * float64(frequency*i) / 1024
		s, c := math.Sincos(angle)
		re += v * c
		im += v * s
	}
	return re*re + im*im
}
func FingerprintSamples(samples []int16, startSeconds float64) ([]Fingerprint, FingerprintMetrics) {
	h, _, m := fingerprintVariants(samples, startSeconds)
	return h, m
}
func fingerprintVariants(samples []int16, startSeconds float64) ([]Fingerprint, []Fingerprint, FingerprintMetrics) {
	if len(samples) < 1024 {
		return []Fingerprint{}, nil, FingerprintMetrics{Chunks: 1}
	}
	begin := time.Now()
	mag := fingerprintSpectrum(samples)
	stft := time.Since(begin).Seconds()
	h, m := fingerprintFromSpectrum(samples, startSeconds, mag, false)
	m.STFT = stft
	var alternative []Fingerprint
	if m.TiedPeaks > 0 {
		alternative, _ = fingerprintFromSpectrum(samples, startSeconds, mag, true)
	}
	return h, alternative, m
}
func fingerprintFromSpectrum(samples []int16, startSeconds float64, mag []float32, resolveTies bool) ([]Fingerprint, FingerprintMetrics) {
	metrics := FingerprintMetrics{Chunks: 1}
	if len(samples) < 1024 {
		return []Fingerprint{}, metrics
	}
	frames := 1 + (len(samples)-1024)/128
	begin := time.Now()
	temp := make([]float32, len(mag))
	maximum := make([]float32, len(mag))
	queue := make([]int, max(frames, 513))
	for f := 8; f < 385; f++ {
		maxLine(mag[f*frames:], temp[f*frames:], 1, frames, 5, queue)
	}
	for t := 0; t < frames; t++ {
		maxLine(temp[8*frames+t:], maximum[8*frames+t:], frames, 377, 4, queue)
	}
	metrics.MaxFilter = time.Since(begin).Seconds()
	begin = time.Now()
	values := append([]float32(nil), mag[8*frames:385*frames]...)
	position := .97 * float64(len(values)-1)
	left := int(math.Floor(position))
	a := selectFloat(values, left)
	threshold := float64(a)
	if left+1 < len(values) {
		b := selectFloat(values, left+1)
		threshold += float64(b-a) * (position - float64(left))
	}
	metrics.Percentile = time.Since(begin).Seconds()
	begin = time.Now()
	type peak struct {
		t, f     int
		strength float32
	}
	var peaks []peak
	precise := map[int]float64{}
	resolve := func(t, f int) float64 {
		key := f*frames + t
		if v, ok := precise[key]; ok {
			return v
		}
		v := fingerprintPreciseMagnitude(samples, t, f)
		precise[key] = v
		return v
	}
	for f := 8; f < 385; f++ {
		for t := 0; t < frames; t++ {
			index := f*frames + t
			if mag[index] == maximum[index] && float64(mag[index]) > threshold {
				dominated := false
				for ff := max(8, f-4); ff <= min(384, f+4) && !dominated; ff++ {
					for tt := max(0, t-5); tt <= min(frames-1, t+5); tt++ {
						if (ff != f || tt != t) && mag[ff*frames+tt] == mag[index] {
							metrics.TiedPeaks++
							if resolveTies && resolve(tt, ff) > resolve(t, f) {
								dominated = true
								break
							}
						}
					}
				}
				if dominated {
					continue
				}
				peaks = append(peaks, peak{t, f, mag[index]})
			}
		}
	}
	sort.SliceStable(peaks, func(i, j int) bool { return peaks[i].strength > peaks[j].strength })
	seconds := make(map[int]int)
	selected := peaks[:0]
	for _, p := range peaks {
		s := p.t * 128 / 16000
		if seconds[s] < 12 {
			selected = append(selected, p)
			seconds[s]++
		}
	}
	sort.Slice(selected, func(i, j int) bool {
		if selected[i].t != selected[j].t {
			return selected[i].t < selected[j].t
		}
		return selected[i].f < selected[j].f
	})
	metrics.Peaks = time.Since(begin).Seconds()
	begin = time.Now()
	hashes := make([]Fingerprint, 0, len(selected)*5)
	base := roundFrame(startSeconds * 16000 / 128)
	for i, p := range selected {
		added := 0
		for _, q := range selected[i+1:] {
			dt := q.t - p.t
			if dt > 125 {
				break
			}
			if dt < 10 {
				continue
			}
			key := int64((p.f/3)<<16 | (q.f/3)<<8 | dt/3)
			hashes = append(hashes, Fingerprint{key, base + int64(p.t)})
			added++
			if added == 5 {
				break
			}
		}
	}
	metrics.Pairing = time.Since(begin).Seconds()
	return hashes, metrics
}
func extractNativeFingerprints(r MediaRecord) ([]Fingerprint, FingerprintMetrics, error) {
	var metrics FingerprintMetrics
	if r.Audio == nil {
		return []Fingerprint{}, metrics, nil
	}
	pcm, err := audio.Open(r.Cache)
	if err != nil {
		return nil, metrics, err
	}
	defer pcm.Close()
	var hashes []Fingerprint
	for first := int64(0); first < pcm.Frames(); first += 50 * 16000 {
		count := min(int64(60*16000), pcm.Frames()-first)
		if count < 16000 {
			break
		}
		samples, err := pcm.ReadFrames(first, count)
		if err != nil {
			return nil, metrics, err
		}
		h, m := FingerprintSamples(samples, float64(first)/16000)
		hashes = append(hashes, h...)
		metrics.STFT += m.STFT
		metrics.MaxFilter += m.MaxFilter
		metrics.Percentile += m.Percentile
		metrics.Peaks += m.Peaks
		metrics.Pairing += m.Pairing
		metrics.Chunks++
		if first+count >= pcm.Frames() {
			break
		}
	}
	sort.Slice(hashes, func(i, j int) bool {
		if hashes[i][0] != hashes[j][0] {
			return hashes[i][0] < hashes[j][0]
		}
		return hashes[i][1] < hashes[j][1]
	})
	unique := hashes[:0]
	for _, h := range hashes {
		if len(unique) == 0 || unique[len(unique)-1] != h {
			unique = append(unique, h)
		}
	}
	return unique, metrics, nil
}
func nativeWorkers(jobs int) int {
	// Reserve space for the shared index and OS cache; each analysis task is
	// capped at one 60-second STFT block, conservatively budgeted at 96 MiB.
	budget := min(uint64(8<<30), availableMemory()/4)
	return max(1, min(jobs, runtime.GOMAXPROCS(0), int(budget/(96<<20))))
}
func NativeFingerprintIndex(ctx context.Context, records []MediaRecord, out string, recompute bool) ([][]Fingerprint, []FingerprintMetrics, error) {
	if memoryWorkspace(ctx) != nil {
		return directFingerprintIndex(ctx, records, out)
	}
	folder := filepath.Join(out, "features-native")
	if err := os.MkdirAll(folder, 0755); err != nil {
		return nil, nil, err
	}
	hashes := make([][]Fingerprint, len(records))
	metrics := make([]FingerprintMetrics, len(records))
	type chunkJob struct {
		record, chunk int
		first, count  int64
	}
	type chunkResult struct {
		hashes      []Fingerprint
		alternative []Fingerprint
		metrics     FingerprintMetrics
	}
	results := make([][]chunkResult, len(records))
	readers := make([]*audio.Reader, len(records))
	pending := []chunkJob{}
	defer func() {
		for _, r := range readers {
			if r != nil {
				r.Close()
			}
		}
	}()
	for i, r := range records {
		if len(r.CacheSHA) != 64 {
			return nil, nil, fmt.Errorf("invalid analysis digest for %s", r.Name)
		}
		if !recompute {
			if data, err := os.ReadFile(filepath.Join(folder, r.ID+".json")); err == nil {
				var cached fingerprintCache
				if json.Unmarshal(data, &cached) == nil && cached.Version == fingerprintVersion && cached.Digest == r.CacheSHA {
					hashes[i] = cached.Hashes
					metrics[i] = cached.Metrics
					continue
				}
			}
			legacy := filepath.Join(out, "features", r.ID+".npz")
			if _, err := os.Stat(legacy); err == nil {
				h, err := legacyFingerprints(legacy, r.CacheSHA)
				if err != nil {
					return nil, nil, err
				}
				hashes[i] = h
				continue
			}
		}
		if r.Audio == nil {
			hashes[i] = []Fingerprint{}
			continue
		}
		pcm, err := audio.Open(r.Cache)
		if err != nil {
			return nil, nil, err
		}
		readers[i] = pcm
		for first := int64(0); first < pcm.Frames(); first += 50 * 16000 {
			count := min(int64(60*16000), pcm.Frames()-first)
			if count < 16000 {
				break
			}
			pending = append(pending, chunkJob{i, len(results[i]), first, count})
			results[i] = append(results[i], chunkResult{})
			if first+count >= pcm.Frames() {
				break
			}
		}
	}
	jobs := make(chan chunkJob)
	ioSlots := make(chan struct{}, 4)
	var wg sync.WaitGroup
	var lock sync.Mutex
	var firstErr error
	for worker := 0; worker < nativeWorkers(len(pending)); worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				if ctx.Err() != nil {
					continue
				}
				r := records[job.record]
				checkpoint := filepath.Join(folder, fmt.Sprintf("%s-%s-%d.chunk.json", r.ID, r.CacheSHA[:16], job.first))
				if !recompute {
					if data, err := os.ReadFile(checkpoint); err == nil {
						var c fingerprintCache
						if json.Unmarshal(data, &c) == nil && c.Version == fingerprintVersion && c.Digest == r.CacheSHA {
							results[job.record][job.chunk] = chunkResult{c.Hashes, c.Alternative, c.Metrics}
							continue
						}
					}
				}
				ioSlots <- struct{}{}
				samples, err := readers[job.record].ReadFrames(job.first, job.count)
				<-ioSlots
				if err == nil {
					h, alt, m := fingerprintVariants(samples, float64(job.first)/16000)
					results[job.record][job.chunk] = chunkResult{h, alt, m}
					ioSlots <- struct{}{}
					err = writeJSON(checkpoint, fingerprintCache{Version: fingerprintVersion, Digest: r.CacheSHA, Hashes: h, Metrics: m, Alternative: alt})
					<-ioSlots
				}
				if err != nil {
					lock.Lock()
					if firstErr == nil {
						firstErr = err
					}
					lock.Unlock()
				}
			}
		}()
	}
	for _, job := range pending {
		jobs <- job
	}
	close(jobs)
	wg.Wait()
	if ctx.Err() != nil {
		return nil, nil, ctx.Err()
	}
	if firstErr != nil {
		return nil, nil, firstErr
	}
	for i, r := range records {
		if readers[i] == nil {
			continue
		}
		var alternative []Fingerprint
		hasAlternative := false
		for _, c := range results[i] {
			hashes[i] = append(hashes[i], c.hashes...)
			m := c.metrics
			metrics[i].STFT += m.STFT
			metrics[i].MaxFilter += m.MaxFilter
			metrics[i].Percentile += m.Percentile
			metrics[i].Peaks += m.Peaks
			metrics[i].Pairing += m.Pairing
			metrics[i].Chunks += m.Chunks
			metrics[i].TiedPeaks += m.TiedPeaks
			if c.alternative != nil {
				alternative = append(alternative, c.alternative...)
				hasAlternative = true
			} else {
				alternative = append(alternative, c.hashes...)
			}
		}
		results[i] = nil
		sort.Slice(hashes[i], func(a, b int) bool {
			if hashes[i][a][0] != hashes[i][b][0] {
				return hashes[i][a][0] < hashes[i][b][0]
			}
			return hashes[i][a][1] < hashes[i][b][1]
		})
		unique := hashes[i][:0]
		for _, h := range hashes[i] {
			if len(unique) == 0 || unique[len(unique)-1] != h {
				unique = append(unique, h)
			}
		}
		hashes[i] = unique
		if hasAlternative {
			alternative = uniqueFingerprints(alternative)
		} else {
			alternative = nil
		}
		if err := writeJSON(filepath.Join(folder, r.ID+".json"), fingerprintCache{Version: fingerprintVersion, Digest: r.CacheSHA, Hashes: hashes[i], Metrics: metrics[i], Alternative: alternative}); err != nil {
			return nil, nil, err
		}
	}
	alternativeIDs := []string{}
	for i, r := range records {
		if metrics[i].TiedPeaks > 0 {
			alternativeIDs = append(alternativeIDs, r.ID)
		}
	}
	if err := writeJSON(filepath.Join(folder, "alternatives.json"), alternativeIDs); err != nil {
		return nil, nil, err
	}
	return hashes, metrics, firstErr
}
func uniqueFingerprints(h []Fingerprint) []Fingerprint {
	sort.Slice(h, func(i, j int) bool {
		if h[i][0] != h[j][0] {
			return h[i][0] < h[j][0]
		}
		return h[i][1] < h[j][1]
	})
	out := h[:0]
	for _, v := range h {
		if len(out) == 0 || out[len(out)-1] != v {
			out = append(out, v)
		}
	}
	return out
}
func discoverWithNumericalAlternatives(ctx context.Context, records []MediaRecord, hashes [][]Fingerprint, out string) ([]NativePair, error) {
	primary, err := NativeDiscover(ctx, records, hashes)
	if err != nil {
		return nil, err
	}
	alternatives := append([][]Fingerprint(nil), hashes...)
	changed := false
	var ids []string
	listed := false
	if data, err := workspaceRead(ctx, filepath.Join(out, "features-native", "alternatives.json")); err == nil && json.Unmarshal(data, &ids) == nil {
		listed = true
	}
	hasAlternative := map[string]bool{}
	for _, id := range ids {
		hasAlternative[id] = true
	}
	for i, r := range records {
		if listed && !hasAlternative[r.ID] {
			continue
		}
		data, err := workspaceRead(ctx, filepath.Join(out, "features-native", r.ID+".json"))
		if err != nil {
			continue
		}
		var c fingerprintCache
		if json.Unmarshal(data, &c) != nil || c.Digest != r.CacheSHA || c.Version != fingerprintVersion || len(c.Alternative) == 0 {
			continue
		}
		alternatives[i] = c.Alternative
		changed = true
	}
	if !changed {
		return primary, nil
	}
	secondary, err := NativeDiscover(ctx, records, alternatives)
	if err != nil {
		return nil, err
	}
	byPair := map[[2]int]int{}
	for i, p := range primary {
		byPair[[2]int{p.Reference, p.Query}] = i
	}
	for _, p := range secondary {
		key := [2]int{p.Reference, p.Query}
		i, ok := byPair[key]
		if !ok {
			byPair[key] = len(primary)
			primary = append(primary, p)
			continue
		}
		for _, c := range p.Candidates {
			exists := false
			for _, old := range primary[i].Candidates {
				if old.Offset == c.Offset {
					exists = true
					break
				}
			}
			if !exists {
				c.Method = "fingerprint_numerical_tie_alternative"
				primary[i].Candidates = append(primary[i].Candidates, c)
			}
		}
	}
	sort.SliceStable(primary, func(i, j int) bool {
		if primary[i].Query != primary[j].Query {
			return primary[i].Query < primary[j].Query
		}
		return primary[i].Reference < primary[j].Reference
	})
	return primary, nil
}

type NativeCandidate struct {
	Offset                 float64 `json:"offset"`
	Votes                  int     `json:"votes,omitempty"`
	EnvelopeScore          float64 `json:"envelope_score,omitempty"`
	AnchorTime             float64 `json:"anchor_time,omitempty"`
	Method                 string  `json:"method,omitempty"`
	VerificationWindow     float64 `json:"verification_window_seconds,omitempty"`
	VerificationMaxWindows int     `json:"verification_max_windows,omitempty"`
}
type NativePair struct {
	Reference  int               `json:"reference"`
	Query      int               `json:"query"`
	Candidates []NativeCandidate `json:"candidates"`
	raw        map[string]any
}

func NativeDiscover(ctx context.Context, records []MediaRecord, hashes [][]Fingerprint) ([]NativePair, error) {
	type posting struct {
		record int
		time   int64
	}
	postings := map[int64][]posting{}
	overfull := map[int64]bool{}
	for i, list := range hashes {
		for _, h := range list {
			if overfull[h[0]] {
				continue
			}
			p := append(postings[h[0]], posting{i, h[1]})
			if len(p) > 150 {
				delete(postings, h[0])
				overfull[h[0]] = true
			} else {
				postings[h[0]] = p
			}
		}
	}
	results := make([][]NativePair, len(records))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for worker := 0; worker < nativeWorkers(len(records)); worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for q := range jobs {
				if ctx.Err() != nil {
					continue
				}
				type key struct {
					ref    int
					bucket int64
				}
				votes := map[key]int{}
				for _, h := range hashes[q] {
					for _, p := range postings[h[0]] {
						if records[p.record].Duration < records[q].Duration || (records[p.record].Duration == records[q].Duration && p.record <= q) {
							continue
						}
						k := key{p.record, roundFrame(float64(p.time-h[1]) / 16)}
						votes[k]++
					}
				}
				byRef := map[int][]struct {
					count  int
					bucket int64
				}{}
				for k := range votes {
					sum := 0
					for d := int64(-1); d <= 1; d++ {
						sum += votes[key{k.ref, k.bucket + d}]
					}
					byRef[k.ref] = append(byRef[k.ref], struct {
						count  int
						bucket int64
					}{sum, k.bucket})
				}
				refs := make([]int, 0, len(byRef))
				for r := range byRef {
					refs = append(refs, r)
				}
				sort.Ints(refs)
				for _, r := range refs {
					choices := byRef[r]
					sort.Slice(choices, func(i, j int) bool {
						if choices[i].count != choices[j].count {
							return choices[i].count > choices[j].count
						}
						return choices[i].bucket < choices[j].bucket
					})
					pair := NativePair{Reference: r, Query: q}
					for _, c := range choices {
						if c.count < 6 {
							break
						}
						offset := float64(c.bucket) * 16 * 128 / 16000
						distinct := true
						for _, p := range pair.Candidates {
							if math.Abs(offset-p.Offset) < .75 {
								distinct = false
								break
							}
						}
						if distinct {
							pair.Candidates = append(pair.Candidates, NativeCandidate{Offset: offset, Votes: c.count})
						}
						if len(pair.Candidates) == 8 {
							break
						}
					}
					if len(pair.Candidates) > 0 {
						results[q] = append(results[q], pair)
					}
				}
			}
		}()
	}
	for q := range records {
		jobs <- q
	}
	close(jobs)
	wg.Wait()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	var pairs []NativePair
	for _, p := range results {
		pairs = append(pairs, p...)
	}
	return pairs, nil
}
func NativeDiscoveryBenchmark(ctx context.Context, inventory, out string, recompute bool) error {
	data, err := os.ReadFile(inventory)
	if err != nil {
		return err
	}
	var input struct {
		Records []MediaRecord `json:"records"`
	}
	if err = json.Unmarshal(data, &input); err != nil {
		return err
	}
	if len(input.Records) == 0 {
		return fmt.Errorf("empty inventory")
	}
	if err = os.MkdirAll(out, 0755); err != nil {
		return err
	}
	cpu, err := os.Create(filepath.Join(out, "cpu.pprof"))
	if err != nil {
		return err
	}
	defer cpu.Close()
	if err = pprof.StartCPUProfile(cpu); err != nil {
		return err
	}
	defer pprof.StopCPUProfile()
	start := time.Now()
	hashes, metrics, err := NativeFingerprintIndex(ctx, input.Records, out, recompute)
	if err != nil {
		return err
	}
	indexTime := time.Since(start).Seconds()
	start = time.Now()
	pairs, err := discoverWithNumericalAlternatives(ctx, input.Records, hashes, out)
	if err != nil {
		return err
	}
	discoveryTime := time.Since(start).Seconds()
	if err = writeJSON(filepath.Join(out, "discovery.json"), map[string]any{"version": "opensync-autonomous-v1", "status": "discovery_only", "pairs": pairs}); err != nil {
		return err
	}
	report := map[string]any{"recordings": len(input.Records), "workers": nativeWorkers(len(input.Records)), "fingerprint_seconds": indexTime, "candidate_discovery_seconds": discoveryTime, "total_seconds": indexTime + discoveryTime, "pairs": len(pairs), "per_recording_metrics": metrics, "cache_phase": "reused_existing_guarded_PCM"}
	if err = writeJSON(filepath.Join(out, "native-discovery-profile.json"), report); err != nil {
		return err
	}
	fmt.Printf("NATIVE DISCOVERY: index %.3fs, voting %.3fs, total %.3fs, %d pairs\n", indexTime, discoveryTime, indexTime+discoveryTime, len(pairs))
	return nil
}
