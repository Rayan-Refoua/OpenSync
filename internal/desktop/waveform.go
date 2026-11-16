package desktop

import (
	"container/list"
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"opensync/internal/storageio"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const peakRate = 16000
const peakBaseSamples = 128
const peakChunkBins = 4096
const peakChunkSamples = peakBaseSamples * peakChunkBins
const peakChunkSeconds = float64(peakChunkSamples) / peakRate

type peakPair struct{ min, max float32 }
type peakChunk struct {
	levels [][]peakPair
	bytes  int
}
type peakEntry struct {
	key   string
	chunk *peakChunk
}
type peakFlight struct {
	done  chan struct{}
	chunk *peakChunk
	err   error
	life  context.Context
}
type peakStore struct {
	mu            sync.Mutex
	entries       map[string]*list.Element
	order         *list.List
	flights       map[string]*peakFlight
	pending       *mediaLimiter
	bytes, limit  int
	hits, decodes atomic.Int64
}

func newPeakStore(limit int) *peakStore {
	return &peakStore{entries: map[string]*list.Element{}, order: list.New(), flights: map[string]*peakFlight{}, pending: newMediaLimiter(4), limit: limit}
}
func (p *peakStore) evict() {
	for p.bytes > p.limit && p.order.Len() > 0 {
		e := p.order.Back()
		v := e.Value.(peakEntry)
		delete(p.entries, v.key)
		p.bytes -= v.chunk.bytes
		p.order.Remove(e)
	}
}
func (p *peakStore) setLimit(limit int) { p.mu.Lock(); defer p.mu.Unlock(); p.limit = limit; p.evict() }
func (p *peakStore) stats() (bytes, limit, pending int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.bytes, p.limit, len(p.flights)
}

// Request cancellation abandons only the waiter. A source chunk already being
// decoded remains useful for the next pan/zoom request and completes under the
// project lifetime. Project close/replacement cancels outstanding decoders.
func (p *peakStore) load(ctx, life context.Context, key string, build func(context.Context) (*peakChunk, error)) (*peakChunk, error) {
	for {
		p.mu.Lock()
		if e := p.entries[key]; e != nil {
			p.order.MoveToFront(e)
			c := e.Value.(peakEntry).chunk
			p.mu.Unlock()
			p.hits.Add(1)
			return c, nil
		}
		f := p.flights[key]
		p.mu.Unlock()
		if f == nil {
			if err := p.pending.acquire(ctx); err != nil {
				return nil, err
			}
			p.mu.Lock()
			// Another caller may have completed or started this chunk while this
			// request waited for bounded admission.
			if e := p.entries[key]; e != nil {
				p.order.MoveToFront(e)
				c := e.Value.(peakEntry).chunk
				p.mu.Unlock()
				p.pending.release()
				p.hits.Add(1)
				return c, nil
			}
			if f = p.flights[key]; f == nil {
				f = &peakFlight{done: make(chan struct{}), life: life}
				p.flights[key] = f
				go func(f *peakFlight) {
					defer p.pending.release()
					job, cancel := context.WithTimeout(life, 2*time.Minute)
					defer cancel()
					f.chunk, f.err = build(job)
					p.mu.Lock()
					if f.err == nil && f.chunk != nil && f.chunk.bytes <= p.limit {
						p.entries[key] = p.order.PushFront(peakEntry{key, f.chunk})
						p.bytes += f.chunk.bytes
						p.evict()
					}
					delete(p.flights, key)
					close(f.done)
					p.mu.Unlock()
				}(f)
				p.mu.Unlock()
			} else {
				p.mu.Unlock()
				p.pending.release()
			}
		} else {
			p.hits.Add(1)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-f.done:
			if f.err != nil && f.life.Err() != nil && life.Err() == nil {
				continue
			}
			return f.chunk, f.err
		}
	}
}

func sourcePeakKey(c Clip) (string, error) {
	st, err := os.Stat(c.Source)
	if err != nil {
		return "", err
	}
	return peakIdentity(c.Source, st.Size(), st.ModTime().UnixNano())
}

func peakIdentity(source string, bytes, mtime int64) (string, error) {
	path, err := filepath.Abs(source)
	if err != nil {
		return "", err
	}
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path + "\x00" + strconv.FormatInt(bytes, 10) + "\x00" + strconv.FormatInt(mtime, 10), nil
}

func (s *Service) decodePeakChunk(ctx context.Context, c Clip, index int64) (*peakChunk, error) {
	return s.loadPeakChunk(ctx, c, index, "", "")
}

func (s *Service) loadPeakChunk(ctx context.Context, c Clip, index int64, dir, key string) (*peakChunk, error) {
	ctx, done, gateErr := s.preview.enter(ctx)
	if gateErr != nil {
		return nil, gateErr
	}
	defer done()
	if dir != "" {
		// Validate the original's identity before reusing a project sidecar.
		// Read only metadata here; a cache hit skips opening the audio stream.
		actual, err := sourcePeakKey(c)
		if err != nil {
			return nil, err
		}
		if actual+"\x00"+strconv.FormatInt(index, 10) != key {
			dir = ""
		}
	}
	if cached := readPeakChunk(dir, key); cached != nil {
		return cached, nil
	}
	if err := s.visual.acquire(ctx); err != nil {
		return nil, err
	}
	defer s.visual.release()
	s.active.Add(1)
	defer s.active.Add(-1)
	s.peaks.decodes.Add(1)
	base := make([]peakPair, peakChunkBins)
	for i := range base {
		base[i] = peakPair{float32(math.Inf(1)), float32(math.Inf(-1))}
	}
	start := float64(index) * peakChunkSeconds
	var frame int64
	finish, err := s.waveIO.acquire(ctx, c.Source)
	if err != nil {
		return nil, err
	}
	err = decodeAudio(ctx, c.Source, start, math.Min(peakChunkSeconds, c.Duration-start), peakRate, storageio.DirectMediaArgs(c.Source), func(data []byte) error {
		for off := 0; off+7 < len(data); off += 8 {
			i := frame / peakBaseSamples
			if i >= peakChunkBins {
				break
			}
			frame++
			for ch := 0; ch < 2; ch++ {
				v := sample(data[off+ch*4:])
				if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
					continue
				}
				base[i].min = min(base[i].min, v)
				base[i].max = max(base[i].max, v)
			}
		}
		return nil
	})
	finish(err == nil)
	if err != nil {
		return nil, err
	}
	for i, v := range base {
		if math.IsInf(float64(v.min), 1) {
			base[i] = peakPair{}
		}
	}
	chunk := buildPeakChunk(base)
	if dir != "" {
		_ = writePeakChunk(dir, key, chunk)
	}
	return chunk, nil
}

