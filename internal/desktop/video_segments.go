package desktop

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"
)

type segmentBuffer struct{ bytes.Buffer }

func (b *segmentBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 16<<20 {
		return 0, fmt.Errorf("preview segment exceeds 16 MiB limit")
	}
	return b.Buffer.Write(p)
}

// Independently decodable display segments give MediaSource an explicit
// forward queue. Source media and synchronization evidence are never modified.
// Keep source frame timestamps (including VFR); only preview resolution/codec
// change. The first-frame timestamp anchors each guarded segment in source time.
func (s *Service) videoSegment(ctx context.Context, w http.ResponseWriter, r *http.Request, c Clip) {
	start, err := strconv.ParseFloat(r.URL.Query().Get("start"), 64)
	if err != nil || math.IsNaN(start) || math.IsInf(start, 0) || start < 0 || start >= c.Duration {
		http.Error(w, "invalid segment time", 400)
		return
	}
	job, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if err = s.playback.acquire(job); err != nil {
		return
	}
	defer s.playback.release()
	s.active.Add(1)
	defer s.active.Add(-1)
	requestStart := start
	start = math.Max(0, start-.2)
	duration := math.Min(requestStart+12.2-start, c.Duration-start)
	cmd, err := command(job, "-hide_banner", "-v", "info", "-nostats", "-nostdin", "-threads", "2", "-filter_threads", "1", "-ss", fmt.Sprintf("%.9f", start), "-i", c.Source, "-t", fmt.Sprintf("%.9f", duration), "-map", "0:v:0", "-an", "-vf", "scale=640:640:force_original_aspect_ratio=decrease:force_divisible_by=2:reset_sar=1,showinfo", "-fps_mode", "passthrough", "-c:v", "libx264", "-profile:v", "baseline", "-level:v", "3.1", "-preset", "ultrafast", "-crf", "23", "-maxrate", "4M", "-bufsize", "8M", "-pix_fmt", "yuv420p", "-threads", "1", "-force_key_frames", "expr:gte(t,n_forced*1)", "-movflags", "frag_keyframe+empty_moov+default_base_moof", "-f", "mp4", "pipe:1")
	if err != nil {
		http.Error(w, err.Error(), 422)
		return
	}
	var data segmentBuffer
	var stderr segmentBuffer
	cmd.Stdout = &data
	cmd.Stderr = &stderr
	if err = cmd.Run(); err != nil {
		http.Error(w, fmt.Sprintf("buffered preview: %v: %.800s", err, stderr.String()), 422)
		return
	}
	if data.Len() == 0 {
		http.Error(w, "no video frames at requested source time", 422)
		return
	}
	pts := thumbnailPTS.FindSubmatch(stderr.Bytes())
	if len(pts) != 2 {
		http.Error(w, "preview frame timestamp unavailable", 422)
		return
	}
	first, e := strconv.ParseFloat(string(pts[1]), 64)
	if e != nil {
		http.Error(w, "invalid preview frame timestamp", 422)
		return
	}
	w.Header().Set("X-OpenSync-Segment-Origin", strconv.FormatFloat(start+first, 'f', 9, 64))
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(data.Len()))
	_, _ = w.Write(data.Bytes())
}
