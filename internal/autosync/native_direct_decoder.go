package autosync

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"

	"opensync/internal/storageio"
)

// Tagged Windows builds register a library decoder. The executable remains
// the default while codec, timing, and performance parity are evaluated.
var openExperimentalPCM func(context.Context, string) (io.ReadCloser, error)

// A bounded pool of live decoders continues forward reads instead of restarting
// at sample zero for each window. Keeping the original resampler origin preserves
// exactly the same PCM. No intermediate files are used.
type directDecoder struct {
	id       string
	position int64
	command  *exec.Cmd
	output   io.ReadCloser
	cancel   context.CancelFunc
	ctx      context.Context
	stderr   strings.Builder
}

func (d *directAudio) acquire(ctx context.Context, id string) (*directDecoder, error) {
	d.waiting.Add(1)
	defer d.waiting.Add(-1)
	var a *directDecoder
	select {
	case a = <-d.decoders:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if a.id != id {
		select {
		case b := <-d.decoders:
			if b.id == id {
				d.decoders <- a
				return b, nil
			}
			d.decoders <- b
		default:
		}
	}
	return a, nil
}
func (s *directDecoder) close() {
	if s.cancel != nil {
		s.cancel()
	}
	if s.output != nil {
		s.output.Close()
	}
	if s.command != nil {
		s.command.Wait()
	}
	s.command = nil
	s.output = nil
	s.cancel = nil
	s.ctx = nil
	s.id = ""
	s.position = 0
}
func (s *directDecoder) read(ctx context.Context, r MediaRecord, first, count int64, starts *atomic.Int64) ([]int16, error) {
	if err := checkSource(r); err != nil {
		return nil, err
	}
	if s.id != r.ID || first < s.position || (s.ctx != nil && s.ctx.Err() != nil) {
		s.close()
		s.stderr.Reset()
		if err := s.open(ctx, r); err != nil {
			s.close()
			return nil, err
		}
		s.id = r.ID
		starts.Add(1)
	}
	if s.command == nil {
		release, err := storageio.Acquire(ctx, r.Source)
		if err != nil {
			return nil, err
		}
		defer release()
	}
	fail := func(err error) ([]int16, error) {
		s.close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("decode window in %s: %w: %s", r.Name, err, s.stderr.String())
	}
	if first > s.position {
		if _, err := io.CopyN(io.Discard, s.output, (first-s.position)*2); err != nil {
			return fail(err)
		}
	}
	data := make([]byte, count*2)
	if _, err := io.ReadFull(s.output, data); err != nil {
		return fail(err)
	}
	s.position = first + count
	pcm := make([]int16, count)
	for i := range pcm {
		pcm[i] = int16(binary.LittleEndian.Uint16(data[i*2:]))
	}
	if err := checkSource(r); err != nil {
		return fail(err)
	}
	return pcm, nil
}

func (s *directDecoder) open(ctx context.Context, r MediaRecord) error {
	child, cancel := context.WithCancel(ctx)
	s.ctx, s.cancel = child, cancel
	reader, err := tryLibraryPCM(child, r.Source)
	if err != nil {
		return err
	}
	if reader != nil {
		s.output = reader
		return nil
	}
	tool, err := mediaTool("ffmpeg")
	if err != nil {
		return err
	}
	s.command = mediaCommandContext(child, tool, append(analysisAudioArgs(ctx, r.Source), "-f", "s16le", "-")...)
	s.command.Stderr = &s.stderr
	s.output, err = s.command.StdoutPipe()
	if err != nil {
		return err
	}
	return s.command.Start()
}

func libraryPCMEnabled() bool {
	return openExperimentalPCM != nil && os.Getenv("OPENSYNC_EXPERIMENTAL_FFMPEG_LIB") == "1"
}

// A nil reader keeps the executable fallback available before any PCM is used.
func tryLibraryPCM(ctx context.Context, source string) (io.ReadCloser, error) {
	if !libraryPCMEnabled() {
		return nil, nil
	}
	release, err := storageio.Acquire(ctx, source)
	if err != nil {
		return nil, err
	}
	defer release()
	reader, err := openExperimentalPCM(ctx, source)
	if err != nil {
		return nil, ctx.Err()
	}
	return reader, nil
}
