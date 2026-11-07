package dsp

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"

	"opensync/internal/audio"
)

func makeWAV(t *testing.T, samples []int16) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "signal.wav")
	dataBytes := len(samples) * 2
	data := make([]byte, 44+dataBytes)

	copy(data[0:], "RIFF")
	binary.LittleEndian.PutUint32(data[4:], uint32(len(data)-8))
	copy(data[8:], "WAVE")
	copy(data[12:], "fmt ")
	binary.LittleEndian.PutUint32(data[16:], 16)
	binary.LittleEndian.PutUint16(data[20:], 1)
	binary.LittleEndian.PutUint16(data[22:], 1)
	binary.LittleEndian.PutUint32(data[24:], SampleRate)
	binary.LittleEndian.PutUint32(data[28:], SampleRate*2)
	binary.LittleEndian.PutUint16(data[32:], 2)
	binary.LittleEndian.PutUint16(data[34:], 16)
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:], uint32(dataBytes))

	for i, s := range samples {
		binary.LittleEndian.PutUint16(data[44+i*2:], uint16(s))
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSilenceHasNoFeatures(t *testing.T) {
	r, err := audio.Open(makeWAV(t, make([]int16, 4096)))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	f, err := Extract(r, 0, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Frames) != 29 {
		t.Fatalf("expected 29 frames; got %d", len(f.Frames))
	}
	for _, frame := range f.Frames {
		if frame.Energy != 0 || frame.Flux != [3]float64{} {
			t.Fatal("silence generated nonzero features")
		}
	}
}

func TestAbsoluteSampleCoordinates(t *testing.T) {
	samples := make([]int16, 16000)
	for i := range samples {
		samples[i] = int16(10000 * math.Sin(
			2*math.Pi*1000*float64(i)/SampleRate))
	}

	r, err := audio.Open(makeWAV(t, samples))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	f, err := Extract(r, 4000, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if f.Frames[0].CenterSample != 4256 {
		t.Fatal("incorrect first absolute sample coordinate")
	}
	if f.Frames[1].CenterSample != 4384 {
		t.Fatal("incorrect hop sample coordinate")
	}
	if f.Frames[0].Energy < 0.01 {
		t.Fatal("sine signal energy unexpectedly low")
	}
}

func TestTransientProducesOnset(t *testing.T) {
	samples := make([]int16, 8192)
	for i := 4096; i < 5000; i++ {
		samples[i] = int16(11000 * math.Sin(
			2*math.Pi*1400*float64(i)/SampleRate))
	}

	r, err := audio.Open(makeWAV(t, samples))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	f, err := Extract(r, 0, int64(len(samples)))
	if err != nil {
		t.Fatal(err)
	}

	var peak float64
	for _, frame := range f.Frames {
		for _, v := range frame.Flux {
			if v > peak {
				peak = v
			}
		}
	}
	if peak <= 0 {
		t.Fatal("failed to detect spectral onset")
	}
}
