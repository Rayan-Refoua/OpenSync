package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/png"
	"math"
	"net/http/httptest"
	"opensync/internal/autosync"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPeakStoreCoalescesAndRetainsAbandonedViewportWork(t *testing.T) {
	store := newPeakStore(1 << 20)
	life, cancelLife := context.WithCancel(context.Background())
	defer cancelLife()
	started, release := make(chan struct{}), make(chan struct{})
	var builds atomic.Int64
	build := func(ctx context.Context) (*peakChunk, error) {
		if builds.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
			return &peakChunk{levels: [][]peakPair{{{-.5, .5}}}, bytes: 32}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	viewport, cancelViewport := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() { _, err := store.load(viewport, life, "source/chunk", build); first <- err }()
	<-started
	cancelViewport()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatal("viewport cancellation not returned", err)
	}
	var wg sync.WaitGroup
	errCh := make(chan error, 24)
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := store.load(context.Background(), life, "source/chunk", build)
			if err == nil && (c == nil || c.levels[0][0].max != .5) {
				err = fmt.Errorf("wrong shared chunk")
			}
			errCh <- err
		}()
	}
	close(release)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	if builds.Load() != 1 {
		t.Fatalf("same source decoded %d times", builds.Load())
	}
	if _, err := store.load(context.Background(), life, "source/chunk", build); err != nil {
		t.Fatal(err)
	}
	if builds.Load() != 1 {
		t.Fatal("completed source chunk was not retained")
	}
}

func TestProjectCancellationStopsChunkAndNewProjectCanRetry(t *testing.T) {
	store := newPeakStore(1 << 20)
	old, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		_, e := store.load(context.Background(), old, "key", func(ctx context.Context) (*peakChunk, error) { close(started); <-ctx.Done(); return nil, ctx.Err() })
		errCh <- e
	}()
	<-started
	cancel()
	if !errors.Is(<-errCh, context.Canceled) {
		t.Fatal("project cancellation did not stop work")
	}
	_, err := store.load(context.Background(), context.Background(), "key", func(context.Context) (*peakChunk, error) { return &peakChunk{bytes: 32}, nil })
	if err != nil {
		t.Fatal("new project could not retry", err)
	}
}

func TestVisualizationLimitChangesEnforceAdmissionAndEviction(t *testing.T) {
	l := newMediaLimiter(2)
	ctx := context.Background()
	if err := l.acquire(ctx); err != nil {
		t.Fatal(err)
	}
	if err := l.acquire(ctx); err != nil {
		t.Fatal(err)
	}
	l.set(1)
	limited, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()
	if err := l.acquire(limited); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("lower limit did not block new job", err)
	}
	l.release()
	limited2, cancel2 := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel2()
	if err := l.acquire(limited2); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("one active job should fill lower limit", err)
	}
	l.release()
	if err := l.acquire(ctx); err != nil {
		t.Fatal(err)
	}
	l.release()
	s := NewService()
	defer s.Close()
	s.cache.put("large", make([]byte, 8<<20))
	if err := s.SetVisualizationLimits(16, 1, 1); err != nil {
		t.Fatal(err)
	}
	if b := s.Stats()["cacheBytes"].(int); b > 16<<20 {
		t.Fatal("cache budget not enforced", b)
	}
	if _, ok := s.cache.get("large"); ok {
		t.Fatal("response cache did not evict when its share shrank")
	}
	if s.SetVisualizationLimits(1, 2, 1) == nil || s.SetVisualizationLimits(64, 0, 1) == nil {
		t.Fatal("invalid resource limits accepted")
	}
}

