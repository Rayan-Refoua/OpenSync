package desktop

import (
	"context"
	"encoding/json"
	"math"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

func TestPreviewTimestampNotation(t *testing.T) {
	for _, value := range []string{"0", "0.02", "-0.001", "1.11e-05", "2.4E+02"} {
		match := thumbnailPTS.FindStringSubmatch("[showinfo] pts_time:" + value + " duration:1")
		if len(match) != 2 || match[1] != value {
			t.Fatalf("incomplete timestamp %q: %v", value, match)
		}
	}
}

func TestBufferedPreviewPreservesSourceFrameTimes(t *testing.T) {
	if os.Getenv("OPENSYNC_INTEGRATION") != "1" {
		t.Skip("FFmpeg integration")
	}
	ctx := context.Background()
	root := t.TempDir()
	frames := func(path string) []float64 {
		t.Helper()
		ff, e := tool("ffprobe")
		if e != nil {
			t.Fatal(e)
		}
		data, e := exec.Command(ff, "-v", "error", "-select_streams", "v:0", "-show_entries", "frame=best_effort_timestamp_time", "-of", "json", path).Output()
		if e != nil {
			t.Fatal(e)
		}
		var report struct {
			Frames []struct {
				Time string `json:"best_effort_timestamp_time"`
			} `json:"frames"`
		}
		if e = json.Unmarshal(data, &report); e != nil {
			t.Fatal(e)
		}
		out := []float64{}
		for _, f := range report.Frames {
			v, e := strconv.ParseFloat(f.Time, 64)
			if e != nil {
				t.Fatal(e)
			}
			out = append(out, v)
		}
		return out
	}
	for _, vfr := range []bool{false, true} {
		name := "50fps"
		filter := "null"
		if vfr {
			name = "vfr"
			filter = `select=not(mod(n\,3))+not(mod(n\,7))`
		}
		path := filepath.Join(root, name+".mp4")
		cmd, e := command(ctx, "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=128x72:rate=50:duration=26", "-vf", filter, "-fps_mode", "vfr", "-c:v", "libx264", "-preset", "ultrafast", path)
		if e != nil {
			t.Fatal(e)
		}
		if b, e := cmd.CombinedOutput(); e != nil {
			t.Fatal(e, string(b))
		}
		original := frames(path)
		s := NewService()
		c := Clip{Source: path, Duration: 26, Video: true}
		r := httptest.NewRequest("GET", "/media/segment?start=12", nil)
		w := httptest.NewRecorder()
		s.videoSegment(ctx, w, r, c)
		s.Close()
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		if w.Body.Len() > 16<<20 {
			t.Fatal("unbounded segment")
		}
		origin, e := strconv.ParseFloat(w.Header().Get("X-OpenSync-Segment-Origin"), 64)
		if e != nil {
			t.Fatal(e)
		}
		if origin < 11.8-.001 || origin > 12 {
			t.Fatal("wrong guard/source origin", origin)
		}
		output := filepath.Join(root, name+"-preview.mp4")
		os.WriteFile(output, w.Body.Bytes(), 0600)
		preview := frames(output)
		if len(preview) < 50 {
			t.Fatal("not enough frames")
		}
		for _, p := range preview {
			matched := false
			for _, o := range original {
				if math.Abs(o-(origin+p)) < .001 {
					matched = true
					break
				}
			}
			if !matched {
				t.Fatalf("%s preview changed source timestamp %.9f", name, origin+p)
			}
		}
		if !vfr && math.Abs(preview[1]-preview[0]-.02) > .0001 {
			t.Fatal("50fps was changed")
		}
		if origin+preview[len(preview)-1] < 24.1 {
			t.Fatal("missing right guard")
		}
	}
}
