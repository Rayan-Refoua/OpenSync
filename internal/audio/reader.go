package audio

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

var ErrWindowBounds = errors.New("audio window outside recording")

type Reader struct {
	file       *os.File
	dataOffset int64
	frames     int64
	sampleRate int
}

func Open(path string) (_ *Reader, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			f.Close()
		}
	}()

	st, err := f.Stat()
	if err != nil {
		return nil, err
	}

	var header [12]byte
	if _, err = io.ReadFull(f, header[:]); err != nil {
		return nil, err
	}

	if string(header[:4]) != "RIFF" ||
		string(header[8:]) != "WAVE" {
		return nil, errors.New("not a RIFF/WAVE file")
	}

	riffEnd := int64(binary.LittleEndian.Uint32(header[4:])) + 8
	if riffEnd < 12 || riffEnd > st.Size() {
		return nil, errors.New("truncated RIFF container")
	}

	var formatOK, dataFound bool
	var dataStart, dataBytes int64
	var sampleRate int

	for pos := int64(12); pos < riffEnd; {
		if riffEnd-pos < 8 {
			return nil, errors.New("incomplete chunk header")
		}

		var chunk [8]byte
		if _, err = f.ReadAt(chunk[:], pos); err != nil {
			return nil, err
		}

		n := int64(binary.LittleEndian.Uint32(chunk[4:]))
		start := pos + 8
		end := start + n
		next := end + n%2

		if end < start || next > riffEnd || end > st.Size() {
			return nil, errors.New("truncated WAV chunk")
		}

		switch string(chunk[:4]) {
		case "fmt ":
			if formatOK || n < 16 {
				return nil, errors.New("invalid or duplicate fmt chunk")
			}
			var b [16]byte
			if _, err = f.ReadAt(b[:], start); err != nil {
				return nil, err
			}

			format := binary.LittleEndian.Uint16(b[0:])
			channels := binary.LittleEndian.Uint16(b[2:])
			rate := binary.LittleEndian.Uint32(b[4:])
			byteRate := binary.LittleEndian.Uint32(b[8:])
			align := binary.LittleEndian.Uint16(b[12:])
			bits := binary.LittleEndian.Uint16(b[14:])

			if format != 1 || channels != 1 ||
				bits != 16 || rate != 16000 ||
				align != 2 || byteRate != rate*2 {
				return nil, errors.New("unsupported PCM format")
			}
			sampleRate = int(rate)
			formatOK = true

		case "data":
			if dataFound || n%2 != 0 {
				return nil, errors.New("invalid or duplicate data chunk")
			}
			dataFound = true
			dataStart = start
			dataBytes = n
		}

		pos = next
	}

	if !formatOK || !dataFound {
		return nil, errors.New("missing WAV format or audio data")
	}

	return &Reader{
		file:       f,
		dataOffset: dataStart,
		frames:     dataBytes / 2,
		sampleRate: sampleRate,
	}, nil
}

func (r *Reader) Close() error {
	return r.file.Close()
}

func (r *Reader) Frames() int64 {
	return r.frames
}

func (r *Reader) SampleRate() int {
	return r.sampleRate
}

// PCMStream exposes only the validated PCM payload, without the WAV header.
// Independent streams and ReadFrames calls use ReadAt and can run concurrently.
func (r *Reader) PCMStream() io.Reader {
	return io.NewSectionReader(r.file, r.dataOffset, r.frames*2)
}

// ReadFrames returns exactly count PCM16 mono samples.
// start and count are sample coordinates, never floating-point seconds.
// Memory allocation is proportional only to the requested window.
func (r *Reader) ReadFrames(start, count int64) ([]int16, error) {
	if start < 0 || count < 0 ||
		start > r.frames || count > r.frames-start {
		return nil, ErrWindowBounds
	}
	if count > 16_000_000 {
		return nil, errors.New("window exceeds 1000-second safety limit")
	}

	buf := make([]byte, int(count)*2)
	if _, err := r.file.ReadAt(buf, r.dataOffset+start*2); err != nil {
		if count == 0 && errors.Is(err, io.EOF) {
			return []int16{}, nil
		}
		return nil, fmt.Errorf("reading PCM window: %w", err)
	}

	samples := make([]int16, int(count))
	for i := range samples {
		samples[i] = int16(binary.LittleEndian.Uint16(buf[i*2:]))
	}
	return samples, nil
}