func TestWaveformPyramidReusesSourceAcrossZoomAndReopen(t *testing.T) {
	if os.Getenv("OPENSYNC_INTEGRATION") != "1" {
		t.Skip("FFmpeg integration")
	}
	path := testWAV(t)
	s := NewService()
	defer s.Close()
	c := Clip{Source: path, Duration: 1, Audio: true}
	base, err := s.waveFromPeaks(context.Background(), s.ctx, c, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if s.peaks.decodes.Load() != 1 {
		t.Fatal("unexpected first decode count")
	}
	for _, level := range []int{1, 2, 3, 2, 0, 4, 1, 0} {
		b, err := s.waveFromPeaks(context.Background(), s.ctx, c, level, 0)
		if err != nil {
			t.Fatal(err)
		}
		if s.peaks.decodes.Load() != 1 {
			t.Fatalf("zoom level %d decoded the source again", level)
		}
		count := 1 << level
		for i := 0; i < min(4, 512/count); i++ {
			lo, hi := float32(math.Inf(1)), float32(math.Inf(-1))
			for j := 0; j < count; j++ {
				lo = min(lo, sample(base[(i*count+j)*8:]))
				hi = max(hi, sample(base[(i*count+j)*8+4:]))
			}
			if sample(b[i*8:]) != lo || sample(b[i*8+4:]) != hi {
				t.Fatalf("level %d is not exact min/max reduction at bin %d", level, i)
			}
		}
	}
	s.SetProject(&Project{Name: "Reopened same media"})
	if _, err = s.waveFromPeaks(context.Background(), s.ctx, c, 0, 0); err != nil {
		t.Fatal(err)
	}
	if s.peaks.decodes.Load() != 1 {
		t.Fatal("reopening discarded reusable source peaks")
	}
	st, _ := os.Stat(path)
	changed := st.ModTime().Add(time.Second)
	if err = os.Chtimes(path, changed, changed); err != nil {
		t.Fatal(err)
	}
	if _, err = s.waveFromPeaks(context.Background(), s.ctx, c, 0, 0); err != nil {
		t.Fatal(err)
	}
	if s.peaks.decodes.Load() != 2 {
		t.Fatal("changed source identity reused stale peaks")
	}
}

func TestPNGThumbnailHandlesOddHighBitDepthPortraitAndEOFFallback(t *testing.T) {
	if os.Getenv("OPENSYNC_INTEGRATION") != "1" {
		t.Skip("FFmpeg integration")
	}
	path := filepath.Join(t.TempDir(), "portrait.mkv")
	cmd, err := command(context.Background(), "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=size=159x281:rate=25:duration=0.4", "-c:v", "ffv1", "-pix_fmt", "yuv444p10le", path)
	if err != nil {
		t.Fatal(err)
	}
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatal(e, string(b))
	}
	s := NewService()
	defer s.Close()
	c := Clip{Name: "portrait", Source: path, Duration: 1, Video: true, Metadata: autosync.MediaRecord{Video: map[string]any{"duration": .4, "r_frame_rate": "25/1"}}}
	r := httptest.NewRequest("GET", "/media/thumb?time=0.2", nil)
	b, err := s.thumbnail(context.Background(), r, c)
	if err != nil {
		t.Fatal(err)
	}
	config, format, err := image.DecodeConfig(strings.NewReader(string(b)))
	if err != nil || format != "png" || config.Width != 160 || config.Height != 90 {
		t.Fatal("wrong thumbnail", config, format, err)
	}
	p := s.SetProject(&Project{Clips: []Clip{c}})
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest("GET", fmt.Sprintf("/media/thumb?g=%d&clip=0&time=0.9", p.Generation), nil))
		if w.Code != 200 {
			t.Fatal("valid last video frame unavailable", w.Code, w.Body.String())
		}
		actual := metadataFloat(w.Header().Get("X-OpenSync-Frame-Time"))
		if actual <= 0 || actual >= .4 {
			t.Fatal("fallback mislabeled source timestamp", actual)
		}
		if w.Header().Get("Content-Type") != "image/png" {
			t.Fatal("wrong PNG MIME")
		}
	}
	c.Source = testWAV(t)
	if _, err = s.thumbnail(context.Background(), httptest.NewRequest("GET", "/media/thumb?time=0.2", nil), c); err == nil {
		t.Fatal("missing video stream concealed")
	}
}

