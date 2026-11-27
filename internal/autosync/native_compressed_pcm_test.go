package autosync

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// Keep this oracle on the executable path when adding library implementations.
func TestCompressedDecoderWindowParity(t *testing.T) {
	root := integrationFolder(t)
	source := filepath.Join(root, "source.wav")
	if err := writeFixtureWAV(source, fixtureNoise(4, 4321)); err != nil {
		t.Fatal(err)
	}
	ffmpeg, err := mediaTool("ffmpeg")
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		name string
		args []string
	}{
		{"camera.mp4", []string{"-c:a", "aac", "-ar", "48000", "-ac", "2"}},
		{"recorder.mp3", []string{"-c:a", "libmp3lame", "-ar", "44100", "-ac", "1"}},
		{"lossless.flac", []string{"-c:a", "flac", "-ar", "48000", "-ac", "2"}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(root, fixture.name)
			args := append([]string{"-nostdin", "-v", "error", "-i", source}, fixture.args...)
			if output, err := mediaCommandContext(ctx, ffmpeg, append(args, path)...).CombinedOutput(); err != nil {
				t.Fatalf("fixture: %v: %s", err, output)
			}
			st, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			record := MediaRecord{ID: fixture.name, Name: fixture.name, Source: path, SourceBytes: st.Size(), SourceMtime: st.ModTime().UnixNano()}
			read := func(first, count int64) []byte {
				t.Helper()
				var result []byte
				err := decodedStream(ctx, record, first, count, func(r io.Reader) error {
					var readErr error
					result, readErr = io.ReadAll(r)
					return readErr
				})
				if err != nil {
					t.Fatal(err)
				}
				return result
			}
			reference := read(0, -1)
			if openExperimentalPCM != nil {
				reader, err := openExperimentalPCM(ctx, path)
				if err != nil {
					t.Fatal(err)
				}
				actual, err := io.ReadAll(reader)
				reader.Close()
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(actual, reference) {
					t.Fatalf("library PCM differs from executable: got %d bytes, want %d", len(actual), len(reference))
				}
				t.Setenv("OPENSYNC_EXPERIMENTAL_FFMPEG_LIB", "1")
			}
			frames := int64(len(reference) / 2)
			if frames < 48000 {
				t.Fatalf("unexpected stream length: %d", frames)
			}
			var session directDecoder
			defer session.close()
			var starts atomic.Int64
			for _, window := range [][2]int64{{0, 1}, {1, 1023}, {15999, 321}, {frames - 113, 113}, {21, 20001}} {
				first, count := window[0], window[1]
				want := reference[first*2 : (first+count)*2]
				if !bytes.Equal(want, read(first, count)) {
					t.Fatalf("trimmed stream differs for %v", window)
				}
				pcm, err := session.read(ctx, record, first, count, &starts)
				if err != nil {
					t.Fatal(err)
				}
				if openExperimentalPCM != nil && session.command != nil {
					t.Fatal("window read fell back to the executable")
				}
				got, err := binary.Append(nil, binary.LittleEndian, pcm)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(want, got) {
					t.Fatalf("persistent decoder differs for %v", window)
				}
			}
			if starts.Load() != 2 {
				t.Fatalf("decoder starts = %d, want 2 for forward reads and one rewind", starts.Load())
			}
		})
	}
}

// Benchmark the first 50-second verification block, including decoder setup.
func BenchmarkDecoderFirstBlock(b *testing.B) {
	if openExperimentalPCM == nil {
		b.Skip("requires the ffmpeglib build tag and shared SDK")
	}
	ctx := context.Background()
	ffmpeg, err := mediaTool("ffmpeg")
	if err != nil {
		b.Fatal(err)
	}
	path := filepath.Join(b.TempDir(), "camera.mp4")
	args := []string{"-v", "error", "-f", "lavfi", "-i", "anoisesrc=r=48000:seed=42", "-t", "55", "-ac", "2", "-c:a", "aac", path}
	if output, err := mediaCommandContext(ctx, ffmpeg, args...).CombinedOutput(); err != nil {
		b.Fatalf("fixture: %v: %s", err, output)
	}
	st, err := os.Stat(path)
	if err != nil {
		b.Fatal(err)
	}
	record := MediaRecord{ID: "benchmark", Name: "camera.mp4", Source: path, SourceBytes: st.Size(), SourceMtime: st.ModTime().UnixNano()}
	for _, backend := range []struct{ name, flag string }{{"executable", "0"}, {"library", "1"}} {
		b.Run(backend.name, func(b *testing.B) {
			b.Setenv("OPENSYNC_EXPERIMENTAL_FFMPEG_LIB", backend.flag)
			var starts atomic.Int64
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				var decoder directDecoder
				_, err := decoder.read(ctx, record, 0, 50*16000, &starts)
				usedLibrary := decoder.command == nil
				decoder.close()
				if err != nil {
					b.Fatal(err)
				}
				if usedLibrary != (backend.flag == "1") {
					b.Fatal("unexpected decoder backend")
				}
			}
		})
	}
}
