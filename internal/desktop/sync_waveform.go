package desktop

import (
	"encoding/binary"
	"math"
	"opensync/internal/autosync"
	"strconv"
)

// SyncWaveform builds the normal peak pyramid from borrowed 16 kHz mono PCM.
// One partial chunk per decoder is held outside the bounded shared LRU.
func (s *Service) SyncWaveform(record autosync.MediaRecord) func([]byte) {
	if record.SourceBytes <= 0 || record.SourceMtime <= 0 {
		return nil
	}
	identity, err := peakIdentity(record.Source, record.SourceBytes, record.SourceMtime)
	if err != nil {
		return nil
	}
	var base []peakPair
	var samples int
	var index int64
	flush := func() {
		key := identity + "\x00" + strconv.FormatInt(index, 10)
		chunk := buildPeakChunk(base)
		p := s.peaks
		p.mu.Lock()
		if p.entries[key] == nil && chunk.bytes <= p.limit {
			p.entries[key] = p.order.PushFront(peakEntry{key, chunk})
			p.bytes += chunk.bytes
			p.evict()
		}
		p.mu.Unlock()
		base = nil
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
			// Chunk-sized zero regions need no per-sample computation.
			if base == nil {
				base = make([]peakPair, peakChunkBins)
			}
			n := min(offset, int64(peakChunkSamples-samples))
			samples += int(n)
			offset -= n
			if samples == peakChunkSamples {
				flush()
			}
		}
		for i := 0; i+1 < len(data); i += 2 {
			if offset < 0 {
				offset++
				continue
			}
			push(float32(int16(binary.LittleEndian.Uint16(data[i:]))) / 32768)
		}
	}
}
