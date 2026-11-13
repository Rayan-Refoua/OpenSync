package desktop

import (
	"bytes"
	"container/list"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"opensync/internal/autosync"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type cached struct {
	key  string
	data []byte
}
type byteCache struct {
	mu           sync.Mutex
	entries      map[string]*list.Element
	order        *list.List
	bytes, limit int
}

func newCache(limit int) *byteCache {
	return &byteCache{entries: map[string]*list.Element{}, order: list.New(), limit: limit}
}
func (c *byteCache) get(k string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[k]
	if !ok {
		return nil, false
	}
	c.order.MoveToFront(e)
	return e.Value.(cached).data, true
}
func (c *byteCache) put(k string, b []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.entries[k]; ok || len(b) > c.limit {
		return
	}
	c.entries[k] = c.order.PushFront(cached{k, b})
	c.bytes += len(b)
	for c.bytes > c.limit {
		e := c.order.Back()
		v := e.Value.(cached)
		delete(c.entries, v.key)
		c.bytes -= len(v.data)
		c.order.Remove(e)
	}
}

type Service struct {
	mu                       sync.RWMutex
	project                  *Project
	generation               uint64
	cache                    *byteCache
	visual, playback, thumbs chan struct{}
	active                   atomic.Int64
	completed                atomic.Int64
	ctx                      context.Context
	cancel                   context.CancelFunc
}

func NewService() *Service {
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{cache: newCache(64 << 20), visual: make(chan struct{}, 2), playback: make(chan struct{}, 2), thumbs: make(chan struct{}, 1), ctx: ctx, cancel: cancel}
}
func (s *Service) Close() { s.cancel() }
func (s *Service) SetProject(p *Project) *Project {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancel()
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.generation++
	p.Generation = s.generation
	s.project = p
	return p
}
func (s *Service) Project() *Project { s.mu.RLock(); defer s.mu.RUnlock(); return s.project }
func (s *Service) Stats() map[string]any {
	s.cache.mu.Lock()
	b := s.cache.bytes
	s.cache.mu.Unlock()
	return map[string]any{"active": s.active.Load(), "completed": s.completed.Load(), "cacheBytes": b, "cacheLimit": 64 << 20, "diskCache": false}
}

// Relink resolves only unambiguous basename + byte-size matches. Source evidence
// and saved project files remain unchanged. A new generation invalidates views.
func (s *Service) Relink(root string) (*Project, error) {
	p := s.Project()
	if p == nil {
		return nil, fmt.Errorf("open a project first")
	}
	files := map[string][]string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() {
			files[strings.ToLower(d.Name())] = append(files[strings.ToLower(d.Name())], path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	n := *p
	n.Clips = append([]Clip(nil), p.Clips...)
	for i, c := range n.Clips {
		if !c.Missing {
			continue
		}
		var found []string
		for _, f := range files[strings.ToLower(c.Name)] {
			st, e := os.Stat(f)
			if e == nil && (c.Metadata.SourceBytes == 0 || st.Size() == c.Metadata.SourceBytes) {
				found = append(found, f)
			}
		}
		if len(found) == 1 {
			n.Clips[i].Source = found[0]
			n.Clips[i].Missing = false
		}
	}
	return s.SetProject(&n), nil
}
func tool(name string) (string, error) {
	if exe, e := os.Executable(); e == nil {
		p := filepath.Join(filepath.Dir(exe), "runtime", "bin", name+".exe")
		if exists(p) {
			return p, nil
		}
	}
	return exec.LookPath(name)
}
func command(ctx context.Context, args ...string) (*exec.Cmd, error) {
	path, e := tool("ffmpeg")
	if e != nil {
		return nil, fmt.Errorf("FFmpeg is required: %w", e)
	}
	c := exec.CommandContext(ctx, path, args...)
	hideProcess(c)
	return c, nil
}
func acquire(ctx context.Context, slots chan struct{}) error {
	select {
	case slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func decode(ctx context.Context, path string, start, duration float64, rate int, consume func([]byte) error) error {
	if duration <= 0 {
		return nil
	}
	cmd, err := command(ctx, "-hide_banner", "-loglevel", "error", "-nostdin", "-threads", "1", "-ss", strconv.FormatFloat(start, 'f', 9, 64), "-i", path, "-t", strconv.FormatFloat(duration, 'f', 9, 64), "-map", "0:a:0", "-vn", "-ac", "2", "-ar", strconv.Itoa(rate), "-f", "f32le", "pipe:1")
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err = cmd.Start(); err != nil {
		return err
	}
	buf := make([]byte, 32768)
	for {
		n, e := io.ReadFull(stdout, buf)
		if n > 0 {
			if err = consume(buf[:n]); err != nil {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				return err
			}
		}
		if e == io.EOF || e == io.ErrUnexpectedEOF {
			break
		}
		if e != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return e
		}
	}
	if err = cmd.Wait(); err != nil {
		return fmt.Errorf("audio decode: %w: %.600s", err, stderr.String())
	}
	return nil
}
func sample(b []byte) float32 { return math.Float32frombits(binary.LittleEndian.Uint32(b)) }

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/media/") {
		http.NotFound(w, r)
		return
	}
	if r.Method != "GET" {
		http.Error(w, "GET required", 405)
		return
	}
	s.mu.RLock()
	p := s.project
	life := s.ctx
	s.mu.RUnlock()
	g, e := strconv.ParseUint(r.URL.Query().Get("g"), 10, 64)
	if e != nil || p == nil || g != p.Generation {
		http.Error(w, "stale project", 409)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	stop := context.AfterFunc(life, cancel)
	defer stop()
	if r.URL.Path == "/media/audio" {
		s.serveAudio(ctx, w, r, p)
		return
	}
	idx, e := strconv.Atoi(r.URL.Query().Get("clip"))
	if e != nil || idx < 0 || idx >= len(p.Clips) {
		http.Error(w, "invalid recording", 400)
		return
	}
	c := p.Clips[idx]
	if c.Missing {
		http.Error(w, "source media is missing; locate its folder", 404)
		return
	}
	key := fmt.Sprintf("%d:%s?%s", g, r.URL.Path, r.URL.RawQuery)
	typ := "application/octet-stream"
	if r.URL.Path == "/media/thumb" {
		typ = "image/jpeg"
	}
	w.Header().Set("Content-Type", typ)
	w.Header().Set("Cache-Control", "no-store")
	if b, ok := s.cache.get(key); ok {
		_, _ = w.Write(b)
		return
	}
	var b []byte
	var err error
	slots := s.visual
	if r.URL.Path == "/media/thumb" {
		slots = s.thumbs
	}
	if err = acquire(ctx, slots); err != nil {
		return
	}
	defer func() { <-slots }()
	s.active.Add(1)
	defer s.active.Add(-1)
	switch r.URL.Path {
	case "/media/wave":
		b, err = s.wave(ctx, r, c)
	case "/media/thumb":
		b, err = s.thumbnail(ctx, r, c)
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), 422)
		return
	}
	s.completed.Add(1)
	s.cache.put(key, b)
	_, _ = w.Write(b)
}
func (s *Service) wave(ctx context.Context, r *http.Request, c Clip) ([]byte, error) {
	level, e := strconv.Atoi(r.URL.Query().Get("level"))
	if e != nil || level < 0 || level > 20 {
		return nil, fmt.Errorf("invalid waveform level")
	}
	tile, e := strconv.Atoi(r.URL.Query().Get("tile"))
	if e != nil || tile < 0 {
		return nil, fmt.Errorf("invalid waveform tile")
	}
	bin := int64(128) << level
	start := float64(int64(tile)*bin*512) / 16000
	if start >= c.Duration || !c.Audio {
		return make([]byte, 512*8), nil
	}
	mins, maxs := make([]float32, 512), make([]float32, 512)
	var frame int64
	duration := math.Min(float64(bin*512)/16000, c.Duration-start)
	err := decode(ctx, c.Source, start, duration, 16000, func(b []byte) error {
		for off := 0; off+7 < len(b); off += 8 {
			bucket := frame / bin
			if bucket >= 512 {
				break
			}
			for ch := 0; ch < 2; ch++ {
				v := sample(b[off+ch*4:])
				if v < mins[bucket] {
					mins[bucket] = v
				}
				if v > maxs[bucket] {
					maxs[bucket] = v
				}
			}
			frame++
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := make([]byte, 512*8)
	for i := 0; i < 512; i++ {
		binary.LittleEndian.PutUint32(out[i*8:], math.Float32bits(mins[i]))
		binary.LittleEndian.PutUint32(out[i*8+4:], math.Float32bits(maxs[i]))
	}
	return out, nil
}
func (s *Service) thumbnail(ctx context.Context, r *http.Request, c Clip) ([]byte, error) {
	t, e := strconv.ParseFloat(r.URL.Query().Get("time"), 64)
	if e != nil || math.IsNaN(t) || math.IsInf(t, 0) || t < 0 || t >= c.Duration || !c.Video {
		return nil, fmt.Errorf("invalid thumbnail time")
	}
	cmd, err := command(ctx, "-hide_banner", "-loglevel", "error", "-nostdin", "-threads", "1", "-ss", fmt.Sprintf("%.6f", t), "-i", c.Source, "-frames:v", "1", "-an", "-vf", "scale=160:90:force_original_aspect_ratio=decrease,pad=160:90:(ow-iw)/2:(oh-ih)/2", "-threads", "1", "-f", "image2pipe", "-vcodec", "mjpeg", "pipe:1")
	if err != nil {
		return nil, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	b, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("thumbnail: %w: %.500s", err, stderr.String())
	}
	return b, nil
}

// Audio returns a bounded stereo float32 audition block. All selected lanes are
// mixed on one timeline grid, including gaps and piecewise timing models.
func (s *Service) serveAudio(ctx context.Context, w http.ResponseWriter, r *http.Request, p *Project) {
	start, e := strconv.ParseFloat(r.URL.Query().Get("start"), 64)
	duration, e2 := strconv.ParseFloat(r.URL.Query().Get("duration"), 64)
	var tracks []int
	e3 := json.Unmarshal([]byte(r.URL.Query().Get("tracks")), &tracks)
	if e != nil || e2 != nil || e3 != nil || math.IsNaN(start) || math.IsNaN(duration) || math.IsInf(start, 0) || start < 0 || start > p.Duration || duration <= 0 || duration > 4 || len(tracks) == 0 || len(tracks) > 2 {
		http.Error(w, "invalid audition request", 400)
		return
	}
	seen := map[int]bool{}
	for _, t := range tracks {
		if t < 0 || t >= len(p.Tracks) || seen[t] {
			http.Error(w, "invalid audition track", 400)
			return
		}
		seen[t] = true
	}
	if e = acquire(ctx, s.playback); e != nil {
		return
	}
	defer func() { <-s.playback }()
	s.active.Add(1)
	defer s.active.Add(-1)
	const rate = 48000
	out := make([]float32, int(math.Round(duration*rate))*2)
	for _, track := range tracks {
		for _, idx := range p.Tracks[track].Clips {
			c := p.Clips[idx]
			if c.Start >= start+duration {
				break
			}
			if c.End <= start || !c.Audio {
				continue
			}
			if c.Missing {
				http.Error(w, "selected track has missing media: "+c.Name, 404)
				return
			}
			lo, hi := math.Max(start, c.Start), math.Min(start+duration, c.End)
			inv := autosync.InverseMapping(c.Mapping)
			a, b := autosync.MapTime(inv, lo), autosync.MapTime(inv, hi)
			if b-a > 12 {
				http.Error(w, "timing rate outside audition limits", 422)
				return
			}
			var pcm []float32
			err := decode(ctx, c.Source, math.Max(0, a), math.Min(c.Duration-a, b-a+2./rate), rate, func(data []byte) error {
				for j := 0; j+3 < len(data); j += 4 {
					pcm = append(pcm, sample(data[j:]))
				}
				return nil
			})
			if err != nil {
				http.Error(w, err.Error(), 422)
				return
			}
			first, last := max(0, int(math.Ceil((lo-start)*rate-1e-7))), min(len(out)/2, int(math.Ceil((hi-start)*rate-1e-7)))
			for j := first; j < last; j++ {
				src := (autosync.MapTime(inv, start+float64(j)/rate) - a) * rate
				k := int(math.Floor(src))
				f := float32(src - float64(k))
				if k < 0 || k*2+3 >= len(pcm) {
					continue
				}
				for ch := 0; ch < 2; ch++ {
					out[j*2+ch] += (pcm[k*2+ch]*(1-f) + pcm[(k+1)*2+ch]*f) / float32(len(tracks))
				}
			}
		}
	}
	data := make([]byte, len(out)*4)
	for i, v := range out {
		binary.LittleEndian.PutUint32(data[i*4:], math.Float32bits(v))
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}
