package desktop

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"opensync/internal/autosync"
)

func TestSyncPreviewDrainsReadsAndServesSharedPeaks(t *testing.T) {
	s := NewService()
	defer s.Close()
	job, finish, err := s.preview.enter(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	drained := make(chan struct{})
	go func() { s.PausePreview(); close(drained) }()
	select {
	case <-job.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("active read not cancelled")
	}
	select {
	case <-drained:
		t.Fatal("pause did not wait for active read")
	default:
	}
	finish()
	<-drained
	if _, _, err = s.preview.enter(context.Background()); !errors.Is(err, errPreviewPaused) {
		t.Fatal("new read admitted", err)
	}

	source := filepath.Join(t.TempDir(), "original.wav")
	// The peak cache checks source identity before accessing the waveform.
	if err := os.WriteFile(source, make([]byte, 64), 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	r := autosync.MediaRecord{Source: source, SourceBytes: info.Size(), SourceMtime: info.ModTime().UnixNano(), Duration: 65.536}
	c := Clip{Source: r.Source, Metadata: r, Audio: true, Duration: r.Duration}
	ctx := context.Background()
	if _, err = s.waveFromPeaks(ctx, ctx, c, 0, 0); !errors.Is(err, errPreviewPaused) {
		t.Fatal("cache miss should defer", err)
	}
	pcm := make([]byte, peakChunkSamples*2+4)
	binary.LittleEndian.PutUint16(pcm, 0xc000)
	binary.LittleEndian.PutUint16(pcm[2:], 0x4000)
	binary.LittleEndian.PutUint16(pcm[peakChunkSamples*2:], 0x2000)
	write := s.SyncWaveform(r)
	write(pcm[:960000])
	write(pcm[960000:])
	write(nil)
	b, err := s.waveFromPeaks(ctx, ctx, c, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if math.Float32frombits(binary.LittleEndian.Uint32(b)) != -.5 || math.Float32frombits(binary.LittleEndian.Uint32(b[4:])) != .5 {
		t.Fatal("waveform peak mismatch")
	}
	b, err = s.waveFromPeaks(ctx, ctx, c, 0, 8)
	if err != nil || math.Float32frombits(binary.LittleEndian.Uint32(b[4:])) != .25 {
		t.Fatal("tail peak mismatch", err)
	}
	if s.peaks.decodes.Load() != 0 {
		t.Fatal("waveform opened source again")
	}
	s.ResumePreview()
	_, finish, err = s.preview.enter(ctx)
	if err != nil {
		t.Fatal(err)
	}
	finish()
}

func TestRealSyncFeedsPreviewWithoutSourceDecode(t *testing.T) {
	root := os.Getenv("OPENSYNC_PREVIEW_SYNC_MEDIA")
	if root == "" {
		t.Skip("set OPENSYNC_PREVIEW_SYNC_MEDIA to a real source folder")
	}
	s := NewService()
	defer s.Close()
	s.PausePreview()
	ctx := autosync.WithFingerprintPCM(context.Background(), s.SyncWaveform)
	out := t.TempDir()
	err := autosync.NativeSync(ctx, autosync.NativeSyncOptions{Folders: []string{root}, Output: out, Name: "OpenSync", Mode: "chronological", Stage: "all", Unsynced: "append", MaxResidualMS: 150})
	if err != nil {
		t.Fatal(err)
	}
	p, err := LoadProject(filepath.Join(out, "OpenSync.opensync"))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range p.Clips {
		if !c.Audio {
			continue
		}
		b, err := s.waveFromPeaks(ctx, ctx, c, 0, 0)
		if err != nil || len(b) == 0 {
			t.Fatalf("%s: %v", c.Name, err)
		}
	}
	if s.peaks.decodes.Load() != 0 {
		t.Fatal("duplicates decoded by preview workers")
	}
}
