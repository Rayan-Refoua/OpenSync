package desktop

import (
	"encoding/binary"
	"fmt"
	"math"
	"net/http/httptest"
	"opensync/internal/autosync"
	"os"
	"path/filepath"
	"testing"
)

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
