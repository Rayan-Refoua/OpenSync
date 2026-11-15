package autosync

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
)

func nativePrepareDirect(ctx context.Context, o NativeSyncOptions, files []string, excluded []map[string]any) ([]MediaRecord, error) {
	if _, err := mediaTool("ffprobe"); err != nil {
		return nil, err
	}
	if _, err := mediaTool("ffmpeg"); err != nil {
		return nil, err
	}
	records := make([]MediaRecord, len(files))
	errs := make([]error, len(files))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < min(2, nativeWorkers(len(files))); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if ctx.Err() != nil {
					continue
				}
				path := files[i]
				st, err := os.Stat(path)
				if err != nil {
					errs[i] = err
					continue
				}
				info, err := probeNative(ctx, path)
				if err != nil {
					errs[i] = fmt.Errorf("unreadable or unsupported media: %w", err)
					continue
				}
				r := MediaRecord{ID: shaBytes([]byte(path))[:20], Name: filepath.Base(path), Source: path, LocalSource: path, SourceBytes: st.Size(), SourceMtime: st.ModTime().UnixNano(), SourceGroup: filepath.Dir(path)}
				streams, _ := info["streams"].([]any)
				for _, raw := range streams {
					s, ok := raw.(map[string]any)
					if !ok {
						continue
					}
					disp, _ := s["disposition"].(map[string]any)
					if s["codec_type"] == "video" && r.Video == nil && number(disp["attached_pic"]) == 0 {
						r.Video = s
					}
					if s["codec_type"] == "audio" && r.Audio == nil {
						r.Audio = s
					}
				}
				if r.Audio == nil && r.Video == nil {
					errs[i] = fmt.Errorf("no readable audio or video stream")
					continue
				}
				format, _ := info["format"].(map[string]any)
				r.FormatTags, _ = format["tags"].(map[string]any)
				r.Duration = number(format["duration"])
				if r.Audio != nil && number(r.Audio["duration"]) > 0 {
					r.Duration = number(r.Audio["duration"])
				}
				if r.Duration <= 0 && r.Video != nil {
					r.Duration = number(r.Video["duration"])
				}
				if r.Audio == nil && (r.Duration <= 0 || math.IsNaN(r.Duration) || math.IsInf(r.Duration, 0)) {
					errs[i] = fmt.Errorf("video has no usable duration")
					continue
				}
				r.AnalysisAudio = "decoded_source_audio"
				if r.Audio == nil {
					r.AnalysisAudio = "synthetic_silence_no_audio"
				}
				// The first sequential decode replaces this identity with the PCM digest
				// and exact decoded duration before matching begins.
				r.CacheSHA = shaBytes([]byte(fmt.Sprintf("%s:%d:%d", path, r.SourceBytes, r.SourceMtime)))
				r.SourceTimecode = nativeSourceTimecode(r)
				if o.Input != "" {
					root, _ := filepath.Abs(o.Input)
					r.SourceGroup, _ = filepath.Rel(root, filepath.Dir(path))
				}
				records[i] = r
			}
		}()
	}
	for i := range files {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	result := []MediaRecord{}
	for i, r := range records {
		if errs[i] != nil {
			excluded = append(excluded, map[string]any{"path": files[i], "reason": errs[i].Error()})
			fmt.Printf("SKIP %s: %v\n", files[i], errs[i])
			continue
		}
		result = append(result, r)
	}
	if err := writeJSON(filepath.Join(o.Output, "input-report.json"), map[string]any{"included": len(result), "excluded": excluded}); err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("no readable recordings found; see input-report.json")
	}
	fmt.Printf("INPUT %d readable recordings; %d skipped; persistent cache %t\n", len(result), len(excluded), o.CacheEnabled)
	return result, nil
}
