package desktop

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"net/http/httptest"
	"opensync/internal/autosync"
	"os"
	"path/filepath"
	"testing"
)

func TestDelayedAudioUsesVideoTimeGrid(t *testing.T) {
	if os.Getenv("OPENSYNC_INTEGRATION") != "1" {
		t.Skip("FFmpeg integration")
	}
	path := filepath.Join(t.TempDir(), "delayed.mkv")
	cmd, err := command(context.Background(), "-v", "error", "-f", "lavfi", "-i", "color=black:s=32x32:r=25:d=2", "-itsoffset", "0.5", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=16000:duration=1", "-c:v", "ffv1", "-c:a", "pcm_s16le", path)
	if err != nil {
		t.Fatal(err)
	}
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatal(e, string(out))
	}
	for _, start := range []float64{0, .3, .7} {
		var pcm []float32
		err = decode(context.Background(), path, start, .6, 16000, func(b []byte) error {
			for i := 0; i+7 < len(b); i += 8 {
				pcm = append(pcm, sample(b[i:]))
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		first := -1
		for i, v := range pcm {
			if math.Abs(float64(v)) > .001 {
				first = i
				break
			}
		}
		want := math.Max(0, .5-start)
		got := float64(first) / 16000
		if first < 0 || math.Abs(got-want) > .002 {
			t.Fatalf("seek %.3f onset %.6f expected %.6f", start, got, want)
		}
	}
}

func testWAV(t *testing.T) string {
	t.Helper()
	const n = 16000
	b := make([]byte, 44+n*4)
	copy(b, "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(len(b)-8))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)
	binary.LittleEndian.PutUint16(b[22:], 2)
	binary.LittleEndian.PutUint32(b[24:], 16000)
	binary.LittleEndian.PutUint32(b[28:], 64000)
	binary.LittleEndian.PutUint16(b[32:], 4)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], n*4)
	for i := 0; i < n; i++ {
		v := int16(math.Sin(float64(i)*2*math.Pi*440/16000) * 12000)
		binary.LittleEndian.PutUint16(b[44+i*4:], uint16(v))
		binary.LittleEndian.PutUint16(b[46+i*4:], uint16(-v))
	}
	p := filepath.Join(t.TempDir(), "stereo.wav")
	if e := os.WriteFile(p, b, 0600); e != nil {
		t.Fatal(e)
	}
	return p
}
func TestMediaRequestsAndAntiphaseWaveform(t *testing.T) {
	if os.Getenv("OPENSYNC_INTEGRATION") != "1" {
		t.Skip("set OPENSYNC_INTEGRATION=1 for FFmpeg")
	}
	path := testWAV(t)
	s := NewService()
	defer s.Close()
	p := s.SetProject(BuildProject(autosync.NativePayload{Records: []autosync.MediaRecord{{ID: "a", Name: "a", Duration: 1, Source: path, Audio: map[string]any{"channels": 2}}}, Models: []autosync.TimeModel{{Recording: 0, Rate: 1, Component: 1, Resolved: true, Status: "supported"}}}))
	get := func(query string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest("GET", query, nil))
		return w
	}
	w := get(fmt.Sprintf("/media/wave?g=%d&clip=0&level=0&tile=0", p.Generation))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	peak := float32(0)
	for i := 0; i < w.Body.Len(); i += 4 {
		v := sample(w.Body.Bytes()[i:])
		if v > peak {
			peak = v
		}
	}
	if peak < .35 {
		t.Fatal("stereo cancellation in peak extraction", peak)
	}
	w = get(fmt.Sprintf("/media/audio?g=%d&start=0&duration=0.5&tracks=[0]", p.Generation))
	if w.Code != 200 || w.Body.Len() != 48000*4 {
		t.Fatal("bad audition block", w.Code, w.Body.Len())
	}
	for i := 400; i < 800; i += 8 {
		l, r := sample(w.Body.Bytes()[i:]), sample(w.Body.Bytes()[i+4:])
		if math.Abs(float64(l+r)) > 1e-6 {
			t.Fatal("stereo phase changed")
		}
	}
	if get("/media/wave?g=999&clip=0&level=0&tile=0").Code != 409 {
		t.Fatal("stale project accepted")
	}
	if get(fmt.Sprintf("/media/wave?g=%d&clip=-1&level=0&tile=0", p.Generation)).Code != 400 {
		t.Fatal("arbitrary input accepted")
	}
}

func TestAuditionPreservesFractionalPlacementAndGaps(t *testing.T) {
	if os.Getenv("OPENSYNC_INTEGRATION") != "1" {
		t.Skip("FFmpeg integration")
	}
	path := testWAV(t)
	s := NewService()
	defer s.Close()
	const offset = .123456789
	r := autosync.MediaRecord{ID: "a", Name: "a", Duration: 1, Source: path, Audio: map[string]any{"channels": 2}}
	r2 := r
	r2.ID = "b"
	p := s.SetProject(BuildProject(autosync.NativePayload{Records: []autosync.MediaRecord{r, r2}, Models: []autosync.TimeModel{{Recording: 0, Component: 1, Rate: 1, Resolved: true, Status: "supported"}, {Recording: 1, Component: 1, Offset: offset, Rate: 1, Resolved: true, Status: "supported"}}}))
	if p.Clips[0].Track == p.Clips[1].Track {
		t.Fatal("missing separate overlapping lanes")
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", fmt.Sprintf("/media/audio?g=%d&start=0&duration=0.3&tracks=[1]", p.Generation), nil))
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	pcm := w.Body.Bytes()
	for i := 0; i < int(math.Floor(offset*48000)); i++ {
		if sample(pcm[i*8:]) != 0 {
			t.Fatal("audio shifted into timeline gap", i)
		}
	}
	// Compare measured samples to the known source sine, after the exact
	// fractional placement. Tolerance allows PCM quantization/resampling.
	for _, i := range []int{6500, 7000, 10000} {
		want := math.Sin((float64(i)/48000-offset)*2*math.Pi*440) * 12000 / 32768
		got := float64(sample(pcm[i*8:]))
		if math.Abs(got-want) > .003 {
			t.Fatalf("sample %d got %.6f want %.6f", i, got, want)
		}
	}
}
