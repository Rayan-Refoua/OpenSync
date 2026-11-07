package dsp

import (
	"errors"
	"fmt"

	"opensync/internal/audio"
)

// Stream extracts features from an entire PCM recording using bounded
// analysis blocks. Output frame centers retain absolute sample positions.
//
// One complete FFT frame is repeated at each block boundary.
// Its duplicate output is discarded, but its spectrum provides the
// preceding-frame state needed to calculate spectral flux correctly.
func Stream(
	r *audio.Reader,
	blockFrames int64,
	visit func(Frame) error,
) error {
	if r == nil || visit == nil {
		return errors.New("nil reader or callback")
	}
	if blockFrames < WindowSize+HopSize ||
		blockFrames > 10*SampleRate {
		return ErrFeatureRange
	}
	if r.SampleRate() != SampleRate {
		return errors.New("unsupported sample rate")
	}

	total := r.Frames()
	var start int64
	firstBlock := true

	for total-start >= WindowSize {
		count := blockFrames
		if count > total-start {
			count = total - start
		}

		features, err := Extract(r, start, count)
		if err != nil {
			return fmt.Errorf("extract at sample %d: %w", start, err)
		}

		begin := 0
		if !firstBlock {
			// This is the previous block's final FFT frame.
			// Its next frame has the correct continuous flux.
			begin = 1
		}

		for _, frame := range features.Frames[begin:] {
			if err := visit(frame); err != nil {
				return err
			}
		}

		// Advance to the start of the final processed FFT frame.
		// This keeps every FFT center aligned to the same global grid.
		step := int64(len(features.Frames)-1) * HopSize
		if step == 0 {
			// Exactly one FFT frame remains. It was emitted above
			// on the first block, or is the already-emitted overlap
			// frame on subsequent blocks. Streaming is complete.
			return nil
		}

		start += step
		firstBlock = false
	}
	return nil
}
