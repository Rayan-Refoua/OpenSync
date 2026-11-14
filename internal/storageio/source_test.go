package storageio

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestBucketReaderSeeksAndCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.bin")
	data := make([]byte, sourceBlock*2+513)
	for i := range data {
		data[i] = byte(i*31 + i/333)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r, e := Open(ctx, path)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	r.serialized = true
	for _, off := range []int64{0, 100, sourceBlock - 11, sourceBlock + 31, 3, int64(len(data) - 8)} {
		r.Seek(off, io.SeekStart)
		b := make([]byte, min(800, int64(len(data))-off))
		if _, e = io.ReadFull(r, b); e != nil {
			t.Fatal(e)
		}
		if !bytes.Equal(b, data[off:off+int64(len(b))]) {
			t.Fatalf("bytes changed at %d", off)
		}
	}
	cancel()
	if _, e = r.Read(make([]byte, 1)); e != context.Canceled {
		t.Fatal(e)
	}
}
func TestSourceRangeAndRevocation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.bin")
	os.WriteFile(path, []byte("0123456789"), 0600)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	url, e := registerSource(ctx, path)
	if e != nil {
		t.Fatal(e)
	}
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Range", "bytes=3-6")
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 206 || string(data) != "3456" || resp.Header.Get("Content-Range") != "bytes 3-6/10" {
		t.Fatal(resp.Status, string(data))
	}
	cancel()
	resp, e = http.Get(url)
	if e != nil {
		t.Fatal(e)
	}
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatal(resp.Status)
	}
}
func TestFFmpegSourceParity(t *testing.T) {
	ff, e := exec.LookPath("ffmpeg")
	if e != nil {
		t.Skip("FFmpeg not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	dir := t.TempDir()
	for _, format := range []struct{ ext, codec string }{{"wav", "pcm_s16le"}, {"m4a", "aac"}, {"mp3", "libmp3lame"}} {
		path := filepath.Join(dir, "source."+format.ext)
		cmd := exec.CommandContext(ctx, ff, "-v", "error", "-f", "lavfi", "-i", "aevalsrc=0.4*sin(2*PI*(200*t+23*t*t)):s=48000:d=7", "-ac", "2", "-c:a", format.codec, path)
		if out, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("fixture %s: %v %s", format.ext, e, out)
		}
		url, e := registerSource(ctx, path)
		if e != nil {
			t.Fatal(e)
		}
		for _, trim := range []string{"anull", "aresample=16000,aformat=sample_fmts=s16:channel_layouts=mono,atrim=start_sample=37123:end_sample=81123"} {
			decode := func(input string) []byte {
				t.Helper()
				c := exec.CommandContext(ctx, ff, "-v", "error", "-i", input, "-map", "0:a:0", "-vn", "-ac", "1", "-ar", "16000", "-af", trim, "-f", "s16le", "-")
				var stderr bytes.Buffer
				c.Stderr = &stderr
				b, e := c.Output()
				if e != nil {
					t.Fatalf("decode %s: %v %s", input, e, stderr.String())
				}
				return b
			}
			original, scheduled := decode(path), decode(url)
			if !bytes.Equal(original, scheduled) {
				t.Fatalf("PCM changed for %s, %s (%d/%d bytes)", format.ext, trim, len(original), len(scheduled))
			}
			t.Logf("%s %s: %d PCM bytes identical", format.ext, trim, len(original))
		}
	}
}
