package autosync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// Opt-in regression for a large final input among many short recordings. It
// measures indexing only; generated source fixtures are removed by testing.
func TestSkewedFingerprintBuckets(t *testing.T) {
	report := os.Getenv("OPENSYNC_SKEW_REPORT")
	if report == "" {
		t.Skip("set OPENSYNC_SKEW_REPORT for uneven-workload benchmark")
	}
	root := t.TempDir()
	records := []MediaRecord{}
	add := func(name string, samples []int16) {
		path := filepath.Join(root, name)
		if err := writeFixtureWAV(path, samples); err != nil {
			t.Fatal(err)
		}
		st, _ := os.Stat(path)
		records = append(records, MediaRecord{ID: shaBytes([]byte(path))[:20], Name: name, Source: path, LocalSource: path, SourceBytes: st.Size(), SourceMtime: st.ModTime().UnixNano(), Duration: float64(len(samples)) / 16000, Audio: map[string]any{"sample_rate": "16000", "channels": 1}})
	}
	short := fixtureNoise(60, 9123)
	for i := 0; i < 50; i++ {
		add(fmt.Sprintf("short-%02d.wav", i), short)
	}
	add("zzz-hour-long.wav", fixtureNoise(3600, 98761))
	runtime.GC()
	w := &nativeWorkspace{data: map[string][]byte{}, limit: 512 << 20, audio: newDirectAudio()}
	defer w.audio.Close()
	ctx := context.WithValue(context.Background(), workspaceKey{}, w)
	started := time.Now()
	hashes, metrics, err := NativeFingerprintIndex(ctx, records, root, false)
	elapsed := time.Since(started).Seconds()
	if err != nil {
		t.Fatal(err)
	}
	if len(hashes) != 51 || metrics[50].Chunks != 72 {
		t.Fatal("hour-long recording did not produce 72 independent fingerprint buckets")
	}
	for i := 0; i < 50; i++ {
		if metrics[i].Chunks != 1 || len(hashes[i]) == 0 {
			t.Fatalf("short file missing: %d", i)
		}
	}
	if w.indexCompleted.Load() != 122 {
		t.Fatal("lost buckets")
	}
	if runtime.GOMAXPROCS(0) > 1 && w.indexPeak.Load() < 2 {
		t.Fatal("bucket work stayed serial")
	}
	result := map[string]any{"short_recordings": 50, "short_seconds": 60, "long_recordings": 1, "long_seconds": 3600, "index_seconds": elapsed, "bucket_count": w.indexCompleted.Load(), "worker_limit": w.indexWorkers.Load(), "peak_active_buckets": w.indexPeak.Load(), "disk_analysis_cache": false, "source_fixtures": "temporary generated PCM16 WAV", "scope": "fingerprint indexing, not full sync"}
	if err := writeJSON(report, result); err != nil {
		t.Fatal(err)
	}
	t.Logf("51 inputs / 122 buckets: %.3fs, peak %d workers", elapsed, w.indexPeak.Load())
}