func buildPeakChunk(base []peakPair) *peakChunk {
	chunk := &peakChunk{levels: [][]peakPair{base}, bytes: 1024 + len(base)*8}
	for len(base) > 1 {
		next := make([]peakPair, len(base)/2)
		for i := range next {
			a, b := base[i*2], base[i*2+1]
			next[i] = peakPair{min(a.min, b.min), max(a.max, b.max)}
		}
		chunk.levels = append(chunk.levels, next)
		chunk.bytes += len(next) * 8
		base = next
	}
	return chunk
}

func (s *Service) waveFromPeaks(ctx, life context.Context, c Clip, level, tile int) ([]byte, error) {
	bin := int64(peakBaseSamples) << level
	bins := 512
	for bins > 1 && bin*int64(bins) > peakRate*120 {
		bins /= 2
	}
	startFloat := float64(tile) * float64(bin) * float64(bins)
	if startFloat >= c.Duration*peakRate || !c.Audio {
		return make([]byte, bins*8), nil
	}
	if startFloat > math.MaxInt64 {
		return nil, fmt.Errorf("waveform tile outside source")
	}
	start := int64(startFloat)
	var identity string
	var err error
	if c.Metadata.SourceBytes > 0 && c.Metadata.SourceMtime > 0 {
		identity, err = peakIdentity(c.Source, c.Metadata.SourceBytes, c.Metadata.SourceMtime)
	} else {
		identity, err = sourcePeakKey(c)
	}
	if err != nil {
		return nil, err
	}
	dir := ""
	if p := s.Project(); p != nil && p.Path != "" {
		path := p.Path
		if revision, ok := p.Graph["solution_revision"].(map[string]any); ok {
			if base, ok := revision["base_project"].(string); ok && base != "" {
				path = base
			}
		}
		dir = filepath.Join(filepath.Dir(path), "Waveform Peaks")
	}
	out := make([]byte, bins*8)
	// One tile can touch several source chunks. Hold local references only for
	// the duration of this request; the shared LRU owns the bounded resident set.
	var held *peakChunk
	heldIndex := int64(-1)
	for i := 0; i < bins; i++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		first, last := start+int64(i)*bin, start+int64(i+1)*bin
		if float64(first) >= c.Duration*peakRate {
			break
		}
		value := peakPair{float32(math.Inf(1)), float32(math.Inf(-1))}
		for pos := first; pos < last && float64(pos) < c.Duration*peakRate; {
			index := pos / peakChunkSamples
			if index != heldIndex {
				key := identity + "\x00" + strconv.FormatInt(index, 10)
				held, err = s.peaks.load(ctx, life, key, func(job context.Context) (*peakChunk, error) { return s.loadPeakChunk(job, c, index, dir, key) })
				if err != nil {
					return nil, err
				}
				heldIndex = index
			}
			l := min(level, len(held.levels)-1)
			local := (pos % peakChunkSamples) / (int64(peakBaseSamples) << l)
			v := held.levels[l][local]
			value.min = min(value.min, v.min)
			value.max = max(value.max, v.max)
			pos += int64(peakBaseSamples) << l
		}
		if math.IsInf(float64(value.min), 1) {
			value = peakPair{}
		}
		binary.LittleEndian.PutUint32(out[i*8:], math.Float32bits(value.min))
		binary.LittleEndian.PutUint32(out[i*8+4:], math.Float32bits(value.max))
	}
	return out, nil
}
