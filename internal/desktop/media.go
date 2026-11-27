package desktop

import (
	"bytes"
	"container/list"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"opensync/internal/autosync"
	"opensync/internal/storageio"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type cached struct {
	key       string
	data      []byte
	frameTime *float64
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
	v, ok := c.lookup(k)
	return v.data, ok
}
func (c *byteCache) lookup(k string) (cached, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[k]
	if !ok {
		return cached{}, false
	}
	c.order.MoveToFront(e)
	return e.Value.(cached), true
}
func (c *byteCache) put(k string, b []byte) {
	c.putTimed(k, b, nil)
}
func (c *byteCache) putTimed(k string, b []byte, frameTime *float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.entries[k]; ok || len(b) > c.limit {
		return
	}
	c.entries[k] = c.order.PushFront(cached{k, b, frameTime})
	c.bytes += len(b)
	for c.bytes > c.limit {
		e := c.order.Back()
		v := e.Value.(cached)
		delete(c.entries, v.key)
		c.bytes -= len(v.data)
		c.order.Remove(e)
	}
}
func (c *byteCache) setLimit(limit int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.limit = limit
	for c.bytes > c.limit {
		e := c.order.Back()
		v := e.Value.(cached)
		delete(c.entries, v.key)
		c.bytes -= len(v.data)
		c.order.Remove(e)
	}
}

type Service struct {
	recent                   recentProjects
	preview                  previewGate
	mu                       sync.RWMutex
	project                  *Project
	generation               uint64
	cache                    *byteCache
	peaks                    *peakStore
	visual, playback, thumbs *mediaLimiter
	waveIO, thumbIO          previewIO
	active                   atomic.Int64
	completed                atomic.Int64
	ctx                      context.Context
	cancel                   context.CancelFunc
}

func NewService() *Service {
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{cache: newCache(16 << 20), peaks: newPeakStore(48 << 20), visual: newMediaLimiter(2), playback: newMediaLimiter(2), thumbs: newMediaLimiter(1), ctx: ctx, cancel: cancel}
}
func (s *Service) Close() { s.cancel() }
func (s *Service) SetProject(p *Project) *Project {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancel()
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.generation++
	if p != nil {
		p.Generation = s.generation
	}
	s.project = p
	return p
}
func (s *Service) Project() *Project { s.mu.RLock(); defer s.mu.RUnlock(); return s.project }
func (s *Service) Stats() map[string]any {
	s.cache.mu.Lock()
	b, limit := s.cache.bytes, s.cache.limit
	s.cache.mu.Unlock()
	peaks, peakLimit, pending := s.peaks.stats()
	activeWave, waveLimit := s.visual.stats()
	activeThumb, thumbLimit := s.thumbs.stats()
	return map[string]any{"active": s.active.Load(), "completed": s.completed.Load(), "cacheBytes": b + peaks, "cacheLimit": limit + peakLimit, "diskCache": false, "peakBytes": peaks, "peakChunksDecoded": s.peaks.decodes.Load(), "peakCacheHits": s.peaks.hits.Load(), "peakPending": pending, "waveformActive": activeWave, "waveformWorkers": waveLimit, "thumbnailActive": activeThumb, "thumbnailWorkers": thumbLimit}
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
	args, e = storageio.MediaArgs(ctx, path, args)
	if e != nil {
		return nil, e
	}
	c := exec.CommandContext(ctx, path, args...)
	hideProcess(c)
	return c, nil
}
func acquire(ctx context.Context, slots *mediaLimiter) error { return slots.acquire(ctx) }
func decode(ctx context.Context, path string, start, duration float64, rate int, consume func([]byte) error) error {
	return decodeAudio(ctx, path, start, duration, rate, nil, consume)
}
func decodeAudio(ctx context.Context, path string, start, duration float64, rate int, inputOptions []string, consume func([]byte) error) error {
	if duration <= 0 {
		return nil
	}
	// first_pts=0 retains an audio stream's delayed start as silence on the
	// requested source-time grid instead of sliding it to the first sample.
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-threads", "1", "-ss", strconv.FormatFloat(start, 'f', 9, 64)}
	args = append(args, inputOptions...)
	args = append(args, "-i", path, "-t", strconv.FormatFloat(duration, 'f', 9, 64), "-map", "0:a:0", "-vn", "-ac", "2", "-af", fmt.Sprintf("aresample=%d:async=0:first_pts=0", rate), "-ar", strconv.Itoa(rate), "-f", "f32le", "pipe:1")
	cmd, err := command(ctx, args...)
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
		job, done, err := s.preview.enter(ctx)
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		defer done()
		s.serveAudio(job, w, r, p)
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
	if r.URL.Path == "/media/review-audio" {
		if !c.Audio {
			http.Error(w, "recording has no audio", 422)
			return
		}
		job, done, err := s.preview.enter(ctx)
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		defer done()
		c.Start, c.End = 0, c.Duration
		c.Mapping = []autosync.TimePoint{{0, 0}, {c.Duration, c.Duration}}
		isolated := &Project{Duration: c.Duration, Clips: []Clip{c}, Tracks: []Track{{Clips: []int{0}}}}
		q := r.URL.Query()
		q.Set("tracks", "[0]")
		request := r.Clone(job)
		request.URL.RawQuery = q.Encode()
		s.serveAudio(job, w, request, isolated)
		return
	}
	key := fmt.Sprintf("%d:%s?%s", g, r.URL.Path, r.URL.RawQuery)
	typ := "application/octet-stream"
	if r.URL.Path == "/media/thumb" {
		typ = "image/png"
	}
	w.Header().Set("Content-Type", typ)
	w.Header().Set("Cache-Control", "no-store")
	if v, ok := s.cache.lookup(key); ok {
		if v.frameTime != nil {
			w.Header().Set("X-OpenSync-Frame-Time", strconv.FormatFloat(*v.frameTime, 'f', 9, 64))
		}
		_, _ = w.Write(v.data)
		return
	}
	var b []byte
	var err error
	var frameTime *float64
	switch r.URL.Path {
	case "/media/wave":
		b, err = s.wave(ctx, life, r, c)
	case "/media/thumb":
		job, done, gateErr := s.preview.enter(ctx)
		if gateErr != nil {
			http.Error(w, gateErr.Error(), http.StatusServiceUnavailable)
			return
		}
		defer done()
		ctx = job
		if err = acquire(ctx, s.thumbs); err != nil {
			return
		}
		defer s.thumbs.release()
		s.active.Add(1)
		defer s.active.Add(-1)
		var at float64
		finish, admissionErr := s.thumbIO.acquire(ctx, c.Source)
		if admissionErr != nil {
			return
		}
		b, at, err = s.thumbnailFrame(ctx, r, c)
		finish(err == nil)
		if err == nil && !math.IsNaN(at) {
			frameTime = &at
			w.Header().Set("X-OpenSync-Frame-Time", strconv.FormatFloat(at, 'f', 9, 64))
		}
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		if errors.Is(err, errPreviewPaused) || errors.Is(err, context.Canceled) {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		http.Error(w, err.Error(), 422)
		return
	}
	s.completed.Add(1)
	s.cache.putTimed(key, b, frameTime)
	_, _ = w.Write(b)
}
func (s *Service) wave(ctx, life context.Context, r *http.Request, c Clip) ([]byte, error) {
	level, e := strconv.Atoi(r.URL.Query().Get("level"))
	if e != nil || level < 0 || level > 20 {
		return nil, fmt.Errorf("invalid waveform level")
	}
	tile, e := strconv.Atoi(r.URL.Query().Get("tile"))
	if e != nil || tile < 0 {
		return nil, fmt.Errorf("invalid waveform tile")
	}
	return s.waveFromPeaks(ctx, life, c, level, tile)
}
func (s *Service) thumbnail(ctx context.Context, r *http.Request, c Clip) ([]byte, error) {
	b, _, err := s.thumbnailFrame(ctx, r, c)
	return b, err
}

var thumbnailPTS = regexp.MustCompile(`pts_time:([+-]?[0-9]+(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?)`)

func metadataFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case json.Number:
		f, _ := n.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(n, 64)
		return f
	}
	return 0
}
func metadataFPS(v any) float64 {
	parts := strings.Split(fmt.Sprint(v), "/")
	if len(parts) == 2 {
		a, _ := strconv.ParseFloat(parts[0], 64)
		b, _ := strconv.ParseFloat(parts[1], 64)
		if b > 0 {
			return a / b
		}
	}
	return metadataFloat(v)
}
func (s *Service) thumbnailFrame(ctx context.Context, r *http.Request, c Clip) ([]byte, float64, error) {
	t, e := strconv.ParseFloat(r.URL.Query().Get("time"), 64)
	if e != nil || math.IsNaN(t) || math.IsInf(t, 0) || t < 0 || t >= c.Duration || !c.Video {
		return nil, 0, fmt.Errorf("invalid thumbnail time")
	}
	end := c.Duration
	if d := metadataFloat(c.Metadata.Video["duration"]); d > 0 {
		end = math.Min(end, d)
	}
	fps := metadataFPS(c.Metadata.Video["r_frame_rate"])
	if fps <= 0 || math.IsNaN(fps) || math.IsInf(fps, 0) {
		fps = 30
	}
	step := math.Min(.5, math.Max(.001, 1/fps))
	t = math.Min(t, math.Max(0, end-step))
	// Explicit RGB output avoids MJPEG's YUV range/encoder constraints and
	// geometry surprises from odd-sized/rotated/high-bit-depth sources.
	// A VFR recording can hold its last frame beyond that frame's PTS. Accurate
	// input seeking then discards the last frame. Retry a bounded earlier point
	// only when the decoder returned no frame; never conceal a decode error.
	previous := -1.
	for _, back := range []float64{0, step, .25, 1, 2} {
		at := math.Max(0, t-back)
		if at == previous {
			continue
		}
		previous = at
		args := []string{"-hide_banner", "-loglevel", "info", "-nostdin", "-copyts", "-start_at_zero", "-threads", "1", "-filter_threads", "1", "-ss", fmt.Sprintf("%.9f", at)}
		args = append(args, storageio.DirectMediaArgs(c.Source)...)
		args = append(args, "-i", c.Source, "-frames:v", "1", "-an", "-vf", "scale=160:90:force_original_aspect_ratio=decrease:force_divisible_by=2,setsar=1,pad=160:90:(ow-iw)/2:(oh-ih)/2:color=black,format=rgb24,showinfo", "-threads", "1", "-fps_mode", "passthrough", "-f", "image2pipe", "-vcodec", "png", "pipe:1")
		cmd, err := command(ctx, args...)
		if err != nil {
			return nil, 0, err
		}
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		b, err := cmd.Output()
		if err != nil {
			detail := stderr.String()
			if len(detail) > 1600 {
				detail = detail[len(detail)-1600:]
			}
			return nil, 0, fmt.Errorf("thumbnail %s at %.6f: %w: %s", c.Name, at, err, detail)
		}
		if len(b) > 0 {
			actual := math.NaN()
			if match := thumbnailPTS.FindStringSubmatch(stderr.String()); len(match) == 2 {
				actual, _ = strconv.ParseFloat(match[1], 64)
			}
			return b, actual, nil
		}
	}
	return nil, 0, fmt.Errorf("no video frame available near %.6f seconds in %s", t, c.Name)
}

