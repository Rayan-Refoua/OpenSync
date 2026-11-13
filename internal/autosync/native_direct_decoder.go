package autosync

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync/atomic"
)

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
		tool, err := mediaTool("ffmpeg")
		if err != nil {
			return nil, err
		}
		child, cancel := context.WithCancel(ctx)
		s.ctx = child
		s.cancel = cancel
		s.command = mediaCommandContext(child, tool, "-nostdin", "-v", "error", "-threads", "1", "-i", r.Source, "-map", "0:a:0", "-vn", "-ac", "1", "-ar", "16000", "-f", "s16le", "-")
		s.command.Stderr = &s.stderr
		s.output, err = s.command.StdoutPipe()
		if err != nil {
			s.close()
			return nil, err
		}
		if err = s.command.Start(); err != nil {
			s.close()
			return nil, err
		}
		s.id = r.ID
		starts.Add(1)
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
