package dsp

import (
	"os"
	"testing"

	"opensync/internal/audio"
)

func BenchmarkExtractRealCache(b *testing.B) {
	path := os.Getenv("OPENSYNC_TEST_WAV")
	if path == "" {
		b.Skip("OPENSYNC_TEST_WAV not configured")
	}

	r, err := audio.Open(path)
	if err != nil {
		b.Fatal(err)
	}
	defer r.Close()

	const window = int64(10 * SampleRate)

	if r.Frames() < window {
		b.Fatal("recording shorter than benchmark window")
	}

	var totalFrames int64
	available := r.Frames() - window + 1

	b.SetBytes(window * 2)
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Cycle through the recording instead of repeatedly
		// analyzing only the beginning.
		start := (int64(i) * 17 * SampleRate) % available

		result, err := Extract(r, start, window)
		if err != nil {
			b.Fatal(err)
		}

		totalFrames += int64(len(result.Frames))
	}

	b.StopTimer()

	if totalFrames == 0 {
		b.Fatal("no features produced")
	}

	// ns/op is the measured wall-clock processing cost.
	// Benchmark input is always 10 seconds of source audio.
	b.ReportMetric(
		10*1e9/float64(b.Elapsed().Nanoseconds())*
			float64(b.N),
		"audio-sec/sec",
	)
}
