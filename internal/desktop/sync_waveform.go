package desktop

import (
	"encoding/binary"
	"math"
	"opensync/internal/autosync"
	"path/filepath"
	"strconv"
)

// SyncWaveform builds the normal peak pyramid from borrowed 16 kHz mono PCM.
// One partial chunk per decoder is held outside the bounded shared LRU.
func (s *Service) SyncWaveform(record autosync.MediaRecord) func([]byte) {
	return s.syncWaveform(record, "", nil)
}

// SyncWaveformTo persists complete chunks beside the project as sync progresses.
func (s *Service) SyncWaveformTo(record autosync.MediaRecord, projectDir string, onError func(error)) func([]byte) {
	return s.syncWaveform(record, filepath.Join(projectDir, "Waveform Peaks"), onError)
}

func (s *Service) syncWaveform(record autosync.MediaRecord, dir string, onError func(error)) func([]byte) {
	if record.SourceBytes <= 0 || record.SourceMtime <= 0 {
		return nil
	}
	identity, err := peakIdentity(record.Source, record.SourceBytes, record.SourceMtime)
	if err != nil {
		return nil
	}
	var base []peakPair
	var cached *peakChunk
	var checked bool
	var samples int
	var index int64
	var recent []string
	if dir != "" {
		recent = s.recentPeakDirs()
	}
	checkCache := func() {
		if !checked {
			checked = true
			key := identity + "\x00" + strconv.FormatInt(index, 10)
			cached = readRecentPeak(recent, dir, key)
		}
	}
	flush := func() {
		key := identity + "\x00" + strconv.FormatInt(index, 10)
		chunk := cached
		if chunk == nil {
			chunk = buildPeakChunk(base)
		}
		p := s.peaks
		p.mu.Lock()
		if p.entries[key] == nil && chunk.bytes <= p.limit {
			p.entries[key] = p.order.PushFront(peakEntry{key, chunk})
			p.bytes += chunk.bytes
			p.evict()
		}
		p.mu.Unlock()
		if dir != "" {
			if err := writePeakChunk(dir, key, chunk); err != nil {
				if onError != nil {
					onError(err)
				}
				dir = "" // Keep providing RAM peaks if storage is unavailable.
			}
		}
		base = nil
		cached = nil
		checked = false
		samples = 0
		index++
	}
	// The sync stream starts at the audio PTS. Peak tiles use video time
	// for A/V sources, so account for initial silence or negative preroll.
	var offset int64
	if record.Video != nil {
		delta := metadataFloat(record.Audio["start_time"]) - metadataFloat(record.Video["start_time"])
		if math.IsNaN(delta) || math.IsInf(delta, 0) || math.Abs(delta) > record.Duration {
			return nil
		}
		offset = int64(math.Round(delta * peakRate))
	}
	push := func(value float32) {
		if base == nil {
			base = make([]peakPair, peakChunkBins)
		}
		bin := samples / peakBaseSamples
		if samples%peakBaseSamples == 0 {
			base[bin] = peakPair{value, value}
		} else {
			base[bin].min = min(base[bin].min, value)
			base[bin].max = max(base[bin].max, value)
		}
		samples++
		if samples == peakChunkSamples {
			flush()
		}
	}
	return func(data []byte) {
		if data == nil {
			if samples > 0 {
				flush()
			}
			return
		}
		for offset > 0 {
			checkCache()
			// Chunk-sized zero regions need no per-sample computation.
			if cached == nil && base == nil {
				base = make([]peakPair, peakChunkBins)
			}
			n := min(offset, int64(peakChunkSamples-samples))
			samples += int(n)
			offset -= n
			if samples == peakChunkSamples {
				flush()
			}
		}
		if offset < 0 {
			skip := min(-offset, int64(len(data)/2))
			offset += skip
			data = data[2*skip:]
		}
		for len(data) >= 2 {
			checkCache()
			if cached != nil {
				n := min(len(data)/2, peakChunkSamples-samples)
				samples += n
				data = data[2*n:]
				if samples == peakChunkSamples {
					flush()
				}
				continue
			}
			push(float32(int16(binary.LittleEndian.Uint16(data))) / 32768)
			data = data[2:]
		}
	}
}
