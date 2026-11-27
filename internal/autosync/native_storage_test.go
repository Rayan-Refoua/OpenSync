package autosync

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestFolderInputsRecurseDeduplicateAndExcludeOutput(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "nested", "drift_corrected")
	out := filepath.Join(root, "output")
	for _, dir := range []string{nested, out} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{filepath.Join(root, "first.wav"), filepath.Join(nested, "readable.unknown"), filepath.Join(nested, "notes.txt"), filepath.Join(out, "OpenSync.xml")} {
		os.WriteFile(p, []byte("fixture"), 0600)
	}
	files, _, err := nativeInputFiles(NativeSyncOptions{Folders: []string{root, nested}, Files: []string{filepath.Join(root, "first.wav")}, Output: out})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 {
		t.Fatalf("expected three distinct scan candidates, got %v", files)
	}
	for _, p := range files {
		if pathWithin(out, p) {
			t.Fatal("output selected as input")
		}
	}
}

func TestAnalysisWorkspaceCannotReadOrWriteDisk(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "cache", "evidence.json")
	ctx := context.WithValue(context.Background(), workspaceKey{}, &nativeWorkspace{data: map[string][]byte{}, limit: 8, audio: newDirectAudio()})
	if err := workspaceMkdir(ctx, filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := workspacePut(ctx, path, []byte("1234")); err != nil {
		t.Fatal(err)
	}
	if err := workspacePut(ctx, path+"2", []byte("12345")); err == nil {
		t.Fatal("memory limit silently exceeded")
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatal("memory workspace created a disk directory")
	}
	disk := filepath.Join(root, "old.json")
	os.WriteFile(disk, []byte("old cached evidence"), 0600)
	if _, err := workspaceRead(ctx, disk); !os.IsNotExist(err) {
		t.Fatal("no-cache mode reused an existing disk file")
	}
}

