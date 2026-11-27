package autosync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"opensync/internal/audio"
	"opensync/internal/storageio"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type NativeSyncOptions struct {
	Analysis                  AnalysisSettings
	TrackLayout               string
	KeepTrackOrder            bool
	OmitProjectMarker         bool
	AudioEarlyMS, AudioLateMS float64
	// ManagedMemoryMiB bounds persistent in-memory analysis/audio working sets.
	// Zero preserves the CLI's adaptive defaults; temporary DSP allocations are
	// additionally governed by the embedding application's Go memory target.
	ManagedMemoryMiB                                    int
	FileTimePlacement                                   *bool
	Mode                                                string
	Corrections                                         string
	reviewLedger                                        *ReviewLedger
	Input                                               string
	Sources, Files, Folders                             []string
	FileList, Output, CacheIndex, Name, Unsynced, Stage string
	MaxResidualMS                                       float64
	Resume, IncludeProvisional, CacheEnabled            bool
}

func mediaPathLess(a, b string) bool {
	left := strings.Split(strings.ToLower(filepath.ToSlash(a)), "/")
	right := strings.Split(strings.ToLower(filepath.ToSlash(b)), "/")
	for i := 0; i < min(len(left), len(right)); i++ {
		if left[i] != right[i] {
			return left[i] < right[i]
		}
	}
	return len(left) < len(right)
}