// Optional read-only real-source probe. Output goes only to the dedicated
// artifact path chosen by the caller; generated data never sits with media.
func TestRealMediaWaveformReuseAndThumbnails(t *testing.T) {
	path := os.Getenv("OPENSYNC_MEDIA_MANIFEST")
	if path == "" {
		t.Skip("set OPENSYNC_MEDIA_MANIFEST for read-only real-media probes")
	}
	var manifest struct {
		Files []struct {
			Index    int              `json:"index"`
			Path     string           `json:"path"`
			Duration float64          `json:"duration"`
			Video    []map[string]any `json:"video"`
			Audio    []map[string]any `json:"audio"`
		} `json:"files"`
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	s := NewService()
	defer s.Close()
	reports := []map[string]any{}
	for _, f := range manifest.Files {
		st, err := os.Stat(f.Path)
		if err != nil {
			t.Fatal(err)
		}
		c := Clip{Name: filepath.Base(f.Path), Source: f.Path, Duration: f.Duration, Video: len(f.Video) > 0, Audio: len(f.Audio) > 0, Metadata: autosync.MediaRecord{SourceBytes: st.Size(), SourceMtime: st.ModTime().UnixNano()}}
		if c.Video {
			c.Metadata.Video = f.Video[0]
		}
		if c.Audio {
			c.Metadata.Audio = f.Audio[0]
		}
		r := map[string]any{"index": f.Index, "path": f.Path, "waveform": "no audio", "thumbnails": []map[string]any{}}
		if c.Audio {
			before := s.peaks.decodes.Load()
			start := time.Now()
			_, e := s.waveFromPeaks(context.Background(), s.ctx, c, 0, 0)
			first := s.peaks.decodes.Load() - before
			if e != nil {
				r["waveform"] = e.Error()
				t.Errorf("%s wave: %v", c.Name, e)
			} else {
				r["waveform"] = "pass"
				r["first_chunk_seconds"] = time.Since(start).Seconds()
				r["first_chunk_decodes"] = first
				for _, level := range []int{1, 2, 3, 2, 0} {
					if _, e = s.waveFromPeaks(context.Background(), s.ctx, c, level, 0); e != nil {
						t.Errorf("%s zoom: %v", c.Name, e)
					}
				}
				r["extra_zoom_decodes"] = s.peaks.decodes.Load() - before - first
				if s.peaks.decodes.Load()-before-first != 0 {
					t.Errorf("%s zoom reread source", c.Name)
				}
			}
		}
		if c.Video {
			thumbs := []map[string]any{}
			for _, at := range []float64{min(.2, c.Duration/4), c.Duration / 2, max(0, c.Duration-.05)} {
				row := map[string]any{"time": at}
				b, actual, e := s.thumbnailFrame(context.Background(), httptest.NewRequest("GET", fmt.Sprintf("/media/thumb?time=%.9f", at), nil), c)
				if e == nil && !math.IsNaN(actual) {
					row["actual_frame_time"] = actual
				}
				if e == nil {
					cfg, format, ie := image.DecodeConfig(strings.NewReader(string(b)))
					if ie != nil || cfg.Width != 160 || cfg.Height != 90 || format != "png" {
						e = fmt.Errorf("invalid PNG thumbnail: %v", ie)
					}
				}
				if e != nil {
					row["status"] = "fail"
					row["error"] = e.Error()
					t.Errorf("%s thumb %.3f: %v", c.Name, at, e)
				} else {
					row["status"] = "pass"
					row["bytes"] = len(b)
				}
				thumbs = append(thumbs, row)
			}
			r["thumbnails"] = thumbs
		}
		after, e := os.Stat(f.Path)
		r["source_unchanged"] = e == nil && after.Size() == st.Size() && after.ModTime() == st.ModTime()
		reports = append(reports, r)
	}
	if out := os.Getenv("OPENSYNC_MEDIA_RESULTS"); out != "" {
		b, e := json.MarshalIndent(reports, "", "  ")
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(out, b, 0600); e != nil {
			t.Fatal(e)
		}
	}
}