// Audio returns a bounded stereo float32 audition block. All selected lanes are
// mixed on one timeline grid, including gaps and piecewise timing models.
func (s *Service) serveAudio(ctx context.Context, w http.ResponseWriter, r *http.Request, p *Project) {
	start, e := strconv.ParseFloat(r.URL.Query().Get("start"), 64)
	duration, e2 := strconv.ParseFloat(r.URL.Query().Get("duration"), 64)
	var tracks []int
	e3 := json.Unmarshal([]byte(r.URL.Query().Get("tracks")), &tracks)
	maxDuration := 4.0
	if r.URL.Path == "/media/review-audio" {
		maxDuration = maxReviewDuration
	}
	if e != nil || e2 != nil || e3 != nil || math.IsNaN(start) || math.IsNaN(duration) || math.IsInf(start, 0) || start < 0 || start > p.Duration || duration <= 0 || duration > maxDuration || len(tracks) == 0 || len(tracks) > 2 {
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
	defer s.playback.release()
	s.active.Add(1)
	defer s.active.Add(-1)
	const rate = 48000
	out := make([]float32, int(math.Round(duration*rate))*2)
	for _, clip := range planAudition(p, tracks, start, start+duration, r.URL.Path == "/media/audio" && r.URL.Query().Get("fallback") == "1") {
		c := p.Clips[clip.index]
		if c.Missing {
			http.Error(w, "selected track has missing media: "+c.Name, 404)
			return
		}
		lo, hi := clip.spans[0].start, clip.spans[len(clip.spans)-1].end
		inv := autosync.InverseMapping(c.Mapping)
		a, b := autosync.MapTime(inv, lo), autosync.MapTime(inv, hi)
		if b-a > 12 {
			http.Error(w, "timing rate outside audition limits", 422)
			return
		}
		var pcm []float32
		// Decode a guard on both sides so resampler startup/ending transients
		// stay outside adjoining audition blocks. Keep the grid sample-aligned.
		decodeStart := math.Max(0, math.Floor((a-.05)*rate)/rate)
		err := decode(ctx, c.Source, decodeStart, math.Min(c.Duration-decodeStart, b-decodeStart+.05), rate, func(data []byte) error {
			for j := 0; j+3 < len(data); j += 4 {
				pcm = append(pcm, sample(data[j:]))
			}
			return nil
		})
		if err != nil {
			http.Error(w, err.Error(), 422)
			return
		}
		for _, span := range clip.spans {
			first, last := max(0, int(math.Ceil((span.start-start)*rate-1e-7))), min(len(out)/2, int(math.Ceil((span.end-start)*rate-1e-7)))
			for j := first; j < last; j++ {
				src := (autosync.MapTime(inv, start+float64(j)/rate) - decodeStart) * rate
				k := int(math.Floor(src))
				f := float32(src - float64(k))
				if k < 0 || k*2+1 >= len(pcm) {
					continue
				}
				next := min(k+1, len(pcm)/2-1)
				for ch := 0; ch < 2; ch++ {
					out[j*2+ch] += (pcm[k*2+ch]*(1-f) + pcm[next*2+ch]*f) * span.gain
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
