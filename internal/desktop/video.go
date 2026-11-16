package desktop

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"opensync/internal/storageio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func (s *Service) SetPlaybackWorkers(n int) { s.playback.set(max(1, min(8, n))) }

// Handler keeps browser-native, hardware-capable media playback inside Wails'
// asset transport. Only recordings in the opened project are addressable.
func (s *Service) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/media/source" && r.URL.Path != "/media/preview" && r.URL.Path != "/media/segment" {
			s.ServeHTTP(w, r)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			http.Error(w, "GET or HEAD required", 405)
			return
		}
		s.mu.RLock()
		p, life := s.project, s.ctx
		s.mu.RUnlock()
		g, err := strconv.ParseUint(r.URL.Query().Get("g"), 10, 64)
		idx, e := strconv.Atoi(r.URL.Query().Get("clip"))
		if err != nil || p == nil || g != p.Generation {
			http.Error(w, "stale project", 409)
			return
		}
		if e != nil || idx < 0 || idx >= len(p.Clips) {
			http.Error(w, "invalid recording", 400)
			return
		}
		c := p.Clips[idx]
		if c.Missing || !c.Video {
			http.Error(w, "video source unavailable", 404)
			return
		}
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		stop := context.AfterFunc(life, cancel)
		defer stop()
		previewCtx, finishPreview, gateErr := s.preview.enter(ctx)
		if gateErr != nil {
			http.Error(w, gateErr.Error(), http.StatusServiceUnavailable)
			return
		}
		defer finishPreview()
		ctx = previewCtx
		r = r.WithContext(ctx)
		w.Header().Set("Cache-Control", "no-store")
		if r.URL.Path == "/media/segment" {
			s.videoSegment(ctx, w, r, c)
			return
		}
		if r.URL.Path == "/media/source" {
			f, e := storageio.Open(ctx, c.Source)
			if e != nil {
				http.Error(w, e.Error(), 404)
				return
			}
			defer f.Close()
			st, e := f.Stat()
			if e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
			switch strings.ToLower(filepath.Ext(c.Source)) {
			case ".mp4", ".m4v", ".mov":
				w.Header().Set("Content-Type", "video/mp4")
			case ".webm":
				w.Header().Set("Content-Type", "video/webm")
			case ".mkv":
				w.Header().Set("Content-Type", "video/x-matroska")
			}
			serveVideoRange(w, r, f, st)
			return
		}
		start, e := strconv.ParseFloat(r.URL.Query().Get("start"), 64)
		if e != nil || math.IsNaN(start) || math.IsInf(start, 0) || start < 0 || start >= c.Duration {
			http.Error(w, "invalid preview time", 400)
			return
		}
		if r.Method == "HEAD" {
			w.Header().Set("Content-Type", "video/mp4")
			return
		}
		// Unsupported native formats use a bounded, temporary-in-memory preview.
		// No proxy files are written; this fallback is clearly labeled in the UI.
		job, done := context.WithTimeout(ctx, 60*time.Second)
		defer done()
		if err = s.playback.acquire(job); err != nil {
			return
		}
		defer s.playback.release()
		s.active.Add(1)
		defer s.active.Add(-1)
		cmd, err := command(job, "-v", "error", "-nostdin", "-threads", "1", "-filter_threads", "1", "-ss", fmt.Sprintf("%.9f", start), "-i", c.Source, "-t", fmt.Sprintf("%.6f", math.Min(20, c.Duration-start)), "-map", "0:v:0", "-an", "-vf", "scale=640:360:force_original_aspect_ratio=decrease:force_divisible_by=2,setsar=1", "-r", "25", "-c:v", "libx264", "-preset", "ultrafast", "-crf", "25", "-pix_fmt", "yuv420p", "-threads", "1", "-g", "25", "-movflags", "frag_keyframe+empty_moov+default_base_moof", "-f", "mp4", "pipe:1")
		if err != nil {
			http.Error(w, err.Error(), 422)
			return
		}
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		pipe, err := cmd.StdoutPipe()
		if err != nil {
			http.Error(w, err.Error(), 422)
			return
		}
		if err = cmd.Start(); err != nil {
			http.Error(w, err.Error(), 422)
			return
		}
		// Check the first bytes before committing a successful HTTP response.
		first := make([]byte, 1024)
		n, e := pipe.Read(first)
		if n == 0 {
			_ = cmd.Wait()
			http.Error(w, "preview encoder failed: "+stderr.String(), 422)
			return
		}
		w.Header().Set("Content-Type", "video/mp4")
		if _, e = w.Write(first[:n]); e == nil {
			_, e = io.Copy(w, pipe)
		}
		if e != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	})
}

// Wails' Windows asset writer buffers a response before handing it to
// WebView2. Open-ended multi-GB responses would exhaust memory and starve
// simultaneous video requests. Serve bounded original-byte ranges instead.
func serveVideoRange(w http.ResponseWriter, r *http.Request, f io.ReadSeeker, st os.FileInfo) {
	const block int64 = 4 << 20
	size := st.Size()
	start, end := int64(0), size-1
	invalid := func() {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", size))
		http.Error(w, "invalid byte range", http.StatusRequestedRangeNotSatisfiable)
	}
	if value := r.Header.Get("Range"); value != "" {
		if !strings.HasPrefix(value, "bytes=") || strings.Contains(value, ",") {
			invalid()
			return
		}
		parts := strings.SplitN(strings.TrimPrefix(value, "bytes="), "-", 2)
		if len(parts) != 2 {
			invalid()
			return
		}
		var err error
		if parts[0] == "" {
			var tail int64
			tail, err = strconv.ParseInt(parts[1], 10, 64)
			if err != nil || tail <= 0 {
				invalid()
				return
			}
			start = max(0, size-tail)
		} else {
			start, err = strconv.ParseInt(parts[0], 10, 64)
			if err != nil {
				invalid()
				return
			}
			if parts[1] != "" {
				end, err = strconv.ParseInt(parts[1], 10, 64)
				if err != nil {
					invalid()
					return
				}
			}
		}
		if start < 0 || start >= size || end < start {
			invalid()
			return
		}
	}
	if size <= 0 {
		invalid()
		return
	}
	end = min(end, size-1, start+block-1)
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, size))
	w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.WriteHeader(http.StatusPartialContent)
	if r.Method != "HEAD" {
		_, _ = io.CopyN(w, f, end-start+1)
	}
}
