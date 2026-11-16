package desktop

import (
	"context"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"

	"opensync/internal/autosync"
)

func TestProgressivePeakCacheReopensWithoutDecode(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "input.wav")
	if err := os.WriteFile(path, []byte("synthetic source identity"), 0600); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	r := autosync.MediaRecord{Source: path, SourceBytes: st.Size(), SourceMtime: st.ModTime().UnixNano(), Duration: peakChunkSeconds}
	clip := Clip{Source: path, Metadata: r, Audio: true, Duration: r.Duration}
	first := NewService()
	defer first.Close()
	sink := first.SyncWaveformTo(r, root, func(err error) { t.Error(err) })
	pcm := make([]byte, peakChunkSamples*2)
	binary.LittleEndian.PutUint16(pcm, 0x4000)
	sink(pcm) // A complete chunk must be available even before EOF.
	second := NewService()
	defer second.Close()
	second.SetProject(&Project{Path: filepath.Join(root, "OpenSync.opensync"), Clips: []Clip{clip}})
	ctx := context.Background()
	data, err := second.waveFromPeaks(ctx, ctx, clip, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if math.Float32frombits(binary.LittleEndian.Uint32(data[4:])) != .5 || second.peaks.decodes.Load() != 0 {
		t.Fatal("reopened project failed to reuse persisted waveform")
	}
	key, err := sourcePeakKey(clip)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "Waveform Peaks")
	if err = os.WriteFile(peakDiskPath(dir, key+"\x000"), []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	if readPeakChunk(dir, key+"\x000") != nil {
		t.Fatal("partial cache accepted")
	}
}
