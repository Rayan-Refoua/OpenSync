package autosync

import (
	"context"
	"fmt"
	"io"
	"opensync/internal/audio"
	"opensync/internal/storageio"
	"os"

	"strings"
	"sync"
	"sync/atomic"
)

// Decoded audio is never written to disk in the default workflow. Small active
// recordings may occupy a bounded RAM working set; large recordings are read in
// windows. There is no disk spill or reuse between runs.
type directAudio struct {
	mu                            sync.Mutex
	clips                         map[string][]int16
	age                           map[string]uint64
	tick                          uint64
	used, limit                   int64
	decoders                      chan *directDecoder
	starts, misses                atomic.Int64
	pcmReaders                    map[string]*audio.Reader
	pcmChecked                    map[string]bool
	pcmOpening                    map[string]chan struct{}
	readSlots                     chan struct{}
	waiting, reading, nativeReads atomic.Int64
}

func newDirectAudio() *directAudio {
	d := &directAudio{clips: map[string][]int16{}, age: map[string]uint64{}, limit: audioWorkingBudget(availableMemory()), decoders: make(chan *directDecoder, 2), pcmReaders: map[string]*audio.Reader{}, pcmChecked: map[string]bool{}, readSlots: make(chan struct{}, 8)}
	for i := 0; i < cap(d.decoders); i++ {
		d.decoders <- &directDecoder{}
	}
	return d
}
func audioWorkingBudget(available uint64) int64 {
	return int64(min(uint64(4<<30), max(uint64(32<<20), available/8)))
}
func (d *directAudio) Close() {
	for i := 0; i < cap(d.decoders); i++ {
		s := <-d.decoders
		s.close()
	}
	for _, reader := range d.pcmReaders {
		reader.Close()
	}
}

