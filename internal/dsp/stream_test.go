package dsp

import (
	"errors"
	"math"
	"reflect"
	"testing"

	"opensync/internal/audio"
)

func TestStreamingEqualsSingleBlock(t *testing.T) {
	const sampleCount = 16000

	samples := make([]int16, sampleCount)
	for i := range samples {
		amplitude := 8000.0
		if i >= 3900 && i < 7900 {
			amplitude = 19000
		}
		// Different frequency components and changing amplitudes
		// exercise spectral flux across multiple block boundaries.
		x := float64(i) / SampleRate
		samples[i] = int16(amplitude * (0.55*math.Sin(2*math.Pi*430*x) +
			0.35*math.Sin(2*math.Pi*1700*x)))
	}

	r, err := audio.Open(makeWAV(t, samples))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	expected, err := Extract(r, 0, sampleCount)
	if err != nil {
		t.Fatal(err)
	}

	for _, blockSize := range []int64{
		WindowSize + HopSize,
		2048,
		4096,
		8192,
	} {
		var actual []Frame

		err := Stream(r, blockSize, func(f Frame) error {
			actual = append(actual, f)
			return nil
		})
		if err != nil {
			t.Fatalf("block=%d: %v", blockSize, err)
		}

		if !reflect.DeepEqual(expected.Frames, actual) {
			t.Fatalf(
				"block=%d: streaming mismatch, "+
					"expected=%d frames, actual=%d",
				blockSize,
				len(expected.Frames),
				len(actual),
			)
		}
	}
}

func TestStreamEmptyShortRecording(t *testing.T) {
	r, err := audio.Open(makeWAV(t, make([]int16, 100)))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	count := 0
	err = Stream(r, 2048, func(f Frame) error {
		count++
		return nil
	})
	if err != nil || count != 0 {
		t.Fatalf("unexpected short-recording result: %d %v",
			count, err)
	}
}

func TestStreamPropagatesCallbackError(t *testing.T) {
	r, err := audio.Open(makeWAV(t, make([]int16, 4096)))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	want := errors.New("stop requested")
	err = Stream(r, 2048, func(Frame) error {
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("callback error was lost: %v", err)
	}
}