func mediaTool(name string) (string, error) {
	if exe, err := os.Executable(); err == nil {
		for _, suffix := range []string{".exe", ""} {
			path := filepath.Join(filepath.Dir(exe), "runtime", "bin", name+suffix)
			if st, err := os.Stat(path); err == nil && !st.IsDir() {
				return path, nil
			}
		}
	}
	return exec.LookPath(name)
}
func runMediaTool(ctx context.Context, name string, args ...string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	tool, err := mediaTool(name)
	if err != nil {
		return err
	}
	cmd := mediaCommandContext(ctx, tool, args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err = cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w: %s", name, err, stderr.String())
	}
	return nil
}
func probeNative(ctx context.Context, path string) (map[string]any, error) {
	tool, err := mediaTool("ffprobe")
	if err != nil {
		return nil, err
	}
	limited, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	data, err := mediaCommandContext(limited, tool, "-v", "error", "-show_format", "-show_streams", "-of", "json", path).Output()
	if err != nil {
		return nil, err
	}
	object, err := rawObject(data)
	if format, ok := object["format"].(map[string]any); ok {
		format["filename"] = path
	}
	return object, err
}
func hashFile(path string) (string, error) {
	f, err := storageio.Open(context.Background(), path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.CopyBuffer(h, f, make([]byte, 1<<20)); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func nativeInputFiles(o NativeSyncOptions) ([]string, []map[string]any, error) {
	explicit := append([]string(nil), o.Files...)
	if o.FileList != "" {
		manifest, err := filepath.Abs(o.FileList)
		if err != nil {
			return nil, nil, err
		}
		data, err := os.ReadFile(manifest)
		if err != nil {
			return nil, nil, err
		}
		content := strings.TrimPrefix(string(data), "\ufeff")
		var listed []string
		if strings.EqualFold(filepath.Ext(manifest), ".json") {
			if err = json.Unmarshal([]byte(content), &listed); err != nil {
				return nil, nil, err
			}
		} else {
			for _, line := range strings.Split(content, "\n") {
				if v := strings.TrimSpace(line); v != "" {
					listed = append(listed, v)
				}
			}
		}
		for _, p := range listed {
			if !filepath.IsAbs(p) {
				p = filepath.Join(filepath.Dir(manifest), p)
			}
			explicit = append(explicit, p)
		}
	}
	files := []string{}
	excluded := []map[string]any{}
	explicit = append(explicit, o.Folders...)
	if o.Input != "" {
		root, err := filepath.Abs(o.Input)
		if err != nil {
			return nil, nil, err
		}
		if len(o.Sources) == 0 {
			explicit = append(explicit, root)
		}
		for _, folder := range o.Sources {
			selected := filepath.Join(root, folder)
			if !pathWithin(root, selected) {
				return nil, nil, fmt.Errorf("source directory outside input root")
			}
			explicit = append(explicit, selected)
		}
	} else if len(o.Sources) > 0 {
		return nil, nil, fmt.Errorf("--source requires --input")
	}
	if len(explicit) == 0 {
		return nil, nil, fmt.Errorf("supply --files, --folders, --file-list, or --input")
	}
	output, _ := filepath.Abs(o.Output)
	seen := map[string]bool{}
	selectedFiles := map[string]bool{}
	for _, selected := range explicit {
		root, err := filepath.Abs(selected)
		if err != nil {
			return nil, nil, err
		}
		if _, err = os.Stat(root); err != nil {
			return nil, nil, fmt.Errorf("input %s: %w", root, err)
		}
		if pathWithin(output, root) {
			return nil, nil, fmt.Errorf("input is inside output directory: %s", root)
		}
		err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				excluded = append(excluded, map[string]any{"path": path, "reason": walkErr.Error()})
				return nil
			}
			if pathWithin(output, path) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.Type()&os.ModeSymlink != 0 {
				excluded = append(excluded, map[string]any{"path": path, "reason": "symbolic link skipped to avoid duplicate media or directory cycles"})
				return nil
			}
			if !d.IsDir() && strings.HasSuffix(strings.ToLower(d.Name()), "_driftcorrection.wav") {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if !d.Type().IsRegular() {
				excluded = append(excluded, map[string]any{"path": path, "reason": "not a regular file"})
				return nil
			}
			key := strings.ToLower(filepath.Clean(path))
			if path == root {
				selectedFiles[key] = true
			}
			if !seen[key] {
				seen[key] = true
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			return nil, nil, err
		}
	}
	media := files[:0]
	for _, path := range files {
		key := strings.ToLower(filepath.Clean(path))
		if !selectedFiles[key] && strings.HasSuffix(key, "t01.jpg") {
			stem := strings.TrimSuffix(key, "t01.jpg")
			movieStem := stem
			if folder := filepath.Dir(key); filepath.Base(folder) == "thmbnl" {
				movieStem = filepath.Join(filepath.Dir(folder), "clip", filepath.Base(stem))
			}
			if seen[stem+".mp4"] || seen[stem+".mov"] || seen[stem+".mxf"] ||
				seen[movieStem+".mp4"] || seen[movieStem+".mov"] || seen[movieStem+".mxf"] {
				excluded = append(excluded, map[string]any{"path": path, "reason": "camera thumbnail sidecar with matching video"})
				continue
			}
		}
		media = append(media, path)
	}
	files = media
	sort.SliceStable(files, func(i, j int) bool { return mediaPathLess(files[i], files[j]) })
	if len(files) == 0 {
		return nil, excluded, fmt.Errorf("empty source selection")
	}
	return files, excluded, nil
}
func pathWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
}
func nativePrepare(ctx context.Context, o NativeSyncOptions, files []string, excluded []map[string]any) ([]MediaRecord, error) {
	if !o.CacheEnabled {
		return nativePrepareDirect(ctx, o, files, excluded)
	}
	folder := filepath.Join(o.Output, "cache")
	if err := os.MkdirAll(folder, 0755); err != nil {
		return nil, err
	}
	old := map[string]MediaRecord{}
	prepared := map[string]MediaRecord{}
	if !o.Resume && o.CacheIndex == "" {
		usable, err := nativePrepareDirect(ctx, o, files, excluded)
		if err != nil {
			return nil, err
		}
		files = nil
		for _, r := range usable {
			files = append(files, r.Source)
			prepared[r.Source] = r
		}
		// Preserve the scan report in the resume inventory as well.
		if data, err := os.ReadFile(filepath.Join(o.Output, "input-report.json")); err == nil {
			var report struct{ Excluded []map[string]any }
			if json.Unmarshal(data, &report) == nil {
				excluded = report.Excluded
			}
		}
	}
	if o.CacheIndex != "" {
		data, err := os.ReadFile(o.CacheIndex)
		if err != nil {
			return nil, err
		}
		var index map[string]MediaRecord
		if err = json.Unmarshal(data, &index); err != nil {
			return nil, err
		}
		for _, r := range index {
			p, _ := filepath.Abs(r.Source)
			old[strings.ToLower(p)] = r
		}
	}
	records := make([]MediaRecord, len(files))
	errs := make([]error, len(files))
	type job struct {
		index  int
		record MediaRecord
		state  string
	}
	jobs := make(chan job, 2)
	var wg sync.WaitGroup
	for worker := 0; worker < min(4, nativeWorkers(len(files))); worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				if ctx.Err() != nil {
					continue
				}
				r := j.record
				var info map[string]any
				var err error
				if r.Video == nil && r.Audio == nil {
					info, err = probeNative(ctx, r.LocalSource)
					if err != nil {
						errs[j.index] = err
						continue
					}
				}
				streams, _ := info["streams"].([]any)
				if format, ok := info["format"].(map[string]any); ok {
					r.FormatTags, _ = format["tags"].(map[string]any)
				}
				for _, raw := range streams {
					s, ok := raw.(map[string]any)
					if !ok {
						continue
					}
					disposition, _ := s["disposition"].(map[string]any)
					switch s["codec_type"] {
					case "video":
						if r.Video == nil && number(disposition["attached_pic"]) == 0 {
							r.Video = s
						}
					case "audio":
						if r.Audio == nil {
							r.Audio = s
						}
					}
				}
				if r.Video == nil && r.Audio == nil {
					errs[j.index] = fmt.Errorf("no usable streams in %s", r.Source)
					continue
				}
				if !r.CacheReused {
					temporary := r.Cache + ".partial.wav"
					args := []string{"-nostdin", "-v", "error", "-y", "-threads", "1"}
					if r.Audio != nil {
						args = append(args, "-i", r.LocalSource, "-map", "0:a:0", "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", temporary)
						r.AnalysisAudio = "decoded_source_audio"
					} else {
						format, _ := info["format"].(map[string]any)
						duration := number(format["duration"])
						if duration <= 0 {
							duration = r.Duration
						}
						args = append(args, "-f", "lavfi", "-i", "anullsrc=r=16000:cl=mono", "-t", fmt.Sprintf("%.12f", duration), "-c:a", "pcm_s16le", temporary)
						r.AnalysisAudio = "synthetic_silence_no_audio"
					}
					err = runMediaTool(ctx, "ffmpeg", args...)
					if err == nil {
						err = os.Rename(temporary, r.Cache)
					}
					if err != nil {
						errs[j.index] = err
						continue
					}
				}
				pcm, err := audio.Open(r.Cache)
				if err != nil {
					errs[j.index] = err
					continue
				}
				if pcm.SampleRate() != 16000 {
					pcm.Close()
					errs[j.index] = fmt.Errorf("analysis cache must be 16kHz mono PCM16")
					continue
				}
				r.Duration = float64(pcm.Frames()) / 16000
				pcm.Close()
				previousDigest := r.CacheSHA
				r.CacheSHA, err = hashFile(r.Cache)
				if err != nil {
					errs[j.index] = err
					continue
				}
				if previousDigest != "" && r.CacheSHA != previousDigest {
					errs[j.index] = fmt.Errorf("analysis cache digest changed: %s", r.Name)
					continue
				}
				r.SourceTimecode = nativeSourceTimecode(r)
				err = writeJSON(j.state, r)
				if err != nil {
					errs[j.index] = err
					continue
				}
				records[j.index] = r
				fmt.Printf("CACHE %d/%d ready %s (%.2fs)\n", j.index+1, len(files), r.Name, r.Duration)
			}
		}()
	}
	var prepareErr error
	for i, path := range files {
		if ctx.Err() != nil {
			prepareErr = ctx.Err()
			break
		}
		st, err := os.Stat(path)
		if err != nil {
			prepareErr = err
			break
		}
		id := shaBytes([]byte(path))[:20]
		state := filepath.Join(folder, id+".json")
		if data, err := os.ReadFile(state); err == nil {
			var r MediaRecord
			if json.Unmarshal(data, &r) != nil || r.Source != path || r.SourceBytes != st.Size() || r.SourceMtime != st.ModTime().UnixNano() {
				prepareErr = fmt.Errorf("stale source cache: %s", path)
				break
			}
			if _, err = os.Stat(r.Cache); err != nil {
				prepareErr = err
				break
			}
			records[i] = r
			continue
		}
		r := MediaRecord{ID: id, Name: filepath.Base(path), Source: path, LocalSource: path, Cache: filepath.Join(folder, id+".wav"), SourceBytes: st.Size(), SourceMtime: st.ModTime().UnixNano(), SourceGroup: filepath.Dir(path)}
		if p, ok := prepared[path]; ok {
			r.Video = p.Video
			r.Audio = p.Audio
			r.Duration = p.Duration
			r.SourceTimecode = p.SourceTimecode
			r.FormatTags = p.FormatTags
		}
		if o.Input != "" {
			root, _ := filepath.Abs(o.Input)
			r.SourceGroup, _ = filepath.Rel(root, filepath.Dir(path))
		}
		if prior, ok := old[strings.ToLower(path)]; ok && prior.SourceBytes == r.SourceBytes && prior.SourceMtime == r.SourceMtime {
			if _, err = os.Stat(prior.Cache); err == nil {
				r.Cache = prior.Cache
				r.CacheReused = true
				r.Video = prior.Video
				r.Audio = prior.Audio
				r.SourceTimecode = prior.SourceTimecode
				r.FormatTags = prior.FormatTags
				r.CacheSHA = prior.CacheSHA
				r.AnalysisAudio = prior.AnalysisAudio
				if _, err = os.Stat(prior.LocalSource); err == nil {
					r.LocalSource = prior.LocalSource
				}
			}
		}
		// Even opt-in caching stores analysis only, never a duplicate original.
		r.LocalSource = path
		jobs <- job{i, r, state}
	}
	close(jobs)
	wg.Wait()
	if prepareErr != nil {
		return nil, prepareErr
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	err := writeJSON(filepath.Join(o.Output, "inventory.json"), map[string]any{"version": "opensync-autonomous-v1", "root": o.Input, "records": records, "excluded": excluded, "source_hash_policy": "size and mtime guarded; analysis PCM SHA256; no full rescan of original HDD media"})
	return records, err
}
