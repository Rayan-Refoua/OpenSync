package autosync

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

func TestDirectPCMExactlyMatchesFFmpeg(t *testing.T) {
	root := integrationFolder(t)
	pcm := fixtureNoise(121, 4321)
	// Exercise the full signed PCM16 range, not only quiet samples.
	pcm[0] = -32768
	pcm[1] = 32767
	pcm[len(pcm)-1] = -32768
	path := filepath.Join(root, "native.wav")
	if err := writeFixtureWAV(path, pcm); err != nil {
		t.Fatal(err)
	}
	// Unknown odd-sized RIFF chunks must not shift the sample origin.
	original, _ := os.ReadFile(path)
	chunk := []byte{'J', 'U', 'N', 'K', 3, 0, 0, 0, 19, 27, 42, 0}
	extended := append(append(append([]byte(nil), original[:12]...), chunk...), original[12:]...)
	binary.LittleEndian.PutUint32(extended[4:], uint32(len(extended)-8))
	if err := os.WriteFile(path, extended, 0600); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(path)
	r := MediaRecord{ID: "native", Source: path, Name: "native.wav", Duration: 121, Audio: map[string]any{"sample_rate": "16000"}, SourceBytes: st.Size(), SourceMtime: st.ModTime().UnixNano()}
	d := newDirectAudio()
	defer d.Close()
	d.limit = 1
	var reference []byte
	if err := decodedStream(context.Background(), r, 0, -1, func(reader io.Reader) error { var err error; reference, err = io.ReadAll(reader); return err }); err != nil {
		t.Fatal(err)
	}
	var direct []byte
	if err := d.stream(context.Background(), r, func(reader io.Reader) error { var err error; direct, err = io.ReadAll(reader); return err }); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(direct, reference) {
		t.Fatal("direct PCM stream differs from FFmpeg")
	}
	var wg sync.WaitGroup
	for _, window := range [][2]int64{{0, 1}, {1, 16001}, {799999, 17}, {959991, 32001}, {1935990, 10}, {0, 1904000}} {
		wg.Add(1)
		go func(w [2]int64) {
			defer wg.Done()
			got, err := d.read(context.Background(), r, w[0], w[1])
			if err != nil {
				t.Error(err)
				return
			}
			want := make([]int16, w[1])
			for i := range want {
				want[i] = int16(binary.LittleEndian.Uint16(reference[(w[0]+int64(i))*2:]))
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("PCM window differs: %v", w)
			}
		}(window)
	}
	wg.Wait()
	if d.starts.Load() != 0 || d.used != 0 || d.nativeReads.Load() != 6 {
		t.Fatal("native PCM used decoding or retained audio buffers")
	}
}

func TestDirectPCMRejectsFormatsRequiringConversion(t *testing.T) {
	root := integrationFolder(t)
	source := filepath.Join(root, "source.wav")
	writeFixtureWAV(source, fixtureNoise(2, 119))
	for _, v := range []struct {
		name string
		args []string
	}{
		{"stereo.wav", []string{"-ac", "2"}},
		{"rate.wav", []string{"-ar", "48000"}},
		{"depth.wav", []string{"-c:a", "pcm_s24le"}},
		{"float.wav", []string{"-c:a", "pcm_f32le"}},
	} {
		path := filepath.Join(root, v.name)
		args := append([]string{"-v", "error", "-y", "-i", source}, v.args...)
		args = append(args, path)
		if err := runMediaTool(context.Background(), "ffmpeg", args...); err != nil {
			t.Fatal(err)
		}
		d := newDirectAudio()
		reader := d.nativePCM(MediaRecord{ID: v.name, Source: path})
		d.Close()
		if reader != nil {
			t.Fatalf("format requiring conversion accepted: %s", v.name)
		}
	}
}