func (d *directAudio) nativePCM(r MediaRecord) *audio.Reader {
	return d.nativePCMContext(context.Background(), r)
}
func (d *directAudio) nativePCMContext(ctx context.Context, r MediaRecord) *audio.Reader {
	d.mu.Lock()
	if d.pcmChecked[r.ID] {
		reader := d.pcmReaders[r.ID]
		d.mu.Unlock()
		return reader
	}
	if wait := d.pcmOpening[r.ID]; wait != nil {
		d.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil
		case <-wait:
		}
		d.mu.Lock()
		reader := d.pcmReaders[r.ID]
		d.mu.Unlock()
		return reader
	}
	if d.pcmOpening == nil {
		d.pcmOpening = map[string]chan struct{}{}
	}
	wait := make(chan struct{})
	d.pcmOpening[r.ID] = wait
	d.mu.Unlock()
	// Do not hold the PCM/cache mutex while waiting for a mechanical disk.
	reader, err := audio.OpenContext(ctx, r.Source)
	d.mu.Lock()
	defer d.mu.Unlock()
	if err == nil {
		d.pcmReaders[r.ID] = reader
	}
	d.pcmChecked[r.ID] = ctx.Err() == nil
	delete(d.pcmOpening, r.ID)
	close(wait)
	return reader
}
func (d *directAudio) stream(ctx context.Context, r MediaRecord, consume func(io.Reader) error) error {
	if reader := d.nativePCMContext(ctx, r); reader != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := checkSource(r); err != nil {
			return err
		}
		if err := reader.WithPCMStream(ctx, consume); err != nil {
			return err
		}
		return checkSource(r)
	}
	return decodedStream(ctx, r, 0, -1, consume)
}
func (d *directAudio) retain(key string, pcm []int16) {
	size := int64(len(pcm) * 2)
	if size > 32<<20 || size > d.limit {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.clips[key]; ok {
		return
	}
	for d.used+size > d.limit {
		oldest := ""
		var age uint64 = ^uint64(0)
		for k, v := range d.age {
			if v < age {
				oldest = k
				age = v
			}
		}
		if oldest == "" {
			break
		}
		d.used -= int64(len(d.clips[oldest]) * 2)
		delete(d.clips, oldest)
		delete(d.age, oldest)
	}
	d.tick++
	d.clips[key] = pcm
	d.age[key] = d.tick
	d.used += size
}
func checkSource(r MediaRecord) error {
	st, err := os.Stat(r.Source)
	if err != nil {
		return err
	}
	if st.Size() != r.SourceBytes || st.ModTime().UnixNano() != r.SourceMtime {
		return fmt.Errorf("source changed during sync: %s", r.Source)
	}
	return nil
}

// Direct AVIO skips video-only spans in indexed containers. Restrict the
// seek-heavy path to confirmed SSDs, where the source is opened locally.
func analysisAudioArgs(source string) []string {
	args := []string{"-nostdin", "-v", "error", "-threads", "1"}
	args = append(args, storageio.DirectMediaArgs(source)...)
	return append(args, "-i", source, "-map", "0:a:0", "-vn", "-ac", "1", "-ar", "16000")
}

func decodedStream(ctx context.Context, r MediaRecord, first, count int64, consume func(io.Reader) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := checkSource(r); err != nil {
		return err
	}
	tool, err := mediaTool("ffmpeg")
	if err != nil {
		return err
	}
	args := analysisAudioArgs(r.Source)
	// Trim AFTER resampling, with an unchanged decoder/resampler origin. Input
	// seeking can change PCM phase or codec priming and is not used as a shortcut.
	if count >= 0 {
		args = append(args, "-af", fmt.Sprintf("aresample=16000,aformat=sample_fmts=s16:channel_layouts=mono,atrim=start_sample=%d:end_sample=%d", first, first+count))
	}
	args = append(args, "-f", "s16le", "-")
	cmd := mediaCommandContext(ctx, tool, args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err = cmd.Start(); err != nil {
		return err
	}
	consumeErr := consume(pipe)
	if consumeErr != nil {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if consumeErr != nil {
		return consumeErr
	}
	if waitErr != nil {
		return fmt.Errorf("decode %s: %w: %s", r.Name, waitErr, stderr.String())
	}
	return checkSource(r)
}
func audioWindowKey(id string, first int64) string { return fmt.Sprintf("%s@%d", id, first) }
func (d *directAudio) window(key string) []int16 {
	d.mu.Lock()
	defer d.mu.Unlock()
	pcm := d.clips[key]
	if pcm != nil {
		d.tick++
		d.age[key] = d.tick
	}
	return pcm
}
func (d *directAudio) read(ctx context.Context, r MediaRecord, first, count int64) ([]int16, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	total := roundFrame(r.Duration * 16000)
	if first < 0 || count < 0 || first+count > total {
		return nil, fmt.Errorf("PCM interval outside recording")
	}
	if count > 120*16000 {
		return nil, fmt.Errorf("audio request exceeds 120-second working window")
	}
	if r.Audio == nil {
		return make([]int16, count), nil
	}
	if reader := d.nativePCMContext(ctx, r); reader != nil {
		d.waiting.Add(1)
		select {
		case d.readSlots <- struct{}{}:
		case <-ctx.Done():
			d.waiting.Add(-1)
			return nil, ctx.Err()
		}
		d.waiting.Add(-1)
		d.reading.Add(1)
		pcm, err := reader.ReadFramesContext(ctx, first, count)
		d.reading.Add(-1)
		<-d.readSlots
		d.nativeReads.Add(1)
		return pcm, err
	}
	result := make([]int16, count)
	for copied := int64(0); copied < count; {
		position := first + copied
		blockFirst := position / (50 * 16000) * (50 * 16000)
		key := audioWindowKey(r.ID, blockFirst)
		pcm := d.window(key)
		if pcm == nil {
			session, err := d.acquire(ctx, r.ID)
			if err != nil {
				return nil, err
			}
			pcm = d.window(key)
			if pcm == nil {
				size := min(int64(50*16000), total-blockFirst)
				d.misses.Add(1)
				d.reading.Add(1)
				pcm, err = session.read(ctx, r, blockFirst, size, &d.starts)
				d.reading.Add(-1)
				if err != nil {
					d.decoders <- session
					return nil, err
				}
				d.retain(key, pcm)
			}
			d.decoders <- session
		}
		offset := position - blockFirst
		take := min(count-copied, int64(len(pcm))-offset)
		if take <= 0 {
			return nil, fmt.Errorf("invalid decoded window for %s", r.Name)
		}
		copy(result[copied:copied+take], pcm[offset:offset+take])
		copied += take
	}
	return result, nil
}
