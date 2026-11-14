package desktop

import (
	"bytes"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestVideoResponseBoundedAndByteAccurate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "media.mp4")
	data := make([]byte, 9<<20)
	for i := range data {
		data[i] = byte(i * 13)
	}
	if e := os.WriteFile(path, data, 0600); e != nil {
		t.Fatal(e)
	}
	f, e := os.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	st, _ := f.Stat()
	for _, tc := range []struct {
		value      string
		start, end int
		code       int
	}{{"bytes=0-", 0, 4 << 20, 206}, {"bytes=4194304-", 4 << 20, 8 << 20, 206}, {"bytes=7-12", 7, 13, 206}, {"bytes=-4", len(data) - 4, len(data), 206}, {"bytes=10000000-", 0, 0, 416}, {"bytes=1-0", 0, 0, 416}, {"bytes=0-1,7-8", 0, 0, 416}} {
		req := httptest.NewRequest("GET", "/media/source", nil)
		req.Header.Set("Range", tc.value)
		w := httptest.NewRecorder()
		serveVideoRange(w, req, f, st)
		if w.Code != tc.code {
			t.Fatalf("%s: %d", tc.value, w.Code)
		}
		if tc.code == 206 && !bytes.Equal(w.Body.Bytes(), data[tc.start:tc.end]) {
			t.Fatalf("wrong source bytes: %s", tc.value)
		}
	}
	req := httptest.NewRequest("HEAD", "/media/source", nil)
	w := httptest.NewRecorder()
	serveVideoRange(w, req, f, st)
	if w.Body.Len() != 0 || w.Header().Get("Content-Length") != "4194304" {
		t.Fatal("HEAD must describe bounded body without reading it")
	}
}
