package autosync

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"opensync/internal/storageio"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type fingerprintPCMKey struct{}

// WithFingerprintPCM attaches a streaming visualization consumer. The returned
// writer receives borrowed little-endian mono PCM; nil marks successful EOF.
func WithFingerprintPCM(ctx context.Context, open func(MediaRecord) func([]byte)) context.Context {
	return context.WithValue(ctx, fingerprintPCMKey{}, open)
}

// Decode sequentially with two source readers, then dispatch bounded overlapping
// windows to CPU workers. A large video is never copied or fully loaded into RAM.
func directFingerprintIndex(ctx context.Context, records []MediaRecord, out string) ([][]Fingerprint, []FingerprintMetrics, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type job struct {
		record  int
		first   int64
		samples []int16
	}
	type result struct {
		first                int64
		primary, alternative []Fingerprint
		metrics              FingerprintMetrics
	}
	estimatedChunks := 0
	var readTotal int
	var readDuration float64
	for _, r := range records {
		if r.Audio != nil {
			readTotal++
			readDuration += finiteDuration(r.Duration)
		}
		estimatedChunks += max(1, int(r.Duration/50)+1)
	}
	progress := newIOReadProgress(ctx, readTotal, readDuration)
	workers := nativeWorkers(estimatedChunks)
	stats := memoryWorkspace(ctx)
	stats.indexWorkers.Store(int64(workers))
	jobs := make(chan job, workers)
	inputs := make(chan int)
	results := make([][]result, len(records))
	errs := make([]error, len(records))
	var cpu, decoders sync.WaitGroup
	var mu sync.Mutex
	for w := 0; w < workers; w++ {
		cpu.Add(1)
		go func() {
			defer cpu.Done()
			for j := range jobs {
				if ctx.Err() != nil {
					continue
				}
				active := stats.indexActive.Add(1)
				for old := stats.indexPeak.Load(); active > old; old = stats.indexPeak.Load() {
					if stats.indexPeak.CompareAndSwap(old, active) {
						break
					}
				}
				h, alt, m := fingerprintVariants(j.samples, float64(j.first)/16000)
				stats.indexActive.Add(-1)
				stats.indexCompleted.Add(1)
				mu.Lock()
				results[j.record] = append(results[j.record], result{j.first, h, alt, m})
				mu.Unlock()
			}
		}()
	}
	probes := make([]*storageio.IOProbe, len(records))
	byDevice := map[string]*storageio.IOProbe{}
	readerMode := analysisSettings(ctx).Readers
	for i, r := range records {
		if r.Audio == nil || readerMode != "auto" {
			continue
		}
		key := storageio.IOProbeKey(r.Source)
		if key == "" {
			continue
		}
		if byDevice[key] == nil {
			byDevice[key] = storageio.NewIOProbe()
		}
		probes[i] = byDevice[key]
	}
	readerLimit := 2
	if readerMode == "serial" {
		readerLimit = 1
	}
	for w := 0; w < min(readerLimit, len(records)); w++ {
		decoders.Add(1)
		go func() {
			defer decoders.Done()
			for index := range inputs {
				if ctx.Err() != nil {
					continue
				}
				r := records[index]
				if r.Audio == nil {
					continue
				}
				probe := probes[index]
				release := func() {}
				if probe != nil {
					var err error
					release, err = probe.Acquire(ctx)
					if err != nil {
						continue
					}
				}
				var preview func([]byte)
				if open, ok := ctx.Value(fingerprintPCMKey{}).(func(MediaRecord) func([]byte)); ok {
					preview = open(r)
				}
				began := time.Now()
				expected := finiteDuration(r.Duration)
				progress.report(index, r.Name, began, 0, expected, 0, false)
				digest := sha256.New()
				var frames int64
				direct := memoryWorkspace(ctx).audio.nativePCMContext(ctx, r) != nil
				err := memoryWorkspace(ctx).audio.stream(ctx, r, func(reader io.Reader) error {
					source := io.TeeReader(reader, digest)
					var overlap []int16
					var first int64
					for {
						need := 60*16000 - len(overlap)
						raw := make([]byte, need*2)
						n, err := io.ReadFull(source, raw)
						if probe != nil && n > 0 {
							probe.Observe(n)
						}
						if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
							return err
						}
						if n%2 != 0 {
							return fmt.Errorf("partial PCM sample in %s", r.Name)
						}
						if preview != nil && n > 0 {
							preview(raw[:n])
						}
						fresh := make([]int16, n/2)
						for i := range fresh {
							fresh[i] = int16(binary.LittleEndian.Uint16(raw[i*2:]))
						}
						frames += int64(len(fresh))
						progress.report(index, r.Name, began, float64(frames)/16000, expected, float64(len(fresh))/16000, false)
						samples := append(overlap, fresh...)
						if !direct {
							memoryWorkspace(ctx).audio.retain(audioWindowKey(r.ID, first), samples)
						}
						if len(samples) >= 16000 {
							select {
							case jobs <- job{index, first, samples}:
							case <-ctx.Done():
								return ctx.Err()
							}
						}
						if err != nil {
							break
						}
						// A short final window is generated only if new samples follow the
						// overlap; exactly-ended 60s windows must not add a redundant tail.
						overlap = append([]int16(nil), samples[50*16000:]...)
						first += 50 * 16000
						// Peek by reading the next sample without losing digest accounting.
						var next [2]byte
						n, err = io.ReadFull(source, next[:])
						if err == io.EOF {
							break
						}
						if err != nil {
							return err
						}
						if preview != nil {
							preview(next[:])
						}
						v := int16(binary.LittleEndian.Uint16(next[:]))
						frames++
						overlap = append(overlap, v)
					}
					return nil
				})
				release()
				if err != nil {
					errs[index] = err
					cancel()
					continue
				}
				if frames == 0 {
					errs[index] = fmt.Errorf("no decoded audio in %s", r.Name)
					cancel()
					continue
				}
				if preview != nil {
					preview(nil)
				}
				records[index].Duration = float64(frames) / 16000
				progress.report(index, r.Name, began, records[index].Duration, expected, 0, true)
				records[index].CacheSHA = hex.EncodeToString(digest.Sum(nil))
				fmt.Printf("READ %s (%.2fs), source streamed; no disk cache\n", r.Name, records[index].Duration)
			}
		}()
	}
	order := make([]int, len(records))
	for i := range order {
		order[i] = i
	}
	// Feed expensive recordings first, but retain inventory indices for every
	// result. This avoids starting the hour-long source only after short clips.
	sort.SliceStable(order, func(i, j int) bool { return records[order[i]].Duration > records[order[j]].Duration })
	for _, i := range order {
		select {
		case inputs <- i:
		case <-ctx.Done():
		}
	}
	close(inputs)
	decoders.Wait()
	close(jobs)
	cpu.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, nil, err
		}
	}
	if ctx.Err() != nil {
		return nil, nil, ctx.Err()
	}
	hashes := make([][]Fingerprint, len(records))
	metrics := make([]FingerprintMetrics, len(records))
	ids := []string{}
	for i, r := range records {
		sort.Slice(results[i], func(a, b int) bool { return results[i][a].first < results[i][b].first })
		var alternative []Fingerprint
		changed := false
		for _, c := range results[i] {
			hashes[i] = append(hashes[i], c.primary...)
			if c.alternative != nil {
				changed = true
				alternative = append(alternative, c.alternative...)
			} else {
				alternative = append(alternative, c.primary...)
			}
			m := c.metrics
			metrics[i].STFT += m.STFT
			metrics[i].MaxFilter += m.MaxFilter
			metrics[i].Percentile += m.Percentile
			metrics[i].Peaks += m.Peaks
			metrics[i].Pairing += m.Pairing
			metrics[i].Chunks += m.Chunks
			metrics[i].TiedPeaks += m.TiedPeaks
		}
		hashes[i] = uniqueFingerprints(hashes[i])
		results[i] = nil
		if changed {
			ids = append(ids, r.ID)
			alternative = uniqueFingerprints(alternative)
			// Retain only the alternative stream between indexing and voting.
			if err := workspaceJSON(ctx, filepath.Join(out, "features-native", r.ID+".json"), fingerprintCache{Version: fingerprintVersion, Digest: r.CacheSHA, Alternative: alternative}); err != nil {
				return nil, nil, err
			}
		}
	}
	if err := workspaceJSON(ctx, filepath.Join(out, "features-native", "alternatives.json"), ids); err != nil {
		return nil, nil, err
	}
	return hashes, metrics, nil
}
