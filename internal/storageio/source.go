package storageio

// The source adapter lets a seekable external decoder retain its state while
// Go schedules actual disk reads. No decoding, resampling or media rewriting
// occurs here. In particular, an idle FFmpeg pipe never owns an HDD lease.
import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const sourceBlock = 4 << 20

var sourceBuffers = make(chan []byte, 8)
var bufferSlots = make(chan struct{}, 8)

// Reader groups requested byte reads into up to 4 MiB contiguous HDD buckets.
// Its lease ends before decoding/network consumption. Across readers the
// prefetched working set is bounded at 32 MiB; pressure falls back to direct
// scheduled reads instead of blocking while an idle client holds a buffer.
type Reader struct {
	f          *os.File
	ctx        context.Context
	serialized bool
	pos, base  int64
	buf        []byte
	n          int
	pooled     bool
}

func Open(ctx context.Context, path string) (*Reader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return &Reader{f: f, ctx: ctx, serialized: Inspect(path).Serialized, base: -1}, nil
}
func (r *Reader) Stat() (os.FileInfo, error) { return r.f.Stat() }
func (r *Reader) Close() error {
	if r.pooled {
		sourceBuffers <- r.buf
		<-bufferSlots
		r.pooled = false
		r.buf = nil
	}
	return r.f.Close()
}
func (r *Reader) Seek(off int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		off += r.pos
	case io.SeekEnd:
		st, e := r.f.Stat()
		if e != nil {
			return 0, e
		}
		off += st.Size()
	default:
		return 0, fmt.Errorf("invalid seek origin")
	}
	if off < 0 {
		return 0, fmt.Errorf("negative source position")
	}
	r.pos = off
	return off, nil
}
func (r *Reader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if len(p) == 0 {
		return 0, nil
	}
	if !r.serialized {
		n, e := r.f.ReadAt(p, r.pos)
		r.pos += int64(n)
		return n, e
	}
	if r.buf == nil {
		select {
		case bufferSlots <- struct{}{}:
			r.pooled = true
			select {
			case r.buf = <-sourceBuffers:
			default:
				r.buf = make([]byte, sourceBlock)
			}
		default:
		}
	}
	if r.buf != nil && r.pos >= r.base && r.pos < r.base+int64(r.n) {
		n := copy(p, r.buf[r.pos-r.base:int64(r.n)])
		r.pos += int64(n)
		return n, nil
	}
	release, err := Acquire(r.ctx, r.f.Name())
	if err != nil {
		return 0, err
	}
	if r.buf == nil {
		n, e := r.f.ReadAt(p, r.pos)
		release()
		r.pos += int64(n)
		return n, e
	}
	r.base = r.pos
	r.n, err = r.f.ReadAt(r.buf, r.base)
	release()
	n := copy(p, r.buf[:r.n])
	r.pos += int64(n)
	if n > 0 {
		return n, nil
	}
	return 0, err
}

type sourceEntry struct {
	path string
	ctx  context.Context
}
type sourceServer struct {
	once    sync.Once
	mu      sync.RWMutex
	entries map[string]sourceEntry
	url     string
	err     error
}

var localSources sourceServer

func (s *sourceServer) start() {
	s.once.Do(func() {
		l, e := net.Listen("tcp4", "127.0.0.1:0")
		if e != nil {
			s.err = e
			return
		}
		s.entries = map[string]sourceEntry{}
		s.url = "http://" + l.Addr().String()
		server := &http.Server{Handler: http.HandlerFunc(s.serve), ReadHeaderTimeout: 10 * time.Second, MaxHeaderBytes: 8192}
		go server.Serve(l)
	})
}
func (s *sourceServer) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		http.Error(w, "read only", 405)
		return
	}
	token := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")[0]
	s.mu.RLock()
	entry, ok := s.entries[token]
	s.mu.RUnlock()
	if !ok || entry.ctx.Err() != nil {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	stop := context.AfterFunc(entry.ctx, cancel)
	defer stop()
	file, e := Open(ctx, entry.path)
	if e != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	st, e := file.Stat()
	if e != nil {
		http.Error(w, "source unavailable", 500)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, filepath.Base(entry.path), st.ModTime(), file)
}

// SourceURL is scoped to a decoder's context and unguessable token. Loopback
// serves only that source, not an arbitrary path or a directory listing.
func SourceURL(ctx context.Context, path string) (string, error) {
	if !Inspect(path).Serialized {
		return path, nil
	}
	return registerSource(ctx, path)
}
func registerSource(ctx context.Context, path string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	localSources.start()
	if localSources.err != nil {
		return "", localSources.err
	}
	var id [24]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	token := hex.EncodeToString(id[:])
	localSources.mu.Lock()
	localSources.entries[token] = sourceEntry{path, ctx}
	localSources.mu.Unlock()
	context.AfterFunc(ctx, func() { localSources.mu.Lock(); delete(localSources.entries, token); localSources.mu.Unlock() })
	return localSources.url + "/" + token + "/source" + strings.ToLower(filepath.Ext(path)), nil
}

// MediaArgs rewrites local inputs only. Output filenames, pipe streams, and
// confirmed non-seeking devices are untouched. FFprobe uses a final bare input.
func MediaArgs(ctx context.Context, name string, args []string) ([]string, error) {
	out := append([]string(nil), args...)
	for i, v := range out {
		input := i > 0 && out[i-1] == "-i"
		if strings.Contains(strings.ToLower(filepath.Base(name)), "ffprobe") && i == len(out)-1 {
			input = true
		}
		if !input || v == "-" || strings.HasPrefix(v, "pipe:") {
			continue
		}
		st, e := os.Stat(v)
		if e != nil || st.IsDir() {
			continue
		}
		out[i], e = SourceURL(ctx, v)
		if e != nil {
			return nil, e
		}
	}
	return out, nil
}