func TestDirectAndCachedPCMAndFingerprintsAgree(t *testing.T) {
	root := integrationFolder(t)
	source := filepath.Join(root, "source.wav")
	// Multiple windows, fractional tail, negative polarity, and resampling.
	if err := writeFixtureWAV(source, fixtureNoise(112, 5566)[:111*16000+813]); err != nil {
		t.Fatal(err)
	}
	encoded := filepath.Join(root, "stereo-44100.flac")
	if err := runMediaTool(context.Background(), "ffmpeg", "-v", "error", "-y", "-i", source, "-ar", "44100", "-ac", "2", encoded); err != nil {
		t.Fatal(err)
	}
	directOut := filepath.Join(root, "direct")
	cachedOut := filepath.Join(root, "cached")
	os.MkdirAll(directOut, 0755)
	os.MkdirAll(cachedOut, 0755)
	ctx := context.WithValue(context.Background(), workspaceKey{}, &nativeWorkspace{data: map[string][]byte{}, limit: 64 << 20, audio: newDirectAudio()})
	defer func() { memoryWorkspace(ctx).audio.Close() }()
	direct, err := nativePrepareDirect(ctx, NativeSyncOptions{Output: directOut}, []string{encoded}, nil)
	if err != nil {
		t.Fatal(err)
	}
	dh, _, err := NativeFingerprintIndex(ctx, direct, directOut, false)
	if err != nil {
		t.Fatal(err)
	}
	cached, err := nativePrepare(context.Background(), NativeSyncOptions{Output: cachedOut, CacheEnabled: true}, []string{encoded}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ch, _, err := NativeFingerprintIndex(context.Background(), cached, cachedOut, false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(dh, ch) {
		t.Fatal("direct-source fingerprint coverage differs from cached PCM")
	}
	if direct[0].Duration != cached[0].Duration {
		t.Fatal("decoded durations differ")
	}
	a := NewNativeAnalyzer(direct)
	a.ctx = ctx
	defer a.Close()
	b := NewNativeAnalyzer(cached)
	defer b.Close()
	// Force the streaming window path, rather than the small-clip RAM working set.
	memoryWorkspace(ctx).audio.Close()
	memoryWorkspace(ctx).audio = newDirectAudio()
	for _, start := range []float64{0, 20.136, 60.123, 108.001} {
		x, e := a.read(0, start, 1.87)
		if e != nil {
			t.Fatal(e)
		}
		y, e := b.read(0, start, 1.87)
		if e != nil {
			t.Fatal(e)
		}
		if !reflect.DeepEqual(x, y) {
			t.Fatalf("direct window changed PCM at %.3f", start)
		}
	}
	dpath := filepath.Join(directOut, "features")
	cpath := filepath.Join(cachedOut, "features")
	if err = distributedFeaturesForRecord(ctx, direct[0], dpath); err != nil {
		t.Fatal(err)
	}
	if err = distributedFeaturesForRecord(context.Background(), cached[0], cpath); err != nil {
		t.Fatal(err)
	}
	dx, _ := workspaceRead(ctx, dpath)
	cx, _ := os.ReadFile(cpath)
	if !reflect.DeepEqual(dx, cx) {
		t.Fatal("distributed feature conversion changed")
	}
	files, err := os.ReadDir(filepath.Join(cachedOut, "cache"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if filepath.Ext(f.Name()) != ".wav" && filepath.Ext(f.Name()) != ".json" {
			t.Fatal("opt-in cache copied original media")
		}
	}
	entries, _ := os.ReadDir(directOut)
	if len(entries) != 1 || entries[0].Name() != "input-report.json" {
		t.Fatalf("default analysis wrote intermediates: %v", entries)
	}
}

func TestAudioWorkingBudgetAdaptsAndStaysBounded(t *testing.T) {
	if audioWorkingBudget(64<<30) != 4<<30 {
		t.Fatal("large-memory machines retain the fixed tiny working set")
	}
	if audioWorkingBudget(2<<30) != 256<<20 {
		t.Fatal("working set must leave most available memory for other work")
	}
	if audioWorkingBudget(512<<30) > 4<<30 {
		t.Fatal("unbounded audio memory")
	}
}

func TestSequentialUnbufferedWindowsContinueOneDecoder(t *testing.T) {
	root := integrationFolder(t)
	path := filepath.Join(root, "long.wav")
	samples := fixtureNoise(170, 8765)
	if err := writeFixtureWAV(path, samples); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(path)
	r := MediaRecord{ID: "sequential", Name: "long.wav", Source: path, Duration: 170, SourceBytes: st.Size(), SourceMtime: st.ModTime().UnixNano(), Audio: map[string]any{"sample_rate": "16000"}}
	d := newDirectAudio()
	d.limit = 1
	d.pcmChecked[r.ID] = true // Exercise the FFmpeg fallback despite this PCM fixture.
	defer d.Close()           // Force every read through decoding.
	for _, first := range []int64{0, 50 * 16000, 100 * 16000, 150 * 16000} {
		count := min(int64(50*16000), int64(len(samples))-first)
		actual, err := d.read(context.Background(), r, first, count)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(actual, samples[first:first+count]) {
			t.Fatal("continued decoding changed PCM")
		}
	}
	if d.starts.Load() != 1 {
		t.Fatalf("sequential reads restarted source %d times", d.starts.Load())
	}
	if d.misses.Load() != 4 {
		t.Fatal("regression did not exercise four unbuffered windows")
	}
	// A backward read must restart without reusing the wrong timing origin.
	actual, err := d.read(context.Background(), r, 16000, 16000)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, samples[16000:32000]) || d.starts.Load() != 2 {
		t.Fatal("backward read failed")
	}
	// A completed/canceled feature stage must not leave a poisoned live decoder.
	child, cancel := context.WithCancel(context.Background())
	_, err = d.read(child, r, 0, 16000)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	actual, err = d.read(context.Background(), r, 50*16000, 16000)
	if err != nil || !reflect.DeepEqual(actual, samples[50*16000:51*16000]) {
		t.Fatal("decoder was not restarted after its stage ended", err)
	}
}

func TestRecursiveMediaContentScan(t *testing.T) {
	root := integrationFolder(t)
	nested := filepath.Join(root, "sub")
	os.MkdirAll(nested, 0755)
	media := filepath.Join(nested, "audio.unlisted-extension")
	writeFixtureWAV(media, fixtureNoise(2, 98))
	os.WriteFile(filepath.Join(nested, "notes.txt"), []byte("ordinary document"), 0600)
	out := filepath.Join(root, "result")
	os.MkdirAll(out, 0755)
	files, excluded, err := nativeInputFiles(NativeSyncOptions{Folders: []string{root}, Output: out})
	if err != nil {
		t.Fatal(err)
	}
	records, err := nativePrepareDirect(context.Background(), NativeSyncOptions{Output: out}, files, excluded)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Source != media {
		t.Fatalf("content-based selection failed: %+v", records)
	}
	data, _ := os.ReadFile(filepath.Join(out, "input-report.json"))
	var report struct{ Excluded []any }
	json.Unmarshal(data, &report)
	if len(report.Excluded) != 1 {
		t.Fatal("unsupported file not reported")
	}
}

func TestExplicitCacheFolderResume(t *testing.T) {
	root := integrationFolder(t)
	writeFixtureWAV(filepath.Join(root, "clip.wav"), fixtureNoise(2, 765))
	os.WriteFile(filepath.Join(root, "notes.txt"), []byte("not media"), 0600)
	out := filepath.Join(root, "project")
	o := NativeSyncOptions{Folders: []string{root}, Output: out, Name: "Resume", Stage: "cache", Unsynced: "append", MaxResidualMS: 150}
	if err := NativeSync(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if err := NativeSync(context.Background(), o); err == nil {
		t.Fatal("existing analysis reused without --resume")
	}
	o.Resume = true
	if err := NativeSync(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(out, "inventory.json"))
	var inventory struct {
		Records  []MediaRecord
		Excluded []any
	}
	if err := json.Unmarshal(data, &inventory); err != nil {
		t.Fatal(err)
	}
	if len(inventory.Records) != 1 || len(inventory.Excluded) != 1 {
		t.Fatal("resume lost the filtered input selection")
	}
}

func TestConcurrentVerificationReadsShareOneUncachedBlock(t *testing.T) {
	root := integrationFolder(t)
	path := filepath.Join(root, "overlap.wav")
	samples := fixtureNoise(4, 1927)
	if err := writeFixtureWAV(path, samples); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	r := MediaRecord{ID: "overlap", Name: "overlap.wav", Source: path, Duration: 4, SourceBytes: st.Size(), SourceMtime: st.ModTime().UnixNano(), Audio: map[string]any{}}
	d := newDirectAudio()
	defer d.Close()
	d.limit = 1
	d.pcmChecked[r.ID] = true
	// Occupy both decoder slots so all requests register before decoding.
	firstSlot, secondSlot := <-d.decoders, <-d.decoders
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	errs := make(chan error, 8)
	for i := 0; i < cap(errs); i++ {
		go func(i int) {
			first := int64(i * 500)
			got, err := d.read(ctx, r, first, 1600)
			if err == nil && !reflect.DeepEqual(got, samples[first:first+1600]) {
				err = fmt.Errorf("shared PCM changed samples for request %d", i)
			}
			errs <- err
		}(i)
	}
	for d.sharedReads.Load() < 7 && ctx.Err() == nil {
		time.Sleep(time.Millisecond)
	}
	d.decoders <- firstSlot
	d.decoders <- secondSlot
	for i := 0; i < cap(errs); i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if d.misses.Load() != 1 || d.sharedReads.Load() != 7 {
		t.Fatalf("got %d decodes and %d shares, want one decode and seven shares", d.misses.Load(), d.sharedReads.Load())
	}
}
